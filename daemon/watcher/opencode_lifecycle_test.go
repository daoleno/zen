package watcher

import (
	"crypto/sha256"
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
	classifier.Agent,
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
	hostID := "brain-agent-brain-hidden:@host-capability"
	w.agents[hostID] = &classifier.Agent{
		ID: hostID, Command: "opencode", Cwd: "/repo/zen",
		PaneAlive: true, State: classifier.StateDone,
	}
	eventID := "event-exact"
	claimToken := "claim-exact"
	workID := "work-exact"
	providerTurnID := "provider-turn-exact"

	result, err := w.SubmitBrainHostInput(
		hostID, payload, eventID, claimToken, workID, providerTurnID, now,
	)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != providerTurnID {
		t.Fatalf("Host admission result=%+v err=%v", result, err)
	}
	submission, found, err := ledger.TurnSubmission(hostID, providerTurnID)
	if err != nil || !found || submission.State != TurnSubmissionResolved ||
		submission.Receipt != eventID || submission.ClaimToken != claimToken ||
		submission.WorkID != workID || submission.SessionID != hostID ||
		submission.ProposedTurnID != providerTurnID {
		t.Fatalf("canonical Host submission=%+v found=%v err=%v", submission, found, err)
	}
	if len(io.queues) != 1 || len(io.submissions) != 1 {
		t.Fatalf("Host input mutation count queues=%d submissions=%d, want one each", len(io.queues), len(io.submissions))
	}
}

