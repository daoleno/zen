package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func TestStructuredTurnRegistryAcceptsBeforeProviderAndDeduplicatesRetry(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	dispatches := 0

	_, err := registry.acceptInput(key, "turn-failed", "", false, func() error {
		dispatches++
		return errors.New("tmux unavailable")
	})
	if err == nil {
		t.Fatal("failed dispatch must be returned")
	}
	if turn, queued := registry.snapshot(key); turn != nil || len(queued) != 0 {
		t.Fatalf("failed dispatch registered lifecycle: turn=%#v queued=%#v", turn, queued)
	}

	startedAt := now.Add(-time.Minute).Format(time.RFC3339Nano)
	accepted, err := registry.acceptInput(key, "turn-public", startedAt, false, func() error {
		dispatches++
		return nil
	})
	if err != nil || accepted.Queued || accepted.Duplicate {
		t.Fatalf("first acceptance = %#v, err=%v", accepted, err)
	}
	turn, queued := registry.snapshot(key)
	assertStructuredTurn(t, turn, "turn-public", work.CodexConversationTurnRunning, startedAt)
	if len(queued) != 0 {
		t.Fatalf("queued = %#v, want empty", queued)
	}

	retry, err := registry.acceptInput(key, "turn-public", now.Format(time.RFC3339Nano), false, func() error {
		dispatches++
		return nil
	})
	if err != nil || !retry.Duplicate || retry.Queued {
		t.Fatalf("retry acceptance = %#v, err=%v", retry, err)
	}
	if dispatches != 2 {
		t.Fatalf("dispatches = %d, want failed first attempt + one successful delivery", dispatches)
	}
	turn, _ = registry.snapshot(key)
	if turn.StartedAt != startedAt {
		t.Fatalf("retry reset start = %q, want %q", turn.StartedAt, startedAt)
	}
}

func TestStructuredTurnRegistryConcurrentAcceptedSameIDDispatchesOnce(t *testing.T) {
	registry := newStructuredTurnRegistry()
	key := structuredTurnRegistryKey("", "work-agent")
	var dispatches int32
	const attempts = 12
	results := make([]structuredInputAcceptance, attempts)
	errorsByAttempt := make([]error, attempts)
	var wait sync.WaitGroup
	for index := 0; index < attempts; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			results[index], errorsByAttempt[index] = registry.acceptInput(
				key,
				"turn-concurrent",
				"2026-07-15T12:00:00Z",
				false,
				func() error {
					atomic.AddInt32(&dispatches, 1)
					return nil
				},
			)
		}(index)
	}
	wait.Wait()
	if dispatches != 1 {
		t.Fatalf("concurrent accepted dispatches = %d, want 1", dispatches)
	}
	firstAcceptances := 0
	for index, err := range errorsByAttempt {
		if err != nil {
			t.Fatalf("attempt %d error = %v", index, err)
		}
		if !results[index].Duplicate {
			firstAcceptances++
		}
	}
	if firstAcceptances != 1 {
		t.Fatalf("first acceptances = %d, want 1", firstAcceptances)
	}
}

func TestStructuredTurnRegistryConcurrentUnconfirmedSameIDDispatchesOnce(t *testing.T) {
	registry := newStructuredTurnRegistry()
	key := structuredTurnRegistryKey("", "work-agent")
	var dispatches int32
	const attempts = 12
	errorsByAttempt := make([]error, attempts)
	var wait sync.WaitGroup
	for index := 0; index < attempts; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, errorsByAttempt[index] = registry.acceptInput(
				key,
				"turn-uncertain",
				"2026-07-15T12:00:00Z",
				false,
				func() error {
					atomic.AddInt32(&dispatches, 1)
					return errors.New("executor acknowledgement lost")
				},
			)
		}(index)
	}
	wait.Wait()
	if dispatches != 1 {
		t.Fatalf("concurrent unconfirmed dispatches = %d, want 1", dispatches)
	}
	for index, err := range errorsByAttempt {
		var unconfirmed *structuredInputDeliveryUnconfirmedError
		if !errors.As(err, &unconfirmed) {
			t.Fatalf("attempt %d error = %T %v, want unconfirmed", index, err, err)
		}
	}
}

func TestStructuredTurnRegistryQueuedHintWaitsBehindExistingProviderTurn(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 30, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	providerStart := now.Add(-time.Minute).Format(time.RFC3339Nano)
	turn, queued := registry.project(key, &work.CodexConversationTurn{
		ID: "provider-existing", Status: work.CodexConversationTurnRunning, StartedAt: providerStart,
	})
	assertStructuredTurn(t, turn, "provider-existing", work.CodexConversationTurnRunning, providerStart)
	if len(queued) != 0 {
		t.Fatalf("initial provider queue = %#v", queued)
	}
	queuedStart := now.Format(time.RFC3339Nano)
	accepted, err := registry.acceptInput(key, "public-queued", queuedStart, true, func() error { return nil })
	if err != nil || !accepted.Queued {
		t.Fatalf("queued acceptance = %#v, err=%v", accepted, err)
	}
	turn, queued = registry.snapshot(key)
	if turn == nil || turn.ID != "provider-existing" || len(queued) != 1 || queued[0].ID != "public-queued" || queued[0].Status != work.CodexConversationTurnQueued {
		t.Fatalf("pre-provider queue = turn %#v queued %#v", turn, queued)
	}

	turn, queued = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-existing", Status: work.CodexConversationTurnRunning, StartedAt: providerStart,
	})
	assertStructuredTurn(t, turn, "provider-existing", work.CodexConversationTurnRunning, providerStart)
	if len(queued) != 1 || queued[0].ID != "public-queued" || queued[0].StartedAt != queuedStart {
		t.Fatalf("existing provider did not retain accepted queue: %#v", queued)
	}

	now = now.Add(time.Second)
	turn, queued = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-existing", Status: work.CodexConversationTurnCompleted,
		StartedAt: providerStart, SettledAt: now.Format(time.RFC3339Nano),
	})
	assertStructuredTurn(t, turn, "public-queued", work.CodexConversationTurnRunning, queuedStart)
	if len(queued) != 0 {
		t.Fatalf("provider terminal did not advance queued accepted input: %#v", queued)
	}
}

func TestStructuredTurnRegistryRejectsUnverifiedQueuedHintBeforeDispatch(t *testing.T) {
	registry := newStructuredTurnRegistry()
	key := structuredTurnRegistryKey("", "work-agent")
	dispatches := 0
	accepted, err := registry.acceptInput(
		key,
		"public-queued",
		"2026-07-15T12:30:00Z",
		true,
		func() error {
			dispatches++
			return nil
		},
	)
	if !errors.Is(err, errStructuredLifecycleSyncing) {
		t.Fatalf("acceptance = %#v error = %v, want lifecycle syncing", accepted, err)
	}
	if dispatches != 0 {
		t.Fatalf("ambiguous queued input dispatched %d times", dispatches)
	}
	if turn, queued := registry.snapshot(key); turn != nil || len(queued) != 0 {
		t.Fatalf("ambiguous hint mutated lifecycle: turn=%#v queue=%#v", turn, queued)
	}
}

func TestStructuredTurnRegistryFailedDispatchDoesNotAssociateBrainHost(t *testing.T) {
	registry := newStructuredTurnRegistry()
	key := structuredTurnRegistryKey("brain-thread:current", "host-a")
	_, err := registry.acceptInputWithOptions(
		key, "host-a", "failed", "", false, false, "", func() error {
			return errors.New("host disappeared")
		},
	)
	if err == nil {
		t.Fatal("failed dispatch unexpectedly succeeded")
	}
	registry.mu.Lock()
	if got := registry.byScope[key].agentID; got != "" {
		registry.mu.Unlock()
		t.Fatalf("failed dispatch associated host %q", got)
	}
	registry.mu.Unlock()
	_, err = registry.acceptInputWithOptions(
		key, "host-b", "accepted", "", false, false, "", func() error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if got := registry.byScope[key].agentID; got != "host-b" {
		t.Fatalf("successful replacement host = %q, want host-b", got)
	}
}

func TestStructuredTurnRegistryQueuesAndAdvancesOnProviderTerminal(t *testing.T) {
	now := time.Date(2026, 7, 15, 13, 0, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	firstStart := now.Format(time.RFC3339Nano)
	secondStart := now.Add(time.Second).Format(time.RFC3339Nano)

	mustAcceptStructuredInput(t, registry, key, "public-1", firstStart, false)
	second, err := registry.acceptInput(key, "public-2", secondStart, true, func() error { return nil })
	if err != nil || !second.Queued {
		t.Fatalf("second acceptance = %#v, err=%v", second, err)
	}

	turn, queued := registry.project(key, &work.CodexConversationTurn{
		ID: "provider-1", Status: work.CodexConversationTurnRunning,
		StartedAt: now.Add(2 * time.Second).Format(time.RFC3339Nano),
	})
	assertStructuredTurn(t, turn, "public-1", work.CodexConversationTurnRunning, firstStart)
	if len(queued) != 1 || queued[0].ID != "public-2" || queued[0].Status != work.CodexConversationTurnQueued || queued[0].StartedAt != secondStart {
		t.Fatalf("queued = %#v", queued)
	}

	now = now.Add(3 * time.Second)
	turn, queued = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-1", Status: work.CodexConversationTurnCompleted,
		StartedAt: now.Add(-time.Second).Format(time.RFC3339Nano),
		SettledAt: now.Format(time.RFC3339Nano),
	})
	assertStructuredTurn(t, turn, "public-2", work.CodexConversationTurnRunning, secondStart)
	if len(queued) != 0 {
		t.Fatalf("terminal provider fact did not advance queue: %#v", queued)
	}

	// A stale running poll for the already-settled provider turn cannot reopen
	// or steal the public identity of the advanced turn.
	turn, _ = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-1", Status: work.CodexConversationTurnRunning,
		StartedAt: firstStart,
	})
	assertStructuredTurn(t, turn, "public-2", work.CodexConversationTurnRunning, secondStart)

	now = now.Add(time.Second)
	turn, _ = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-2", Status: work.CodexConversationTurnRunning,
		StartedAt: now.Format(time.RFC3339Nano),
	})
	assertStructuredTurn(t, turn, "public-2", work.CodexConversationTurnRunning, secondStart)
	now = now.Add(time.Second)
	turn, _ = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-2", Status: work.CodexConversationTurnFailed,
		StartedAt: now.Add(-time.Second).Format(time.RFC3339Nano),
		SettledAt: now.Format(time.RFC3339Nano),
	})
	assertStructuredTurn(t, turn, "public-2", work.CodexConversationTurnFailed, secondStart)
	settledAt := turn.SettledAt

	turn, _ = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-2", Status: work.CodexConversationTurnRunning,
		StartedAt: now.Format(time.RFC3339Nano),
	})
	assertStructuredTurn(t, turn, "public-2", work.CodexConversationTurnFailed, secondStart)
	if turn.SettledAt != settledAt {
		t.Fatalf("stale active poll reset settlement: got %q want %q", turn.SettledAt, settledAt)
	}
}

