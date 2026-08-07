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
// consumed by the delegated-turn observation loop.
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

// TestOpenCodeAmbiguousAdmissionPromotedByLiveProviderActivityAndSettlesOnce
// reproduces the observed failure: an initial delegated spawn is judged
// byte-mismatched (ambiguous) while OpenCode actually accepted the prompt and
// keeps working. A stale failed attempt must not terminalize the Session: live
// provider activity promotes it back to running, clears the failed-attempt
// metadata, and only the authoritative turn settlement may reach done.
func TestOpenCodeAmbiguousAdmissionPromotedByLiveProviderActivityAndSettlesOnce(t *testing.T) {
	io := newFakeSessionInputIO()
	now := time.Now().UTC()
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
			// step 2: authoritative provider activity for the accepted turn.
			{ID: "act-turn", Status: "running", StartedAt: now.Add(2 * time.Second), Structured: true},
			// step 3: authoritative settlement.
			{ID: "act-turn", Status: "completed", StartedAt: now.Add(2 * time.Second), SettledAt: now.Add(20 * time.Second), Structured: true},
		},
	}
	w := watcherWithAdmissionProbe(probe)
	w.sessionInput = newSessionInputOwner(io)
	w.targetCommandResolver = func(string) (string, bool) { return "opencode", true }
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
	if !io.hasTurn || io.turn.Status != delegatedTurnAmbiguous {
		t.Fatalf("durable turn after ambiguous admission = %+v, want ambiguous marker", io.turn)
	}

	// The watcher poll picks up the durable ambiguous marker (production
	// timing: one poll after the admission wait releases the serialized owner).
	w.delegatedTurns[sessionID] = io.turn

	// Historical control-plane behavior: the spawn reports the stale failed
	// attempt while the provider Session is actually live.
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
		agent.EventKind != "" || agent.LastProgressAt != nil || agent.LeaseSeconds != 0 {
		t.Fatalf("stale failed attempt survived a live turn: %+v", agent)
	}

	// Concurrently observe provider-native running and replay the stale
	// failed progress; both go through the serialized Session input owner.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _, err := w.sessionInputOwner().observeDelegatedTurn(sessionID, turnID, delegatedTurnObservation{
			Provider: probe.next(),
			Live:     true,
			Now:      now.Add(3 * time.Second),
		}, w.targetForSession)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := w.UpdateAgentProgress(sessionID, classifier.AgentProgress{
			Status:    "failed",
			Phase:     "starting",
			Attention: "failed",
			Summary:   "stale failed admission attempt",
		})
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	turn, _, _ := io.delegatedTurn("%9")
	if turn.Status != delegatedTurnRunning {
		t.Fatalf("provider-native running did not promote the turn: %+v", turn)
	}
	agent = w.GetAgent(sessionID)
	if agent == nil || agent.State != classifier.StateRunning || agent.Attention != "none" || agent.Phase != "" {
		t.Fatalf("live turn retained failed attempt: %+v", agent)
	}
	state, summary := applyLiveTurnProjection(agent, turn, classifier.StateUnknown, "")
	if state != classifier.StateRunning || summary != "Delegated turn running" {
		t.Fatalf("poll projection = (%s, %q)", state, summary)
	}

	// Authoritative settlement: provider completed plus settled evidence.
	if _, _, err := w.sessionInputOwner().observeDelegatedTurn(sessionID, turnID, delegatedTurnObservation{
		Provider: probe.next(),
		Live:     true,
		Now:      now.Add(21 * time.Second),
	}, w.targetForSession); err != nil {
		t.Fatal(err)
	}
	turn, _, _ = io.delegatedTurn("%9")
	if turn.Status != delegatedTurnDone || turn.SettledAt == nil {
		t.Fatalf("authoritative settlement = %+v, want done with settled_at", turn)
	}

	// The terminal turn is immutable: later activity must not reopen it.
	settledTurn := turn
	if _, _, err := w.sessionInputOwner().observeDelegatedTurn(sessionID, turnID, delegatedTurnObservation{
		Provider: ProviderActivityObservation{ID: "act-turn", Status: "running", StartedAt: now.Add(25 * time.Second), Structured: true},
		Live:     true,
		Now:      now.Add(26 * time.Second),
	}, w.targetForSession); err != nil {
		t.Fatal(err)
	}
	after, _, _ := io.delegatedTurn("%9")
	if after != settledTurn {
		t.Fatalf("terminal turn reopened after settlement: before=%+v after=%+v", settledTurn, after)
	}
	state, _ = applyLiveTurnProjection(w.GetAgent(sessionID), turn, classifier.StateDone, turn.Summary)
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
	now := time.Now().UTC()
	followUpPayload := "confirmed follow-up"
	followUpDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(followUpPayload)))
	probe := &scriptedProviderActivityProbe{
		steps: []ProviderActivityObservation{
			{Structured: true, FallbackAllowed: true},
			{
				ID: "act-admission", Status: "running", StartedAt: now.Add(time.Second),
				AdmissionStream: "opencode_db\x00ses_2\x00/db",
				AdmissionID:     "msg_user", AdmissionCursor: 1, AdmissionAt: now.Add(time.Second),
				InputSHA256: "wrong-digest", Structured: true,
			},
			{ID: "act-turn", Status: "running", StartedAt: now.Add(2 * time.Second), Structured: true},
			{Structured: true, FallbackAllowed: true},
			{
				ID: "act-followup", Status: "running", StartedAt: now.Add(6 * time.Second),
				AdmissionStream: "opencode_db\x00ses_2\x00/db",
				AdmissionID:     "msg_followup", AdmissionCursor: 2, AdmissionAt: now.Add(6 * time.Second),
				InputSHA256: followUpDigest, Structured: true,
			},
			{ID: "act-turn", Status: "completed", StartedAt: now.Add(2 * time.Second), SettledAt: now.Add(40 * time.Second), Structured: true},
		},
	}
	w := watcherWithAdmissionProbe(probe)
	w.sessionInput = newSessionInputOwner(io)
	w.targetCommandResolver = func(string) (string, bool) { return "opencode", true }
	sessionID := "opencode-followup:@1"
	w.agents[sessionID] = &classifier.Agent{
		ID: sessionID, Command: "opencode", Cwd: "/repo/zen", PaneAlive: true, Delegated: true,
		State: classifier.StateRunning,
	}

	initialTurn := "opencode-followup:@1:turn:initial"
	if result, err := w.SubmitDelegatedInput(sessionID, "initial brief", initialTurn, now); err == nil || result.Outcome != InputAmbiguous {
		t.Fatalf("initial admission = (%+v, %v), want ambiguous", result, err)
	}
	w.delegatedTurns[sessionID] = io.turn
	if _, _, err := w.sessionInputOwner().observeDelegatedTurn(sessionID, initialTurn, delegatedTurnObservation{
		Provider: probe.next(), Live: true, Now: now.Add(3 * time.Second),
	}, w.targetForSession); err != nil {
		t.Fatal(err)
	}

	followTurn := "opencode-followup:@1:turn:followup"
	result, err := w.SubmitDelegatedInput(sessionID, followUpPayload, followTurn, now.Add(5*time.Second))
	if err != nil || result.Outcome != InputAccepted {
		t.Fatalf("confirmed follow-up = (%+v, %v), want accepted", result, err)
	}
	if result.TurnID != initialTurn {
		t.Fatalf("confirmed follow-up created a new turn identity: %q, want steering on %q", result.TurnID, initialTurn)
	}
	if len(io.queues) != 2 || len(io.submissions) != 2 {
		t.Fatalf("follow-up queues/submissions = %d/%d, want 2/2 without replay", len(io.queues), len(io.submissions))
	}
	turn, _, _ := io.delegatedTurn("%9")
	if turn.ID != initialTurn || turn.Status != delegatedTurnRunning {
		t.Fatalf("follow-up did not steer the existing turn: %+v", turn)
	}
	if _, _, err := w.sessionInputOwner().observeDelegatedTurn(sessionID, initialTurn, delegatedTurnObservation{
		Provider: probe.next(), Live: true, Now: now.Add(41 * time.Second),
	}, w.targetForSession); err != nil {
		t.Fatal(err)
	}
	turn, _, _ = io.delegatedTurn("%9")
	if turn.Status != delegatedTurnDone {
		t.Fatalf("follow-up turn did not settle: %+v", turn)
	}
}

