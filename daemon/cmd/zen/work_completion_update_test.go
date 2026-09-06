package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/lifecycle"
)

func TestWorkUpdateCompletionCLICommitsCanonicalContract(t *testing.T) {
	root := t.TempDir()
	store, err := brain.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(brain.Work{Title: "completion update", Objective: "verify policy command", CompletionPolicy: brain.CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	socket, err := control.DefaultSocketPath(root)
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{brainStore: store}
	server := &control.Server{Path: socket, Handler: app}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	t.Cleanup(func() { cancel(); waitForCLIControlServerShutdown(t, done) })
	waitForCLISocketPath(t, socket)
	var stderr bytes.Buffer
	if err := runBrainCommand([]string{"work", "update", "--state-dir", root, "--id", item.ID, "--completion", "until_done", "--done-criteria", "Brain accepts verified result"}, &stderr); err != nil {
		t.Fatalf("CLI update: %v %s", err, stderr.String())
	}
	updated, err := store.Work(item.ID)
	if err != nil || updated.CompletionPolicy != brain.CompletionUntilDone || updated.DoneCriteriaRef != "Brain accepts verified result" {
		t.Fatalf("success did not update contract: %+v %v", updated, err)
	}
	canonical, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil || canonical.Policy != lifecycle.PolicyUntilDone || canonical.DoneCriteriaRef != updated.DoneCriteriaRef {
		t.Fatalf("canonical contract=%+v %v", canonical, err)
	}
	reopened, err := brain.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	durable, err := reopened.Work(item.ID)
	if err != nil || durable.CompletionPolicy != updated.CompletionPolicy || durable.DoneCriteriaRef != updated.DoneCriteriaRef {
		t.Fatalf("reload lost contract: %+v %v", durable, err)
	}
	for _, args := range [][]string{{"--completion", "invalid"}, {"--done-criteria", ""}} {
		command := append([]string{"work", "update", "--state-dir", root, "--id", item.ID}, args...)
		if err := runBrainCommand(command, &stderr); err == nil {
			t.Fatalf("invalid contract accepted: %v", args)
		}
		unchanged, _ := store.FSM().State(canonical.ID)
		if unchanged.Revision != canonical.Revision {
			t.Fatal("invalid command partially committed")
		}
	}
	if _, err := store.FSM().Complete(canonical.ID, 0, "test", "terminal fixture"); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncWorkProjection(item.ID); err != nil {
		t.Fatal(err)
	}
	if err := runBrainCommand([]string{"work", "update", "--state-dir", root, "--id", item.ID, "--completion", "bounded"}, &stderr); err == nil {
		t.Fatal("terminal contract update falsely succeeded")
	}
}
