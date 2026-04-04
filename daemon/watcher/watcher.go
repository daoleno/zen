package watcher

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// SessionEvent represents a state change or output update for an agent.
type SessionEvent struct {
	Type     string              `json:"type"`
	AgentID  string              `json:"agent_id"`
	Agent    *classifier.Agent   `json:"agent,omitempty"`
	Agents   []*classifier.Agent `json:"agents,omitempty"`
	Lines    []string            `json:"lines,omitempty"`
	OldState string              `json:"old,omitempty"`
	NewState string              `json:"new,omitempty"`
}

// Watcher monitors tmux windows and classifies agent states.
type Watcher struct {
	pollInterval time.Duration
	agents       map[string]*classifier.Agent
	prevContent  map[string]string
	mu           sync.RWMutex
	events       chan SessionEvent
}

// New creates a Watcher that polls tmux windows at the given interval.
func New(pollInterval time.Duration) *Watcher {
	return &Watcher{
		pollInterval: pollInterval,
		agents:       make(map[string]*classifier.Agent),
		prevContent:  make(map[string]string),
		events:       make(chan SessionEvent, 100),
	}
}

// Events returns the channel on which state changes and output updates are sent.
func (w *Watcher) Events() <-chan SessionEvent {
	return w.events
}

// Agents returns a snapshot of all current agents.
func (w *Watcher) Agents() []*classifier.Agent {
	w.mu.RLock()
	defer w.mu.RUnlock()
	result := make([]*classifier.Agent, 0, len(w.agents))
	for _, a := range w.agents {
		copy := *a
		result = append(result, &copy)
	}
	return result
}

// GetAgent returns a snapshot of a single agent, or nil if not found.
func (w *Watcher) GetAgent(id string) *classifier.Agent {
	w.mu.RLock()
	defer w.mu.RUnlock()
	a, ok := w.agents[id]
	if !ok {
		return nil
	}
	copy := *a
	return &copy
}

// Run starts the polling loop. Blocks until context is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.poll()
		}
	}
}

func (w *Watcher) poll() {
	windows, err := listTmuxWindows()
	if err != nil {
		return
	}
	processes := snapshotProcesses()

	w.mu.Lock()
	defer w.mu.Unlock()

	seen := make(map[string]bool)

	for _, win := range windows {
		seen[win.target] = true

		content, alive := capturePaneContent(win.target)
		lines := strings.Split(content, "\n")

		prev, existed := w.prevContent[win.target]
		contentChanged := content != prev
		w.prevContent[win.target] = content

		agent, exists := w.agents[win.target]
		if !exists {
			agent = &classifier.Agent{
				ID:   win.target,
				Name: win.name + " (" + win.target + ")",
			}
			w.agents[win.target] = agent
		}
		agent.Cwd = win.cwd
		agent.Project = projectNameFromPath(win.cwd)
		agent.Command = detectAgentCommand(win.command, win.panePID, processes)

		if contentChanged {
			agent.StaleCount = 0
		} else {
			agent.StaleCount++
		}

		agent.PaneAlive = alive
		agent.LastLines = lastN(lines, 20)
		agent.UpdatedAt = time.Now()

		oldState := agent.State
		newState, summary := classifier.Classify(alive, lines, agent.StaleCount)
		agent.State = newState
		agent.Summary = summary

		if oldState != newState {
			agent.StateVersion++
		}

		if contentChanged && existed {
			prevLines := strings.Split(prev, "\n")
			if len(lines) > len(prevLines) {
				newLines := lines[len(prevLines):]
				w.events <- SessionEvent{
					Type:    "agent_output",
					AgentID: win.target,
					Lines:   newLines,
				}
			}
		}

		if oldState != newState && existed {
			w.events <- SessionEvent{
				Type:     "agent_state_change",
				AgentID:  win.target,
				OldState: string(oldState),
				NewState: string(newState),
			}
		}
	}

	// Check for removed windows.
	for id := range w.agents {
		if !seen[id] {
			old := w.agents[id]
			delete(w.agents, id)
			delete(w.prevContent, id)
			w.events <- SessionEvent{
				Type:     "agent_state_change",
				AgentID:  id,
				OldState: string(old.State),
				NewState: string(classifier.StateDone),
			}
		}
	}
}

