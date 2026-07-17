package work

import (
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

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

// SendWhenReady waits for a freshly spawned known agent UI before sending the
// initial prompt.
func (TmuxRunner) SendWhenReady(agentID, command, text string) error {
	return watcher.SendInputWhenReady(agentID, command, strings.TrimRight(text, "\r\n")+"\n")
}

// Abort terminates the one fresh window created for a failed Calendar launch.
func (TmuxRunner) Abort(agentID string) error {
	out, err := exec.Command("tmux", "kill-window", "-t", agentID).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux kill-window: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
