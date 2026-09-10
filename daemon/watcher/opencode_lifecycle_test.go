package watcher

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// scriptedProviderActivityProbe replays a fixed observation sequence. The
// first observations are consumed by the admission confirmer; later steps are
// consumed by the canonical-turn poll fact pipeline.
type scriptedProviderActivityProbe struct {
	mu      sync.Mutex
	steps   []ProviderActivityObservation
	stepIdx int
}

func (p *scriptedProviderActivityProbe) ObserveProviderActivity(
	classifier.Worker,
	time.Time,
) ProviderActivityObservation {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stepIdx >= len(p.steps) {
		return ProviderActivityObservation{Structured: true, FallbackAllowed: true}
	}
	observation := p.steps[p.stepIdx]
	p.stepIdx++
	return observation
}

func (p *scriptedProviderActivityProbe) ForgetProviderActivity(string) {}

func (p *scriptedProviderActivityProbe) next() ProviderActivityObservation {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stepIdx >= len(p.steps) {
		return ProviderActivityObservation{Structured: true, FallbackAllowed: true}
	}
	observation := p.steps[p.stepIdx]
	p.stepIdx++
	return observation
}

func lifecycleTestWatcher(io *fakeSessionInputIO, ledger *fakeTurnLedger, probe ProviderActivityProbe) *Watcher {
	w := watcherWithAdmissionProbe(probe)
	owner := newSessionInputOwner(io)
	owner.ledger = ledger
	w.sessionInput = owner
	w.turnLedger = ledger
	w.targetCommandResolver = func(string) (string, bool) { return "opencode", true }
	w.targetOwnershipResolver = func(string) (bool, error) { return true, nil }
	return w
}

// TestBrainHostInputCarriesExactClaimCapabilityThroughCanonicalSubmission
// exercises the real Host entry point through Session Input's durable
// prepare/provider/resolve transaction. The Brain Store separately proves
// that only a matching live Event claim may prepare this tuple.
func TestBrainHostInputCarriesExactClaimCapabilityThroughCanonicalSubmission(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	now := time.Now().UTC()
	payload := "handle the claimed Brain Work Event"
	payloadDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	probe := &scriptedProviderActivityProbe{steps: []ProviderActivityObservation{
		{Structured: true, FallbackAllowed: true},
		{
			ID: "host-activity", Status: "running", StartedAt: now.Add(time.Second),
			AdmissionStream: "opencode_db\x00host\x00/db", AdmissionID: "host-message",
			AdmissionCursor: 1, AdmissionAt: now.Add(time.Second),
			InputSHA256: payloadDigest, Structured: true,
		},
	}}
	w := lifecycleTestWatcher(io, ledger, probe)
	hostID := "zen-worker-brain-hidden:@host-capability"
	w.workers[hostID] = &classifier.Worker{
		ID: hostID, Command: "opencode", Cwd: "/repo/zen",
		PaneAlive: true, State: classifier.StateDone,
	}
	claimToken := "claim-exact"
	workID := "work-exact"
	providerTurnID := "provider-turn-exact"

	result, err := w.SubmitBrainHostInput(
		hostID, payload, claimToken, workID, providerTurnID, now,
	)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != providerTurnID {
		t.Fatalf("Host admission result=%+v err=%v", result, err)
	}
	submission, found, err := ledger.InputAdmission(hostID, providerTurnID)
	if err != nil || !found || submission.State != InputAdmissionResolved ||
		submission.Receipt != providerTurnID || submission.ClaimToken != claimToken ||
		submission.WorkID != workID || submission.SessionID != hostID ||
		submission.ProposedTurnID != providerTurnID {
		t.Fatalf("canonical Host submission=%+v found=%v err=%v", submission, found, err)
	}
	if len(io.queues) != 1 || len(io.submissions) != 1 {
		t.Fatalf("Host input mutation count queues=%d submissions=%d, want one each", len(io.queues), len(io.submissions))
	}
}