// tmuxWindow represents a single tmux window target.
type tmuxWindow struct {
	target  string // "session:window_index" — usable as tmux -t target
	name    string // window name (e.g. "claude", "node")
	cwd     string // active pane cwd
	command string // active pane command
	panePID int
}

// listTmuxWindows returns all windows across all tmux sessions.
func listTmuxWindows() ([]tmuxWindow, error) {
	cmd := exec.Command("tmux", "list-windows", "-a", "-F", "#{session_name}:#{window_index}\t#{window_name}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_pid}")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-windows: %w", err)
	}
	var windows []tmuxWindow
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		target := parts[0]
		// Skip grouped sessions created by the terminal backend (zen-<pid>-<counter>).
		sessionName := strings.SplitN(target, ":", 2)[0]
		if strings.HasPrefix(sessionName, "zen-") {
			continue
		}
		name := target
		if len(parts) >= 2 {
			name = parts[1]
		}
		cwd := ""
		if len(parts) >= 3 {
			cwd = strings.TrimSpace(parts[2])
		}
		command := ""
		if len(parts) >= 4 {
			command = strings.TrimSpace(parts[3])
		}
		panePID := 0
		if len(parts) == 5 {
			panePID, _ = strconv.Atoi(strings.TrimSpace(parts[4]))
		}
		windows = append(windows, tmuxWindow{target: target, name: name, cwd: cwd, command: command, panePID: panePID})
	}
	return windows, nil
}

// capturePaneContent captures the visible content of a tmux window's active pane.
func capturePaneContent(target string) (string, bool) {
	cmd := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", "-200")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}

	cmdAlive := exec.Command("tmux", "list-panes", "-t", target, "-F", "#{pane_dead}")
	aliveOut, err := cmdAlive.Output()
	alive := true
	if err == nil && strings.TrimSpace(string(aliveOut)) == "1" {
		alive = false
	}

	return string(out), alive
}

// SendInput sends raw text to a tmux window via send-keys.
func (w *Watcher) SendInput(sessionID, text string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", sessionID, text)
	return cmd.Run()
}

// SendAction executes a predefined action on a tmux window.
func (w *Watcher) SendAction(sessionID, action string) error {
	var args []string
	switch action {
	case "approve":
		args = []string{"send-keys", "-t", sessionID, "y", "Enter"}
	case "reject":
		args = []string{"send-keys", "-t", sessionID, "n", "Enter"}
	case "pause":
		args = []string{"send-keys", "-t", sessionID, "C-c"}
	case "show_diff":
		args = []string{"send-keys", "-t", sessionID, "/diff", "Enter"}
	case "run_tests":
		args = []string{"send-keys", "-t", sessionID, "/test", "Enter"}
	case "git_status":
		args = []string{"send-keys", "-t", sessionID, "git status", "Enter"}
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
	return exec.Command("tmux", args...).Run()
}

type CreateSessionOptions struct {
	Cwd     string
	Command string
	Name    string
}

// CreateSession creates a new tmux window and returns its target id.
// If preferredTarget is set, the new window is created in the same tmux
// session as that target. Otherwise the first non-zen tmux session is used.
func (w *Watcher) CreateSession(preferredTarget string, opts CreateSessionOptions) (string, error) {
	sessionName := baseSessionName(preferredTarget)
	if sessionName == "" {
		sessions, err := listTmuxSessions()
		if err != nil {
			return "", err
		}
		if len(sessions) == 0 {
			return "", fmt.Errorf("no tmux sessions available")
		}
		sessionName = sessions[0]
	}

	cwd := strings.TrimSpace(opts.Cwd)
	if cwd == "" && preferredTarget != "" {
		currentPath, err := currentPathForTarget(preferredTarget)
		if err == nil {
			cwd = currentPath
		}
	}

	args := []string{
		"new-window",
		"-P",
		"-F",
		"#{session_name}:#{window_index}",
		"-t",
		sessionName,
	}
	if name := strings.TrimSpace(opts.Name); name != "" {
		args = append(args, "-n", name)
	}
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	if shellCommand, err := buildWindowCommand(strings.TrimSpace(opts.Command)); err != nil {
		return "", err
	} else if shellCommand != "" {
		args = append(args, shellCommand)
	}

	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("create tmux window: %w", err)
	}

	target := strings.TrimSpace(string(out))
	if target == "" {
		return "", fmt.Errorf("tmux returned empty window target")
	}
	return target, nil
}