// TestOpenCodeAmbiguousAdmissionNoProviderEvidenceFailsOnceAtStartTimeout
// keeps the bounded fail-closed path: no provider activity after an ambiguous
// admission eventually fails the turn exactly once.
func TestOpenCodeAmbiguousAdmissionNoProviderEvidenceFailsOnceAtStartTimeout(t *testing.T) {
	io := newFakeSessionInputIO()
	now := time.Now().UTC()
	probe := &scriptedProviderActivityProbe{
		steps: []ProviderActivityObservation{
			{Structured: true, FallbackAllowed: true},
			{
				ID: "act-admission", Status: "running", StartedAt: now.Add(time.Second),
				AdmissionStream: "opencode_db\x00ses_3\x00/db",
				AdmissionID:     "msg_user", AdmissionCursor: 1, AdmissionAt: now.Add(time.Second),
				InputSHA256: "wrong-digest", Structured: true,
			},
			{Structured: true, FallbackAllowed: true},
		},
	}
	w := watcherWithAdmissionProbe(probe)
	w.sessionInput = newSessionInputOwner(io)
	w.targetCommandResolver = func(string) (string, bool) { return "opencode", true }
	sessionID := "opencode-timeout:@1"
	w.agents[sessionID] = &classifier.Agent{
		ID: sessionID, Command: "opencode", Cwd: "/repo/zen", PaneAlive: true, Delegated: true,
		State: classifier.StateRunning,
	}
	turnID := "opencode-timeout:@1:turn:1"
	if result, err := w.SubmitDelegatedInput(sessionID, "brief", turnID, now); err == nil || result.Outcome != InputAmbiguous {
		t.Fatalf("initial admission = (%+v, %v), want ambiguous", result, err)
	}
	turn, changed, err := w.sessionInputOwner().observeDelegatedTurn(sessionID, turnID, delegatedTurnObservation{
		Provider:     ProviderActivityObservation{Structured: true},
		Live:         true,
		Now:          now.Add(15 * time.Second),
		StartTimeout: 15 * time.Second,
	}, w.targetForSession)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || turn.Status != delegatedTurnFailed {
		t.Fatalf("no-evidence ambiguous admission = (%+v, %v), want failed exactly once", turn, changed)
	}
	settled := turn
	if _, _, err := w.sessionInputOwner().observeDelegatedTurn(sessionID, turnID, delegatedTurnObservation{
		Provider: ProviderActivityObservation{ID: "act-late", Status: "running", StartedAt: now.Add(20 * time.Second), Structured: true},
		Live:     true,
		Now:      now.Add(21 * time.Second),
	}, w.targetForSession); err != nil {
		t.Fatal(err)
	}
	after, _, _ := io.delegatedTurn("%9")
	if after != settled {
		t.Fatalf("late provider activity reopened terminal turn: before=%+v after=%+v", settled, after)
	}
}