// TestBrainHostSubmissionStillRequiresProviderEvidence proves the delegated
// transport relaxation is scoped: the Brain host has no prompt-carried signal
// and must still resolve through provider-native evidence, never self-confirm.
func TestBrainHostSubmissionStillRequiresProviderEvidence(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("codex")
	owner := newLedgerSessionInputOwner(io, ledger)
	turn := testTurnDraft("host-evidence-required", time.Now().UTC(), identity)
	turn.ClaimToken = "host-claim"
	confirmer := delegatedInputConfirmer{
		baseline: func() (delegatedInputBaseline, error) {
			return delegatedInputBaseline{Provider: ProviderActivityObservation{
				ID: "host-activity", Status: "running", Structured: true,
			}}, nil
		},
		confirm: func(delegatedAdmissionEvidence, time.Time, string) (delegatedInputConfirmation, error) {
			return delegatedInputConfirmation{Outcome: InputAmbiguous}, errors.New("provider evidence unavailable")
		},
	}
	result, err := owner.submitHost(
		"host:@evidence", identity, fixedSessionInputResolver(identity), identity.Command,
		"host payload", turn, confirmer,
	)
	if err == nil || result.Outcome != InputAmbiguous || result.ProviderConfirmed {
		t.Fatalf("host transport self-confirmed = (%+v, %v), want ambiguous", result, err)
	}
	if len(io.queues) != 1 || len(io.submissions) != 1 {
		t.Fatalf("host transport effects queues=%d submissions=%d", len(io.queues), len(io.submissions))
	}
	if submission, found, _ := ledger.InputAdmission("host:@evidence", turn.ID); !found || submission.State != InputAdmissionPending {
		t.Fatalf("host admission = %+v found=%v, want pending", submission, found)
	}
}