func buildWindowCommand(command string) (string, error) {
	shellPath, err := currentLoginShell()
	if err != nil {
		return "", err
	}

	quotedShell := shellQuote(shellPath)
	if command == "" {
		return "exec " + quotedShell + " -l", nil
	}
	return "exec " + quotedShell + " -l -c " + shellQuote(command), nil
}

func currentLoginShell() (string, error) {
	if shell := loginShellFromPasswd(); shell != "" {
		return shell, nil
	}
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell, nil
	}
	return "/bin/sh", nil
}

func loginShellFromPasswd() string {
	currentUser, err := user.Current()
	if err != nil {
		return ""
	}

	passwd, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}

	username := strings.TrimSpace(currentUser.Username)
	uid := strings.TrimSpace(currentUser.Uid)
	for _, line := range strings.Split(string(passwd), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			continue
		}
		if fields[0] != username && fields[2] != uid {
			continue
		}
		shell := strings.TrimSpace(fields[6])
		if shell != "" {
			return shell
		}
	}
	return ""
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

// KillSession terminates the tmux window backing a single agent.
// Agent IDs use the form session:window_index, so killing the window
// exits only that agent instead of the whole tmux session.
func (w *Watcher) KillSession(sessionID string) error {
	return exec.Command("tmux", "kill-window", "-t", sessionID).Run()
}

func listTmuxSessions() ([]string, error) {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-sessions: %w", err)
	}

	sessions := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		sessionName := strings.TrimSpace(line)
		if sessionName == "" || strings.HasPrefix(sessionName, "zen-") {
			continue
		}
		sessions = append(sessions, sessionName)
	}
	return sessions, nil
}

func baseSessionName(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}

	sessionName, _, ok := strings.Cut(target, ":")
	if !ok {
		return ""
	}
	if strings.HasPrefix(sessionName, "zen-") {
		return ""
	}
	return sessionName
}

func currentPathForTarget(target string) (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", target, "#{pane_current_path}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux current path: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

type processInfo struct {
	pid  int
	ppid int
	comm string
	args string
}

func snapshotProcesses() map[int]processInfo {
	out, err := exec.Command("ps", "-eo", "pid=,ppid=,comm=,args=").Output()
	if err != nil {
		return nil
	}

	processes := make(map[int]processInfo)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}

		args := ""
		if len(fields) > 3 {
			args = strings.Join(fields[3:], " ")
		}

		processes[pid] = processInfo{
			pid:  pid,
			ppid: ppid,
			comm: fields[2],
			args: args,
		}
	}
	return processes
}

func detectAgentCommand(baseCommand string, panePID int, processes map[int]processInfo) string {
	command := normalizeCommand(baseCommand)
	if command == "claude" || command == "codex" {
		return command
	}
	if panePID <= 0 || len(processes) == 0 {
		return command
	}

	descendants := descendantProcesses(panePID, processes)
	for _, proc := range descendants {
		lowerComm := normalizeCommand(proc.comm)
		lowerArgs := strings.ToLower(proc.args)

		if lowerComm == "claude" || strings.Contains(lowerArgs, " claude") || strings.HasPrefix(lowerArgs, "claude ") {
			return "claude"
		}
		if lowerComm == "codex" || strings.Contains(lowerArgs, "/bin/codex") || strings.Contains(lowerArgs, " codex ") || strings.HasPrefix(lowerArgs, "codex ") {
			return "codex"
		}
	}

	return command
}

func descendantProcesses(rootPID int, processes map[int]processInfo) []processInfo {
	if rootPID <= 0 || len(processes) == 0 {
		return nil
	}

	children := make(map[int][]processInfo)
	for _, proc := range processes {
		children[proc.ppid] = append(children[proc.ppid], proc)
	}

	var result []processInfo
	queue := append([]processInfo(nil), children[rootPID]...)
	for len(queue) > 0 {
		proc := queue[0]
		queue = queue[1:]
		result = append(result, proc)
		queue = append(queue, children[proc.pid]...)
	}

	return result
}

func normalizeCommand(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "./")
	if idx := strings.LastIndex(value, "/"); idx >= 0 {
		value = value[idx+1:]
	}
	return value
}

func projectNameFromPath(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}

	base := filepath.Base(cwd)
	if base == "." || base == string(filepath.Separator) {
		return cwd
	}
	return base
}

func lastN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
