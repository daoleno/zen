package watcher

import (
	"fmt"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestApplyPollFactsReconcilesExactHistoricalProviderTerminal(t *testing.T) {
	now := time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC)
	sessionID := "codex-reused:@1"
	oldActivityID := "session:activity:native-old"
	ledger := newFakeTurnLedger()
	ledger.seed(sessionID, TurnSnapshot{
		SessionID:  sessionID,
		TurnID:     "canonical-old-turn",
		Status:     TurnRunning,
		AcceptedAt: now.Add(-time.Hour),
		ActivityID: oldActivityID,
	})
	w := New(time.Second)
	w.turnLedger = ledger

	turn, _, _ := ledger.Turn(sessionID)
	settled := w.applyPollFacts(sessionID, true, -1, now, turn, ProviderActivityObservation{
		ID:        "session:activity:native-follow-up",
		Status:    "completed",
		StartedAt: now.Add(-time.Minute),
		SettledAt: now.Add(-time.Second),
		TerminalActivities: []ProviderTerminalActivity{
			{
				ID:        oldActivityID,
				Status:    "completed",
				StartedAt: now.Add(-time.Hour),
				SettledAt: now.Add(-30 * time.Minute),
			},
		},
	})
	if settled.Status != TurnDone || settled.ActivityID != oldActivityID {
		t.Fatalf("exact historical terminal did not settle canonical turn: %+v", settled)
	}
	agent := &classifier.Agent{Attention: "done", NeedsAttention: true}
	state, _ := projectDelegatedTurn(agent, settled)
	if state != classifier.StateDone || agent.Attention != "none" || agent.NeedsAttention {
		t.Fatalf("terminal Session projection = state %q agent=%+v", state, agent)
	}
	if len(ledger.applied) != 1 || ledger.applied[0].ActivityID != oldActivityID ||
		ledger.applied[0].Kind != "done" || !ledger.applied[0].Admission.Empty() {
		t.Fatalf("historical terminal fact = %+v, want exact activity-only done", ledger.applied)
	}

	// Reordered/repeated polls cannot reopen the immutable turn or apply a
	// second terminal fact.
	replayed := w.applyPollFacts(sessionID, true, -1, now.Add(time.Second), settled, ProviderActivityObservation{
		ID:        "session:activity:native-follow-up",
		Status:    "running",
		StartedAt: now.Add(-time.Minute),
	})
	if replayed.Status != TurnDone || len(ledger.applied) != 1 {
		t.Fatalf("later running poll reopened settled turn: turn=%+v facts=%+v", replayed, ledger.applied)
	}
}

func TestApplyPollFactsHistoricalProviderTerminalRequiresExactActivityID(t *testing.T) {
	now := time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC)
	sessionID := "codex-reused:@2"
	ledger := newFakeTurnLedger()
	ledger.seed(sessionID, TurnSnapshot{
		SessionID:  sessionID,
		TurnID:     "canonical-old-turn",
		Status:     TurnRunning,
		AcceptedAt: now.Add(-time.Hour),
		ActivityID: "session:activity:canonical",
	})
	w := New(time.Second)
	w.turnLedger = ledger

	turn, _, _ := ledger.Turn(sessionID)
	unchanged := w.applyPollFacts(sessionID, true, -1, now, turn, ProviderActivityObservation{
		ID:        "session:activity:current",
		Status:    "completed",
		StartedAt: now.Add(-time.Minute),
		SettledAt: now,
		TerminalActivities: []ProviderTerminalActivity{
			{
				ID:        "session:activity:near-miss",
				Status:    "completed",
				StartedAt: now.Add(-time.Hour),
				SettledAt: now.Add(-30 * time.Minute),
			},
		},
	})
	if unchanged.Status != TurnRunning {
		t.Fatalf("non-matching historical terminal settled canonical turn: %+v", unchanged)
	}
}

func TestApplyPollFactsTerminalOlderThanBoundFailsClosed(t *testing.T) {
	now := time.Date(2026, 8, 9, 4, 27, 0, 0, time.UTC)
	sessionID := "codex-reused:@older-than-bound"
	desiredActivityID := "session:activity:unrecoverable-old"
	ledger := newFakeTurnLedger()
	ledger.seed(sessionID, TurnSnapshot{
		SessionID: sessionID, TurnID: "canonical-old-turn", Status: TurnRunning,
		AcceptedAt: now.Add(-time.Hour), ActivityID: desiredActivityID,
	})
	terminals := make([]ProviderTerminalActivity, 0, 64)
	for index := 0; index < 64; index++ {
		terminals = append(terminals, ProviderTerminalActivity{
			ID: fmt.Sprintf("session:activity:retained-%02d", index), Status: "completed",
			StartedAt: now.Add(-time.Minute), SettledAt: now.Add(-time.Second),
		})
	}
	w := New(time.Second)
	w.turnLedger = ledger
	turn, _, _ := ledger.Turn(sessionID)
	unchanged := w.applyPollFacts(sessionID, true, -1, now, turn, ProviderActivityObservation{
		ID: "session:activity:latest", Status: "completed",
		StartedAt: now.Add(-time.Minute), SettledAt: now,
		TerminalActivities: terminals,
	})
	if unchanged.Status != TurnRunning || len(ledger.applied) != 1 ||
		ledger.applied[0].ActivityID == desiredActivityID {
		t.Fatalf("out-of-bound terminal was guessed or settled: turn=%+v facts=%+v", unchanged, ledger.applied)
	}
}
