package watcher

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRealTmuxInputQueueSurvivesLinkedViewRemoval(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	caller := createHarnessPane(t, h.selected, "caller", "exec /bin/sh")
	target := createHarnessPane(t, h.selected, "worker", "exec /bin/sh")
	io := realSessionInputIO{}
	callerPane := io.pane(h.selected, caller)
	workerPane := io.pane(h.selected, target)
	if out, err := tmuxHarnessCommand(h.selected, "new-session", "-d", "-s", "view", ";", "link-window", "-k", "-s", target, "-t", "view:0").CombinedOutput(); err != nil {
		t.Fatalf("link view: %v: %s", err, out)
	}
	t.Setenv("TMUX_PANE", callerPane.paneID)
	if err := io.loadBuffer(h.selected, "handoff-test", "printf 'QUEUE_%s\\n' VERIFIED"); err != nil {
		t.Fatal(err)
	}
	type queueResult struct {
		started bool
		err     error
	}
	done := make(chan queueResult, 1)
	go func() {
		started, err := io.runQueue(h.selected, sessionInputSubmitQueue(workerPane.paneID, "handoff-test", sessionInputProvider{submitKey: "Enter", settle: 2 * time.Second}), nil)
		done <- queueResult{started, err}
	}()
	waitForHarness(t, "queue waiting", func() bool {
		out, err := tmuxHarnessCommand(h.selected, "show-messages", "-J").Output()
		return err == nil && strings.Contains(string(out), "sleep")
	})
	if out, err := tmuxHarnessCommand(h.selected, "kill-session", "-t", "view").CombinedOutput(); err != nil {
		t.Fatalf("remove linked view: %v: %s", err, out)
	}
	result := <-done
	if !result.started || result.err != nil {
		t.Fatalf("queue started=%v err=%v", result.started, result.err)
	}
	waitForHarness(t, "worker result", func() bool { return strings.Contains(captureHarnessPane(t, h.selected, target), "QUEUE_VERIFIED") })
}

func TestRealTmuxInputQueuePreservesStderrAndExitError(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	createHarnessPane(t, h.selected, "stderr-proof", "exec /bin/sh")
	started, err := (realSessionInputIO{}).runQueue(h.selected, []string{"zen-test-invalid-command"}, nil)
	var exitErr *exec.ExitError
	if !started || !errors.As(err, &exitErr) || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("queue lost stderr or exit identity: started=%v err=%v", started, err)
	}
}