func TestStructuredTurnRegistryDrainsMultipleProviderTerminalsSkippedBetweenPolls(t *testing.T) {
	now := time.Date(2026, 7, 15, 13, 30, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	starts := []string{
		now.Format(time.RFC3339Nano),
		now.Add(time.Second).Format(time.RFC3339Nano),
		now.Add(2 * time.Second).Format(time.RFC3339Nano),
	}
	for index, id := range []string{"public-a", "public-b", "public-c"} {
		mustAcceptStructuredInput(t, registry, key, id, starts[index], index > 0)
	}

	history := []work.CodexConversationTurn{
		{
			ID: "provider-a", Status: work.CodexConversationTurnCompleted,
			StartedAt: now.Add(10 * time.Second).Format(time.RFC3339Nano),
			SettledAt: now.Add(11 * time.Second).Format(time.RFC3339Nano),
		},
		{
			ID: "provider-b", Status: work.CodexConversationTurnFailed,
			StartedAt: now.Add(12 * time.Second).Format(time.RFC3339Nano),
			SettledAt: now.Add(13 * time.Second).Format(time.RFC3339Nano),
		},
		{
			ID: "provider-c", Status: work.CodexConversationTurnCompleted,
			StartedAt: now.Add(14 * time.Second).Format(time.RFC3339Nano),
			SettledAt: now.Add(15 * time.Second).Format(time.RFC3339Nano),
		},
	}
	now = now.Add(16 * time.Second)
	turn, queued := registry.projectProviderHistory(key, history, &history[len(history)-1])
	assertStructuredTurn(t, turn, "public-c", work.CodexConversationTurnCompleted, starts[2])
	if turn.SettledAt != history[2].SettledAt || len(queued) != 0 {
		t.Fatalf("skipped-poll drain = turn %#v queued %#v", turn, queued)
	}

	// Provider history is repeated on every transcript load. Replaying it must
	// not apply an older terminal to the already-drained public turn.
	turn, queued = registry.projectProviderHistory(key, history, &history[len(history)-1])
	assertStructuredTurn(t, turn, "public-c", work.CodexConversationTurnCompleted, starts[2])
	if turn.SettledAt != history[2].SettledAt || len(queued) != 0 {
		t.Fatalf("history replay changed lifecycle = turn %#v queued %#v", turn, queued)
	}
}

func TestStructuredTurnRegistryIgnoresPreexistingTerminalAndAcceptsFastTerminal(t *testing.T) {
	now := time.Date(2026, 7, 15, 14, 0, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	old := work.CodexConversationTurn{
		ID: "provider-old", Status: work.CodexConversationTurnCompleted,
		StartedAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
		SettledAt: now.Add(-time.Second).Format(time.RFC3339Nano),
	}
	registry.project(key, &old)

	// The public clock is deliberately far from the server clock. Correlation
	// must use acceptedAt internally rather than trusting device time.
	publicStart := now.Add(24 * time.Hour).Format(time.RFC3339Nano)
	mustAcceptStructuredInput(t, registry, key, "public-new", publicStart, false)
	turn, _ := registry.project(key, &old)
	assertStructuredTurn(t, turn, "public-new", work.CodexConversationTurnRunning, publicStart)

	now = now.Add(time.Second)
	turn, _ = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-new", Status: work.CodexConversationTurnCompleted,
		StartedAt: now.Format(time.RFC3339Nano),
		SettledAt: now.Format(time.RFC3339Nano),
	})
	assertStructuredTurn(t, turn, "public-new", work.CodexConversationTurnCompleted, publicStart)
}

func TestStructuredTurnRegistryNormalizesMissingProviderTimesOnce(t *testing.T) {
	now := time.Date(2026, 7, 15, 15, 0, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")

	turn, _ := registry.project(key, &work.CodexConversationTurn{
		ID: "provider-only", Status: work.CodexConversationTurnRunning,
	})
	wantStart := now.Format(time.RFC3339Nano)
	assertStructuredTurn(t, turn, "provider-only", work.CodexConversationTurnRunning, wantStart)

	now = now.Add(time.Minute)
	turn, _ = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-only", Status: work.CodexConversationTurnRunning,
	})
	assertStructuredTurn(t, turn, "provider-only", work.CodexConversationTurnRunning, wantStart)

	turn, _ = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-only", Status: work.CodexConversationTurnCompleted,
	})
	assertStructuredTurn(t, turn, "provider-only", work.CodexConversationTurnCompleted, wantStart)
	wantSettled := now.Format(time.RFC3339Nano)
	if turn.SettledAt != wantSettled {
		t.Fatalf("settled_at = %q, want %q", turn.SettledAt, wantSettled)
	}

	now = now.Add(time.Minute)
	turn, _ = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-only", Status: work.CodexConversationTurnCompleted,
	})
	if turn.StartedAt != wantStart || turn.SettledAt != wantSettled {
		t.Fatalf("repeated missing timestamps reset lifecycle: %#v", turn)
	}
}

func TestStructuredTurnRegistryProjectsLatestProviderOnlyTerminalHistory(t *testing.T) {
	now := time.Date(2026, 7, 15, 15, 30, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	history := []work.CodexConversationTurn{
		{ID: "provider-a", Status: work.CodexConversationTurnCompleted, StartedAt: now.Add(-6 * time.Second).Format(time.RFC3339Nano), SettledAt: now.Add(-5 * time.Second).Format(time.RFC3339Nano)},
		{ID: "provider-b", Status: work.CodexConversationTurnFailed, StartedAt: now.Add(-4 * time.Second).Format(time.RFC3339Nano), SettledAt: now.Add(-3 * time.Second).Format(time.RFC3339Nano)},
		{ID: "provider-c", Status: work.CodexConversationTurnCancelled, StartedAt: now.Add(-2 * time.Second).Format(time.RFC3339Nano), SettledAt: now.Add(-time.Second).Format(time.RFC3339Nano)},
	}
	turn, queued := registry.projectProviderHistory(key, history, &history[len(history)-1])
	assertStructuredTurn(t, turn, "provider-c", work.CodexConversationTurnCancelled, history[2].StartedAt)
	if turn.SettledAt != history[2].SettledAt || len(queued) != 0 {
		t.Fatalf("provider-only latest history = turn %#v queued %#v", turn, queued)
	}
}

func TestStructuredTurnRegistryStopInterruptsOnlyCurrentAndKeepsQueue(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 0, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	mustAcceptStructuredInput(t, registry, key, "public-1", now.Format(time.RFC3339Nano), false)
	registry.project(key, &work.CodexConversationTurn{
		ID: "provider-1", Status: work.CodexConversationTurnRunning,
		StartedAt: now.Add(time.Second).Format(time.RFC3339Nano),
	})
	mustAcceptStructuredInput(t, registry, key, "public-2", now.Add(2*time.Second).Format(time.RFC3339Nano), true)

	now = now.Add(3 * time.Second)
	if !registry.interrupt(key, "public-1", now) {
		t.Fatal("matching active stop was not accepted")
	}
	turn, queued := registry.snapshot(key)
	assertStructuredTurn(t, turn, "public-1", work.CodexConversationTurnInterrupted, time.Date(2026, 7, 15, 16, 0, 0, 0, time.UTC).Format(time.RFC3339Nano))
	if len(queued) != 1 || queued[0].ID != "public-2" {
		t.Fatalf("stop altered queued submissions: %#v", queued)
	}
	if registry.interrupt(key, "public-2", now) {
		t.Fatal("stale/mismatched stop interrupted a queued turn")
	}

	// Provider confirmation for the stopped current turn advances the already
	// accepted queued turn without changing its public start.
	turn, queued = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-1", Status: work.CodexConversationTurnInterrupted,
		StartedAt: now.Add(-time.Second).Format(time.RFC3339Nano),
		SettledAt: now.Format(time.RFC3339Nano),
	})
	assertStructuredTurn(t, turn, "public-2", work.CodexConversationTurnRunning, time.Date(2026, 7, 15, 16, 0, 2, 0, time.UTC).Format(time.RFC3339Nano))
	if len(queued) != 0 {
		t.Fatalf("confirmed stop did not advance queue: %#v", queued)
	}
	dispatches := 0
	accepted, err := registry.interruptWithDispatch(key, "public-1", now, func() error {
		dispatches++
		return nil
	})
	if err != nil || accepted || dispatches != 0 {
		t.Fatalf("stale stop = accepted %t dispatches %d err %v", accepted, dispatches, err)
	}
	turn, _ = registry.snapshot(key)
	assertStructuredTurn(t, turn, "public-2", work.CodexConversationTurnRunning, time.Date(2026, 7, 15, 16, 0, 2, 0, time.UTC).Format(time.RFC3339Nano))
}

