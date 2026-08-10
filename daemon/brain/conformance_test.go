package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

// conformanceFixture is one executor's provider-neutral adapter contract
// evidence (worklog C.10 / Research B): the native durable identities and
// cursor semantics each adapter derives for the frozen deterministic FactID,
// plus the turn-bound terminal evidence. Every supported executor must pass
// the shared conformance harness below.
type conformanceFixture struct {
	provider string
	// admission tuple as the adapter derives it from the provider's durable
	// source (stream = provider session identity, id = native message/event
	// id, cursor = monotone provider cursor).
	admission watcher.TurnAdmission
	// activity identity the adapter derives for the accepted turn.
	activityID string
	// native activity statuses the adapter maps to canonical kinds.
	runningStatus string
	doneStatus    string
	failedStatus  string
	// stable source identity components for FactID derivation.
	sourceID string
}

// executorConformanceFixtures are derived from Research B (B.6): every
// supported executor can supply deterministic FactID inputs and turn-bound
// terminal evidence from its durable source.
func executorConformanceFixtures() []conformanceFixture {
	stream := func(provider, session string) string {
		return provider + "\x00" + session + "\x00path"
	}
	return []conformanceFixture{
		{
			provider:      "codex",
			admission:     watcher.TurnAdmission{Stream: stream("codex", "thread-11111111-1111-7111-8111-111111111111"), ID: "turn-22222222-2222-7222-8222-222222222222", Cursor: 1, SHA256: "payload"},
			activityID:    "item-33333333-3333-7333-8333-333333333333",
			runningStatus: "running", doneStatus: "completed", failedStatus: "failed",
			sourceID: "provider\x00codex\x00rollout\x00item-33333333-3333-7333-8333-333333333333\x003",
		},
		{
			provider:      "claude",
			admission:     watcher.TurnAdmission{Stream: stream("claude", "44444444-4444-4444-8444-444444444444"), ID: "user-record-1", Cursor: 1, SHA256: "payload"},
			activityID:    "assistant-record-2",
			runningStatus: "running", doneStatus: "completed", failedStatus: "failed",
			sourceID: "provider\x00claude\x00jsonl\x00assistant-record-2\x002",
		},
		{
			provider:      "cursor",
			admission:     watcher.TurnAdmission{Stream: stream("cursor", "55555555-5555-4555-8555-555555555555"), ID: "request-1", Cursor: 1, SHA256: "payload"},
			activityID:    "turn_ended-1",
			runningStatus: "running", doneStatus: "completed", failedStatus: "failed",
			sourceID: "provider\x00cursor\x00transcript\x00turn_ended-1\x001",
		},
		{
			provider:      "grok",
			admission:     watcher.TurnAdmission{Stream: stream("grok", "66666666-6666-7666-8666-666666666666"), ID: "prompt-1", Cursor: 1, SHA256: "payload"},
			activityID:    "event-66666666-6666-7666-8666-666666666666-1",
			runningStatus: "running", doneStatus: "completed", failedStatus: "failed",
			sourceID: "provider\x00grok\x00events\x00event-66666666-6666-7666-8666-666666666666-1\x001",
		},
		{
			provider:      "opencode",
			admission:     watcher.TurnAdmission{Stream: stream("opencode", "ses_77777777777777777777777777"), ID: "msg_77777777", Cursor: 42, SHA256: "payload"},
			activityID:    "msg_88888888",
			runningStatus: "running", doneStatus: "completed", failedStatus: "failed",
			sourceID: "provider\x00opencode\x00db\x00msg_88888888\x0042",
		},
		{
			provider:      "pi",
			admission:     watcher.TurnAdmission{Stream: stream("pi", "99999999-9999-7999-8999-999999999999"), ID: "entry-a1b2c3d4", Cursor: 1, SHA256: "payload"},
			activityID:    "entry-e5f6a7b8",
			runningStatus: "running", doneStatus: "completed", failedStatus: "failed",
			sourceID: "provider\x00pi\x00session-file\x00entry-e5f6a7b8\x001",
		},
	}
}

