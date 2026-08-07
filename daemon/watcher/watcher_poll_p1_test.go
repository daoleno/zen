package watcher

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// TestPollDeadPaneWithReplacedIdentityCannotFailRecordedTurn covers P1.5:
// a non-zero exit from a replaced or unreadable pane lifetime can never
// produce a canonical Failed fact — it resolves to end-of-identity Unknown.
func TestPollDeadPaneWithReplacedIdentityCannotFailRecordedTurn(t *testing.T) {
	for _, test := range []struct {
		name          string
		recordedGen   string
		currentGen    string
		deadStatus    int
		wantFactKinds []string
	}{
		{
			name:          "replaced pane with nonzero exit",
			recordedGen:   "recorded-pane",
			currentGen:    "replacement-pane",
			deadStatus:    1,
			wantFactKinds: []string{"uncertain"},
		},
		{
			name:          "unreadable pane identity with nonzero exit",
			recordedGen:   "recorded-pane",
			currentGen:    "",
			deadStatus:    1,
			wantFactKinds: []string{"uncertain"},
		},
		{
			name:          "missing recorded identity with nonzero exit",
			recordedGen:   "",
			currentGen:    "",
			deadStatus:    1,
			wantFactKinds: []string{"uncertain"},
		},
		{
			name:          "recorded pane matched with nonzero exit is abnormal",
			recordedGen:   "recorded-pane",
			currentGen:    "recorded-pane",
			deadStatus:    1,
			wantFactKinds: []string{"failed"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			io := newFakeSessionInputIO()
			io.paneValue = sessionInputPane{alive: false, paneID: "%9", generation: test.currentGen}
			ledger := newFakeTurnLedger()
			ledger.seed("agent:@1", TurnSnapshot{
				SessionID:       "agent:@1",
				TurnID:          "agent:@1:turn:1",
				Status:          TurnRunning,
				AcceptedAt:      time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC),
				PaneGeneration:  test.recordedGen,
				ProcessIdentity: "recorded-proc",
			})
			w := New(time.Second)
			owner := newSessionInputOwner(io)
			owner.ledger = ledger
			w.sessionInput = owner
			w.turnLedger = ledger

			turn, _, _ := ledger.Turn("agent:@1")
			turn = w.applyPollFacts("agent:@1", false, test.deadStatus, time.Now().UTC(), turn, ProviderActivityObservation{})

			kinds := map[string]bool{}
			for _, fact := range ledger.applied {
				kinds[fact.Kind] = true
			}
			for _, want := range test.wantFactKinds {
				if !kinds[want] {
					t.Fatalf("applied facts = %#v, want kind %q", ledger.applied, want)
				}
			}
			if kinds["failed"] != (test.deadStatus != 0 && test.recordedGen != "" && test.recordedGen == test.currentGen) {
				t.Fatalf("failed fact presence = %v, applied = %#v", kinds["failed"], ledger.applied)
			}
			// The fake ledger resolves every liveness fact to Unknown; the
			// point of this test is the fact choice (never a Failed fact for
			// replaced/unreadable identities), which is asserted above.
			if turn.Status != TurnUnknown {
				t.Fatalf("liveness resolution = %+v, want Unknown", turn)
			}
		})
	}
}

