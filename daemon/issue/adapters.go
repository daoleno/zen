package issue

import (
	"fmt"
	"math/rand"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

// WatcherRegistry adapts watcher.Watcher to SessionRegistry.
type WatcherRegistry struct {
	W *watcher.Watcher
}

// IdleSessions returns sessions that match the requested role and cwd and are
// currently in the classifier's running state.
func (r *WatcherRegistry) IdleSessions(role, cwd string) []SessionInfo {
	if r == nil || r.W == nil {
		return nil
	}

	out := []SessionInfo{}
	for _, agent := range r.W.Agents() {
		if strings.TrimSpace(agent.Cwd) != strings.TrimSpace(cwd) {
			continue
		}
		if !roleMatches(agent, role) {
			continue
		}
		if agent.State != classifier.StateRunning {
			continue
		}
		out = append(out, SessionInfo{
			ID:      agent.ID,
			Project: agent.Project,
			Cwd:     agent.Cwd,
			Role:    role,
		})
	}
	return out
}

func roleMatches(agent *classifier.Agent, role string) bool {
	if agent == nil {
		return false
	}
	first := filepath.Base(firstWord(strings.TrimSpace(agent.Command)))
	return first == role
}

func firstWord(value string) string {
	for i, r := range value {
		if r == ' ' || r == '\t' {
			return value[:i]
		}
	}
	return value
}

// TmuxRunner adapts tmux CLI commands to SessionRunner.
type TmuxRunner struct{}

var (
	tmuxCounter atomic.Uint64
	tmuxRand    = rand.New(rand.NewSource(time.Now().UnixNano()))
)

func sessionName(role string) string {
	n := tmuxCounter.Add(1)
	return fmt.Sprintf("%s-%s-%04x%x",
		role,
		time.Now().Format("060102"),
		tmuxRand.Intn(0xffff),
		n%0xf,
	)
}

// Spawn creates a detached tmux session and returns the watcher-compatible
// session identifier "<session>:<window_id>".
func (TmuxRunner) Spawn(role, cwd, command string) (string, error) {
	name := sessionName(role)
	create := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", cwd, command)
	if out, err := create.CombinedOutput(); err != nil {
		return "", fmt.Errorf("tmux new-session: %w: %s", err, strings.TrimSpace(string(out)))
	}

	listWindows := exec.Command("tmux", "list-windows", "-t", name, "-F", "#{window_id}")
	out, err := listWindows.Output()
	if err != nil {
		_ = exec.Command("tmux", "kill-session", "-t", name).Run()
		return "", fmt.Errorf("tmux list-windows: %w", err)
	}

	windowID := strings.TrimSpace(string(out))
	if windowID == "" {
		_ = exec.Command("tmux", "kill-session", "-t", name).Run()
		return "", fmt.Errorf("tmux list-windows: no window id returned")
	}
	return name + ":" + windowID, nil
}

// Send writes text followed by Enter into the session's active pane.
func (TmuxRunner) Send(agentID, text string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", agentID, text, "C-m")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux send-keys: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
