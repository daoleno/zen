package work

import (
	"fmt"

	"github.com/daoleno/zen/daemon/watcher"
)

// TmuxRunner adapts Watcher's owned tmux lifecycle to SessionRunner. Calendar
// Work must use this path instead of opening raw tmux sessions, otherwise it
// bypasses delegated ownership markers, resource limits, and orphan cleanup.
type TmuxRunner struct {
	Watcher *watcher.Watcher
	Env     map[string]string
}

// Spawn creates a detached tmux session and returns the watcher-compatible
// session identifier "<session>:<window_id>".
func (r TmuxRunner) Spawn(role, cwd, command string) (string, error) {
	if r.Watcher == nil {
		return "", fmt.Errorf("delegated watcher is required")
	}
	return r.Watcher.CreateSession("", watcher.CreateSessionOptions{
		Cwd:         cwd,
		Command:     command,
		Name:        role,
		Detached:    true,
		ProgressEnv: true,
		Delegated:   true,
		Env:         r.Env,
	})
}

// SendWhenReady waits for a freshly spawned known agent UI before sending the
// initial prompt.
func (r TmuxRunner) SendWhenReady(agentID, command, text string) error {
	if r.Watcher == nil {
		return fmt.Errorf("delegated watcher is required")
	}
	return r.Watcher.SendInputWhenReady(agentID, command, text)
}

// Abort terminates the one fresh window created for a failed Calendar launch.
func (r TmuxRunner) Abort(agentID string) error {
	if r.Watcher == nil {
		return fmt.Errorf("delegated watcher is required")
	}
	return r.Watcher.KillSession(agentID)
}
