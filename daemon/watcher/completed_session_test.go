package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// No tmux command is needed: stale ownership must fail before any transport IO.
// ZEN007: Given old cleanup waiting for the input lock, when a new turn is
// admitted first, then cleanup rejects the old identity before transport IO.
func TestBDD_ZEN007_CompletedCleanupSerializesWithNewInput(t *testing.T) {
	w := New(time.Second)
	ledger := &fakeTurnLedger{turns: map[string]TurnSnapshot{
		"worker": {SessionID: "worker", TurnID: "old", Status: TurnDone, SignalProtocol: true},
	}}
	w.SetTurnLedger(ledger)
	owner := w.sessionInputOwner()
	session := owner.session("worker")
	session.mu.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(started); done <- w.KillCompletedSession("worker", "old") }()
	<-started
	// Represents the new admission committed inside the shared input lock.
	ledger.turns["worker"] = TurnSnapshot{SessionID: "worker", TurnID: "new", Status: TurnRunning, SignalProtocol: true}
	session.mu.Unlock()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("old cleanup accepted a newer turn")
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup did not release input serialization")
	}
}

func TestBDD_ZEN008_AlreadyReclaimedSessionCleanupIsIdempotent(t *testing.T) {
	// Given exact completed ownership in the ledger but a reclaimed tmux window.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte("#!/bin/sh\necho \"can't find window: missing:@1\" >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	w := New(0)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"missing:@1": {SessionID: "missing:@1", TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	// When cleanup is reconciled repeatedly, proven absence is success, not an
	// ownership conflict. Other ownership checks remain covered separately.
	for range 2 {
		if err := w.KillCompletedSession("missing:@1", "completed"); err != nil {
			t.Fatalf("already reclaimed: %v", err)
		}
	}
}

func TestCompletedSessionCleanupRejectsNonterminalAndUncontractedTurns(t *testing.T) {
	for _, turn := range []TurnSnapshot{
		{SessionID: "worker", TurnID: "turn", Status: TurnRunning, SignalProtocol: true},
		{SessionID: "worker", TurnID: "turn", Status: TurnUnknown, SignalProtocol: true},
		{SessionID: "worker", TurnID: "turn", Status: TurnDone},
	} {
		w := New(time.Second)
		w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{"worker": turn}})
		if err := w.KillCompletedSession("worker", "turn"); err == nil {
			t.Fatalf("unsafe cleanup accepted: %+v", turn)
		}
	}
}
