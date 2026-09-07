package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

type completionControlWatcher struct{ *fakeControlWatcher }

func (w *completionControlWatcher) UpdateWorkerProgress(id string, progress classifier.WorkerProgress) (*classifier.Worker, error) {
	result, err := w.turnStore.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: id, TurnID: progress.TurnID, Class: watcher.EvidenceControl,
		Kind: string(progress.Status), SourceID: "control:" + string(progress.Status),
		Summary: progress.Summary, Phase: progress.Phase, Attention: progress.Attention, At: time.Now(),
	})
	if err != nil {
		return nil, err
	}
	if !result.Matched {
		return nil, fmt.Errorf("report did not match current turn")
	}
	return w.fakeControlWatcher.UpdateWorkerProgress(id, progress)
}

func TestBDD_ZEN013_DecisionSavedWithCleanupPending(t *testing.T) {
	// Given a genuine cleanup failure on this exact completed owned Session.
	root := t.TempDir()
	store, err := brain.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(brain.Work{Title: "cleanup pending", Objective: "unambiguous decision"})
	if err != nil {
		t.Fatal(err)
	}
	fw := &completionControlWatcher{newFakeControlWatcher()}
	fw.turnStore = store
	fw.workers["worker"] = &classifier.Worker{ID: "worker", Delegated: true, State: classifier.StateDone}
	service := brain.NewService(store, fw, nil)
	app := &controlApp{watcher: fw, brainStore: store, brainService: service}
	admitted := app.HandleControlRequest(control.Request{Type: "worker_send", WorkerID: "worker", WorkID: item.ID, Text: "scoped task", Submit: true})
	if !admitted.OK {
		t.Fatalf("admission: %+v", admitted)
	}
	if result, err := service.ApplyDelegatedTurnProgress(watcher.TurnFact{SessionID: "worker", TurnID: admitted.TurnID, Class: watcher.EvidenceControl, Kind: "done", SourceID: "done", At: time.Now()}); err != nil || !result.Changed {
		t.Fatalf("report: %+v %v", result, err)
	}
	fw.killErr, fw.killLeavesLive = errors.New("owned generation cleanup unavailable"), true
	// When control commits acceptance, the failure response names the remaining
	// effect and carries durable Work instead of implying decision failure.
	response := app.HandleControlRequest(control.Request{Type: "brain_work_update", WorkID: item.ID, WorkFields: []string{"status"}, BrainWork: &brain.Work{Status: brain.WorkDone}})
	if response.OK || response.Error == nil || response.Error.Code != "brain_work_cleanup_pending" || response.BrainWork == nil || response.BrainWork.Status != brain.WorkDone {
		t.Fatalf("ambiguous decision response: %+v", response)
	}
	reopened, err := brain.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := reopened.Work(item.ID)
	if err != nil || closed.Status != brain.WorkDone || fw.GetWorker("worker") == nil {
		t.Fatalf("durability/cleanup: %+v %v", closed, err)
	}
	// Then explicit fault removal plus ordinary recovery completes only cleanup.
	fw.killErr, fw.killLeavesLive, fw.turnStore = nil, false, reopened
	if err := brain.NewService(reopened, fw, nil).ReconcileCompletedSessions(); err != nil {
		t.Fatal(err)
	}
	if fw.GetWorker("worker") != nil || len(fw.submitted) != 1 {
		t.Fatal("cleanup failed or business action replayed")
	}
}

// Real CLI, Unix control server, Store, Service and admission ledger. Provider
// transport and the explicit model decision are scripted, not a live LLM test.
// ZEN004: Given an admitted Worker, when its real CLI submits exact progress,
// then durable delivery, explicit decision and exact cleanup complete in order.
func TestBDD_ZEN004_CLIReportToDecisionAndSessionRemoval(t *testing.T) {
	root := t.TempDir()
	store, err := brain.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const hostID, workerID = "host:@cli-loop", "worker:@cli-loop"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(brain.Work{Title: "CLI completion loop", Objective: "review exact evidence", CompletionPolicy: brain.CompletionUntilDone, DoneCriteriaRef: "Brain accepts verified result"})
	if err != nil {
		t.Fatal(err)
	}
	fw := &completionControlWatcher{newFakeControlWatcher()}
	fw.turnStore = store
	fw.workers[hostID] = &classifier.Worker{ID: hostID, Hidden: true, State: classifier.StateDone}
	fw.workers[workerID] = &classifier.Worker{ID: workerID, Delegated: true, State: classifier.StateRunning, Command: "codex"}
	service := brain.NewService(store, fw, nil)
	app := &controlApp{watcher: fw, brainStore: store, brainService: service}
	// The same canonical Worker send path carries the randomly minted contract.
	response := app.HandleControlRequest(control.Request{Type: "worker_send", WorkerID: workerID, WorkID: item.ID, Text: "Return the scoped test evidence", Submit: true})
	if !response.OK || response.TurnID == "" {
		t.Fatalf("admission: %+v", response)
	}
	socket, err := control.DefaultSocketPath(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() { serverDone <- (&control.Server{Path: socket, Handler: app}).Run(ctx) }()
	t.Cleanup(func() { cancel(); waitForCLIControlServerShutdown(t, serverDone) })
	waitForCLISocketPath(t, socket)
	id, changes := service.SubscribeWork()
	t.Cleanup(func() { service.UnsubscribeWork(id) })
	var stderr bytes.Buffer
	args := []string{"--state-dir", root, "--id", workerID, "--turn-id", response.TurnID, "--status", "done", "--phase", "reporting", "--attention", "done", "--summary", "Verified exact integration evidence"}
	if err := runWorkerProgress(args, &stderr); err != nil {
		t.Fatalf("canonical CLI report: %v %s", err, stderr.String())
	}
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("persisted report emitted no event")
	}
	if err := service.ReconcileWorkChange(); err != nil {
		t.Fatal(err)
	}
	delivered, err := store.Work(item.ID)
	if err != nil || delivered.Review == nil || delivered.Review.Lease == nil || delivered.Review.Lease.DeliveredAt == nil {
		t.Fatalf("delivery: %+v %v", delivered, err)
	}
	seen := false
	for _, sent := range fw.submitted {
		if input, ok := work.ParseCanonicalDirectWorkEventInput(sent.text); ok && input.WorkID == item.ID {
			seen = input.Summary == "Verified exact integration evidence"
			if strings.Contains(sent.text, "Return the scoped test evidence") {
				t.Fatal("notification replayed original business input")
			}
		}
	}
	if !seen {
		t.Fatal("Brain transport did not receive exact persisted result")
	}
	if err := runWorkerProgress(args, &stderr); err != nil {
		t.Fatalf("duplicate report: %v", err)
	}
	// This is an explicit test decision after inspecting the delivered facts,
	// not a fake provider-success response or inferred Work completion.
	if err := runBrainCommand([]string{"work", "update", "--state-dir", root, "--id", item.ID, "--status", "done"}, &stderr); err != nil {
		t.Fatalf("decision: %v %s", err, stderr.String())
	}
	if fw.GetWorker(workerID) != nil || len(fw.killed) != 1 || fw.killed[0] != workerID {
		t.Fatalf("owned cleanup: %+v", fw.killed)
	}
	reopened, err := brain.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := reopened.Work(item.ID)
	if err != nil || closed.Status != brain.WorkDone || closed.Review != nil {
		t.Fatalf("durable close: %+v %v", closed, err)
	}
}