// TestAdapterConformanceSharedHarness drives every executor fixture through
// the same canonical pipeline: correlated admission → provider running →
// bound terminal, exactly one actionable wake per (session, turn, kind), and
// deterministic FactIDs that dedupe on replay and across restart.
func TestAdapterConformanceSharedHarness(t *testing.T) {
	for _, fixture := range executorConformanceFixtures() {
		t.Run(fixture.provider, func(t *testing.T) {
			store, sessionID, turnID := ledgerTestStore(t)
			acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
			fixture.admission.At = acceptedAt.Add(2 * time.Second)

			// (a) admission tuple + (d) stable native identity: the receipt
			// admission fact binds the turn deterministically.
			if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
				SessionID: sessionID, TurnID: turnID,
				Class:     watcher.EvidenceReceipt,
				Kind:      "admission",
				SourceID:  "receipt\x00" + turnID + "\x00accepted\x00payload",
				Admission: fixture.admission,
				At:        acceptedAt.Add(2 * time.Second),
			}); err != nil {
				t.Fatal(err)
			}

			// (b) activity status stream with start.
			running := watcher.TurnFact{
				SessionID: sessionID, TurnID: turnID,
				Class:      watcher.EvidenceProvider,
				Kind:       "running",
				SourceID:   fixture.sourceID,
				Cursor:     fixture.admission.Cursor,
				Admission:  fixture.admission,
				ActivityID: fixture.activityID,
				StartedAt:  acceptedAt.Add(3 * time.Second),
				At:         acceptedAt.Add(4 * time.Second),
			}
			snapshot, changed, err := store.ApplyTurnFact(running)
			if err != nil || !changed || snapshot.Status != watcher.TurnRunning {
				t.Fatalf("adapter running = (%+v, %v, %v)", snapshot, changed, err)
			}

			// (c) turn-bound terminal via activity identity / admission tuple.
			done := running
			done.Kind = "done"
			done.SettledAt = acceptedAt.Add(9 * time.Second)
			snapshot, changed, err = store.ApplyTurnFact(done)
			if err != nil || !changed || snapshot.Status != watcher.TurnDone {
				t.Fatalf("adapter terminal = (%+v, %v, %v)", snapshot, changed, err)
			}
			workItem, _, _ := store.WorkByOwnerSession(sessionID)
			row, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.done")
			if !found || !row.Actionable {
				t.Fatalf("adapter done row = %+v found=%v", row, found)
			}

			// Exactly one actionable wake per (session, turn, kind).
			actionable := 0
			events, _ := store.ListWorkEvents(workItem.ID)
			for _, event := range events {
				if event.Actionable && strings.HasPrefix(event.DedupeKey, "session:"+sessionID+":turn:") {
					actionable++
				}
			}
			if actionable != 1 {
				t.Fatalf("adapter actionable wakes = %d, want exactly one", actionable)
			}

			// Deterministic FactID: replay and restart re-read dedupe.
			if _, changed, err := store.ApplyTurnFact(done); err != nil || changed {
				t.Fatalf("adapter terminal replay changed state: %v err=%v", changed, err)
			}
			restarted, err := NewStore(store.Root)
			if err != nil {
				t.Fatal(err)
			}
			if _, changed, err := restarted.ApplyTurnFact(done); err != nil || changed {
				t.Fatalf("adapter terminal restart re-read changed state: %v err=%v", changed, err)
			}

			// Terminal turns are immutable; failed/error statuses map to
			// failed only as bound facts.
			if _, changed, err := restarted.ApplyTurnFact(running); err != nil || changed {
				t.Fatalf("adapter terminal turn reopened: %v err=%v", changed, err)
			}
		})
	}
}

// TestAdapterConformanceFailedTerminalMapsExactlyOnce drives the failed
// status mapping (interrupted/cancelled/error) through the shared harness.
func TestAdapterConformanceFailedTerminalMapsExactlyOnce(t *testing.T) {
	for _, fixture := range executorConformanceFixtures() {
		t.Run(fixture.provider, func(t *testing.T) {
			store, sessionID, turnID := ledgerTestStore(t)
			acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
			fixture.admission.At = acceptedAt.Add(2 * time.Second)
			if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
				SessionID: sessionID, TurnID: turnID,
				Class:     watcher.EvidenceReceipt,
				Kind:      "admission",
				SourceID:  "receipt\x00" + turnID + "\x00accepted\x00payload",
				Admission: fixture.admission,
				At:        acceptedAt.Add(2 * time.Second),
			}); err != nil {
				t.Fatal(err)
			}
			failed := watcher.TurnFact{
				SessionID: sessionID, TurnID: turnID,
				Class:      watcher.EvidenceProvider,
				Kind:       "failed",
				SourceID:   fixture.sourceID,
				Cursor:     fixture.admission.Cursor,
				Admission:  fixture.admission,
				ActivityID: fixture.activityID,
				StartedAt:  acceptedAt.Add(3 * time.Second),
				SettledAt:  acceptedAt.Add(9 * time.Second),
				At:         acceptedAt.Add(10 * time.Second),
			}
			snapshot, changed, err := store.ApplyTurnFact(failed)
			if err != nil || !changed || snapshot.Status != watcher.TurnFailed {
				t.Fatalf("adapter failed = (%+v, %v, %v)", snapshot, changed, err)
			}
			workItem, _, _ := store.WorkByOwnerSession(sessionID)
			row, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.failed")
			if !found || !row.Actionable {
				t.Fatalf("adapter failed row = %+v found=%v", row, found)
			}
		})
	}
}