// TestOpenCodeTransportSubmitThenSignalAndProviderEvidenceSettleTurn verifies
// the generic delegated contract: an owned tmux paste+submit is accepted
// immediately without provider transcript byte equality, the prompt-carried
// signal admits the Turn, and a matching provider terminal settles it exactly
// once. No byte gate, no replay, no phantom Turn before the signal.
func TestOpenCodeTransportSubmitThenSignalAndProviderEvidenceSettleTurn(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	now := time.Now().UTC()
	probe := &scriptedProviderActivityProbe{steps: []ProviderActivityObservation{
		// step 0: pre-mutation admission baseline — nothing admitted yet.
		{Structured: true, FallbackAllowed: true},
		// step 1: provider-native settlement (the transcript digest is diagnostic).
		{
			ID: "act-turn", Status: "completed", StartedAt: now.Add(time.Second),
			SettledAt: now.Add(20 * time.Second), Structured: true,
		},
	}}
	w := lifecycleTestWatcher(io, ledger, probe)
	sessionID := "opencode-ambiguous:@1"
	w.workers[sessionID] = &classifier.Worker{
		ID: sessionID, Command: "opencode", Cwd: "/repo/zen",
		PaneAlive: true, Delegated: true, State: classifier.StateUnknown,
	}

	turnID := sessionID + ":turn:1"
	result, err := w.SubmitDelegatedInput(sessionID, "task brief", turnID, now)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != turnID || result.ProviderConfirmed {
		t.Fatalf("transport submit = (%+v, %v), want submitted", result, err)
	}
	if len(io.queues) != 1 || len(io.submissions) != 1 {
		t.Fatalf("transport submit replayed the prompt: queues=%d submissions=%d", len(io.queues), len(io.submissions))
	}
	if turn, hasTurn, _ := ledger.Turn(sessionID); hasTurn {
		t.Fatalf("transport submit created a premature canonical Turn: %+v", turn)
	}
	if pendingList, _ := ledger.PendingInputAdmissions(sessionID); len(pendingList) != 1 || pendingList[0].State != InputAdmissionPending {
		t.Fatalf("canonical pending submission = %+v", pendingList)
	}

	// Exact receipt replay only reports the known submission; it never resends.
	retry, retryErr := w.SubmitDelegatedInput(sessionID, "task brief", turnID, now)
	if retryErr != nil || !retry.Duplicate || retry.Outcome != InputAccepted || len(io.queues) != 1 {
		t.Fatalf("receipt replay = (%+v, %v), queues=%d", retry, retryErr, len(io.queues))
	}

	// The prompt-carried signal admits the current Turn.
	if _, err := ledger.ApplyDelegatedTurnProgress(TurnFact{
		SessionID: sessionID, TurnID: turnID, Class: EvidenceControl, Kind: "running",
		SourceID: "control\x00" + turnID, At: now.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.RebindDelegatedTurnProjection(sessionID); err != nil {
		t.Fatal(err)
	}
	turn, hasTurn, _ := ledger.Turn(sessionID)
	if !hasTurn || turn.TurnID != turnID || turn.Status != TurnAccepted {
		t.Fatalf("signal admission = %+v hasTurn=%v", turn, hasTurn)
	}
	if worker := w.GetWorker(sessionID); worker == nil || worker.State != classifier.StateRunning {
		t.Fatalf("signal projection = %+v", worker)
	}

	// Authoritative provider settlement; the terminal Turn is immutable.
	turn = w.applyPollFacts(sessionID, true, -1, now.Add(21*time.Second), turn, probe.next())
	if turn.Status != TurnDone {
		t.Fatalf("authoritative settlement = %+v, want done", turn)
	}
	applied := w.applyPollFacts(sessionID, true, -1, now.Add(26*time.Second), turn, ProviderActivityObservation{
		ID: "act-turn", Status: "running", StartedAt: now.Add(25 * time.Second), Structured: true,
	})
	if applied.Status != TurnDone {
		t.Fatalf("terminal turn reopened after settlement: %+v", applied)
	}
	if state, _ := projectDelegatedTurn(w.GetWorker(sessionID), turn); state != classifier.StateDone {
		t.Fatalf("terminal turn projection = %s, want done", state)
	}
}

// TestOpenCodeFollowUpSignalOwnsNewTurnAndOldResultCannotCompleteIt verifies
// that after a transport-submitted first turn is completed by its signal, a
// transport-submitted follow-up becomes the current lifecycle only through its
// own prompt signal, and the previous turn's terminal activity can never
// complete the new turn.
func TestOpenCodeFollowUpSignalOwnsNewTurnAndOldResultCannotCompleteIt(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	now := time.Now().UTC()
	probe := &scriptedProviderActivityProbe{steps: []ProviderActivityObservation{
		// step 0: baseline for the first submit.
		{Structured: true, FallbackAllowed: true},
		// step 1: baseline for the follow-up. A terminal activity with no live
		// canonical binding proves the owned session is idle; a fresh turn may
		// cross the mutation boundary, but it is never adopted here.
		{
			ID: "act-first", Status: "completed", StartedAt: now.Add(time.Second),
			SettledAt: now.Add(10 * time.Second), Structured: true,
		},
		// step 2: the previous turn's terminal activity.
		{
			ID: "act-first", Status: "completed", StartedAt: now.Add(time.Second),
			SettledAt: now.Add(10 * time.Second), Structured: true,
		},
	}}
	w := lifecycleTestWatcher(io, ledger, probe)
	sessionID := "opencode-followup:@2"
	w.workers[sessionID] = &classifier.Worker{
		ID: sessionID, Command: "opencode", Cwd: "/repo/zen",
		PaneAlive: true, Delegated: true, State: classifier.StateUnknown,
	}

	firstTurn := sessionID + ":turn:1"
	result, err := w.SubmitDelegatedInput(sessionID, "first brief", firstTurn, now)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != firstTurn || result.ProviderConfirmed {
		t.Fatalf("first transport submit = (%+v, %v)", result, err)
	}
	if _, hasTurn, _ := ledger.Turn(sessionID); hasTurn {
		t.Fatal("transport submit created a Turn before its signal")
	}
	// The first turn's own signal admits and completes it.
	if _, err := ledger.ApplyDelegatedTurnProgress(TurnFact{
		SessionID: sessionID, TurnID: firstTurn, Class: EvidenceControl, Kind: "done",
		SourceID: "control\x00" + firstTurn, At: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if turn, hasTurn, _ := ledger.Turn(sessionID); !hasTurn || turn.TurnID != firstTurn || turn.Status != TurnDone {
		t.Fatalf("first signal completion = %+v hasTurn=%v", turn, hasTurn)
	}

	// The follow-up is transport-submitted; only its own signal owns turn 2.
	followTurn := sessionID + ":turn:2"
	followAt := now.Add(time.Minute)
	result, err = w.SubmitDelegatedInput(sessionID, "follow-up", followTurn, followAt)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != followTurn || result.ProviderConfirmed {
		t.Fatalf("follow-up transport submit = (%+v, %v)", result, err)
	}
	if len(io.queues) != 2 {
		t.Fatalf("follow-up replayed: queues=%d", len(io.queues))
	}
	if _, hasTurn, _ := ledger.Turn(sessionID); hasTurn {
		turn, _, _ := ledger.Turn(sessionID)
		if turn.TurnID == followTurn {
			t.Fatalf("follow-up became canonical before its signal: %+v", turn)
		}
	}
	if _, err := ledger.ApplyDelegatedTurnProgress(TurnFact{
		SessionID: sessionID, TurnID: followTurn, Class: EvidenceControl, Kind: "running",
		SourceID: "control\x00" + followTurn, At: followAt.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	turn, hasTurn, _ := ledger.Turn(sessionID)
	if !hasTurn || turn.TurnID != followTurn || turn.Status != TurnAccepted {
		t.Fatalf("follow-up signal did not own the lifecycle: %+v hasTurn=%v", turn, hasTurn)
	}

	// The first turn's stale terminal activity cannot complete turn 2.
	applied := w.applyPollFacts(sessionID, true, -1, followAt.Add(5*time.Second), turn, probe.next())
	if applied.Status == TurnDone {
		t.Fatalf("stale first-turn result completed the new turn: %+v", applied)
	}
	if applied.TurnID != followTurn {
		t.Fatalf("stale result changed the canonical turn identity: %+v", applied)
	}
}

// TestOpenCodeAmbiguousAdmissionNoProviderEvidenceStaysPending verifies that
// an ambiguous admission with no provider evidence is never terminalized by a
// timeout or by absence of activity: no phantom Turn is created and the
// canonical pending projection stays running.
func TestOpenCodeAmbiguousAdmissionNoProviderEvidenceStaysPending(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	now := time.Now().UTC()
	probe := &scriptedProviderActivityProbe{
		steps: []ProviderActivityObservation{
			{Structured: true, FallbackAllowed: true},
			{Structured: true, FallbackAllowed: true},
		},
	}
	w := lifecycleTestWatcher(io, ledger, probe)
	sessionID := "opencode-noevidence:@3"
	w.workers[sessionID] = &classifier.Worker{
		ID: sessionID, Command: "opencode", Cwd: "/repo/zen",
		PaneAlive: true, Delegated: true, State: classifier.StateUnknown,
	}
	turnID := "opencode-noevidence:@3:turn:1"
	result, err := w.SubmitDelegatedInput(sessionID, "task brief", turnID, now)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != turnID || result.ProviderConfirmed {
		t.Fatalf("spawn transport = (%+v, %v), want submitted", result, err)
	}
	if turn, found, _ := ledger.Turn(sessionID); found {
		t.Fatalf("transport submit created a Turn before its signal: %+v", turn)
	}
	if pendingList, _ := ledger.PendingInputAdmissions(sessionID); len(pendingList) != 1 || pendingList[0].State != InputAdmissionPending {
		t.Fatalf("pending submission = %+v", pendingList)
	}
	if _, err := w.RebindDelegatedTurnProjection(sessionID); err != nil {
		t.Fatal(err)
	}
	if worker := w.GetWorker(sessionID); worker == nil || worker.State != classifier.StateRunning {
		t.Fatalf("pending projection = %+v, want running", worker)
	}
}

// TestOpenCodeFollowUpTurnNotTerminalizedByStaleCompletedProviderActivity
// verifies that a stale completed activity from the previous turn can never
// terminalize the new accepted turn: provider facts bind per-turn (admission
// tuple with monotone cursor, or admission window), so a new Session turn is
// a new lifecycle boundary.
func TestOpenCodeFollowUpTurnNotTerminalizedByStaleCompletedProviderActivity(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	now := time.Now().UTC()
	sessionID := "opencode-reuse:@4"
	firstAt := now.Add(-10 * time.Minute)
	ledger.seed(sessionID, TurnSnapshot{
		SessionID:  sessionID,
		TurnID:     sessionID + ":turn:1",
		Status:     TurnDone,
		AcceptedAt: firstAt,
		ActivityID: "act-old",
	})
	identity := testSessionInputIdentity("opencode")
	followDigest := fmt.Sprintf("%x", sha256.Sum256([]byte("follow-up")))
	probe := &scriptedProviderActivityProbe{
		steps: []ProviderActivityObservation{
			{
				ID: "act-old", Status: "completed",
				StartedAt: firstAt.Add(2 * time.Second), SettledAt: firstAt.Add(time.Minute),
				Structured: true, FallbackAllowed: true,
			},
			{
				ID: "act-new", Status: "running",
				StartedAt:       now.Add(time.Second),
				AdmissionStream: "opencode_db\x00ses_1\x00/db",
				AdmissionID:     "msg_new",
				AdmissionCursor: 6,
				InputSHA256:     followDigest,
				Structured:      true,
			},
		},
	}
	w := lifecycleTestWatcher(io, ledger, probe)
	w.workers[sessionID] = &classifier.Worker{
		ID: sessionID, Command: "opencode", Cwd: "/repo/zen",
		PaneAlive: true, Delegated: true, State: classifier.StateRunning,
	}
	turnID := sessionID + ":turn:2"
	newAt := time.Now().UTC()
	owner := newSessionInputOwner(io)
	owner.ledger = ledger
	result, err := owner.submitDelegated(
		sessionID, identity, fixedSessionInputResolver(identity),
		identity.Command, "follow-up", testTurnDraft(turnID, newAt, identity),
		w.delegatedInputConfirmer(sessionID, identity.Command),
	)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != turnID {
		t.Fatalf("reused-session follow-up = (%+v, %v), want accepted new turn", result, err)
	}
	turn, _, _ := ledger.Turn(sessionID)
	if turn.TurnID != turnID {
		t.Fatalf("reused session lost new turn: %+v", turn)
	}
	if turn.Status != TurnAccepted {
		t.Fatalf("follow-up not accepted: %+v", turn)
	}
	if len(io.submissions) != 1 {
		t.Fatalf("follow-up replay detected: submissions=%d", len(io.submissions))
	}

	// The stale completed observation (older cursor, started before the new
	// admission window) cannot bind to the new turn: the canonical status
	// must stay Accepted — never terminalized by the old turn's completion.
	stale := ProviderActivityObservation{
		ID: "act-old", Status: "completed",
		StartedAt: firstAt.Add(2 * time.Second), SettledAt: firstAt.Add(time.Minute),
		AdmissionStream: "opencode_db\x00ses_1\x00/db",
		AdmissionID:     "msg_old",
		AdmissionCursor: 5,
		Structured:      true,
	}
	turn = w.applyPollFacts(sessionID, true, -1, now.Add(5*time.Second), turn, stale)
	if turn.Status != TurnAccepted {
		t.Fatalf("stale completed activity terminalized the new turn: %+v", turn)
	}
}

// TestOpenCodeReusedSessionDigestMismatchCannotAdoptPending verifies that a
// provider activity for different bytes cannot claim a reused Session's
// pending submission, even when the activity later becomes terminal.
func TestOpenCodeReusedSessionDigestMismatchCannotAdoptPending(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	now := time.Now().UTC()
	sessionID := "zen-worker-opencode:@8174"
	firstAt := now.Add(-30 * time.Minute)
	firstTurn := sessionID + ":turn:1"
	ledger.seed(sessionID, TurnSnapshot{
		SessionID:  sessionID,
		TurnID:     firstTurn,
		Status:     TurnDone,
		AcceptedAt: firstAt,
		ActivityID: "old-activity",
	})
	identity := testSessionInputIdentity("opencode")
	probe := &scriptedProviderActivityProbe{
		steps: []ProviderActivityObservation{
			// Reuse baseline: the exact prior activity is terminal.
			{
				ID: "old-activity", Status: "completed",
				StartedAt: firstAt, SettledAt: firstAt.Add(time.Minute),
				Structured: true, FallbackAllowed: true,
			},
			// The confirm loop fails (SHA mismatch / timeout) — ambiguous.
			{
				ID: "new-activity", Status: "running",
				StartedAt:       now.Add(2 * time.Second),
				AdmissionStream: "opencode_db\x00ses_8174\x00/db",
				AdmissionID:     "msg_new",
				AdmissionCursor: 42,
				AdmissionAt:     now.Add(2 * time.Second),
				InputSHA256:     "normalized-bytes-not-the-payload",
				Structured:      true,
			},
			// The poll observes the same live activity (adoption).
			{
				ID: "new-activity", Status: "running",
				StartedAt:       now.Add(2 * time.Second),
				AdmissionStream: "opencode_db\x00ses_8174\x00/db",
				AdmissionID:     "msg_new",
				AdmissionCursor: 42,
				AdmissionAt:     now.Add(2 * time.Second),
				InputSHA256:     "normalized-bytes-not-the-payload",
				Structured:      true,
			},
			// The true completion.
			{
				ID: "new-activity", Status: "completed",
				StartedAt: now.Add(2 * time.Second), SettledAt: now.Add(10 * time.Minute),
				AdmissionStream: "opencode_db\x00ses_8174\x00/db",
				AdmissionID:     "msg_new",
				AdmissionCursor: 42,
				AdmissionAt:     now.Add(2 * time.Second),
				InputSHA256:     "normalized-bytes-not-the-payload",
				Structured:      true,
			},
		},
	}
	w := lifecycleTestWatcher(io, ledger, probe)
	w.workers[sessionID] = &classifier.Worker{
		ID: sessionID, Command: "opencode", Cwd: "/repo/zen",
		PaneAlive: true, Delegated: true, State: classifier.StateDone,
	}
	followTurn := sessionID + ":turn:2"
	owner := newSessionInputOwner(io)
	owner.ledger = ledger
	result, err := owner.submitDelegated(
		sessionID, identity, fixedSessionInputResolver(identity),
		identity.Command, "follow-up correction", testTurnDraft(followTurn, now, identity),
		w.delegatedInputConfirmer(sessionID, identity.Command),
	)
	if err == nil || result.Outcome != InputAmbiguous {
		t.Fatalf("reused-session ambiguous send = (%+v, %v), want ambiguous", result, err)
	}
	if len(io.queues) != 1 {
		t.Fatalf("ambiguous send replayed: queues=%d", len(io.queues))
	}
	turn, hasTurn, _ := ledger.Turn(sessionID)
	if !hasTurn || turn.TurnID != firstTurn || turn.Status != TurnDone {
		t.Fatalf("ambiguous send replaced prior terminal Turn: %+v", turn)
	}
	if pendingList, _ := ledger.PendingInputAdmissions(sessionID); len(pendingList) != 1 || pendingList[0].ProposedTurnID != followTurn {
		t.Fatalf("fresh pending candidate = %+v", pendingList)
	}

	// Provider evidence for normalized/different bytes cannot claim the
	// pending payload, whether running or terminal.
	pendingList, pendingErr := w.pendingInputAdmissions(sessionID)
	if pendingErr != nil {
		t.Fatal(pendingErr)
	}
	if len(pendingList) != 1 {
		t.Fatalf("pending submissions = %+v", pendingList)
	}
	pending := pendingList[0]
	if _, resolved := w.resolvePendingProviderAdmission(pending, probe.next(), now.Add(3*time.Second)); resolved {
		t.Fatal("mismatched provider digest adopted pending submission")
	}
	if _, resolved := w.resolvePendingProviderAdmission(pending, probe.next(), now.Add(11*time.Minute)); resolved {
		t.Fatal("mismatched terminal digest adopted pending submission")
	}
	if current, _, _ := ledger.Turn(sessionID); current.TurnID != firstTurn || current.Status != TurnDone {
		t.Fatalf("digest mismatch changed current Turn: %+v", current)
	}
}

// TestProjectDelegatedTurnMapsAllCanonicalStatuses guards the projection
// contract: list/capture/close/Work read canonical status only.
func TestProjectDelegatedTurnMapsAllCanonicalStatuses(t *testing.T) {
	worker := &classifier.Worker{Attention: "failed", NeedsAttention: true}
	for _, test := range []struct {
		status TurnStatus
		want   classifier.WorkerState
	}{
		{TurnAdmitted, classifier.StateRunning},
		{TurnAccepted, classifier.StateRunning},
		{TurnRunning, classifier.StateRunning},
		{TurnBlocked, classifier.StateBlocked},
		{TurnDone, classifier.StateDone},
		{TurnFailed, classifier.StateFailed},
		{TurnUnknown, classifier.StateUnknown},
	} {
		turn := TurnSnapshot{SessionID: "s", TurnID: "t", Status: test.status, Summary: "summary"}
		state, _ := projectDelegatedTurn(worker, turn)
		if state != test.want {
			t.Fatalf("status %s projected %s, want %s", test.status, state, test.want)
		}
	}
	if worker.Attention != "none" || worker.NeedsAttention {
		t.Fatalf("non-blocked projection retained attention: %+v", worker)
	}
	blocked := TurnSnapshot{Status: TurnBlocked}
	projectDelegatedTurn(worker, blocked)
	if worker.Attention != "user_input" || !worker.NeedsAttention {
		t.Fatalf("blocked projection attention = %+v", worker)
	}
}

// TestTurnFactIDIsDeterministicAcrossReplay guards the frozen FactID formula:
// the same (session, turn, class, kind, source identity) always derives the
// same fact; kind participates so one native record deriving multiple kinds
// stays distinct; no wall-clock time or per-run UUID appears.
func TestTurnFactIDIsDeterministicAcrossReplay(t *testing.T) {
	first := TurnFactID("s:@1", "s:@1:turn:1", EvidenceProvider, "running", "provider\x00s:@1\x00stream\x00activity-7\x0042")
	second := TurnFactID("s:@1", "s:@1:turn:1", EvidenceProvider, "running", "provider\x00s:@1\x00stream\x00activity-7\x0042")
	if first != second || first == "" {
		t.Fatalf("FactID not deterministic: %q vs %q", first, second)
	}
	done := TurnFactID("s:@1", "s:@1:turn:1", EvidenceProvider, "done", "provider\x00s:@1\x00stream\x00activity-7\x0042")
	if done == first {
		t.Fatalf("kind did not participate in FactID")
	}
	otherTurn := TurnFactID("s:@1", "s:@1:turn:2", EvidenceProvider, "running", "provider\x00s:@1\x00stream\x00activity-7\x0042")
	if otherTurn == first {
		t.Fatalf("turn did not participate in FactID")
	}
	if fmt.Sprintf("%x", [32]byte{}) == first {
		t.Fatalf("FactID looks like an empty hash")
	}
}
