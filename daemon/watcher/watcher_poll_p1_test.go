package watcher

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestObservePanesCapturesImmutableObservation(t *testing.T) {
	windows := []tmuxWindow{{target: "a"}, {target: "b"}}
	got := observePanes(windows, func(target string) (string, bool, int) {
		if target == "a" {
			return "one\ntwo", true, 0
		}
		return "dead", false, 17
	})
	if len(got) != 2 || got[0].win.target != "a" || !got[0].alive || len(got[0].lines) != 2 {
		t.Fatalf("unexpected first observation: %+v", got)
	}
	if got[1].alive || got[1].deadStatus != 17 || len(got[1].lines) != 1 {
		t.Fatalf("unexpected second observation: %+v", got)
	}
}

// TestPollDeadPaneNeverFailsWithoutExactAttribution covers the Round-4
// rule: liveness-derived terminal attribution is removed. A dead pane with a
// non-zero exit never produces a canonical Failed fact, regardless of how
// pane/process evidence looks — wrapper/shell panes can propagate a replaced
// child's status, snapshots may be nil/empty/unreadable (a missing PID
// proves nothing), and dead-pane identity reads fail closed. Every scenario
// resolves end-of-identity Unknown + session.uncertain, exactly once.
func TestPollDeadPaneNeverFailsWithoutExactAttribution(t *testing.T) {
	tests := []struct {
		name              string
		recordedGen       string
		currentGen        string
		deadStatus        int
		recordedPanePID   int
		recordedProcID    int
		recordedProcStart int64
		panePID           int
		processes         map[int]processInfo
	}{
		{
			name:              "replaced pane with nonzero exit",
			recordedGen:       "recorded-pane",
			currentGen:        "replacement-pane",
			deadStatus:        1,
			recordedPanePID:   100,
			recordedProcID:    200,
			recordedProcStart: 1700000000000000000,
			panePID:           100,
		},
		{
			name:              "unreadable pane identity with nonzero exit",
			recordedGen:       "recorded-pane",
			currentGen:        "",
			deadStatus:        1,
			recordedPanePID:   100,
			recordedProcID:    200,
			recordedProcStart: 1700000000000000000,
			panePID:           100,
		},
		{
			name:       "missing recorded identity with nonzero exit",
			deadStatus: 1,
			panePID:    100,
		},
		{
			name:              "wrapper pane root not the provider process",
			recordedGen:       "recorded-pane",
			currentGen:        "recorded-pane",
			deadStatus:        1,
			recordedPanePID:   100,
			recordedProcID:    200,
			recordedProcStart: 1700000000000000000,
			panePID:           100,
		},
		{
			name:              "same pane respawned with new PID and nonzero exit",
			recordedGen:       "recorded-pane",
			currentGen:        "recorded-pane",
			deadStatus:        1,
			recordedPanePID:   100,
			recordedProcID:    200,
			recordedProcStart: 1700000000000000000,
			panePID:           300,
		},
		{
			name:              "recorded process still alive with recorded start",
			recordedGen:       "recorded-pane",
			currentGen:        "recorded-pane",
			deadStatus:        1,
			recordedPanePID:   100,
			recordedProcID:    200,
			recordedProcStart: 1700000000000000000,
			panePID:           100,
			processes: map[int]processInfo{
				200: {pid: 200, startedAt: time.Unix(0, 1700000000000000000).UTC(), comm: "opencode"},
			},
		},
		{
			name:              "recorded process PID reused by different lifetime",
			recordedGen:       "recorded-pane",
			currentGen:        "recorded-pane",
			deadStatus:        1,
			recordedPanePID:   100,
			recordedProcID:    200,
			recordedProcStart: 1700000000000000000,
			panePID:           100,
			processes: map[int]processInfo{
				200: {pid: 200, startedAt: time.Unix(0, 1700000001000000000).UTC(), comm: "opencode"},
			},
		},
		{
			name:              "exact recorded lifetime matched still never fails",
			recordedGen:       "recorded-pane",
			currentGen:        "recorded-pane",
			deadStatus:        1,
			recordedPanePID:   100,
			recordedProcID:    200,
			recordedProcStart: 1700000000000000000,
			panePID:           100,
		},
		{
			name:              "nil process snapshot with nonzero exit",
			recordedGen:       "recorded-pane",
			currentGen:        "recorded-pane",
			deadStatus:        1,
			recordedPanePID:   100,
			recordedProcID:    200,
			recordedProcStart: 1700000000000000000,
			panePID:           100,
			processes:         nil,
		},
		{
			name:              "empty process snapshot with nonzero exit",
			recordedGen:       "recorded-pane",
			currentGen:        "recorded-pane",
			deadStatus:        1,
			recordedPanePID:   100,
			recordedProcID:    200,
			recordedProcStart: 1700000000000000000,
			panePID:           100,
			processes:         map[int]processInfo{},
		},
	}
	for _, test := range tests {
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
			turn = w.applyPollFacts("agent:@1", false, test.deadStatus,
				time.Now().UTC(), turn, ProviderActivityObservation{})

			kinds := map[string]bool{}
			for _, fact := range ledger.applied {
				kinds[fact.Kind] = true
			}
			if kinds["failed"] {
				t.Fatalf("dead pane produced a failed fact: %#v", ledger.applied)
			}
			if !kinds["uncertain"] {
				t.Fatalf("dead pane missing the uncertain resolution: %#v", ledger.applied)
			}
			// The fake ledger resolves every liveness fact to Unknown; the
			// point of this test is the fact choice (never Failed), asserted
			// above.
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
	ledger.seed("zen-worker-worker:@1", TurnSnapshot{
		SessionID:       "zen-worker-worker:@1",
		TurnID:          "zen-worker-worker:@1:turn:1",
		Status:          TurnRunning,
		AcceptedAt:      time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC),
		ProcessIdentity: "recorded-proc",
	})
	w.turnLedger = ledger
	windows := []tmuxWindow{
		{target: "zen-worker-worker:@1", name: "worker", cwd: "/repo/zen", command: "opencode", panePID: 333, delegated: true},
	}
	restore := installFakePollSeams(w, windows, map[string]string{
		"zen-worker-worker:@1": "OpenCode\nworking\n",
	}, map[int]processInfo{
		333: fakeProcess(333, time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)),
	})
	defer restore()

	w.poll()
	drainWatcherEvents(w)
	if worker := workerByID(w.Workers(), "zen-worker-worker:@1"); worker == nil || worker.State != classifier.StateRunning {
		t.Fatalf("session before removal = %#v", worker)
	}

	// The window disappears from a successful inventory.
	restore()
	restore = installFakePollSeams(w, nil, map[string]string{}, map[int]processInfo{})
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
	if worker := workerByID(w.Workers(), "zen-worker-worker:@1"); worker != nil {
		t.Fatalf("session survived removal: %#v", worker)
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
	ledger.seed("zen-worker-pi:@1", TurnSnapshot{
		SessionID:       "zen-worker-pi:@1",
		TurnID:          "zen-worker-pi:@1:turn:1",
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
		{target: "zen-worker-pi:@1", name: "pi", cwd: "/repo/zen", command: "pi", panePID: 444, delegated: true},
	}
	restore := installFakePollSeams(w, windows, map[string]string{
		"zen-worker-pi:@1": content,
	}, map[int]processInfo{
		444: fakeProcess(444, time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)),
	})
	defer restore()

	w.poll()
	drainWatcherEvents(w)
	worker := workerByID(w.Workers(), "zen-worker-pi:@1")
	if worker == nil || worker.State != classifier.StateRunning {
		t.Fatalf("Pi session projection = %#v, want running (never blocked)", worker)
	}
	if worker.NeedsAttention || worker.Attention == "user_input" {
		t.Fatalf("Pi session gained attention from pane: %#v", worker)
	}
	// A repeated poll with the same blocked-looking frame stays running.
	w.poll()
	drainWatcherEvents(w)
	worker = workerByID(w.Workers(), "zen-worker-pi:@1")
	if worker == nil || worker.State != classifier.StateRunning {
		t.Fatalf("Pi session after repeated poll = %#v, want running", worker)
	}
}