// TestPollRemovedTurnReconciliationAppliesUncertain covers the live removal
// path: a window absent from a successful inventory with a nonterminal
// canonical turn resolves end-of-identity uncertainty, never Failed.
func TestPollRemovedTurnReconciliationAppliesUncertain(t *testing.T) {
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
	})
	ledger := newFakeTurnLedger()
	ledger.seed("brain-agent-worker:@1", TurnSnapshot{
		SessionID:       "brain-agent-worker:@1",
		TurnID:          "brain-agent-worker:@1:turn:1",
		Status:          TurnRunning,
		AcceptedAt:      time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC),
		ProcessIdentity: "recorded-proc",
	})
	w.turnLedger = ledger
	windows := []tmuxWindow{
		{target: "brain-agent-worker:@1", name: "worker", cwd: "/repo/zen", command: "opencode", panePID: 333, delegated: true},
	}
	restore := installFakePollSeams(windows, map[string]string{
		"brain-agent-worker:@1": "OpenCode\nworking\n",
	}, map[int]processInfo{
		333: fakeProcess(333, time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)),
	})
	defer restore()

	w.poll()
	drainWatcherEvents(w)
	if agent := agentByID(w.Agents(), "brain-agent-worker:@1"); agent == nil || agent.State != classifier.StateRunning {
		t.Fatalf("session before removal = %#v", agent)
	}

	// The window disappears from a successful inventory.
	restore()
	restore = installFakePollSeams(nil, map[string]string{}, map[int]processInfo{})
	defer restore()
	w.poll()
	drainWatcherEvents(w)

	kinds := map[string]bool{}
	for _, fact := range ledger.applied {
		kinds[fact.Kind] = true
	}
	if !kinds["uncertain"] {
		t.Fatalf("removal reconciliation facts = %#v, want uncertain", ledger.applied)
	}
	if kinds["failed"] {
		t.Fatalf("removal reconciliation produced failed: %#v", ledger.applied)
	}
	if agent := agentByID(w.Agents(), "brain-agent-worker:@1"); agent != nil {
		t.Fatalf("session survived removal: %#v", agent)
	}
}

// TestPollPiBlockedPaneNeverWakesTurnTrackedSession covers the Pi incident
// (A.4.4): a live canonical turn with a pane ending in `?` classifies
// Blocked, but the canonical projection keeps the Session Running and no
// needs_input wake is produced at poll level.
func TestPollPiBlockedPaneNeverWakesTurnTrackedSession(t *testing.T) {
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 3, 0, time.UTC),
	})
	ledger := newFakeTurnLedger()
	ledger.seed("brain-agent-pi:@1", TurnSnapshot{
		SessionID:       "brain-agent-pi:@1",
		TurnID:          "brain-agent-pi:@1:turn:1",
		Status:          TurnRunning,
		AcceptedAt:      time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC),
		ProcessIdentity: "recorded-proc",
	})
	w.turnLedger = ledger
	// The exact Pi frame truncated at pane width: the last line ends in `?`,
	// which the pane classifier reads as blocked.
	content := "┃  ⠼ bun test 2>&1 | grep -E \"(fail)|✗|FAIL\" | head; echo \"exit: $?"
	if classified, _ := classifier.Classify(true, []string{content}, "pi"); classified != classifier.StateBlocked {
		t.Fatalf("fixture classification = %s, want blocked (the Pi frame)", classified)
	}
	windows := []tmuxWindow{
		{target: "brain-agent-pi:@1", name: "pi", cwd: "/repo/zen", command: "pi", panePID: 444, delegated: true},
	}
	restore := installFakePollSeams(windows, map[string]string{
		"brain-agent-pi:@1": content,
	}, map[int]processInfo{
		444: fakeProcess(444, time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)),
	})
	defer restore()

	w.poll()
	drainWatcherEvents(w)
	agent := agentByID(w.Agents(), "brain-agent-pi:@1")
	if agent == nil || agent.State != classifier.StateRunning {
		t.Fatalf("Pi session projection = %#v, want running (never blocked)", agent)
	}
	if agent.NeedsAttention || agent.Attention == "user_input" {
		t.Fatalf("Pi session gained attention from pane: %#v", agent)
	}
	// A repeated poll with the same blocked-looking frame stays running.
	w.poll()
	drainWatcherEvents(w)
	agent = agentByID(w.Agents(), "brain-agent-pi:@1")
	if agent == nil || agent.State != classifier.StateRunning {
		t.Fatalf("Pi session after repeated poll = %#v, want running", agent)
	}
}