func TestStructuredTurnRegistryFailedStopDispatchDoesNotSettle(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 15, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	key := structuredTurnRegistryKey("", "work-agent")
	start := now.Format(time.RFC3339Nano)
	mustAcceptStructuredInput(t, registry, key, "public-current", start, false)

	accepted, err := registry.interruptWithDispatch(key, "public-current", now.Add(time.Second), func() error {
		return errors.New("provider rejected stop")
	})
	if err == nil || accepted {
		t.Fatalf("failed stop = accepted %t err %v", accepted, err)
	}
	turn, _ := registry.snapshot(key)
	assertStructuredTurn(t, turn, "public-current", work.CodexConversationTurnRunning, start)
}

func TestStructuredTurnRegistryAmbiguousLateRunningFactCannotReopenStoppedTurn(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 17, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	start := now.Format(time.RFC3339Nano)
	mustAcceptStructuredInput(t, registry, key, "public-current", start, false)
	now = now.Add(time.Second)
	if !registry.interrupt(key, "public-current", now) {
		t.Fatal("Stop did not settle accepted turn")
	}

	now = now.Add(time.Second)
	turn, _ := registry.project(key, &work.CodexConversationTurn{
		ID:     "late-provider",
		Status: work.CodexConversationTurnRunning,
		// No authoritative StartedAt: the poll time must not make this look new.
	})
	assertStructuredTurn(t, turn, "public-current", work.CodexConversationTurnInterrupted, start)
}

func TestStructuredTurnRegistryStopBeforeProviderPromotesQueuedSuccessor(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 20, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	firstStart := now.Add(-time.Second).Format(time.RFC3339Nano)
	secondStart := now.Add(time.Second).Format(time.RFC3339Nano)
	mustAcceptStructuredInput(t, registry, key, "public-a", firstStart, false)
	mustAcceptStructuredInput(t, registry, key, "public-b", secondStart, true)
	if !registry.interrupt(key, "public-a", now) {
		t.Fatal("pre-provider stop did not settle the current public turn")
	}

	// Provider A never exposes an identity or terminal record. A native turn
	// beginning after Stop is therefore queued B, not a late observation of A.
	providerStart := now.Add(time.Second).Format(time.RFC3339Nano)
	turn, queued := registry.project(key, &work.CodexConversationTurn{
		ID: "provider-b", Status: work.CodexConversationTurnRunning,
		StartedAt: providerStart,
	})
	assertStructuredTurn(t, turn, "public-b", work.CodexConversationTurnRunning, secondStart)
	if len(queued) != 0 {
		t.Fatalf("queued successor was not promoted: %#v", queued)
	}

	now = now.Add(2 * time.Second)
	turn, queued = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-b", Status: work.CodexConversationTurnCompleted,
		StartedAt: providerStart, SettledAt: now.Format(time.RFC3339Nano),
	})
	assertStructuredTurn(t, turn, "public-b", work.CodexConversationTurnCompleted, secondStart)
	if len(queued) != 0 {
		t.Fatalf("settled successor left queue entries: %#v", queued)
	}
}

func TestStructuredTurnRegistryStopBeforeProviderAppliesTerminalOnlySuccessor(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 25, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	firstStart := now.Add(-time.Second).Format(time.RFC3339Nano)
	secondStart := now.Add(time.Second).Format(time.RFC3339Nano)
	mustAcceptStructuredInput(t, registry, key, "public-a", firstStart, false)
	mustAcceptStructuredInput(t, registry, key, "public-b", secondStart, true)
	if !registry.interrupt(key, "public-a", now) {
		t.Fatal("pre-provider stop did not settle the current public turn")
	}

	providerStart := now.Add(time.Second).Format(time.RFC3339Nano)
	providerSettled := now.Add(2 * time.Second).Format(time.RFC3339Nano)
	turn, queued := registry.projectProviderHistory(key, []work.CodexConversationTurn{
		{
			ID: "provider-b", Status: work.CodexConversationTurnCompleted,
			StartedAt: providerStart, SettledAt: providerSettled,
		},
	}, nil)
	assertStructuredTurn(t, turn, "public-b", work.CodexConversationTurnCompleted, secondStart)
	if turn.SettledAt != providerSettled || len(queued) != 0 {
		t.Fatalf("terminal-only successor = turn %#v queued %#v", turn, queued)
	}

	// Replaying bounded provider history cannot reopen the completed successor.
	turn, queued = registry.projectProviderHistory(key, []work.CodexConversationTurn{
		{
			ID: "provider-b", Status: work.CodexConversationTurnCompleted,
			StartedAt: providerStart, SettledAt: providerSettled,
		},
	}, nil)
	assertStructuredTurn(t, turn, "public-b", work.CodexConversationTurnCompleted, secondStart)
	if len(queued) != 0 {
		t.Fatalf("terminal-only replay changed queue: %#v", queued)
	}
}

func TestStructuredTurnRegistryStopAfterObservedProviderAppliesTerminalOnlySuccessor(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 27, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	firstStart := now.Add(-2 * time.Second).Format(time.RFC3339Nano)
	secondStart := now.Add(time.Second).Format(time.RFC3339Nano)
	mustAcceptStructuredInput(t, registry, key, "public-a", firstStart, false)
	registry.project(key, &work.CodexConversationTurn{
		ID: "provider-a", Status: work.CodexConversationTurnRunning,
		StartedAt: firstStart,
	})
	mustAcceptStructuredInput(t, registry, key, "public-b", secondStart, true)
	if !registry.interrupt(key, "public-a", now) {
		t.Fatal("stop after provider identity did not settle current public turn")
	}

	providerSettled := now.Add(2 * time.Second).Format(time.RFC3339Nano)
	turn, queued := registry.projectProviderHistory(key, []work.CodexConversationTurn{
		{
			ID: "provider-b", Status: work.CodexConversationTurnCompleted,
			StartedAt: secondStart, SettledAt: providerSettled,
		},
	}, nil)
	assertStructuredTurn(t, turn, "public-b", work.CodexConversationTurnCompleted, secondStart)
	if turn.SettledAt != providerSettled || len(queued) != 0 {
		t.Fatalf("observed predecessor terminal-only successor = turn %#v queued %#v", turn, queued)
	}
}

func TestStructuredTurnRegistryRejectsDelayedRunningFactForNewlyAcceptedTurn(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 28, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	publicStart := now.Format(time.RFC3339Nano)
	mustAcceptStructuredInput(t, registry, key, "public-b", publicStart, false)

	oldStart := now.Add(-10 * time.Second).Format(time.RFC3339Nano)
	turn, _ := registry.project(key, &work.CodexConversationTurn{
		ID: "provider-a", Status: work.CodexConversationTurnRunning,
		StartedAt: oldStart,
	})
	assertStructuredTurn(t, turn, "public-b", work.CodexConversationTurnRunning, publicStart)

	turn, _ = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-a", Status: work.CodexConversationTurnCompleted,
		StartedAt: oldStart, SettledAt: now.Add(time.Second).Format(time.RFC3339Nano),
	})
	assertStructuredTurn(t, turn, "public-b", work.CodexConversationTurnRunning, publicStart)

	providerBStart := now.Add(2 * time.Second).Format(time.RFC3339Nano)
	turn, _ = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-b", Status: work.CodexConversationTurnRunning,
		StartedAt: providerBStart,
	})
	assertStructuredTurn(t, turn, "public-b", work.CodexConversationTurnRunning, publicStart)
	turn, _ = registry.project(key, &work.CodexConversationTurn{
		ID: "provider-b", Status: work.CodexConversationTurnCompleted,
		StartedAt: providerBStart, SettledAt: now.Add(3 * time.Second).Format(time.RFC3339Nano),
	})
	assertStructuredTurn(t, turn, "public-b", work.CodexConversationTurnCompleted, publicStart)
}

func TestStructuredTurnRegistryExplicitCancellationSettlesButRemovalGapDoesNot(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 30, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	start := now.Format(time.RFC3339Nano)
	mustAcceptStructuredInput(t, registry, key, "public-current", start, false)
	mustAcceptStructuredInput(t, registry, key, "public-next", now.Add(time.Second).Format(time.RFC3339Nano), true)

	// A missing provider observation (including host/process replacement gaps)
	// is not an authoritative terminal fact.
	turn, queued := registry.project(key, nil)
	assertStructuredTurn(t, turn, "public-current", work.CodexConversationTurnRunning, start)
	if len(queued) != 1 {
		t.Fatalf("provider gap changed queue: %#v", queued)
	}
	if registry.cancel(key, "stale-turn", now) {
		t.Fatal("mismatched cancellation settled the current turn")
	}
	if !registry.cancel(key, "public-current", now) {
		t.Fatal("explicit matching cancellation was not accepted")
	}
	turn, queued = registry.snapshot(key)
	assertStructuredTurn(t, turn, "public-current", work.CodexConversationTurnCancelled, start)
	if turn.SettledAt != now.Format(time.RFC3339Nano) || len(queued) != 1 || queued[0].ID != "public-next" {
		t.Fatalf("explicit cancellation projection = turn %#v queued %#v", turn, queued)
	}
}

