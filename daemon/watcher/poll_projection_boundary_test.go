package watcher

import (
	"errors"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

type blockingRemovalProbe struct {
	started chan struct{}
	release chan struct{}
}

func (p *blockingRemovalProbe) ObserveProviderActivity(classifier.Worker, time.Time) ProviderActivityObservation {
	close(p.started)
	<-p.release
	return ProviderActivityObservation{}
}
func (*blockingRemovalProbe) ForgetProviderActivity(string) {}

type failingRemovalLedger struct {
	*fakeTurnLedger
	fail bool
}

func (l *failingRemovalLedger) ApplyTurnFact(fact TurnFact) (TurnSnapshot, bool, error) {
	if l.fail {
		return TurnSnapshot{}, false, errors.New("injected persistence failure")
	}
	return l.fakeTurnLedger.ApplyTurnFact(fact)
}

func missingWorkerFixture(t *testing.T) (*Watcher, *fakeTurnLedger) {
	t.Helper()
	w := New(time.Second)
	w.registerCreatedSession("missing:@1", "/repo", CreateSessionOptions{Name: "missing", Command: "codex", Delegated: true}, time.Now().UTC())
	drainWatcherEvents(w)
	ledger := newFakeTurnLedger()
	turn := TurnSnapshot{SessionID: "missing:@1", TurnID: "old-turn", Status: TurnRunning, ProcessIdentity: "old-process", AcceptedAt: time.Now().UTC()}
	ledger.seed(turn.SessionID, turn)
	w.SetTurnLedger(ledger)
	w.ledgerTurns[turn.SessionID] = turn
	restore := installFakePollSeams(w, nil, map[string]string{}, map[int]processInfo{})
	t.Cleanup(restore)
	return w, ledger
}

func TestPollMissingEvidenceIsUnlockedAndCannotRemoveReplacement(t *testing.T) {
	w, ledger := missingWorkerFixture(t)
	probe := &blockingRemovalProbe{started: make(chan struct{}), release: make(chan struct{})}
	w.SetProviderActivityProbe(probe)
	done := make(chan struct{})
	go func() { defer close(done); w.poll() }()
	t.Cleanup(func() { close(probe.release); <-done })
	select {
	case <-probe.started:
	case <-time.After(time.Second):
		t.Fatal("missing provider probe did not start")
	}
	read := make(chan []*classifier.Worker, 1)
	go func() { read <- w.Workers() }()
	select {
	case <-read:
	case <-time.After(time.Second):
		t.Fatal("missing evidence held the watcher lock")
	}
	w.registerCreatedSession("missing:@1", "/new", CreateSessionOptions{Name: "replacement", Command: "codex", Delegated: true}, time.Now().UTC())
	ledger.seed("missing:@1", TurnSnapshot{SessionID: "missing:@1", TurnID: "new-turn", Status: TurnRunning})
	drainWatcherEvents(w)
	probe.release <- struct{}{}
	<-done
	if worker := w.GetWorker("missing:@1"); worker == nil || worker.Cwd != "/new" {
		t.Fatalf("old absence deleted replacement: %+v", worker)
	}
	if len(w.events) != 0 {
		t.Fatal("superseded absence published removal")
	}
}

func TestPollRetriesMissingEvidencePersistenceBeforeRemoval(t *testing.T) {
	w, base := missingWorkerFixture(t)
	ledger := &failingRemovalLedger{fakeTurnLedger: base, fail: true}
	w.SetTurnLedger(ledger)
	w.poll()
	if w.GetWorker("missing:@1") == nil || len(w.events) != 0 {
		t.Fatal("failed ledger write discarded the retryable disappearance")
	}
	ledger.fail = false
	w.poll()
	if w.GetWorker("missing:@1") != nil {
		t.Fatal("successful retry did not remove missing projection")
	}
	if len(base.applied) != 1 || base.applied[0].Kind != "uncertain" {
		t.Fatalf("removal facts=%+v", base.applied)
	}
	if event := <-w.events; event.Type != "worker_removed" {
		t.Fatalf("removal event=%+v", event)
	}
}

func TestPollDoesNotDeleteWorkerCreatedDuringEvidenceRead(t *testing.T) {
	w := New(time.Second)
	probe := &blockingActivityProbe{started: make(chan struct{}, 1), release: make(chan struct{})}
	w.SetActivityProbe(probe)
	restore := installFakePollSeams(w, testWindows()[:1], map[string]string{"sess-a:@1": contentA}, liveSessionProcesses())
	defer restore()
	done := make(chan struct{})
	go func() { defer close(done); w.poll() }()
	t.Cleanup(func() { close(probe.release); <-done })
	<-probe.started
	w.registerCreatedSession("new:@9", "/new", CreateSessionOptions{Name: "new"}, time.Now().UTC())
	probe.release <- struct{}{}
	<-done
	if w.GetWorker("new:@9") == nil {
		t.Fatal("old inventory removed a concurrently created Worker")
	}
}

func TestPollBackpressureDoesNotBlockSnapshotReaders(t *testing.T) {
	w := New(time.Second)
	w.events = make(chan SessionEvent, 1)
	restore := installFakePollSeams(w, testWindows(), map[string]string{"sess-a:@1": contentA, "sess-b:@2": contentB}, liveSessionProcesses())
	defer restore()
	done := make(chan struct{})
	go func() { defer close(done); w.poll() }()
	t.Cleanup(func() {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-done:
				return
			case <-w.events:
			case <-timer.C:
				t.Error("poll did not drain")
				return
			}
		}
	})
	deadline := time.Now().Add(time.Second)
	for len(w.events) != cap(w.events) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	read := make(chan []*classifier.Worker, 1)
	go func() { read <- w.Workers() }()
	select {
	case workers := <-read:
		if len(workers) != 2 {
			t.Fatalf("committed snapshot=%+v", workers)
		}
	case <-time.After(time.Second):
		t.Fatal("event backpressure held the state lock")
	}
	for _, id := range []string{"sess-a:@1", "sess-b:@2"} {
		if event := <-w.events; event.Type != "worker_discovered" || event.WorkerID != id {
			t.Fatalf("event order=%+v want=%s", event, id)
		}
	}
	<-done
}