// TestOpenCodeFollowUpTurnNotTerminalizedByStaleCompletedProviderActivity
// reproduces the reported failure on f461215: after a follow-up turn was
// accepted, the provider observation still carried the previous turn's
// completed Activity (the reader cache reused the old Activity while new rows
// were only in the SQLite WAL), and the quiet prompt-like TUI frame then
// settled the new accepted turn via the generic quiet-window fallback. A
// stale structured provider fact must never terminalize a newer accepted
// turn; only an authoritative Activity for that turn may, exactly once.
func TestOpenCodeFollowUpTurnNotTerminalizedByStaleCompletedProviderActivity(t *testing.T) {
	io := newFakeSessionInputIO()
	now := time.Now().UTC()
	identity := testSessionInputIdentity("opencode")
	resolver := fixedSessionInputResolver(identity)
	owner := newSessionInputOwner(io)
	sessionID := "opencode-followup-stale:@1"
	io.hasTurn = true
	io.turn = delegatedTurnRecord{
		SchemaVersion:    delegatedTurnSchema,
		ID:               "turn:followup",
		Status:           delegatedTurnRunning,
		AcceptedAt:       now.Add(-time.Second),
		ProcessIdentity:  delegatedTurnIdentity(identity),
		PaneBaseline:     "baseline",
		ProviderActivity: "act-new",
	}
	stale := ProviderActivityObservation{
		ID:              "act-old-turn",
		Status:          "completed",
		StartedAt:       now.Add(-2 * time.Minute),
		SettledAt:       now.Add(-time.Minute),
		Structured:      true,
		FallbackAllowed: true,
	}
	quietPromptFrame := classifier.ActivitySignal{State: classifier.StateUnknown, Source: "generic_pane_stable"}
	// Repeated polls with the stale completed Activity and a quiet prompt-like
	// pane frame, the second far past the generic quiet window: the new
	// accepted turn must hold running, never idle/settled.
	for _, at := range []time.Time{now, now.Add(45 * time.Second)} {
		turn, found, err := owner.observeDelegatedTurn(sessionID, "turn:followup", delegatedTurnObservation{
			Provider: stale,
			Pane:     quietPromptFrame,
			Live:     true,
			Now:      at,
		}, resolver)
		if err != nil {
			t.Fatal(err)
		}
		if !found || turn.Status != delegatedTurnRunning || turn.IdleSince != nil {
			t.Fatalf("stale completed Activity terminalized/idled the accepted turn: %+v", turn)
		}
	}
	// The new turn's authoritative running Activity correlates it.
	turn, _, err := owner.observeDelegatedTurn(sessionID, "turn:followup", delegatedTurnObservation{
		Provider: ProviderActivityObservation{ID: "act-new", Status: "running", StartedAt: now.Add(2 * time.Second), Structured: true},
		Pane:     quietPromptFrame,
		Live:     true,
		Now:      now.Add(3 * time.Second),
	}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Status != delegatedTurnRunning || turn.ProviderActivity != "act-new" {
		t.Fatalf("authoritative running did not correlate the follow-up turn: %+v", turn)
	}
	// Authoritative settlement: exactly one done for the new turn.
	turn, _, err = owner.observeDelegatedTurn(sessionID, "turn:followup", delegatedTurnObservation{
		Provider: ProviderActivityObservation{ID: "act-new", Status: "completed", StartedAt: now.Add(2 * time.Second), SettledAt: now.Add(10 * time.Second), Structured: true},
		Pane:     quietPromptFrame,
		Live:     true,
		Now:      now.Add(11 * time.Second),
	}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Status != delegatedTurnDone || turn.SettledAt == nil {
		t.Fatalf("authoritative settlement = %+v, want done exactly once", turn)
	}
	settled := turn
	// Duplicate settlement and stale observations stay immutable.
	if _, _, err := owner.observeDelegatedTurn(sessionID, "turn:followup", delegatedTurnObservation{
		Provider: stale,
		Pane:     quietPromptFrame,
		Live:     true,
		Now:      now.Add(20 * time.Second),
	}, resolver); err != nil {
		t.Fatal(err)
	}
	after, _, _ := io.delegatedTurn("%9")
	if after != settled {
		t.Fatalf("terminal follow-up turn mutated: before=%+v after=%+v", settled, after)
	}
}