func TestStructuredTurnRegistryThreadControlSettlesOnConversationReplacement(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 35, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("brain-thread:current", "host-a")
	registry.projectProviderHistoryWithIdentity(key, "session:old", nil, nil)
	accepted, err := registry.acceptInputWithOptions(
		key,
		"",
		"control-new",
		now.Format(time.RFC3339Nano),
		false,
		true,
		"",
		func() error { return nil },
	)
	if err != nil || accepted.Queued {
		t.Fatalf("thread control acceptance = %#v err %v", accepted, err)
	}
	turn, queued, epoch, revision := registry.projectProviderHistoryWithIdentity(
		key,
		"session:old",
		nil,
		nil,
	)
	assertStructuredTurn(t, turn, "control-new", work.CodexConversationTurnRunning, now.Format(time.RFC3339Nano))
	if len(queued) != 0 || epoch != accepted.Epoch || revision != accepted.Revision {
		t.Fatalf("same-session control projection = turn %#v queue %#v epoch %q rev %d", turn, queued, epoch, revision)
	}

	now = now.Add(time.Second)
	turn, queued, epoch, revision = registry.projectProviderHistoryWithIdentity(
		key,
		"session:new",
		nil,
		nil,
	)
	assertStructuredTurn(t, turn, "control-new", work.CodexConversationTurnCompleted, time.Date(2026, 7, 15, 16, 35, 0, 0, time.UTC).Format(time.RFC3339Nano))
	if len(queued) != 0 || epoch != accepted.Epoch || revision <= accepted.Revision {
		t.Fatalf("replacement control projection = turn %#v queue %#v epoch %q rev %d", turn, queued, epoch, revision)
	}

	next, err := registry.acceptInput(
		key,
		"real-next",
		now.Add(time.Second).Format(time.RFC3339Nano),
		false,
		func() error { return nil },
	)
	if err != nil || next.Queued {
		t.Fatalf("first prompt after /new = %#v err %v", next, err)
	}
}

func TestStructuredTurnRegistryQueuedThreadControlCompletesAfterPredecessorAndReplacement(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 36, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	registry.projectProviderHistoryWithIdentity(key, "session:old", nil, nil)
	mustAcceptStructuredInput(t, registry, key, "public-a", now.Format(time.RFC3339Nano), false)
	control, err := registry.acceptInputWithOptions(
		key,
		"",
		"control-new",
		now.Add(time.Second).Format(time.RFC3339Nano),
		true,
		true,
		"",
		func() error { return nil },
	)
	if err != nil || !control.Queued {
		t.Fatalf("queued control acceptance = %#v err %v", control, err)
	}

	providerStart := now.Format(time.RFC3339Nano)
	providerSettled := now.Add(2 * time.Second).Format(time.RFC3339Nano)
	turn, queued, _, _ := registry.projectProviderHistoryWithIdentity(
		key,
		"session:new",
		[]work.CodexConversationTurn{{
			ID: "provider-a", Status: work.CodexConversationTurnCompleted,
			StartedAt: providerStart, SettledAt: providerSettled,
		}},
		nil,
	)
	assertStructuredTurn(t, turn, "control-new", work.CodexConversationTurnCompleted, now.Add(time.Second).Format(time.RFC3339Nano))
	if len(queued) != 0 {
		t.Fatalf("completed queued control left queue: %#v", queued)
	}
}

func TestStructuredTurnRegistryRebasesEachSuccessiveQueuedThreadControl(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 36, 30, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	registry.projectProviderHistoryWithIdentity(key, "session:old", nil, nil)
	mustAcceptStructuredInput(t, registry, key, "public-a", now.Format(time.RFC3339Nano), false)
	for index, id := range []string{"control-one", "control-two"} {
		accepted, err := registry.acceptInputWithOptions(
			key,
			"work-agent",
			id,
			now.Add(time.Duration(index+1)*time.Second).Format(time.RFC3339Nano),
			true,
			true,
			"session:old",
			func() error { return nil },
		)
		if err != nil || !accepted.Queued {
			t.Fatalf("control %s acceptance = %#v err %v", id, accepted, err)
		}
	}

	turn, queued, _, _ := registry.projectProviderHistoryWithIdentity(
		key,
		"session:old",
		nil,
		&work.CodexConversationTurn{
			ID: "provider-a", Status: work.CodexConversationTurnCompleted,
			StartedAt: now.Format(time.RFC3339Nano),
			SettledAt: now.Add(3 * time.Second).Format(time.RFC3339Nano),
		},
	)
	assertStructuredTurn(t, turn, "control-one", work.CodexConversationTurnRunning, now.Add(time.Second).Format(time.RFC3339Nano))
	if len(queued) != 1 || queued[0].ID != "control-two" {
		t.Fatalf("first control promotion queue = %#v", queued)
	}

	turn, queued, _, _ = registry.projectProviderHistoryWithIdentity(key, "session:new", nil, nil)
	assertStructuredTurn(t, turn, "control-two", work.CodexConversationTurnRunning, now.Add(2*time.Second).Format(time.RFC3339Nano))
	if len(queued) != 0 {
		t.Fatalf("second control promotion queue = %#v", queued)
	}
	turn, _, _, _ = registry.projectProviderHistoryWithIdentity(key, "session:new", nil, nil)
	assertStructuredTurn(t, turn, "control-two", work.CodexConversationTurnRunning, now.Add(2*time.Second).Format(time.RFC3339Nano))

	turn, _, _, _ = registry.projectProviderHistoryWithIdentity(key, "session:newer", nil, nil)
	assertStructuredTurn(t, turn, "control-two", work.CodexConversationTurnCompleted, now.Add(2*time.Second).Format(time.RFC3339Nano))
}

