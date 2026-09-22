package watcher

import (
	"context"
	"testing"
	"time"
)

func TestPollUnavailableInventoryRetainsLiveOwnership(t *testing.T) {
	for _, phase := range []string{"inventory", "presence", "ownership"} {
		t.Run(phase, func(t *testing.T) {
			script := `cmd=
for arg in "$@"; do
 case "$arg" in list-windows|list-panes|show-options) cmd=$arg ;; esac
done
`
			switch phase {
			case "inventory":
				script += `echo 'no server running on fixture' >&2; exit 1`
			case "presence":
				script += `if [ "$cmd" = list-windows ]; then printf 'missing:@1\tfixture\t/repo\tcodex\t1\t0\t1\t\t\n'; exit 0; fi
echo 'no server running on fixture' >&2; exit 1`
			case "ownership":
				script += `if [ "$cmd" = list-windows ]; then printf 'missing:@1\tfixture\t/repo\tcodex\t1\t0\t1\t\t\n'; exit 0; fi
if [ "$cmd" = list-panes ]; then printf 'missing:@1\n'; exit 0; fi
echo 'no server running on fixture' >&2; exit 1`
			}
			t.Setenv("PATH", writeFakeTmux(t, script))
			w, ledger := missingWorkerFixture(t)
			w.listWindows = w.listTmuxWindows
			w.poll()
			if w.GetWorker("missing:@1") == nil {
				t.Fatal("unavailable observation discarded owned live Session")
			}
			if len(ledger.applied) != 0 || len(w.events) != 0 {
				t.Fatal("unavailable observation emitted disappearance evidence")
			}
			if w.SnapshotReady() {
				t.Fatal("failed first inventory advertised readiness")
			}
			// A later successful inventory is still authoritative: retaining
			// Unknown must not cache an identity forever after real removal.
			w.listWindows = func() ([]tmuxWindow, error) { return nil, nil }
			w.poll()
			if w.GetWorker("missing:@1") != nil || len(ledger.applied) != 1 || !ledger.applied[0].ProcessDead {
				t.Fatal("confirmed absence did not converge after the observation gap")
			}

		})
	}
}

func TestReleasedSignalTurnWithLiveActivityUsesFreshAdmission(t *testing.T) {
	for _, activity := range []string{"bound-activity", "newer-native-activity"} {
		t.Run(activity, func(t *testing.T) {
			ledger := newFakeTurnLedger()
			owner := newLedgerSessionInputOwner(newFakeSessionInputIO(), ledger)
			turn := TurnSnapshot{SessionID: "fixture:@1", TurnID: "released", Status: TurnUnknown, SignalProtocol: true, ActivityID: "bound-activity"}
			decision, err := owner.reconcileSubmissionActivity(turn.SessionID, turn, ProviderActivityObservation{ID: activity, Status: "running", Structured: true})
			if err != nil || decision.Mode != delegatedReuseFresh {
				t.Fatalf("released signal turn selected steer: mode=%v err=%v", decision.Mode, err)
			}
			if len(ledger.applied) != 0 {
				t.Fatal("reuse decision revived historical activity")
			}
		})
	}
}

func TestWaitForSnapshotCompletesOnlyAfterFullPollAndCancels(t *testing.T) {
	w := New(time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() { waiting <- w.WaitForSnapshot(ctx) }()
	select {
	case <-waiting:
		t.Fatal("unstarted discovery passed the barrier")
	default:
	}
	cancel()
	select {
	case err := <-waiting:
		if err != context.Canceled {
			t.Fatalf("cancel=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("discovery wait did not cancel")
	}
	restore := installFakePollSeams(w, testWindows()[:1], map[string]string{"sess-a:@1": contentA}, liveSessionProcesses())
	defer restore()
	probe := &blockingActivityProbe{started: make(chan struct{}, 1), release: make(chan struct{})}
	w.SetActivityProbe(probe)
	done := make(chan struct{})
	go func() { defer close(done); w.poll() }()
	<-probe.started
	if w.SnapshotReady() {
		t.Fatal("partial discovery marked ready")
	}
	ready := make(chan error, 1)
	go func() { ready <- w.WaitForSnapshot(context.Background()) }()
	select {
	case <-ready:
		t.Fatal("slow provider evidence bypassed the barrier")
	default:
	}
	close(probe.release)
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("completed discovery did not release startup")
	}
	<-done
	if err := w.WaitForSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
}
