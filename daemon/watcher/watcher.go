package watcher

import (
	"context"
	"fmt"
	"os/exec"
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
	target string // "session:window_index" — usable as tmux -t target
	name   string // window name (e.g. "claude", "node")
}

// listTmuxWindows returns all windows across all tmux sessions.
func listTmuxWindows() ([]tmuxWindow, error) {
	cmd := exec.Command("tmux", "list-windows", "-a", "-F", "#{session_name}:#{window_index} #{window_name}")
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
		parts := strings.SplitN(line, " ", 2)
		target := parts[0]
		// Skip grouped sessions created by the terminal backend (zen-<pid>-<counter>).
		sessionName := strings.SplitN(target, ":", 2)[0]
		if strings.HasPrefix(sessionName, "zen-") {
			continue
		}
		name := target
		if len(parts) == 2 {
			name = parts[1]
		}
		windows = append(windows, tmuxWindow{target: target, name: name})
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

// KillSession terminates the tmux window backing a single agent.
// Agent IDs use the form session:window_index, so killing the window
// exits only that agent instead of the whole tmux session.
func (w *Watcher) KillSession(sessionID string) error {
	return exec.Command("tmux", "kill-window", "-t", sessionID).Run()
}

func lastN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