func TestStructuredTurnRegistryProjectionVersionMatchesReturnedState(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 37, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	accepted, err := registry.acceptInput(
		key,
		"public-a",
		now.Format(time.RFC3339Nano),
		false,
		func() error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	turn, _, epoch, revision := registry.projectProviderHistoryWithIdentity(
		key,
		"session:a",
		nil,
		nil,
	)
	assertStructuredTurn(t, turn, "public-a", work.CodexConversationTurnRunning, now.Format(time.RFC3339Nano))
	if epoch != accepted.Epoch || revision != accepted.Revision {
		t.Fatalf("projection version = %q/%d, acceptance = %q/%d", epoch, revision, accepted.Epoch, accepted.Revision)
	}
}

func TestStructuredTurnRegistryRebasesQueuedControlAcrossBrainHostReplacement(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 38, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("brain-thread:current", "host-a")
	registry.projectProviderHistoryWithContext(key, "host-a", "session:old", nil, nil)
	mustAcceptStructuredInput(t, registry, key, "public-a", now.Format(time.RFC3339Nano), false)
	_, err := registry.acceptInputWithOptions(
		key,
		"",
		"control-new",
		now.Add(time.Second).Format(time.RFC3339Nano),
		true,
		true,
		"session:old",
		func() error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	turn, queued, _, _ := registry.projectProviderHistoryWithContext(
		key,
		"host-b",
		"session:host-b",
		nil,
		nil,
	)
	assertStructuredTurn(t, turn, "public-a", work.CodexConversationTurnRunning, now.Format(time.RFC3339Nano))
	if len(queued) != 1 || queued[0].ID != "control-new" {
		t.Fatalf("host replacement changed queued control: %#v", queued)
	}

	turn, queued, _, _ = registry.projectProviderHistoryWithContext(
		key,
		"host-b",
		"session:host-b",
		[]work.CodexConversationTurn{{
			ID: "provider-a", Status: work.CodexConversationTurnCompleted,
			StartedAt: now.Format(time.RFC3339Nano),
			SettledAt: now.Add(2 * time.Second).Format(time.RFC3339Nano),
		}},
		nil,
	)
	assertStructuredTurn(t, turn, "control-new", work.CodexConversationTurnRunning, now.Add(time.Second).Format(time.RFC3339Nano))
	if len(queued) != 0 {
		t.Fatalf("promoted control retained queue: %#v", queued)
	}

	turn, _, _, _ = registry.projectProviderHistoryWithContext(
		key,
		"host-b",
		"session:new",
		nil,
		nil,
	)
	assertStructuredTurn(t, turn, "control-new", work.CodexConversationTurnCompleted, now.Add(time.Second).Format(time.RFC3339Nano))
}

func TestStructuredTurnRegistryHydratesBrainHostBeforeQueuedControl(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 38, 30, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("brain-thread:current", "host-a")
	providerStarted := now.Add(-time.Second).Format(time.RFC3339Nano)
	registry.projectProviderHistoryWithContext(
		key,
		"host-a",
		"session:old",
		nil,
		&work.CodexConversationTurn{
			ID: "provider-predecessor", Status: work.CodexConversationTurnRunning,
			StartedAt: providerStarted,
		},
	)

	accepted, err := registry.acceptInputWithOptions(
		key,
		"host-a",
		"control-new",
		now.Format(time.RFC3339Nano),
		true,
		true,
		"session:old",
		func() error { return nil },
	)
	if err != nil || !accepted.Queued {
		t.Fatalf("early Brain control acceptance = %#v err %v", accepted, err)
	}

	turn, queued, _, _ := registry.projectProviderHistoryWithContext(
		key,
		"host-b",
		"session:host-b",
		nil,
		&work.CodexConversationTurn{
			ID: "provider-predecessor", Status: work.CodexConversationTurnRunning,
			StartedAt: providerStarted,
		},
	)
	assertStructuredTurn(t, turn, "provider-predecessor", work.CodexConversationTurnRunning, providerStarted)
	if len(queued) != 1 || queued[0].ID != "control-new" {
		t.Fatalf("first host-b projection prematurely settled queued control: %#v", queued)
	}

	turn, queued, _, _ = registry.projectProviderHistoryWithContext(
		key,
		"host-b",
		"session:host-b",
		nil,
		&work.CodexConversationTurn{
			ID: "provider-predecessor", Status: work.CodexConversationTurnCompleted,
			StartedAt: providerStarted,
			SettledAt: now.Add(time.Second).Format(time.RFC3339Nano),
		},
	)
	assertStructuredTurn(t, turn, "control-new", work.CodexConversationTurnRunning, now.Format(time.RFC3339Nano))
	if len(queued) != 0 {
		t.Fatalf("promoted early control retained queue: %#v", queued)
	}

	turn, _, _, _ = registry.projectProviderHistoryWithContext(
		key,
		"host-b",
		"session:after-control",
		nil,
		nil,
	)
	assertStructuredTurn(t, turn, "control-new", work.CodexConversationTurnCompleted, now.Format(time.RFC3339Nano))
}

func TestStructuredTurnRegistryAcceptanceDoesNotConsumePendingBrainHostRebase(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 38, 45, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("brain-thread:current", "host-a")
	registry.projectProviderHistoryWithContext(key, "host-a", "session:host-a", nil, nil)
	mustAcceptStructuredInput(t, registry, key, "public-a", now.Format(time.RFC3339Nano), false)
	control, err := registry.acceptInputWithOptions(
		key,
		"host-a",
		"control-new",
		now.Add(time.Second).Format(time.RFC3339Nano),
		true,
		true,
		"session:host-a",
		func() error { return nil },
	)
	if err != nil || !control.Queued {
		t.Fatalf("queued control acceptance = %#v err %v", control, err)
	}
	_, err = registry.acceptInputWithOptions(
		key,
		"host-b",
		"ordinary-on-host-b",
		now.Add(2*time.Second).Format(time.RFC3339Nano),
		true,
		false,
		"",
		func() error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	turn, queued, _, _ := registry.projectProviderHistoryWithContext(
		key,
		"host-b",
		"session:host-b",
		nil,
		nil,
	)
	assertStructuredTurn(t, turn, "public-a", work.CodexConversationTurnRunning, now.Format(time.RFC3339Nano))
	if len(queued) != 2 || queued[0].ID != "control-new" || queued[1].ID != "ordinary-on-host-b" {
		t.Fatalf("host-b projection prematurely consumed queue: %#v", queued)
	}

	turn, queued, _, _ = registry.projectProviderHistoryWithContext(
		key,
		"host-b",
		"session:host-b",
		nil,
		&work.CodexConversationTurn{
			ID: "provider-a", Status: work.CodexConversationTurnCompleted,
			StartedAt: now.Format(time.RFC3339Nano),
			SettledAt: now.Add(3 * time.Second).Format(time.RFC3339Nano),
		},
	)
	assertStructuredTurn(t, turn, "control-new", work.CodexConversationTurnRunning, now.Add(time.Second).Format(time.RFC3339Nano))
	if len(queued) != 1 || queued[0].ID != "ordinary-on-host-b" {
		t.Fatalf("promoted rebased control queue = %#v", queued)
	}
}

func TestStructuredTurnRegistryControlIgnoresIdentityMetadataEnrichment(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 39, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	registry.projectProviderHistoryWithIdentity(key, "path:/rollout/old.jsonl", nil, nil)
	_, err := registry.acceptInputWithOptions(
		key,
		"",
		"control-new",
		now.Format(time.RFC3339Nano),
		false,
		true,
		"path:/rollout/old.jsonl",
		func() error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	turn, _, _, _ := registry.projectProviderHistoryWithIdentity(
		key,
		"session:old",
		nil,
		nil,
	)
	assertStructuredTurn(t, turn, "control-new", work.CodexConversationTurnRunning, now.Format(time.RFC3339Nano))
	turn, _, _, _ = registry.projectProviderHistoryWithIdentity(
		key,
		"session:new",
		nil,
		nil,
	)
	assertStructuredTurn(t, turn, "control-new", work.CodexConversationTurnCompleted, now.Format(time.RFC3339Nano))
}

func TestStructuredTurnRegistryControlUsesClientBaselineBeforeFirstProjection(t *testing.T) {
	now := time.Date(2026, 7, 15, 16, 40, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	_, err := registry.acceptInputWithOptions(
		key,
		"",
		"control-new",
		now.Format(time.RFC3339Nano),
		false,
		true,
		"session:old",
		func() error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	turn, _, _, _ := registry.projectProviderHistoryWithIdentity(
		key,
		"session:new",
		nil,
		nil,
	)
	assertStructuredTurn(t, turn, "control-new", work.CodexConversationTurnCompleted, now.Format(time.RFC3339Nano))
}

func TestStructuredThreadControlInput(t *testing.T) {
	for _, input := range []string{"/new\n", " /clear ", "/NEW"} {
		if !isStructuredThreadControlInput(input) {
			t.Fatalf("thread control %q not recognized", input)
		}
	}
	for _, input := range []string{"hello", "/review", "/newer"} {
		if isStructuredThreadControlInput(input) {
			t.Fatalf("ordinary input %q recognized as thread control", input)
		}
	}
}

func TestStructuredTurnRegistryBrainScopeSurvivesHostReplacement(t *testing.T) {
	now := time.Date(2026, 7, 15, 17, 0, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	keyFromHostA := structuredTurnRegistryKey("brain-thread:thread-1", "host-a")
	keyFromHostB := structuredTurnRegistryKey("brain-thread:thread-1", "host-b")
	if keyFromHostA != keyFromHostB {
		t.Fatalf("Brain key changed across host replacement: %q != %q", keyFromHostA, keyFromHostB)
	}
	start := now.Format(time.RFC3339Nano)
	mustAcceptStructuredInput(t, registry, keyFromHostA, "brain-public", start, false)
	registry.project(keyFromHostA, &work.CodexConversationTurn{
		ID: "host-a-provider", Status: work.CodexConversationTurnRunning,
		StartedAt: now.Add(time.Second).Format(time.RFC3339Nano),
	})
	turn, _ := registry.project(keyFromHostB, &work.CodexConversationTurn{
		ID: "host-b-provider", Status: work.CodexConversationTurnRunning,
		StartedAt: now.Add(2 * time.Second).Format(time.RFC3339Nano),
	})
	assertStructuredTurn(t, turn, "brain-public", work.CodexConversationTurnRunning, start)
	if structuredTurnRegistryKey("", "host-a") == structuredTurnRegistryKey("", "host-b") {
		t.Fatal("ordinary Work agents must remain lifecycle-isolated")
	}
}

func TestStructuredTurnRegistryHydratedBrainTurnPreservesIdentityAcrossHostReplacement(t *testing.T) {
	now := time.Date(2026, 7, 15, 17, 30, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("brain-thread:thread-1", "host-a")
	providerAStart := now.Add(-time.Minute).Format(time.RFC3339Nano)

	turn, _, _, _ := registry.projectProviderHistoryWithContext(
		key,
		"host-a",
		"session:host-a",
		nil,
		&work.CodexConversationTurn{
			ID: "provider-host-a", Status: work.CodexConversationTurnRunning,
			StartedAt: providerAStart,
		},
	)
	assertStructuredTurn(t, turn, "provider-host-a", work.CodexConversationTurnRunning, providerAStart)

	replacementHistory := []work.CodexConversationTurn{{
		ID: "unrelated-host-b-history", Status: work.CodexConversationTurnCompleted,
		StartedAt: now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
		SettledAt: now.Add(-90 * time.Second).Format(time.RFC3339Nano),
	}}
	replacementRunning := &work.CodexConversationTurn{
		ID: "provider-host-b", Status: work.CodexConversationTurnRunning,
		StartedAt: now.Format(time.RFC3339Nano),
	}
	turn, _, _, replacementRevision := registry.projectProviderHistoryWithContext(
		key,
		"host-b",
		"session:host-b",
		replacementHistory,
		replacementRunning,
	)
	assertStructuredTurn(t, turn, "provider-host-a", work.CodexConversationTurnRunning, providerAStart)
	turn, _, _, repeatedRevision := registry.projectProviderHistoryWithContext(
		key,
		"host-b",
		"session:host-b",
		replacementHistory,
		replacementRunning,
	)
	assertStructuredTurn(t, turn, "provider-host-a", work.CodexConversationTurnRunning, providerAStart)
	if repeatedRevision != replacementRevision {
		t.Fatalf("repeated replacement history changed revision: %d -> %d", replacementRevision, repeatedRevision)
	}

	now = now.Add(time.Second)
	turn, _, _, _ = registry.projectProviderHistoryWithContext(
		key,
		"host-b",
		"session:host-b",
		nil,
		&work.CodexConversationTurn{
			ID: "provider-host-b", Status: work.CodexConversationTurnCompleted,
			StartedAt: now.Add(-time.Second).Format(time.RFC3339Nano),
			SettledAt: now.Format(time.RFC3339Nano),
		},
	)
	assertStructuredTurn(t, turn, "provider-host-a", work.CodexConversationTurnCompleted, providerAStart)
	if turn.SettledAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("replacement host settlement = %#v", turn)
	}
}

func TestStructuredTurnRegistryBrainHostReplacementConsumesDirectPromotionBeforeRebind(t *testing.T) {
	now := time.Date(2026, 7, 15, 17, 45, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("brain-thread:thread-1", "host-a")
	providerAStart := now.Add(-time.Minute).Format(time.RFC3339Nano)
	registry.projectProviderHistoryWithContext(
		key,
		"host-a",
		"session:host-a",
		nil,
		&work.CodexConversationTurn{
			ID: "provider-a", Status: work.CodexConversationTurnRunning,
			StartedAt: providerAStart,
		},
	)
	publicBStart := now.Format(time.RFC3339Nano)
	accepted, err := registry.acceptInputWithOptions(
		key,
		"host-a",
		"public-b",
		publicBStart,
		true,
		false,
		"",
		func() error { return nil },
	)
	if err != nil || !accepted.Queued {
		t.Fatalf("queued B acceptance = %#v err %v", accepted, err)
	}
	publicCStart := now.Add(500 * time.Millisecond).Format(time.RFC3339Nano)
	accepted, err = registry.acceptInputWithOptions(
		key,
		"host-a",
		"public-c",
		publicCStart,
		true,
		false,
		"",
		func() error { return nil },
	)
	if err != nil || !accepted.Queued {
		t.Fatalf("queued C acceptance = %#v err %v", accepted, err)
	}

	turn, queued, _, _ := registry.projectProviderHistoryWithContext(
		key,
		"host-b",
		"session:host-b",
		[]work.CodexConversationTurn{{
			ID: "unrelated-old-host-b", Status: work.CodexConversationTurnCompleted,
			StartedAt: now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
			SettledAt: now.Add(-90 * time.Second).Format(time.RFC3339Nano),
		}, {
			ID: "provider-a", Status: work.CodexConversationTurnCompleted,
			StartedAt: providerAStart,
			SettledAt: now.Add(time.Second).Format(time.RFC3339Nano),
		}, {
			ID: "provider-b", Status: work.CodexConversationTurnCompleted,
			StartedAt: now.Add(2 * time.Second).Format(time.RFC3339Nano),
			SettledAt: now.Add(3 * time.Second).Format(time.RFC3339Nano),
		}},
		&work.CodexConversationTurn{
			ID: "provider-c", Status: work.CodexConversationTurnRunning,
			StartedAt: now.Add(4 * time.Second).Format(time.RFC3339Nano),
		},
	)
	assertStructuredTurn(t, turn, "public-c", work.CodexConversationTurnRunning, publicCStart)
	if len(queued) != 0 {
		t.Fatalf("replacement host promotion left queue: %#v", queued)
	}

	now = now.Add(3 * time.Second)
	turn, _, _, _ = registry.projectProviderHistoryWithContext(
		key,
		"host-b",
		"session:host-b",
		nil,
		&work.CodexConversationTurn{
			ID: "provider-c", Status: work.CodexConversationTurnCompleted,
			StartedAt: now.Add(-time.Second).Format(time.RFC3339Nano),
			SettledAt: now.Format(time.RFC3339Nano),
		},
	)
	assertStructuredTurn(t, turn, "public-c", work.CodexConversationTurnCompleted, publicCStart)
}

func TestServerHistoricalBrainScopeNeverInheritsLiveTurn(t *testing.T) {
	now := time.Date(2026, 7, 15, 18, 0, 0, 0, time.UTC)
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "current", UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	srv := &Server{brain: brain.NewService(store, nil, nil), structuredTurns: newStructuredTurnRegistry()}
	provider := work.CodexConversationTurn{
		ID: "live-provider", Status: work.CodexConversationTurnRunning, StartedAt: now.Format(time.RFC3339Nano),
	}

	current := srv.projectStructuredConversationTurns("brain-thread:current", "host", work.CodexConversation{Turn: &provider})
	assertStructuredTurn(t, current.Turn, "live-provider", work.CodexConversationTurnRunning, provider.StartedAt)
	historicalKey := structuredTurnRegistryKey("brain-thread:historical", "host")
	historicalStart := now.Add(-time.Minute).Format(time.RFC3339Nano)
	mustAcceptStructuredInput(t, srv.turnRegistry(), historicalKey, "cached-historical", historicalStart, false)
	mustAcceptStructuredInput(t, srv.turnRegistry(), historicalKey, "cached-queued", now.Format(time.RFC3339Nano), true)
	historical := srv.projectStructuredConversationTurns("brain-thread:historical", "host", work.CodexConversation{Turn: &provider})
	if historical.Active == nil || *historical.Active {
		t.Fatalf("historical projection inherited live lifecycle: %#v", historical)
	}
	assertStructuredTurn(t, historical.Turn, "cached-historical", work.CodexConversationTurnCancelled, historicalStart)
	if len(historical.QueuedTurns) != 0 || historical.TurnEpoch == "" || historical.TurnRevision < 3 {
		t.Fatalf("historical lifecycle clear lacks causal metadata: %#v", historical)
	}
	if turn, queued := srv.turnRegistry().snapshot(historicalKey); turn == nil || turn.Status != work.CodexConversationTurnCancelled || len(queued) != 0 {
		t.Fatalf("historical lifecycle was not authoritatively cancelled: turn=%#v queued=%#v", turn, queued)
	}
}

func TestServerStructuredStopRefreshRejectsStaleTargetBeforeDispatch(t *testing.T) {
	now := time.Date(2026, 7, 15, 18, 20, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	key := structuredTurnRegistryKey("", "work-agent")
	mustAcceptStructuredInput(t, registry, key, "public-a", now.Format(time.RFC3339Nano), false)
	mustAcceptStructuredInput(t, registry, key, "public-b", now.Add(time.Second).Format(time.RFC3339Nano), true)

	loads := 0
	srv := &Server{
		structuredTurns: registry,
		structuredSnapshotLoader: func(agentID string) (work.CodexConversation, error) {
			loads++
			if agentID != "work-agent" {
				t.Fatalf("snapshot agent = %q", agentID)
			}
			return work.CodexConversation{
				ProviderTurns: []work.CodexConversationTurn{{
					ID: "provider-a", Status: work.CodexConversationTurnCompleted,
					StartedAt: now.Format(time.RFC3339Nano),
					SettledAt: now.Add(2 * time.Second).Format(time.RFC3339Nano),
				}},
				Turn: &work.CodexConversationTurn{
					ID: "provider-b", Status: work.CodexConversationTurnRunning,
					StartedAt: now.Add(3 * time.Second).Format(time.RFC3339Nano),
				},
			}, nil
		},
	}
	dispatches := 0
	accepted, err := srv.interruptStructuredTurn(clientMessage{
		AgentID: "work-agent",
		TurnID:  "public-a",
		Action:  "pause",
	}, func() error {
		dispatches++
		return nil
	})
	if err != nil || accepted || dispatches != 0 || loads != 1 {
		t.Fatalf("stale Stop = accepted %t loads %d dispatches %d err %v", accepted, loads, dispatches, err)
	}
	turn, queued := registry.snapshot(key)
	assertStructuredTurn(t, turn, "public-b", work.CodexConversationTurnRunning, now.Add(time.Second).Format(time.RFC3339Nano))
	if len(queued) != 0 {
		t.Fatalf("provider promotion left queue: %#v", queued)
	}
}

func TestServerStructuredQueuedSendRefreshRejectsMissingPredecessorBeforeDispatch(t *testing.T) {
	registry := newStructuredTurnRegistry()
	loads := 0
	srv := &Server{
		structuredTurns: registry,
		structuredSnapshotLoader: func(agentID string) (work.CodexConversation, error) {
			loads++
			return work.CodexConversation{Available: false, Reason: "transcript_not_found"}, nil
		},
	}
	dispatches := 0
	_, err := srv.acceptStructuredInput(clientMessage{
		AgentID:    "work-agent",
		Text:       "queued input\n",
		TurnID:     "public-queued",
		TurnQueued: true,
	}, func() error {
		dispatches++
		return nil
	})
	if !errors.Is(err, errStructuredLifecycleSyncing) || loads != 1 || dispatches != 0 {
		t.Fatalf("ambiguous queued send = loads %d dispatches %d err %v", loads, dispatches, err)
	}
	turn, queued := registry.snapshot(structuredTurnRegistryKey("", "work-agent"))
	if turn != nil || len(queued) != 0 {
		t.Fatalf("ambiguous queued send mutated lifecycle: turn=%#v queued=%#v", turn, queued)
	}
}

func TestServerAcceptedDuplicateSkipsFallibleBaselineRefresh(t *testing.T) {
	registry := newStructuredTurnRegistry()
	key := structuredTurnRegistryKey("", "work-agent")
	accepted, err := registry.acceptInput(
		key,
		"turn-accepted",
		"2026-07-15T12:30:00Z",
		false,
		func() error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	loads := 0
	dispatches := 0
	srv := &Server{
		structuredTurns: registry,
		structuredSnapshotLoader: func(string) (work.CodexConversation, error) {
			loads++
			return work.CodexConversation{}, errors.New("snapshot temporarily unavailable")
		},
	}
	replayed, err := srv.acceptStructuredInput(clientMessage{
		AgentID: "work-agent",
		TurnID:  "turn-accepted",
		Text:    "same input\n",
	}, func() error {
		dispatches++
		return nil
	})
	if err != nil || !replayed.Duplicate || replayed.TurnID != accepted.TurnID {
		t.Fatalf("duplicate replay = %#v err %v", replayed, err)
	}
	if loads != 0 || dispatches != 0 {
		t.Fatalf("duplicate replay loaded %d snapshots and dispatched %d times", loads, dispatches)
	}
}

func TestServerPredispatchRejectionRetriesSameTurnIDExactlyOnce(t *testing.T) {
	registry := newStructuredTurnRegistry()
	loads := 0
	dispatches := 0
	srv := &Server{
		structuredTurns: registry,
		structuredSnapshotLoader: func(string) (work.CodexConversation, error) {
			loads++
			if loads == 1 {
				return work.CodexConversation{}, errors.New("snapshot temporarily unavailable")
			}
			return work.CodexConversation{Available: true}, nil
		},
	}
	raw := clientMessage{
		AgentID:       "work-agent",
		TurnID:        "turn-retry",
		TurnStartedAt: json.RawMessage(`"2026-07-15T12:30:00Z"`),
		Text:          "retry me\n",
	}
	if _, err := srv.acceptStructuredInput(raw, func() error {
		dispatches++
		return nil
	}); err == nil {
		t.Fatal("first pre-dispatch refresh unexpectedly succeeded")
	}
	accepted, err := srv.acceptStructuredInput(raw, func() error {
		dispatches++
		return nil
	})
	if err != nil || accepted.Duplicate {
		t.Fatalf("retry acceptance = %#v err %v", accepted, err)
	}
	replayed, err := srv.acceptStructuredInput(raw, func() error {
		dispatches++
		return nil
	})
	if err != nil || !replayed.Duplicate {
		t.Fatalf("accepted replay = %#v err %v", replayed, err)
	}
	if loads != 2 || dispatches != 1 {
		t.Fatalf("loads = %d dispatches = %d, want two refresh attempts and one delivery", loads, dispatches)
	}
}

func TestServerPostDispatchErrorIsDeliveryUnconfirmed(t *testing.T) {
	registry := newStructuredTurnRegistry()
	loads := 0
	srv := &Server{
		structuredTurns: registry,
		structuredSnapshotLoader: func(string) (work.CodexConversation, error) {
			loads++
			return work.CodexConversation{Available: true}, nil
		},
	}
	dispatches := 0
	_, err := srv.acceptStructuredInput(clientMessage{
		AgentID: "work-agent",
		TurnID:  "turn-ambiguous",
		Text:    "possibly delivered\n",
	}, func() error {
		dispatches++
		return errors.New("Enter acknowledgement lost")
	})
	var unconfirmed *structuredInputDeliveryUnconfirmedError
	if !errors.As(err, &unconfirmed) || dispatches != 1 {
		t.Fatalf("post-dispatch result = %T %v, dispatches %d", err, err, dispatches)
	}
	turn, queued := registry.snapshot(structuredTurnRegistryKey("", "work-agent"))
	if turn != nil || len(queued) != 0 {
		t.Fatalf("unconfirmed dispatch invented accepted lifecycle: turn=%#v queue=%#v", turn, queued)
	}
	_, replayErr := srv.acceptStructuredInput(clientMessage{
		AgentID: "work-agent",
		TurnID:  "turn-ambiguous",
		Text:    "possibly delivered\n",
	}, func() error {
		dispatches++
		return nil
	})
	if !errors.As(replayErr, &unconfirmed) || loads != 1 || dispatches != 1 {
		t.Fatalf("unconfirmed replay = %T %v, loads %d dispatches %d", replayErr, replayErr, loads, dispatches)
	}
}

func TestStructuredInputFailureResponsesSeparateRejectionFromUncertainty(t *testing.T) {
	raw := clientMessage{
		RequestID: "request-1",
		AgentID:   "work-agent",
		TurnID:    "turn-1",
	}
	rejected := structuredInputFailureResponse(
		raw,
		&structuredInputRejectedError{
			cause:     fmt.Errorf("%w: provider baseline unavailable", errStructuredLifecycleSyncing),
			code:      "structured_lifecycle_syncing",
			retryable: true,
		},
	)
	if rejected["type"] != "input_rejected" ||
		rejected["request_id"] != "request-1" ||
		rejected["turn_id"] != "turn-1" ||
		rejected["code"] != "structured_lifecycle_syncing" ||
		rejected["retryable"] != true {
		t.Fatalf("pre-dispatch response = %#v", rejected)
	}

	unconfirmed := structuredInputFailureResponse(raw, &structuredInputDeliveryUnconfirmedError{
		cause: errors.New("terminal response lost"),
	})
	if unconfirmed["type"] != "input_unconfirmed" ||
		unconfirmed["request_id"] != "request-1" ||
		unconfirmed["turn_id"] != "turn-1" ||
		unconfirmed["code"] != "send_input_unconfirmed" {
		t.Fatalf("post-dispatch response = %#v", unconfirmed)
	}
	if _, ok := unconfirmed["retryable"]; ok {
		t.Fatalf("unconfirmed delivery advertised retry: %#v", unconfirmed)
	}
	unknown := structuredInputFailureResponse(raw, errors.New("unexpected internal error"))
	if unknown["type"] != "input_unconfirmed" {
		t.Fatalf("unknown error was not conservative: %#v", unknown)
	}
}

func TestServerProjectsAcceptedTurnBeforeFirstProviderToken(t *testing.T) {
	now := time.Date(2026, 7, 15, 18, 30, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	srv := &Server{structuredTurns: registry}
	key := structuredTurnRegistryKey("", "work-agent")
	start := now.Add(-time.Second).Format(time.RFC3339Nano)
	mustAcceptStructuredInput(t, registry, key, "public-pre-token", start, false)

	conversation := srv.projectStructuredConversationTurns(
		"",
		"work-agent",
		work.CodexConversation{Available: true, Events: []work.CodexConversationEvent{}},
	)
	assertStructuredTurn(t, conversation.Turn, "public-pre-token", work.CodexConversationTurnRunning, start)
	if conversation.Active == nil || !*conversation.Active {
		t.Fatalf("pre-token conversation active = %#v", conversation.Active)
	}
	if conversation.QueuedTurns == nil || len(conversation.QueuedTurns) != 0 {
		t.Fatalf("empty queue must be explicit: %#v", conversation.QueuedTurns)
	}
}

func TestServerAgentRemovalFailsWorkTurnWithoutSettlingBrainHostReplacement(t *testing.T) {
	now := time.Date(2026, 7, 15, 18, 45, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	srv := &Server{structuredTurns: registry}

	workKey := structuredTurnRegistryKey("", "shared-host")
	mustAcceptStructuredInput(t, registry, workKey, "work-current", now.Format(time.RFC3339Nano), false)
	mustAcceptStructuredInput(t, registry, workKey, "work-queued", now.Add(time.Second).Format(time.RFC3339Nano), true)

	brainKey := structuredTurnRegistryKey("brain-thread:current", "shared-host")
	mustAcceptStructuredInput(t, registry, brainKey, "brain-current", now.Format(time.RFC3339Nano), false)
	registry.projectProviderHistoryWithContext(brainKey, "shared-host", "session:brain", nil, nil)

	srv.handleWatcherEvent(watcher.SessionEvent{
		Type:    "agent_removed",
		AgentID: "shared-host",
	})

	workTurn, workQueue, _, workRevision := registry.projectProviderHistoryVersioned(workKey, nil, nil)
	assertStructuredTurn(t, workTurn, "work-current", work.CodexConversationTurnFailed, now.Format(time.RFC3339Nano))
	if len(workQueue) != 0 {
		t.Fatalf("removed Work executor retained queue: %#v", workQueue)
	}
	if workRevision < 3 {
		t.Fatalf("Work removal did not advance lifecycle revision: %d", workRevision)
	}

	lateTurn, _ := registry.project(workKey, &work.CodexConversationTurn{
		ID:     "late-fallback-provider",
		Status: work.CodexConversationTurnRunning,
	})
	assertStructuredTurn(t, lateTurn, "work-current", work.CodexConversationTurnFailed, now.Format(time.RFC3339Nano))

	brainTurn, brainQueue, _, _ := registry.projectProviderHistoryVersioned(brainKey, nil, nil)
	assertStructuredTurn(t, brainTurn, "brain-current", work.CodexConversationTurnRunning, now.Format(time.RFC3339Nano))
	if len(brainQueue) != 0 {
		t.Fatalf("unexpected Brain queue mutation: %#v", brainQueue)
	}
}

func TestStructuredTurnRegistryRemovalTombstonesEmptyWorkScope(t *testing.T) {
	now := time.Date(2026, 7, 15, 18, 50, 0, 0, time.UTC)
	for _, existingScope := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "empty"}[existingScope], func(t *testing.T) {
			registry := newStructuredTurnRegistry()
			key := structuredTurnRegistryKey("", "removed-agent")
			if existingScope {
				registry.projectProviderHistoryVersioned(key, nil, nil)
			}
			if !registry.failWorkAgent("removed-agent", now) {
				t.Fatal("first removal did not create a tombstone")
			}
			turn, queued := registry.project(key, &work.CodexConversationTurn{
				ID: "late-running", Status: work.CodexConversationTurnRunning,
				StartedAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
			})
			if turn != nil || len(queued) != 0 {
				t.Fatalf("late fallback reopened removed executor: turn=%#v queue=%#v", turn, queued)
			}

			registry.markWorkAgentPresent("removed-agent")
			turn, _ = registry.project(key, &work.CodexConversationTurn{
				ID: "rediscovered-running", Status: work.CodexConversationTurnRunning,
				StartedAt: now.Format(time.RFC3339Nano),
			})
			assertStructuredTurn(
				t,
				turn,
				"rediscovered-running",
				work.CodexConversationTurnRunning,
				now.Format(time.RFC3339Nano),
			)
		})
	}
}

func TestServerUntrustedFallbackFactsCannotOwnLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 15, 18, 55, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	srv := &Server{structuredTurns: registry}
	key := structuredTurnRegistryKey("", "work-agent")
	stale := work.CodexConversation{
		Available: true,
		SessionID: "stale-session",
		Turn: &work.CodexConversationTurn{
			ID: "stale-provider", Status: work.CodexConversationTurnRunning,
			StartedAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
		},
		Events: []work.CodexConversationEvent{{
			ID: "visible-history", Seq: 1, Kind: "assistant_message", Body: "History remains visible",
		}},
	}

	untrusted := srv.projectStructuredConversationTurnsWithTrust("", "work-agent", stale, false)
	if untrusted.Turn != nil || untrusted.Active == nil || *untrusted.Active {
		t.Fatalf("untrusted running fact became lifecycle: %#v", untrusted)
	}
	if len(untrusted.Events) != 1 || untrusted.Events[0].ID != "visible-history" {
		t.Fatalf("untrusted rendering history was lost: %#v", untrusted.Events)
	}

	trusted := srv.projectStructuredConversationTurnsWithTrust("", "work-agent", stale, true)
	assertStructuredTurn(t, trusted.Turn, "stale-provider", work.CodexConversationTurnRunning, now.Add(-time.Minute).Format(time.RFC3339Nano))

	mustAcceptStructuredInput(t, registry, key, "public-next", now.Format(time.RFC3339Nano), true)
	terminalFallback := stale
	terminalFallback.SessionID = "replacement-session"
	terminalFallback.Turn = &work.CodexConversationTurn{
		ID: "stale-provider", Status: work.CodexConversationTurnCompleted,
		StartedAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
		SettledAt: now.Add(time.Second).Format(time.RFC3339Nano),
	}
	untrusted = srv.projectStructuredConversationTurnsWithTrust("", "work-agent", terminalFallback, false)
	if untrusted.Turn == nil || untrusted.Turn.ID != "stale-provider" || untrusted.Turn.Status != work.CodexConversationTurnRunning {
		t.Fatalf("untrusted terminal settled lifecycle: %#v", untrusted.Turn)
	}
	if len(untrusted.QueuedTurns) != 1 || untrusted.QueuedTurns[0].ID != "public-next" {
		t.Fatalf("untrusted projection changed queue: %#v", untrusted.QueuedTurns)
	}
}

func TestServerUntrustedFallbackIdentityCannotSettleThreadControl(t *testing.T) {
	now := time.Date(2026, 7, 15, 18, 56, 0, 0, time.UTC)
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	srv := &Server{structuredTurns: registry}
	key := structuredTurnRegistryKey("brain-thread:current", "host-a")
	registry.projectProviderHistoryWithContext(key, "host-a", "session:old", nil, nil)
	_, err := registry.acceptInputWithOptions(
		key,
		"host-a",
		"control-new",
		now.Format(time.RFC3339Nano),
		false,
		true,
		"session:old",
		func() error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	untrusted := srv.projectStructuredConversationTurnsWithTrust(
		"brain-thread:current",
		"host-b",
		work.CodexConversation{Available: true, SessionID: "new", Events: []work.CodexConversationEvent{}},
		false,
	)
	assertStructuredTurn(t, untrusted.Turn, "control-new", work.CodexConversationTurnRunning, now.Format(time.RFC3339Nano))
	registry.mu.Lock()
	if got := registry.byScope[key].agentID; got != "host-a" {
		registry.mu.Unlock()
		t.Fatalf("untrusted fallback consumed host replacement: %q", got)
	}
	registry.mu.Unlock()

	trusted := srv.projectStructuredConversationTurnsWithTrust(
		"brain-thread:current",
		"host-b",
		work.CodexConversation{Available: true, SessionID: "new", Events: []work.CodexConversationEvent{}},
		true,
	)
	// The first trusted observation from a replacement Brain host rebases the
	// accepted control to that host's current provider identity. Host process
	// replacement is not itself proof that /new completed.
	assertStructuredTurn(t, trusted.Turn, "control-new", work.CodexConversationTurnRunning, now.Format(time.RFC3339Nano))

	trusted = srv.projectStructuredConversationTurnsWithTrust(
		"brain-thread:current",
		"host-b",
		work.CodexConversation{Available: true, SessionID: "newer", Events: []work.CodexConversationEvent{}},
		true,
	)
	assertStructuredTurn(t, trusted.Turn, "control-new", work.CodexConversationTurnCompleted, now.Format(time.RFC3339Nano))
}

func TestServerBrainThreadReadErrorPreservesScopedLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 15, 18, 58, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := brain.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{ThreadID: "current", UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	registry := newStructuredTurnRegistry()
	registry.now = func() time.Time { return now }
	srv := &Server{
		brain:           brain.NewService(store, nil, nil),
		structuredTurns: registry,
	}
	key := structuredTurnRegistryKey("brain-thread:current", "host")
	startedAt := now.Format(time.RFC3339Nano)
	mustAcceptStructuredInput(t, registry, key, "brain-public", startedAt, false)
	initial := srv.projectStructuredConversationTurns(
		"brain-thread:current",
		"host",
		work.CodexConversation{Available: true, Events: []work.CodexConversationEvent{}},
	)
	assertStructuredTurn(t, initial.Turn, "brain-public", work.CodexConversationTurnRunning, startedAt)

	statePath := store.ChatStatePath()
	backupPath := statePath + ".backup"
	if err := os.Rename(statePath, backupPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	duringError := srv.projectStructuredConversationTurns(
		"brain-thread:current",
		"host",
		work.CodexConversation{Available: true, Events: []work.CodexConversationEvent{}},
	)
	assertStructuredTurn(t, duringError.Turn, "brain-public", work.CodexConversationTurnRunning, startedAt)
	if duringError.TurnEpoch != initial.TurnEpoch || duringError.TurnRevision != initial.TurnRevision {
		t.Fatalf("thread read error changed lifecycle version: before=%s/%d during=%s/%d", initial.TurnEpoch, initial.TurnRevision, duringError.TurnEpoch, duringError.TurnRevision)
	}

	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backupPath, statePath); err != nil {
		t.Fatal(err)
	}
	recovered := srv.projectStructuredConversationTurns(
		"brain-thread:current",
		"host",
		work.CodexConversation{Available: true, Events: []work.CodexConversationEvent{}},
	)
	assertStructuredTurn(t, recovered.Turn, "brain-public", work.CodexConversationTurnRunning, startedAt)
	if recovered.TurnRevision != initial.TurnRevision {
		t.Fatalf("thread read recovery changed revision: before=%d after=%d", initial.TurnRevision, recovered.TurnRevision)
	}
}

func TestConversationFingerprintIncludesTurnAndQueue(t *testing.T) {
	start := time.Date(2026, 7, 15, 19, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	base := work.CodexConversation{Available: true, SessionID: "session", Events: []work.CodexConversationEvent{}}
	running := base
	running.Turn = &work.CodexConversationTurn{ID: "turn", Status: work.CodexConversationTurnRunning, StartedAt: start}
	if codexConversationSubscriptionFingerprint(base) == codexConversationSubscriptionFingerprint(running) {
		t.Fatal("turn-only update must change subscription fingerprint")
	}
	queued := running
	queued.QueuedTurns = []work.CodexConversationTurn{{ID: "next", Status: work.CodexConversationTurnQueued, StartedAt: start}}
	if codexConversationSubscriptionFingerprint(running) == codexConversationSubscriptionFingerprint(queued) {
		t.Fatal("queue-only update must change subscription fingerprint")
	}
	terminal := running
	terminal.Turn = &work.CodexConversationTurn{ID: "turn", Status: work.CodexConversationTurnCompleted, StartedAt: start, SettledAt: start}
	if codexConversationSubscriptionFingerprint(running) == codexConversationSubscriptionFingerprint(terminal) {
		t.Fatal("terminal lifecycle update must change subscription fingerprint")
	}
}

func TestConversationSnapshotExplicitlyClearsQueuedTurns(t *testing.T) {
	payload, err := json.Marshal(work.CodexConversation{
		Available:   true,
		QueuedTurns: []work.CodexConversationTurn{},
		Events:      []work.CodexConversationEvent{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"queued_turns":[]`) {
		t.Fatalf("snapshot cannot clear stale queue: %s", payload)
	}
	if strings.Contains(string(payload), "ProviderTurns") || strings.Contains(string(payload), "provider_turns") {
		t.Fatalf("internal provider history leaked onto wire: %s", payload)
	}
}

func mustAcceptStructuredInput(
	t *testing.T,
	registry *structuredTurnRegistry,
	key string,
	turnID string,
	startedAt string,
	queued bool,
) {
	t.Helper()
	if _, err := registry.acceptInput(key, turnID, startedAt, queued, func() error { return nil }); err != nil {
		t.Fatalf("accept %s: %v", turnID, err)
	}
}

func assertStructuredTurn(t *testing.T, got *work.CodexConversationTurn, id, status, startedAt string) {
	t.Helper()
	if got == nil || got.ID != id || got.Status != status || got.StartedAt != startedAt {
		t.Fatalf("turn = %#v, want id=%q status=%q started_at=%q", got, id, status, startedAt)
	}
}