// TestOpenCodeAmbiguousAdmissionPromotedByLiveProviderActivityAndSettlesOnce
// reproduces the observed failure: an initial delegated spawn is judged
// byte-mismatched (ambiguous) while OpenCode actually accepted the prompt and
// keeps working. The canonical transaction stays Pending outside brain_turns
// (never failed, never replayed); a later exact digest resolves it and only
// authoritative provider settlement may reach done, exactly once.
func TestOpenCodeAmbiguousAdmissionPromotedByLiveProviderActivityAndSettlesOnce(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	now := time.Now().UTC()
	taskDigest := fmt.Sprintf("%x", sha256.Sum256([]byte("task brief")))
	probe := &scriptedProviderActivityProbe{
		steps: []ProviderActivityObservation{
			// step 0: admission baseline — nothing admitted yet.
			{Structured: true, FallbackAllowed: true},
			// step 1: OpenCode admits the prompt but normalizes the bytes, so
			// the provider-admitted digest does not match the submitted UTF-8
			// payload: the admission is ambiguous, never replayed.
			{
				ID:              "act-admission",
				Status:          "running",
				StartedAt:       now.Add(time.Second),
				AdmissionStream: "opencode_db\x00ses_1\x00/db",
				AdmissionID:     "msg_user",
				AdmissionCursor: 1,
				AdmissionAt:     now.Add(time.Second),
				InputSHA256:     "not-the-submitted-payload-digest",
				Structured:      true,
			},
			// step 2: authoritative exact admission after the ambiguous return;
			// the pending ledger transaction resolves without replay.
			{
				ID: "act-turn", Status: "running", StartedAt: now.Add(2 * time.Second),
				AdmissionStream: "opencode_db\x00ses_1\x00/db", AdmissionID: "msg_user_exact",
				AdmissionCursor: 2, AdmissionAt: now.Add(2 * time.Second),
				InputSHA256: taskDigest, Structured: true,
			},
			// step 3: authoritative settlement.
			{ID: "act-turn", Status: "completed", StartedAt: now.Add(2 * time.Second), SettledAt: now.Add(20 * time.Second), Structured: true},
		},
	}
	w := lifecycleTestWatcher(io, ledger, probe)
	sessionID := "opencode-ambiguous:@1"
	w.agents[sessionID] = &classifier.Agent{
		ID:        sessionID,
		Command:   "opencode",
		Cwd:       "/repo/zen",
		PaneAlive: true,
		Delegated: true,
		State:     classifier.StateUnknown,
	}

	turnID := "opencode-ambiguous:@1:turn:1"
	result, err := w.SubmitDelegatedInput(sessionID, "task brief", turnID, now)
	if err == nil || result.Outcome != InputAmbiguous {
		t.Fatalf("spawn admission = (%+v, %v), want ambiguous", result, err)
	}
	if len(io.queues) != 1 || len(io.submissions) != 1 {
		t.Fatalf("ambiguous spawn replayed the prompt: queues=%d submissions=%d", len(io.queues), len(io.submissions))
	}
	turn, hasTurn, _ := ledger.Turn(sessionID)
	if hasTurn {
		t.Fatalf("ambiguous admission created a phantom current Turn: %+v", turn)
	}
	if pending, found, _ := ledger.PendingTurnSubmission(sessionID); !found || pending.State != TurnSubmissionPending {
		t.Fatalf("canonical pending submission = %+v found=%v", pending, found)
	}

	// A stale failed progress report must never terminalize the turn: the
	// canonical projection stays running.
	if _, err := w.UpdateAgentProgress(sessionID, classifier.AgentProgress{
		Status:    "failed",
		Phase:     "starting",
		Attention: "failed",
		Summary:   "Initial delegated prompt was not submitted: Session input outcome is unknown and will not be replayed",
		TaskClass: "lasting_design",
		EventKind: "risk",
	}); err != nil {
		t.Fatal(err)
	}
	agent := w.GetAgent(sessionID)
	if agent == nil || agent.State != classifier.StateRunning || agent.Attention != "none" ||
		agent.NeedsAttention || agent.Phase != "" || agent.TaskClass != "" ||
		agent.EventKind != "" || agent.LeaseSeconds != 0 {
		t.Fatalf("stale failed attempt survived a live canonical turn: %+v", agent)
	}
	if _, found, _ := ledger.Turn(sessionID); found {
		t.Fatal("control failed report promoted pending submission")
	}

	// The poll resolves the exact pending admission first, then the normal
	// reducer advances Accepted → Running; input is never replayed.
	pending, found, pendingErr := w.pendingTurnSubmission(sessionID)
	if pendingErr != nil {
		t.Fatal(pendingErr)
	}
	if !found {
		t.Fatal("pending submission disappeared before provider reconciliation")
	}
	provider := probe.next()
	if _, resolved := w.resolvePendingProviderAdmission(pending, provider, now.Add(3*time.Second)); !resolved {
		t.Fatal("exact pending provider admission did not resolve")
	}
	turn, hasTurn, _ = ledger.Turn(sessionID)
	if !hasTurn {
		t.Fatal("resolved pending submission did not promote a Turn")
	}
	turn = w.applyPollFacts(sessionID, true, -1, now.Add(3*time.Second), turn, provider)
	if turn.Status != TurnRunning {
		t.Fatalf("provider-native running did not promote the turn: %+v", turn)
	}
	agent = w.GetAgent(sessionID)
	state, _ := projectDelegatedTurn(agent, turn)
	if state != classifier.StateRunning {
		t.Fatalf("poll projection = %s", state)
	}

	// Authoritative settlement: provider completed plus settled evidence.
	turn = w.applyPollFacts(sessionID, true, -1, now.Add(21*time.Second), turn, probe.next())
	if turn.Status != TurnDone {
		t.Fatalf("authoritative settlement = %+v, want done", turn)
	}

	// The terminal turn is immutable: a later running observation must not
	// reopen it (the reducer ignores facts for terminal turns).
	applied := w.applyPollFacts(sessionID, true, -1, now.Add(26*time.Second), turn, ProviderActivityObservation{
		ID: "act-turn", Status: "running", StartedAt: now.Add(25 * time.Second), Structured: true,
	})
	if applied.Status != TurnDone {
		t.Fatalf("terminal turn reopened after settlement: %+v", applied)
	}
	state, _ = projectDelegatedTurn(w.GetAgent(sessionID), turn)
	if state != classifier.StateDone {
		t.Fatalf("terminal turn projection = %s, want done", state)
	}
}