// TestPollLivenessAppliesWithoutProviderProbe covers the Round-3 gate
// change: applyPollFacts runs for every mutable ledger-tracked turn even
// when no Provider probe is installed — only the Provider observation is
// skipped, so liveness facts (end-of-identity, abnormal exit) always reach
// the reducer.
func TestPollLivenessAppliesWithoutProviderProbe(t *testing.T) {
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
	})
	ledger := newFakeTurnLedger()
	ledger.seed("zen-worker-worker:@1", TurnSnapshot{
		SessionID:       "zen-worker-worker:@1",
		TurnID:          "zen-worker-worker:@1:turn:1",
		Status:          TurnRunning,
		AcceptedAt:      time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC),
		ProcessIdentity: "recorded-proc",
	})
	w.turnLedger = ledger
	// No Provider probe installed (nil): the mutable turn must still be
	// applied, with only the Provider observation gated.
	windows := []tmuxWindow{
		{target: "zen-worker-worker:@1", name: "worker", cwd: "/repo/zen", command: "opencode", panePID: 100, delegated: true},
	}
	restore := installFakePollSeams(w, windows, map[string]string{
		"zen-worker-worker:@1": "OpenCode\n",
	}, map[int]processInfo{})
	defer restore()
	// The pane dies with a non-zero exit: the liveness fact must reach the
	// reducer even with a nil probe.
	previousCapture := w.capturePane
	w.capturePane = func(target string) (string, bool, int) {
		content, _, _ := previousCapture(target)
		return content, false, 1
	}
	defer func() { w.capturePane = previousCapture }()

	w.poll()
	drainWatcherEvents(w)

	kinds := map[string]bool{}
	providerFacts := 0
	for _, fact := range ledger.applied {
		kinds[fact.Kind] = true
		if fact.Class == EvidenceProvider {
			providerFacts++
		}
	}
	if !kinds["uncertain"] {
		t.Fatalf("liveness fact missing with nil probe: %#v", ledger.applied)
	}
	if kinds["failed"] {
		t.Fatalf("unprovable identity produced a failed fact: %#v", ledger.applied)
	}
	if providerFacts != 0 {
		t.Fatalf("provider observation applied with nil probe: %#v", ledger.applied)
	}
	worker := workerByID(w.Workers(), "zen-worker-worker:@1")
	if worker == nil || worker.State != classifier.StateUnknown {
		t.Fatalf("projection = %#v, want Unknown from the liveness fact", worker)
	}
}
