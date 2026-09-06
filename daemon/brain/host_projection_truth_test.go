package brain

import (
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

type disappearingHostWatcher struct{ *fakeWatcher }

func (w *disappearingHostWatcher) SendInputWithReceiptWhenReadyResult(id, command, text string, receipt watcher.InputReceiptForGeneration) (watcher.InputResult, watcher.OwnedGeneration, error) {
	result, owned, err := w.fakeWatcher.SendInputWithReceiptWhenReadyResult(id, command, text, receipt)
	delete(w.sessions, id)
	w.workers = nil
	return result, owned, err
}

func TestHostActivationCannotFabricateRunningAfterDisappearance(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &disappearingHostWatcher{fakeWatcher: &fakeWatcher{}}
	service := NewService(store, w, nil)
	executor := work.NewWorkerExecutor("codex", work.Executor{Name: "codex", Command: "codex", Kind: "codex"})
	ref, err := service.ensureHostWorker(executor)
	if err == nil || !strings.Contains(err.Error(), "not observable") || ref.ID != "" {
		t.Fatalf("unobservable Host was reported running: ref=%+v err=%v", ref, err)
	}
	if len(w.created) != 1 || len(w.sentCalls) != 1 || len(w.killed) != 0 {
		t.Fatalf("Host failure replayed or killed another Session: created=%d sent=%d killed=%d", len(w.created), len(w.sentCalls), len(w.killed))
	}
}

func TestHostPendingObservationHasNoInventedActivity(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, nil)
	ref := hostBootstrapRef(service, hostDiscovery{id: "host:@1", command: "codex"})
	if ref.Status != string(classifier.StateUnknown) || !ref.Updated.IsZero() {
		t.Fatalf("pending observation invented activity: %+v", ref)
	}
}