// TestOpenCodeConfirmedFollowUpAfterAmbiguousAdmissionSteersExistingTurn
// verifies that a confirmed follow-up after an ambiguous admission establishes
// observable activity on the existing nonterminal turn (steering), does not
// replay, and the same turn settles exactly once.
func TestOpenCodeConfirmedFollowUpAfterAmbiguousAdmissionSteersExistingTurn(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	now := time.Now().UTC()
	firstDigest := fmt.Sprintf("%x", sha256.Sum256([]byte("first brief")))
	followDigest := fmt.Sprintf("%x", sha256.Sum256([]byte("follow-up")))
	probe := &scriptedProviderActivityProbe{
		steps: []ProviderActivityObservation{
			{Structured: true, FallbackAllowed: true},
			{
				ID: "act-first", Status: "running",
				StartedAt:       now.Add(time.Second),
				AdmissionStream: "opencode_db\x00ses_1\x00/db",
				AdmissionID:     "msg_first",
				AdmissionCursor: 1,
				InputSHA256:     "not-the-submitted-payload-digest",
				Structured:      true,
			},
			{
				ID: "act-first", Status: "running",
				StartedAt:       now.Add(time.Second),
				AdmissionStream: "opencode_db\x00ses_1\x00/db",
				AdmissionID:     "msg_first_exact", AdmissionCursor: 2,
				AdmissionAt: now.Add(time.Second), InputSHA256: firstDigest,
				Structured: true, FallbackAllowed: true,
			},
			{
				ID: "act-first", Status: "running",
				StartedAt:       now.Add(time.Second),
				AdmissionStream: "opencode_db\x00ses_1\x00/db",
				AdmissionID:     "msg_first_exact", AdmissionCursor: 2,
				AdmissionAt: now.Add(time.Second), InputSHA256: firstDigest,
				Structured: true,
			},
			{
				ID: "act-first", Status: "running", StartedAt: now.Add(time.Second),
				AdmissionStream: "opencode_db\x00ses_1\x00/db",
				AdmissionID:     "msg_followup",
				AdmissionCursor: 3,
				AdmissionAt:     now.Add(time.Minute + time.Second),
				InputSHA256:     followDigest,
				Structured:      true,
			},
			{ID: "act-first", Status: "running", StartedAt: now.Add(time.Second), Structured: true},
			{ID: "act-first", Status: "completed", StartedAt: now.Add(time.Second), SettledAt: now.Add(20 * time.Second), Structured: true},
		},
	}
	w := lifecycleTestWatcher(io, ledger, probe)
	sessionID := "opencode-followup:@2"
	w.agents[sessionID] = &classifier.Agent{
		ID: sessionID, Command: "opencode", Cwd: "/repo/zen",
		PaneAlive: true, Delegated: true, State: classifier.StateUnknown,
	}

	firstTurn := "opencode-followup:@2:turn:1"
	result, err := w.SubmitDelegatedInput(sessionID, "first brief", firstTurn, now)
	if err == nil || result.Outcome != InputAmbiguous {
		t.Fatalf("first admission = (%+v, %v), want ambiguous", result, err)
	}
	if _, hasTurn, _ := ledger.Turn(sessionID); hasTurn {
		t.Fatal("ambiguous first admission created a phantom Turn")
	}
	// Retry the same receipt only reconciles exact provider evidence; it does
	// not replay the first input.
	result, err = w.SubmitDelegatedInput(sessionID, "first brief", firstTurn, now)
	if err != nil || !result.Duplicate || result.TurnID != firstTurn || len(io.queues) != 1 {
		t.Fatalf("pending first admission reconciliation = (%+v, %v), queues=%d", result, err, len(io.queues))
	}

	// A confirmed follow-up steers into the existing nonterminal turn; the
	// same canonical turn settles exactly once.
	followTurn := "opencode-followup:@2:turn:2"
	result, err = w.SubmitDelegatedInput(sessionID, "follow-up", followTurn, now.Add(time.Minute))
	if err != nil || result.Outcome != InputAccepted || result.TurnID != firstTurn {
		t.Fatalf("follow-up admission = (%+v, %v), want steering into %s", result, err, firstTurn)
	}
	if len(io.queues) != 2 {
		t.Fatalf("follow-up replayed: queues=%d", len(io.queues))
	}
	turn, _, _ := ledger.Turn(sessionID)
	if turn.TurnID != firstTurn {
		t.Fatalf("follow-up created a competing lifecycle: %+v", turn)
	}

	turn = w.applyPollFacts(sessionID, true, -1, now.Add(3*time.Second), turn, probe.next())
	if turn.Status != TurnRunning {
		t.Fatalf("follow-up activity did not promote: %+v", turn)
	}
	turn = w.applyPollFacts(sessionID, true, -1, now.Add(21*time.Second), turn, probe.next())
	if turn.Status != TurnDone {
		t.Fatalf("settlement = %+v, want done", turn)
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
	w.agents[sessionID] = &classifier.Agent{
		ID: sessionID, Command: "opencode", Cwd: "/repo/zen",
		PaneAlive: true, Delegated: true, State: classifier.StateUnknown,
	}
	turnID := "opencode-noevidence:@3:turn:1"
	result, err := w.SubmitDelegatedInput(sessionID, "task brief", turnID, now)
	if err == nil || result.Outcome != InputAmbiguous {
		t.Fatalf("spawn admission = (%+v, %v), want ambiguous", result, err)
	}
	if turn, found, _ := ledger.Turn(sessionID); found {
		t.Fatalf("ambiguous admission without evidence created a Turn: %+v", turn)
	}
	if pending, found, _ := ledger.PendingTurnSubmission(sessionID); !found || pending.State != TurnSubmissionPending {
		t.Fatalf("pending submission = %+v found=%v", pending, found)
	}
	if _, err := w.RebindDelegatedTurnProjection(sessionID); err != nil {
		t.Fatal(err)
	}
	if agent := w.GetAgent(sessionID); agent == nil || agent.State != classifier.StateRunning {
		t.Fatalf("pending projection = %+v, want running", agent)
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
	w.agents[sessionID] = &classifier.Agent{
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
	sessionID := "brain-agent-opencode:@8174"
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
	w.agents[sessionID] = &classifier.Agent{
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
	if pending, found, _ := ledger.PendingTurnSubmission(sessionID); !found || pending.ProposedTurnID != followTurn {
		t.Fatalf("fresh pending candidate = %+v found=%v", pending, found)
	}

	// Provider evidence for normalized/different bytes cannot claim the
	// pending payload, whether running or terminal.
	pending, _, pendingErr := w.pendingTurnSubmission(sessionID)
	if pendingErr != nil {
		t.Fatal(pendingErr)
	}
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
	agent := &classifier.Agent{Attention: "failed", NeedsAttention: true}
	for _, test := range []struct {
		status TurnStatus
		want   classifier.AgentState
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
		state, _ := projectDelegatedTurn(agent, turn)
		if state != test.want {
			t.Fatalf("status %s projected %s, want %s", test.status, state, test.want)
		}
	}
	if agent.Attention != "none" || agent.NeedsAttention {
		t.Fatalf("non-blocked projection retained attention: %+v", agent)
	}
	blocked := TurnSnapshot{Status: TurnBlocked}
	projectDelegatedTurn(agent, blocked)
	if agent.Attention != "user_input" || !agent.NeedsAttention {
		t.Fatalf("blocked projection attention = %+v", agent)
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
