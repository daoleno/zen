package watcher

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// TestWatcherPropagatesPreciseProcessStart proves the full Linux propagation
// path: the watcher poll observes a ps-rounded (whole-second) process start
// for the detected provider pid, refines it with the /proc starttime
// evidence, and the agent snapshot carries the sub-second-precision value.
// On the pre-fix code the agent kept the rounded value, which re-admitted a
// same-second frozen old transcript through the instance arms.
func TestWatcherPropagatesPreciseProcessStart(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	pid := cmd.Process.Pid
	precise, ok := processStartTimeFromProc(pid)
	if !ok {
		t.Fatalf("no /proc start evidence for live pid %d", pid)
	}
	// The ps lstart snapshot rounds the same process start down to its second.
	rounded := precise.Truncate(time.Second)

	w := New(20 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restore := w.SetPollSources(PollSources{
		ListWindows: func() ([]PollWindow, error) {
			return []PollWindow{{
				Target:  "probe:@1",
				Name:    "pi",
				Cwd:     "/tmp",
				Command: "pi",
				PanePID: pid,
			}}, nil
		},
		CapturePane: func(target string) (string, bool, int) {
			return "pi v0.73.1\n", true, -1
		},
		SnapshotProcesses: func() map[int]PollProcess {
			return map[int]PollProcess{
				pid: {PID: pid, PPID: 1, PGID: pid, TPGID: pid, StartedAt: rounded, Comm: "pi", Args: "pi"},
			}
		},
	})
	defer restore()
	go func() { _ = w.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	var agent *classifier.Agent
	for time.Now().Before(deadline) {
		agent = w.GetAgent("probe:@1")
		if agent != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if agent == nil {
		t.Fatal("agent was not discovered")
	}
	if !agent.StartedAt.Equal(precise) {
		t.Fatalf("agent.StartedAt = %v (rounded %v), want precise %v", agent.StartedAt, rounded, precise)
	}
	// The refined value is stable across polls (no equality churn).
	time.Sleep(120 * time.Millisecond)
	if stable := w.GetAgent("probe:@1"); stable == nil || !stable.StartedAt.Equal(precise) {
		t.Fatalf("agent.StartedAt not stable: %+v", stable)
	}
}