// TestAdapterConformanceDeterministicFactIDs distinct fixtures never collide:
// FactID is a hash of (session, turn, class, kind, stable source identity).
func TestAdapterConformanceDeterministicFactIDs(t *testing.T) {
	seen := map[string]string{}
	for _, fixture := range executorConformanceFixtures() {
		key := watcher.TurnFactID("session", "turn", watcher.EvidenceProvider, "running", fixture.sourceID)
		if previous, exists := seen[key]; exists {
			t.Fatalf("FactID collision between %s and %s: %s", previous, fixture.provider, key)
		}
		seen[key] = fixture.provider
		again := watcher.TurnFactID("session", "turn", watcher.EvidenceProvider, "running", fixture.sourceID)
		if again != key {
			t.Fatalf("FactID not deterministic for %s", fixture.provider)
		}
	}
	if len(seen) != len(executorConformanceFixtures()) {
		t.Fatalf("FactID count = %d, want %d", len(seen), len(executorConformanceFixtures()))
	}
}

// TestAdapterConformanceReusedSessionNewTurn covers C.2.8 per adapter: a new
// accepted turn on a reused Session is a new lifecycle boundary; the old
// turn's terminal facts never touch it.
func TestAdapterConformanceReusedSessionNewTurn(t *testing.T) {
	for _, fixture := range executorConformanceFixtures() {
		t.Run(fixture.provider, func(t *testing.T) {
			store, sessionID, turnID := ledgerTestStore(t)
			acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
			fixture.admission.At = acceptedAt.Add(2 * time.Second)
			if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
				SessionID: sessionID, TurnID: turnID,
				Class:     watcher.EvidenceReceipt,
				Kind:      "admission",
				SourceID:  "receipt\x00" + turnID + "\x00accepted\x00payload",
				Admission: fixture.admission,
				At:        acceptedAt.Add(2 * time.Second),
			}); err != nil {
				t.Fatal(err)
			}
			done := watcher.TurnFact{
				SessionID: sessionID, TurnID: turnID,
				Class:      watcher.EvidenceProvider,
				Kind:       "done",
				SourceID:   fixture.sourceID,
				Cursor:     fixture.admission.Cursor,
				Admission:  fixture.admission,
				ActivityID: fixture.activityID,
				StartedAt:  acceptedAt.Add(3 * time.Second),
				SettledAt:  acceptedAt.Add(9 * time.Second),
				At:         acceptedAt.Add(10 * time.Second),
			}
			if _, _, err := store.ApplyTurnFact(done); err != nil {
				t.Fatal(err)
			}

			// Reused Session: a new turn admits with a fresh identity.
			turn2 := sessionID + ":turn:2"
			if err := store.admitTurn(watcher.AdmittedTurn{
				SessionID:       sessionID,
				TurnID:          turn2,
				AcceptedAt:      acceptedAt.Add(30 * time.Second),
				ProcessIdentity: "proc-identity-1",
				PaneGeneration:  "pane-gen-1",
				PayloadSHA256:   "payload-2",
			}); err != nil {
				t.Fatal(err)
			}
			admission2 := fixture.admission
			admission2.Cursor = fixture.admission.Cursor + 1
			admission2.ID = fixture.admission.ID + "-2"
			admission2.At = acceptedAt.Add(32 * time.Second)
			if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
				SessionID: sessionID, TurnID: turn2,
				Class:     watcher.EvidenceReceipt,
				Kind:      "admission",
				SourceID:  "receipt\x00" + turn2 + "\x00accepted\x00payload-2",
				Admission: admission2,
				At:        acceptedAt.Add(32 * time.Second),
			}); err != nil {
				t.Fatal(err)
			}
			// The old turn's terminal fact cannot bind the new turn: at most a
			// non-actionable hint is attached; canonical status stays Accepted.
			oldDone := done
			oldDone.TurnID = turn2
			snapshot, _, err := store.ApplyTurnFact(oldDone)
			if err != nil || snapshot.Status != watcher.TurnAccepted {
				t.Fatalf("old turn fact moved new turn: %+v err=%v", snapshot, err)
			}
			workItem, _, _ := store.WorkByOwnerSession(sessionID)
			row, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turn2+":session.done")
			if found && row.Actionable {
				t.Fatalf("old turn fact woke the new turn: %+v", row)
			}
			current, _, _ := store.Turn(sessionID)
			if current.TurnID != turn2 || current.Status != watcher.TurnAccepted {
				t.Fatalf("reused session current turn = %+v", current)
			}
		})
	}
}