func TestPollOldInventoryCannotDeleteWorkerRegisteredBeforePreparation(t *testing.T) {
	for _, stage := range []string{"process_snapshot", "capture"} {
		t.Run(stage, func(t *testing.T) {
			w := New(time.Second)
			restore := installFakePollSeams(w, testWindows()[:1], map[string]string{"sess-a:@1": contentA}, liveSessionProcesses())
			defer restore()
			started, release := make(chan struct{}), make(chan struct{})
			if stage == "process_snapshot" {
				read := w.snapshotProcesses
				w.snapshotProcesses = func() map[int]processInfo { close(started); <-release; return read() }
			} else {
				read := w.capturePane
				w.capturePane = func(id string) (string, bool, int) { close(started); <-release; return read(id) }
			}
			done := make(chan struct{})
			go func() { defer close(done); w.poll() }()
			t.Cleanup(func() { close(release); <-done })
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("poll did not reach pre-prepare boundary")
			}
			w.registerCreatedSession("new:@9", "/new", CreateSessionOptions{Name: "new"}, time.Now().UTC())
			release <- struct{}{}
			<-done
			if worker := w.GetWorker("new:@9"); worker == nil || worker.Cwd != "/new" {
				t.Fatalf("stale %s inventory removed new Worker: %+v", stage, worker)
			}
			for len(w.events) > 0 {
				if event := <-w.events; event.Type == "worker_removed" && event.WorkerID == "new:@9" {
					t.Fatalf("stale %s inventory published new Worker removal", stage)
				}
			}
		})
	}
}

func TestWorkerSnapshotsDoNotExposeMutableProjectionStorage(t *testing.T) {
	w := New(time.Second)
	at := time.Now().UTC()
	w.workers["worker:@1"] = &classifier.Worker{ID: "worker:@1", LastLines: []string{"original"}, LastProgressAt: &at, ExpectedNextCheckAt: &at}
	w.workerOrder = []string{"worker:@1"}
	for _, snapshot := range []*classifier.Worker{w.GetWorker("worker:@1"), w.Workers()[0]} {
		snapshot.LastLines[0] = "changed"
		*snapshot.LastProgressAt = time.Time{}
		*snapshot.ExpectedNextCheckAt = time.Time{}
	}
	current := w.GetWorker("worker:@1")
	if current.LastLines[0] != "original" || current.LastProgressAt.IsZero() || current.ExpectedNextCheckAt.IsZero() {
		t.Fatalf("read snapshots changed canonical projection: %+v", current)
	}
}

func TestNewWatchersOwnDistinctInputAndPollSources(t *testing.T) {
	left, right := New(time.Second), New(time.Second)
	left.SetTmuxServer("left.sock", "")
	right.SetTmuxServer("right.sock", "")
	if left.sessionInputOwner() == right.sessionInputOwner() {
		t.Fatal("Watchers share a global input owner")
	}
	for _, item := range []struct {
		watcher *Watcher
		socket  string
	}{{left, "left.sock"}, {right, "right.sock"}} {
		list, capture, snapshot := item.watcher.pollReaders()
		if list == nil || capture == nil || snapshot == nil {
			t.Fatal("constructor omitted an instance reader")
		}
		io, ok := item.watcher.sessionInputOwner().io.(realSessionInputIO)
		if !ok || io.socketFor("worker:@1") != item.socket {
			t.Fatal("input IO is not bound to its Watcher socket")
		}
	}
}
