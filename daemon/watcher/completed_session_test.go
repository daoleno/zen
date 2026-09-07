package watcher

import (
	"testing"
	"time"
)

// No tmux command is needed: stale ownership must fail before any transport IO.
func TestCompletedSessionCleanupSerializesWithNewInput(t *testing.T) {
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