// TestAdapterConformanceEvidenceLossUpgrade drives every executor fixture
// through the Slice 2 channel-health contract: a bounded provider-evidence
// loss (transcript unlocatable/unreadable) resolves exactly one canonical
// session.uncertain per turn — never silent Admitted, never fabricated
// done/failed — and a later readable bound terminal upgrades Unknown
// monotonically with exactly one wake per kind.
func TestAdapterConformanceEvidenceLossUpgrade(t *testing.T) {
	for _, fixture := range executorConformanceFixtures() {
		t.Run(fixture.provider, func(t *testing.T) {
			store, sessionID, turnID := ledgerTestStore(t)
			acceptedAt := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
			store.now = func() time.Time { return acceptedAt }
			fixture.admission.At = acceptedAt.Add(2 * time.Second)
			if _, _, err := store.ApplyTurnFact(watcher.TurnFact{
				SessionID: sessionID, TurnID: turnID,
				Class:     watcher.EvidenceReceipt,
				Kind:      "admission",
				SourceID:  "receipt\x00" + turnID + "\x00accepted\x00payload",
				Admission: fixture.admission,
				At:        acceptedAt.Add(2 * time.Second),
			}); err != nil {
				t.Fatal(err)
			}
			loss := watcher.TurnFact{
				SessionID: sessionID, TurnID: turnID,
				Class:    watcher.EvidenceProvider,
				Kind:     "uncertain",
				SourceID: "provider-loss\x00" + turnID,
				At:       acceptedAt.Add(time.Minute),
			}
			snapshot, changed, err := store.ApplyTurnFact(loss)
			if err != nil || !changed || snapshot.Status != watcher.TurnUnknown {
				t.Fatalf("adapter evidence loss = (%+v, %v, %v), want Unknown", snapshot, changed, err)
			}
			workItem, _, _ := store.WorkByOwnerSession(sessionID)
			uncertainRow, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+turnID+":session.uncertain")
			if !found || !uncertainRow.Actionable {
				t.Fatalf("adapter uncertain row = %+v found=%v", uncertainRow, found)
			}
			// The loss fact is exactly once per turn.
			if _, changed, err := store.ApplyTurnFact(loss); err != nil || changed {
				t.Fatalf("adapter evidence-loss replay changed state: %v err=%v", changed, err)
			}

			// Late recovery: the adapter's bound terminal upgrades Unknown.
			done := watcher.TurnFact{
				SessionID: sessionID, TurnID: turnID,
				Class:      watcher.EvidenceProvider,
				Kind:       "done",
				SourceID:   fixture.sourceID,
				Cursor:     fixture.admission.Cursor,
				Admission:  fixture.admission,
				ActivityID: fixture.activityID,
				StartedAt:  acceptedAt.Add(2 * time.Second),
				SettledAt:  acceptedAt.Add(9 * time.Second),
				At:         acceptedAt.Add(2 * time.Minute),
			}
			snapshot, changed, err = store.ApplyTurnFact(done)
			if err != nil || !changed || snapshot.Status != watcher.TurnDone {
				t.Fatalf("adapter late bound terminal = (%+v, %v, %v), want Done", snapshot, changed, err)
			}
			events, _ := store.ListWorkEvents(workItem.ID)
			uncertain := 0
			doneActionable := 0
			for _, event := range events {
				if event.Kind == "session.uncertain" && event.Actionable {
					uncertain++
				}
				if event.Kind == "session.done" && event.Actionable {
					doneActionable++
				}
			}
			if uncertain != 1 || doneActionable != 1 {
				t.Fatalf("adapter wakes uncertain=%d done=%d, want exactly one each: %#v", uncertain, doneActionable, events)
			}
		})
	}
}
