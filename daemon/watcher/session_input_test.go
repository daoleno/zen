package watcher

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

type fakeSessionInputIO struct {
	mu           sync.Mutex
	sockets      []string
	queueSockets []string
	paneValue    sessionInputPane
	// paneIDView models App Terminal link-window resolution: when set,
	// pane("%pane_id") returns this value while pane(sessionID) keeps
	// paneValue. Both must share pane_id/generation; session fields may differ.
	paneIDView          *sessionInputPane
	paneCalls           []string
	postLedgerPaneCalls []string
	recordingPostLedger bool
	buffers             map[string]string
	loadedPayloads      []string
	queues              [][]string
	submissions         []string
	ledger              sessionInputReceiptLedger
	ledgerWrites        []sessionInputReceiptLedger
	writeErrors         map[int]error
	ledgerReads         int
	readErrors          map[int]error
	operations          []string
	runStarted          bool
	runErr              error
	startedQueues       int
	afterLoad           func()
	afterLedgerWrite    func()
	activeQueues        int
	maxQueues           int
	paneContentValue    string
}

type transportAdmissionProbe struct {
	mu        sync.Mutex
	io        *fakeSessionInputIO
	inputs    map[int]string
	missing   map[int]bool
	staleAt   map[int]time.Time
	fixedStep *int
	steps     []int
}

func (p *transportAdmissionProbe) ObserveProviderActivity(
	classifier.Agent,
	time.Time,
) ProviderActivityObservation {
	step := 0
	if p.fixedStep != nil {
		step = *p.fixedStep
	} else if p.io != nil {
		p.io.mu.Lock()
		step = p.io.startedQueues
		p.io.mu.Unlock()
	}
	p.mu.Lock()
	p.steps = append(p.steps, step)
	p.mu.Unlock()
	input := p.inputs[step]
	digest := ""
	if !p.missing[step] {
		digest = fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
	}
	at := time.Now().UTC().Add(time.Minute)
	if stale := p.staleAt[step]; !stale.IsZero() {
		at = stale
	}
	return ProviderActivityObservation{
		ID:              fmt.Sprintf("activity-%d", step),
		AdmissionStream: "cursor-session",
		AdmissionID:     fmt.Sprintf("cursor-user-%d", step),
		AdmissionCursor: uint64(step + 1),
		AdmissionAt:     at,
		InputSHA256:     digest,
		Structured:      true,
	}
}

func (*transportAdmissionProbe) ForgetProviderActivity(string) {}

func newFakeSessionInputIO() *fakeSessionInputIO {
	return &fakeSessionInputIO{
		paneValue: sessionInputPane{
			alive:      true,
			paneID:     "%9",
			generation: "generation-1",
		},
		buffers:     make(map[string]string),
		ledger:      emptySessionInputReceiptLedger(),
		writeErrors: make(map[int]error),
		readErrors:  make(map[int]error),
	}
}

func (io *fakeSessionInputIO) socket(sessionID string) string {
	io.mu.Lock()
	defer io.mu.Unlock()
	return ""
}

func (io *fakeSessionInputIO) pane(socket, target string) sessionInputPane {
	io.mu.Lock()
	defer io.mu.Unlock()
	io.paneCalls = append(io.paneCalls, target)
	if io.recordingPostLedger {
		io.postLedgerPaneCalls = append(io.postLedgerPaneCalls, target)
	}
	if strings.HasPrefix(strings.TrimSpace(target), "%") && io.paneIDView != nil {
		return *io.paneIDView
	}
	return io.paneValue
}

func (io *fakeSessionInputIO) loadBuffer(socket, buffer, payload string) error {
	io.mu.Lock()
	io.sockets = append(io.sockets, socket)
	io.mu.Unlock()
	io.mu.Lock()
	io.buffers[buffer] = payload
	io.loadedPayloads = append(io.loadedPayloads, payload)
	afterLoad := io.afterLoad
	io.mu.Unlock()
	if afterLoad != nil {
		afterLoad()
	}
	return nil
}

func (io *fakeSessionInputIO) deleteBuffer(socket, buffer string) {
	io.mu.Lock()
	delete(io.buffers, buffer)
	io.mu.Unlock()
}

func (io *fakeSessionInputIO) runQueue(socket string, args []string) (bool, error) {
	io.mu.Lock()
	io.queueSockets = append(io.queueSockets, socket)
	io.mu.Unlock()
	io.mu.Lock()
	io.activeQueues++
	if io.activeQueues > io.maxQueues {
		io.maxQueues = io.activeQueues
	}
	copied := append([]string(nil), args...)
	io.queues = append(io.queues, copied)
	io.operations = append(io.operations, "provider_queue")
	buffer := queueArgumentAfter(args, "-b")
	submission := io.buffers[buffer]
	if indexOf(args, "-r") < 0 {
		// tmux paste-buffer replaces LF with CR unless -r is present. In an
		// interactive composer those bytes are submit keys, not payload bytes.
		submission = strings.ReplaceAll(submission, "\n", "\r")
	}
	io.submissions = append(io.submissions, submission)
	started, err := io.runStarted, io.runErr
	if err == nil || started {
		io.startedQueues++
	}
	io.activeQueues--
	io.mu.Unlock()
	return started, err
}

func (io *fakeSessionInputIO) receiptLedger(socket, target string) (sessionInputReceiptLedger, error) {
	io.mu.Lock()
	defer io.mu.Unlock()
	io.ledgerReads++
	if err := io.readErrors[io.ledgerReads]; err != nil {
		return sessionInputReceiptLedger{}, err
	}
	return cloneSessionInputReceiptLedger(io.ledger), nil
}

func (io *fakeSessionInputIO) writeReceiptLedger(socket, _ string, ledger sessionInputReceiptLedger) error {
	io.mu.Lock()
	call := len(io.ledgerWrites) + 1
	io.ledgerWrites = append(io.ledgerWrites, cloneSessionInputReceiptLedger(ledger))
	io.operations = append(io.operations, "ledger_write")
	if err := io.writeErrors[call]; err != nil {
		io.mu.Unlock()
		return err
	}
	io.ledger = cloneSessionInputReceiptLedger(ledger)
	io.recordingPostLedger = true
	afterLedgerWrite := io.afterLedgerWrite
	io.mu.Unlock()
	if afterLedgerWrite != nil {
		afterLedgerWrite()
	}
	return nil
}

func (io *fakeSessionInputIO) paneContent(socket, target string) (string, error) {
	io.mu.Lock()
	defer io.mu.Unlock()
	return io.paneContentValue, nil
}

func cloneSessionInputReceiptLedger(ledger sessionInputReceiptLedger) sessionInputReceiptLedger {
	return sessionInputReceiptLedger{
		SchemaVersion: ledger.SchemaVersion,
		Entries:       append([]sessionInputReceiptEntry(nil), ledger.Entries...),
	}
}

func queueArgumentAfter(args []string, flag string) string {
	for index := range args {
		if args[index] == flag && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}

func indexOf(values []string, wanted string) int {
	for index, value := range values {
		if value == wanted {
			return index
		}
	}
	return -1
}

func testSessionInputIdentity(command string) targetProcessIdentity {
	return targetProcessIdentity{
		Command:         command,
		PanePID:         10,
		PaneStart:       20,
		ForegroundID:    30,
		ForegroundStart: 40,
		ProcessID:       50,
		ProcessStart:    60,
	}
}

func fixedSessionInputResolver(identity targetProcessIdentity) func(string) (targetProcessIdentity, bool) {
	return func(string) (targetProcessIdentity, bool) { return identity, true }
}

func scriptedCorrelatedAdmission(payload string) delegatedInputConfirmer {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	return delegatedInputConfirmer{
		baseline: func() (delegatedAdmissionEvidence, error) {
			return delegatedAdmissionEvidence{Stream: "test", ID: "before", Cursor: 1}, nil
		},
		confirm: func(
			_ delegatedAdmissionEvidence,
			mutationBoundary time.Time,
			payloadSHA256 string,
		) (delegatedInputConfirmation, error) {
			if mutationBoundary.IsZero() || payloadSHA256 != digest {
				return delegatedInputConfirmation{Outcome: InputAmbiguous},
					errors.New("test admission was not correlated")
			}
			return delegatedInputConfirmation{
				Outcome:          InputAccepted,
				ProviderActivity: "activity-accepted",
				Admission: delegatedAdmissionEvidence{
					Stream:      "test",
					ID:          "after",
					Cursor:      2,
					StartedAt:   mutationBoundary.Add(time.Second),
					InputSHA256: digest,
				},
			}, nil
		},
	}
}

func scriptedAmbiguousAdmission() delegatedInputConfirmer {
	return delegatedInputConfirmer{
		baseline: func() (delegatedAdmissionEvidence, error) {
			return delegatedAdmissionEvidence{Stream: "test", ID: "before", Cursor: 1}, nil
		},
		confirm: func(
			delegatedAdmissionEvidence,
			time.Time,
			string,
		) (delegatedInputConfirmation, error) {
			return delegatedInputConfirmation{Outcome: InputAmbiguous},
				errors.New("provider admission not observed")
		},
	}
}

func watcherWithAdmissionProbe(probe ProviderActivityProbe) *Watcher {
	w := New(time.Second)
	w.agents["agent:@1"] = &classifier.Agent{
		ID:        "agent:@1",
		Command:   "cursor-agent --force",
		Cwd:       "/repo/zen",
		PaneAlive: true,
	}
	w.providerActivityProbe = probe
	w.admissionTimeout = func(string) time.Duration { return 0 }
	return w
}

func newLedgerSessionInputOwner(io sessionInputIO, ledger TurnLedger) *sessionInputOwner {
	owner := newSessionInputOwner(io)
	owner.ledger = ledger
	return owner
}

// testTurnDraft builds the pre-dispatch turn draft used by the admission
// boundary tests.
func testTurnDraft(id string, acceptedAt time.Time, identity targetProcessIdentity) delegatedTurnDraft {
	return delegatedTurnDraft{
		ID:              id,
		AcceptedAt:      acceptedAt,
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
}

// fakeTurnLedger is the minimal canonical-turn ledger used by the Session
// input tests. It implements the frozen reducer contract (admission →
// Accepted, provider running → Running, terminal → Done/Failed, terminal
// turns immutable) so the submission boundary can be tested without the
// Brain store; the full transition table is tested in daemon/brain.
type fakeTurnLedger struct {
	mu       sync.Mutex
	turns    map[string]TurnSnapshot
	admitted map[string]AdmittedTurn
	applied  []TurnFact
	admitErr error
}

func newFakeTurnLedger() *fakeTurnLedger {
	return &fakeTurnLedger{
		turns:    map[string]TurnSnapshot{},
		admitted: map[string]AdmittedTurn{},
	}
}

func (l *fakeTurnLedger) Turn(sessionID string) (TurnSnapshot, bool, error) {
	if l == nil {
		return TurnSnapshot{}, false, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	turn, ok := l.turns[sessionID]
	return turn, ok, nil
}

func (l *fakeTurnLedger) AdmitTurn(admitted AdmittedTurn) error {
	if l == nil {
		return fmt.Errorf("ledger unavailable")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.admitErr != nil {
		return l.admitErr
	}
	if existing, exists := l.turns[admitted.SessionID]; exists {
		// Steering into a live nonterminal turn is handled before AdmitTurn;
		// a terminal turn (session reuse) is superseded by the new turn.
		if !TurnTerminal(existing.Status) {
			return nil
		}
	}
	l.admitted[admitted.SessionID] = admitted
	l.turns[admitted.SessionID] = TurnSnapshot{
		SessionID:      admitted.SessionID,
		TurnID:         admitted.TurnID,
		Status:         TurnAdmitted,
		AcceptedAt:     admitted.AcceptedAt,
		PaneGeneration: admitted.PaneGeneration,
	}
	return nil
}

func (l *fakeTurnLedger) ApplyTurnFact(fact TurnFact) (TurnSnapshot, bool, error) {
	if l == nil {
		return TurnSnapshot{}, false, fmt.Errorf("ledger unavailable")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.applied = append(l.applied, fact)
	turn, ok := l.turns[fact.SessionID]
	if !ok || turn.TurnID != fact.TurnID || TurnTerminal(turn.Status) {
		return turn, false, nil
	}
	changed := false
	switch fact.Class {
	case EvidenceReceipt:
		if fact.Kind == "admission" && turn.Status == TurnAdmitted {
			turn.Status = TurnAccepted
			turn.Admission = fact.Admission
			turn.HasAdmission = true
			turn.ActivityID = fact.ActivityID
			changed = true
		}
	case EvidenceControl:
		switch fact.Kind {
		case "running":
			if turn.Status == TurnAccepted || turn.Status == TurnRunning {
				if fact.Summary != "" {
					turn.Summary = fact.Summary
				}
				changed = true
			}
		}
	case EvidenceProvider:
		switch fact.Kind {
		case "running":
			if turn.Status == TurnAdmitted || turn.Status == TurnAccepted || turn.Status == TurnBlocked {
				// Admission uncertainty reconciliation: an Admitted/Accepted
				// turn adopts the provider's newest observation (window gate
				// is enforced by the poll fact builders; the Brain reducer
				// enforces it in the real store).
				if !turn.HasAdmission && !fact.Admission.Empty() {
					turn.Admission = fact.Admission
					turn.HasAdmission = true
				}
				turn.Status = TurnRunning
				turn.ActivityID = firstNonEmptyString(turn.ActivityID, fact.ActivityID)
				changed = true
			}
		case "done", "failed":
			// The frozen binding gate: terminal facts apply only when they
			// carry the recorded admission tuple with a monotone cursor, or
			// prove the turn's own activity identity, or adopt an Admitted
			// turn from inside its admission window. Unbound terminals stay
			// unapplied (the real reducer attaches them as hints).
			bound := false
			if !turn.Admission.Empty() {
				bound = fact.Admission.Stream == turn.Admission.Stream &&
					fact.Admission.ID != "" &&
					fact.Admission.Cursor >= turn.Admission.Cursor &&
					(turn.Admission.SHA256 == "" || fact.Admission.SHA256 == turn.Admission.SHA256)
			}
			if !bound && turn.ActivityID != "" && fact.ActivityID == turn.ActivityID {
				bound = true
			}
			if !bound && !turn.HasAdmission &&
				!fact.StartedAt.IsZero() && !fact.StartedAt.Before(turn.AcceptedAt) &&
				(fact.Admission.ID != "" || fact.ActivityID != "") {
				bound = true
			}
			if bound {
				if fact.Kind == "done" {
					turn.Status = TurnDone
				} else {
					turn.Status = TurnFailed
				}
				changed = true
			}
		}
	case EvidenceLiveness:
		turn.Status = TurnUnknown
		changed = true
	}
	if changed {
		turn.Summary = fact.Summary
		l.turns[fact.SessionID] = turn
	}
	return turn, changed, nil
}

func (l *fakeTurnLedger) snapshot(sessionID string) TurnSnapshot {
	turn, _, _ := l.Turn(sessionID)
	return turn
}

func (l *fakeTurnLedger) seed(sessionID string, turn TurnSnapshot) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.turns[sessionID] = turn
}

func TestSessionInputProviderQueueUsesCanonicalSubmitDelay(t *testing.T) {
	tests := []struct {
		command string
		settle  string
	}{
		{command: "codex --no-alt-screen", settle: "sleep 0.120"},
		{command: "claude --permission-mode bypassPermissions", settle: "sleep 0.250"},
		{command: "cursor-agent --force", settle: "sleep 2.000"},
		{command: "grok --no-alt-screen", settle: "sleep 0.300"},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			io := newFakeSessionInputIO()
			owner := newSessionInputOwner(io)
			identity := testSessionInputIdentity(test.command)
			payload := "你好, providers 👋\nline two\n\n"

			result, err := owner.submit("agent:@1", identity, fixedSessionInputResolver(identity), test.command, payload, "")
			if err != nil || result.Outcome != InputAccepted {
				t.Fatalf("submit = (%+v, %v), want accepted", result, err)
			}
			if !reflect.DeepEqual(io.loadedPayloads, []string{payload}) ||
				!reflect.DeepEqual(io.submissions, []string{payload}) {
				t.Fatalf("payload changed: loaded=%q submitted=%q", io.loadedPayloads, io.submissions)
			}
			if len(io.queues) != 1 {
				t.Fatalf("queues = %d, want one", len(io.queues))
			}
			assertSingleSubmitQueue(t, io.queues[0], "Enter")
			if indexOf(io.queues[0], test.settle) < 0 {
				t.Fatalf("queue %q does not contain provider settle %q", io.queues[0], test.settle)
			}
			if strings.HasPrefix(test.command, "cursor-agent") {
				clearIndex := indexOf(io.queues[0], "C-u")
				prepareIndex := indexOf(io.queues[0], "sleep 0.400")
				pasteIndex := indexOf(io.queues[0], "paste-buffer")
				if clearIndex < 0 || prepareIndex <= clearIndex || pasteIndex <= prepareIndex {
					t.Fatalf("Cursor clear/prepare/paste order = %q", io.queues[0])
				}
			}
		})
	}
}

func assertSingleSubmitQueue(t *testing.T, queue []string, submitKey string) {
	t.Helper()
	joined := strings.Join(queue, "\x00")
	for _, required := range []string{"send-keys\x00-t\x00%9\x00C-u", "paste-buffer", "delete-buffer"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("queue %q missing %q", queue, required)
		}
	}
	count := 0
	for index, value := range queue {
		if value == submitKey && index > 0 && queue[index-1] == "%9" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("submit key count = %d in %q, want one", count, queue)
	}
}

func TestSessionInputSerializesConcurrentProducersPerSession(t *testing.T) {
	io := newFakeSessionInputIO()
	owner := newSessionInputOwner(io)
	identity := testSessionInputIdentity("claude")
	resolver := fixedSessionInputResolver(identity)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for index := 0; index < 12; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := owner.submit("same:@1", identity, resolver, identity.Command, strings.Repeat("x", index+1), "")
			errs <- err
		}(index)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if io.maxQueues != 1 || len(io.queues) != 12 {
		t.Fatalf("max active queues=%d queues=%d, want 1 and 12", io.maxQueues, len(io.queues))
	}
}

func TestSessionInputTargetGenerationChangeIsDefinitelyNotSubmitted(t *testing.T) {
	io := newFakeSessionInputIO()
	io.afterLoad = func() {
		io.mu.Lock()
		io.paneValue.generation = "generation-2"
		io.mu.Unlock()
	}
	owner := newSessionInputOwner(io)
	identity := testSessionInputIdentity("codex")
	_, err := owner.submit("agent:@1", identity, fixedSessionInputResolver(identity), identity.Command, "message", "")
	if InputOutcomeFromError(err) != InputNotSubmitted {
		t.Fatalf("outcome = %s, want definitely not submitted: %v", InputOutcomeFromError(err), err)
	}
	if len(io.queues) != 0 || len(io.submissions) != 0 {
		t.Fatalf("provider mutated after generation change: queues=%d submissions=%d", len(io.queues), len(io.submissions))
	}
}

func TestSessionInputPaneGenerationIsPaneLifetimeOnly(t *testing.T) {
	stable := sessionInputPaneGeneration("%1")
	if stable == "" {
		t.Fatal("pane generation must be non-empty for a live pane id")
	}
	if got := sessionInputPaneGeneration("%1"); got != stable {
		t.Fatalf("pane generation must be deterministic: %s vs %s", stable, got)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte("%1")))
	if stable != want {
		t.Fatalf("pane generation must be sha256(pane_id) only: got %s want %s", stable, want)
	}
	if sessionInputPaneGeneration("%2") == stable {
		t.Fatal("pane_id replacement must change generation")
	}
	if sessionInputPaneGeneration("") != "" {
		t.Fatal("empty pane id must not invent a generation")
	}

	// Direct invariant: launch metadata (pane_pid / pane_start_command) cannot
	// enter the digest because the generation owner accepts only pane_id.
	// A pid/start_command-inclusive legacy digest must therefore differ.
	legacyLaunch := sha256.Sum256([]byte(strings.Join([]string{
		"%1", "12345", "cursor-agent --force",
	}, "\x00")))
	if stable == fmt.Sprintf("%x", legacyLaunch[:]) {
		t.Fatal("pane-lifetime generation must ignore pane_pid and pane_start_command")
	}

	// Historical App false positive: session_id / session_created differ across
	// link-window views for the same pane_id.
	legacySessionA := sha256.Sum256([]byte(strings.Join([]string{
		"$1", "100", "@1", "%1",
	}, "\x00")))
	legacySessionB := sha256.Sum256([]byte(strings.Join([]string{
		"$2", "200", "@1", "%1",
	}, "\x00")))
	if fmt.Sprintf("%x", legacySessionA[:]) == fmt.Sprintf("%x", legacySessionB[:]) {
		t.Fatal("precondition: session-inclusive digests must differ across linked views")
	}
	if stable == fmt.Sprintf("%x", legacySessionA[:]) {
		t.Fatal("pane-lifetime generation must not equal session-inclusive digest")
	}
}

func TestSessionInputLinkedViewPostMarkerPaneIDRereadAllowsExactOnceSubmit(t *testing.T) {
	const (
		sessionTarget = "agent:@1"
		paneID        = "%42"
		receipt       = "receipt-linked-post-marker"
		payload       = "hi from linked view"
	)
	generation := sessionInputPaneGeneration(paneID)

	// Baseline session target vs post-marker %pane_id diverge on session fields
	// that a session-inclusive digest would hash, while sharing pane_id/generation.
	legacySessionView := sha256.Sum256([]byte(strings.Join([]string{
		"$1", "100", "@1", paneID,
	}, "\x00")))
	legacyLinkedView := sha256.Sum256([]byte(strings.Join([]string{
		"$2", "200", "@1", paneID,
	}, "\x00")))
	if fmt.Sprintf("%x", legacySessionView[:]) == fmt.Sprintf("%x", legacyLinkedView[:]) {
		t.Fatal("precondition: linked-view session fields must change session-inclusive digest")
	}
	if generation == fmt.Sprintf("%x", legacySessionView[:]) ||
		generation == fmt.Sprintf("%x", legacyLinkedView[:]) {
		t.Fatal("pane_id-only generation must diverge from session-inclusive digests")
	}

	io := newFakeSessionInputIO()
	io.paneValue = sessionInputPane{
		alive:      true,
		paneID:     paneID,
		generation: generation,
	}
	linked := sessionInputPane{
		alive:      true,
		paneID:     paneID,
		generation: generation,
	}
	io.paneIDView = &linked
	var afterLedgerHookRan bool
	io.afterLedgerWrite = func() {
		afterLedgerHookRan = true
	}

	owner := newSessionInputOwner(io)
	identity := testSessionInputIdentity("cursor-agent")
	result, err := owner.submit(
		sessionTarget, identity, fixedSessionInputResolver(identity),
		identity.Command, payload, receipt,
	)
	if err != nil || result.Outcome != InputAccepted {
		t.Fatalf("post-marker linked-view submit = (%+v, %v)", result, err)
	}
	if !afterLedgerHookRan {
		t.Fatal("after-ledger hook never ran; persistReceiptLedger was not exercised")
	}
	if len(io.postLedgerPaneCalls) == 0 {
		t.Fatal("no pane() calls after persistReceiptLedger")
	}
	foundPostMarker := false
	for _, target := range io.postLedgerPaneCalls {
		if target == paneID {
			foundPostMarker = true
			break
		}
	}
	if !foundPostMarker {
		t.Fatalf("post-ledger pane calls=%v, want pane(%s) after persistReceiptLedger",
			io.postLedgerPaneCalls, paneID)
	}
	foundSessionBaseline := false
	for _, target := range io.paneCalls {
		if target == sessionTarget {
			foundSessionBaseline = true
			break
		}
	}
	if !foundSessionBaseline {
		t.Fatalf("pane calls=%v, want baseline pane(%s)", io.paneCalls, sessionTarget)
	}
	if len(io.queues) != 1 {
		t.Fatalf("queues=%d, want exact-once submit", len(io.queues))
	}

	dup, err := owner.submit(
		sessionTarget, identity, fixedSessionInputResolver(identity),
		identity.Command, payload, receipt,
	)
	if err != nil || dup.Outcome != InputAccepted || !dup.Duplicate {
		t.Fatalf("same-receipt duplicate = (%+v, %v), want accepted duplicate", dup, err)
	}
	if len(io.queues) != 1 {
		t.Fatalf("duplicate same receipt replayed provider queue: queues=%d", len(io.queues))
	}
}

func TestSessionInputTargetIdentityChangeIsDefinitelyNotSubmitted(t *testing.T) {
	io := newFakeSessionInputIO()
	owner := newSessionInputOwner(io)
	expected := testSessionInputIdentity("grok")
	changed := expected
	changed.ProcessStart++
	calls := 0
	resolver := func(string) (targetProcessIdentity, bool) {
		calls++
		if calls == 1 {
			return expected, true
		}
		return changed, true
	}
	_, err := owner.submit("agent:@1", expected, resolver, expected.Command, "message", "")
	if InputOutcomeFromError(err) != InputNotSubmitted {
		t.Fatalf("outcome = %s, want definitely not submitted: %v", InputOutcomeFromError(err), err)
	}
	if len(io.queues) != 0 {
		t.Fatalf("provider mutated after target identity change: queues=%d", len(io.queues))
	}
}

func TestSessionInputChatExplicitlyReplacesManualDraft(t *testing.T) {
	io := newFakeSessionInputIO()
	owner := newSessionInputOwner(io)
	identity := testSessionInputIdentity("codex")
	if _, err := owner.submit("agent:@1", identity, fixedSessionInputResolver(identity), identity.Command, "Chat wins", ""); err != nil {
		t.Fatal(err)
	}
	queue := io.queues[0]
	clearIndex := indexOf(queue, "C-u")
	pasteIndex := indexOf(queue, "paste-buffer")
	if clearIndex < 0 || pasteIndex < 0 || clearIndex >= pasteIndex {
		t.Fatalf("manual draft replacement order = %q", queue)
	}
}

func TestSessionInputReceiptDedupeSurvivesOwnerRestart(t *testing.T) {
	io := newFakeSessionInputIO()
	identity := testSessionInputIdentity("claude")
	resolver := fixedSessionInputResolver(identity)
	first := newSessionInputOwner(io)
	if _, err := first.submit("agent:@1", identity, resolver, identity.Command, "event cue", "event-123"); err != nil {
		t.Fatal(err)
	}
	entry, found := io.ledger.entry("event-123")
	if !found || entry.Outcome != InputAccepted || len(io.ledgerWrites) != 2 {
		t.Fatalf("durable ledger=%#v writes=%#v, want ambiguous then accepted", io.ledger, io.ledgerWrites)
	}
	if !reflect.DeepEqual(io.operations, []string{"ledger_write", "provider_queue", "ledger_write"}) {
		t.Fatalf("receipt mutation ordering = %#v", io.operations)
	}
	restarted := newSessionInputOwner(io)
	result, err := restarted.submit("agent:@1", identity, resolver, identity.Command, "event cue", "event-123")
	if err != nil || result.Outcome != InputAccepted {
		t.Fatalf("restart dedupe = (%+v, %v)", result, err)
	}
	if len(io.queues) != 1 {
		t.Fatalf("queues = %d, want one across restart", len(io.queues))
	}
}

func TestSessionInputReceiptOutcomeReadsDurableTruthWithoutSubmission(t *testing.T) {
	io := newFakeSessionInputIO()
	identity := testSessionInputIdentity("codex")
	resolver := fixedSessionInputResolver(identity)
	owner := newSessionInputOwner(io)
	if _, err := owner.submit("agent:@1", identity, resolver, identity.Command, "direct event", "event-accepted"); err != nil {
		t.Fatal(err)
	}

	restarted := newSessionInputOwner(io)
	result, found, err := restarted.receiptOutcome("agent:@1", identity, resolver, "event-accepted")
	if err != nil || !found || result.Outcome != InputAccepted ||
		result.Receipt != "event-accepted" {
		t.Fatalf("accepted receipt result=%#v found=%v err=%v", result, found, err)
	}
	if len(io.queues) != 1 {
		t.Fatalf("receipt read submitted another provider queue: %d", len(io.queues))
	}

	io.ledger.Entries = append(io.ledger.Entries, sessionInputReceiptEntry{
		Receipt:       "event-ambiguous",
		PayloadSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("ambiguous event"))),
		Outcome:       InputAmbiguous,
	})
	result, found, err = restarted.receiptOutcome("agent:@1", identity, resolver, "event-ambiguous")
	if err != nil || !found || result.Outcome != InputAmbiguous {
		t.Fatalf("ambiguous receipt result=%#v found=%v err=%v", result, found, err)
	}
	result, found, err = restarted.receiptOutcome("agent:@1", identity, resolver, "event-missing")
	if err != nil || found || result.Outcome != InputNotSubmitted {
		t.Fatalf("missing receipt result=%#v found=%v err=%v", result, found, err)
	}
	if len(io.queues) != 1 {
		t.Fatalf("receipt inspection changed provider queues: %d", len(io.queues))
	}
}

func TestSessionInputDelegatedTurnAcceptanceAndFollowUpShareDurableBoundary(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("future-agent")
	resolver := fixedSessionInputResolver(identity)
	owner := newSessionInputOwner(io)
	owner.ledger = ledger
	acceptedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	first := delegatedTurnDraft{
		ID:              "turn-initial",
		AcceptedAt:      acceptedAt,
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	result, err := owner.submitDelegated(
		"agent:@1", identity, resolver, identity.Command, "initial", first,
		scriptedCorrelatedAdmission("initial"),
	)
	if err != nil || result.Outcome != InputAccepted || result.Receipt != first.ID {
		t.Fatalf("initial delegated submit = (%+v, %v)", result, err)
	}
	turn := ledger.snapshot("agent:@1")
	if turn.TurnID != first.ID || turn.Status != TurnAccepted || turn.ActivityID != "activity-accepted" ||
		len(io.submissions) != 1 {
		t.Fatalf("initial canonical turn=%+v submissions=%#v", turn, io.submissions)
	}

	settledAt := acceptedAt.Add(time.Minute)
	ledger.seed("agent:@1", TurnSnapshot{
		SessionID:  "agent:@1",
		TurnID:     first.ID,
		Status:     TurnDone,
		AcceptedAt: acceptedAt,
		SettledAt:  &settledAt,
	})
	second := delegatedTurnDraft{
		ID:              "turn-follow-up",
		AcceptedAt:      settledAt.Add(time.Second),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	restartedOwner := newSessionInputOwner(io)
	restartedOwner.ledger = ledger
	result, err = restartedOwner.submitDelegated(
		"agent:@1", identity, resolver, identity.Command, "follow-up", second,
		scriptedCorrelatedAdmission("follow-up"),
	)
	if err != nil || result.Outcome != InputAccepted || result.Receipt != second.ID {
		t.Fatalf("follow-up delegated submit = (%+v, %v)", result, err)
	}
	turn = ledger.snapshot("agent:@1")
	if turn.TurnID != second.ID || turn.Status != TurnAccepted ||
		len(io.submissions) != 2 {
		t.Fatalf("follow-up canonical turn=%+v submissions=%#v", turn, io.submissions)
	}
}

func TestSessionInputCursorInitialPreservesExactUTF8AndAcceptsAfterProviderStart(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("cursor-agent --force")
	payload := "Concrete task prefix 你好\n\nZen lifecycle protocol:\n- preserve the task\n"
	turn := delegatedTurnDraft{
		ID:              "cursor-initial",
		AcceptedAt:      time.Date(2026, 8, 5, 1, 1, 0, 0, time.UTC),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}

	owner := newSessionInputOwner(io)
	owner.ledger = ledger
	result, err := owner.submitDelegated(
		"agent:@1",
		identity,
		fixedSessionInputResolver(identity),
		identity.Command,
		payload,
		turn,
		scriptedCorrelatedAdmission(payload),
	)
	if err != nil || result.Outcome != InputAccepted {
		t.Fatalf("Cursor initial submit = (%+v, %v)", result, err)
	}
	if !reflect.DeepEqual(io.submissions, []string{payload}) {
		t.Fatalf("Cursor initial payload = %q, want exact %q", io.submissions, payload)
	}
	if ledger.snapshot("agent:@1").Status != TurnAccepted {
		t.Fatalf("Cursor initial accepted before provider start: %+v", ledger.snapshot("agent:@1"))
	}
}

func TestWatcherDelegatedAdmissionConfirmsCursorInitialAndActiveSteering(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	initialPayload := "initial 你好\n\nexact"
	followPayload := "active follow-up"
	probe := &transportAdmissionProbe{
		io: io,
		inputs: map[int]string{
			0: "older input",
			1: initialPayload,
			2: followPayload,
		},
		missing: map[int]bool{},
		staleAt: map[int]time.Time{},
	}
	w := watcherWithAdmissionProbe(probe)
	owner := newSessionInputOwner(io)
	owner.ledger = ledger
	identity := testSessionInputIdentity("cursor-agent --force")
	initial := delegatedTurnDraft{
		ID:              "initial-turn",
		AcceptedAt:      time.Now().UTC(),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}

	result, err := owner.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, initialPayload, initial,
		w.delegatedInputConfirmer("agent:@1", identity.Command),
	)
	if err != nil || result.Outcome != InputAccepted ||
		result.TurnID != initial.ID || ledger.snapshot("agent:@1").Status != TurnAccepted {
		t.Fatalf("production initial admission = (%+v, %v), turn=%+v", result, err, ledger.snapshot("agent:@1"))
	}

	steering := delegatedTurnDraft{
		ID:              "steering-receipt",
		AcceptedAt:      time.Now().UTC(),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	beforeTurn := ledger.snapshot("agent:@1")
	result, err = owner.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, followPayload, steering,
		w.delegatedInputConfirmer("agent:@1", identity.Command),
	)
	if err != nil || result.Outcome != InputAccepted ||
		result.TurnID != initial.ID {
		t.Fatalf("production steering admission = (%+v, %v)", result, err)
	}
	if after := ledger.snapshot("agent:@1"); after.TurnID != beforeTurn.TurnID {
		t.Fatalf("accepted steering created or reset lifecycle turn: before=%+v after=%+v", beforeTurn, after)
	}
	if entry, found := io.ledger.entry(steering.ID); !found || entry.Outcome != InputAccepted {
		t.Fatalf("steering receipt was not accepted independently: entry=%+v found=%v", entry, found)
	}
	if !reflect.DeepEqual(probe.steps, []int{0, 1, 1, 2}) {
		t.Fatalf("admission baselines were not captured inside serialized transactions: %v", probe.steps)
	}
}

func TestWatcherDelegatedAdmissionRejectsUncorrelatedCursorEvidence(t *testing.T) {
	payload := "exact payload\n\nwith blanks "
	wrongPayloads := []string{
		"old nonce",
		"exact payload\n\nwith blanks",
		"exact payload\nwith blanks ",
		"exact payload\r\n\r\nwith blanks ",
	}
	for _, test := range []struct {
		name      string
		after     string
		missing   bool
		staleTime bool
	}{
		{name: "wrong nonce", after: wrongPayloads[0]},
		{name: "trailing whitespace differs", after: wrongPayloads[1]},
		{name: "repeated blank lines differ", after: wrongPayloads[2]},
		{name: "CRLF differs", after: wrongPayloads[3]},
		{name: "new activity without user input", after: payload, missing: true},
		{name: "StartedAt before mutation", after: payload, staleTime: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			io := newFakeSessionInputIO()
			staleAt := map[int]time.Time{}
			if test.staleTime {
				staleAt[1] = time.Unix(1, 0).UTC()
			}
			probe := &transportAdmissionProbe{
				io:      io,
				inputs:  map[int]string{0: "older", 1: test.after},
				missing: map[int]bool{1: test.missing},
				staleAt: staleAt,
			}
			w := watcherWithAdmissionProbe(probe)
			identity := testSessionInputIdentity("cursor-agent --force")
			turn := testTurnDraft("receipt-"+test.name, time.Now().UTC(), identity)
			result, err := newLedgerSessionInputOwner(io, newFakeTurnLedger()).submitDelegated(
				"agent:@1", identity, fixedSessionInputResolver(identity),
				identity.Command, payload, turn,
				w.delegatedInputConfirmer("agent:@1", identity.Command),
			)
			if InputOutcomeFromError(err) != InputAmbiguous ||
				result.Outcome != InputAmbiguous {
				t.Fatalf("uncorrelated admission = (%+v, %v)", result, err)
			}
			if len(io.queues) != 1 {
				t.Fatalf("uncorrelated evidence replayed submission: queues=%d", len(io.queues))
			}
		})
	}
}

func TestCorrelateDelegatedAdmissionRejectsDifferentOrMissingStream(t *testing.T) {
	payloadDigest := fmt.Sprintf("%x", sha256.Sum256([]byte("payload")))
	boundary := time.Date(2026, 8, 5, 2, 0, 0, 0, time.UTC)
	baseline := delegatedAdmissionEvidence{
		Stream: "provider\x00session\x00path",
		ID:     "admission-7",
		Cursor: 7,
	}
	for _, test := range []struct {
		name    string
		current delegatedAdmissionEvidence
		want    delegatedAdmissionCorrelation
	}{
		{
			name: "different stream",
			current: delegatedAdmissionEvidence{
				Stream: "provider\x00other-session\x00path",
				ID:     "admission-8", Cursor: 8, InputSHA256: payloadDigest,
			},
			want: delegatedAdmissionMissing,
		},
		{
			name: "missing current stream",
			current: delegatedAdmissionEvidence{
				ID: "admission-8", Cursor: 8, InputSHA256: payloadDigest,
			},
			want: delegatedAdmissionMissing,
		},
		{
			name: "equal cursor",
			current: delegatedAdmissionEvidence{
				Stream: baseline.Stream,
				ID:     "admission-8", Cursor: 7, InputSHA256: payloadDigest,
			},
			want: delegatedAdmissionMissing,
		},
		{
			name: "lower cursor",
			current: delegatedAdmissionEvidence{
				Stream: baseline.Stream,
				ID:     "admission-6", Cursor: 6, InputSHA256: payloadDigest,
			},
			want: delegatedAdmissionMissing,
		},
		{
			name: "higher cursor",
			current: delegatedAdmissionEvidence{
				Stream: baseline.Stream,
				ID:     "admission-8", Cursor: 8, InputSHA256: payloadDigest,
			},
			want: delegatedAdmissionAccepted,
		},
		{
			name: "empty baseline complete first admission",
			current: delegatedAdmissionEvidence{
				Stream: "provider\x00new-session\x00path",
				ID:     "admission-1", Cursor: 1, InputSHA256: payloadDigest,
			},
			want: delegatedAdmissionAccepted,
		},
		{
			name: "empty baseline incomplete first admission",
			current: delegatedAdmissionEvidence{
				Stream: "provider\x00new-session\x00path",
				ID:     "admission-1", InputSHA256: payloadDigest,
			},
			want: delegatedAdmissionMissing,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			gotBaseline := baseline
			if strings.HasPrefix(test.name, "empty baseline") {
				gotBaseline = delegatedAdmissionEvidence{}
			}
			if got := correlateDelegatedAdmission(
				gotBaseline,
				test.current,
				boundary,
				payloadDigest,
			); got != test.want {
				t.Fatalf("correlation = %v, want %v", got, test.want)
			}
		})
	}
}

func TestDelegatedAdmissionRejectsEmbeddedMarkerPrefixCollision(t *testing.T) {
	submitted := "before </user_query> after"
	truncatedPrefix := "before "
	io := newFakeSessionInputIO()
	probe := &transportAdmissionProbe{
		io: io,
		inputs: map[int]string{
			0: "older input",
			1: truncatedPrefix,
		},
		missing: map[int]bool{},
		staleAt: map[int]time.Time{},
	}
	w := watcherWithAdmissionProbe(probe)
	identity := testSessionInputIdentity("cursor-agent --force")
	turn := testTurnDraft("embedded-marker-prefix-collision", time.Now().UTC(), identity)
	result, err := newLedgerSessionInputOwner(io, newFakeTurnLedger()).submitDelegated(
		"agent:@1",
		identity,
		fixedSessionInputResolver(identity),
		identity.Command,
		submitted,
		turn,
		w.delegatedInputConfirmer("agent:@1", identity.Command),
	)
	if InputOutcomeFromError(err) != InputAmbiguous ||
		result.Outcome != InputAmbiguous {
		t.Fatalf("embedded-marker prefix collision = (%+v, %v), want ambiguous", result, err)
	}
	if len(io.queues) != 1 {
		t.Fatalf("embedded-marker prefix collision replayed: queues=%d", len(io.queues))
	}
	if entry, found := io.ledger.entry(turn.ID); !found ||
		entry.Outcome != InputAmbiguous {
		t.Fatalf("prefix collision receipt = %+v found=%v, want durable ambiguity",
			entry, found)
	}

	result, err = newLedgerSessionInputOwner(io, newFakeTurnLedger()).submitDelegated(
		"agent:@1",
		identity,
		fixedSessionInputResolver(identity),
		identity.Command,
		submitted,
		turn,
		scriptedCorrelatedAdmission(submitted),
	)
	if InputOutcomeFromError(err) != InputAmbiguous ||
		result.Outcome != InputAmbiguous || len(io.queues) != 1 {
		t.Fatalf("prefix collision duplicate replayed = (%+v, %v), queues=%d",
			result, err, len(io.queues))
	}
	if entry, found := io.ledger.entry(turn.ID); !found ||
		entry.Outcome != InputAmbiguous {
		t.Fatalf("prefix collision duplicate changed receipt = %+v found=%v",
			entry, found)
	}
}

func TestWatcherDelegatedAdmissionIgnoresStaleActivityAndPaneRunning(t *testing.T) {
	io := newFakeSessionInputIO()
	fixed := 0
	probe := &transportAdmissionProbe{
		io:        io,
		inputs:    map[int]string{0: "same old input"},
		missing:   map[int]bool{},
		staleAt:   map[int]time.Time{},
		fixedStep: &fixed,
	}
	w := watcherWithAdmissionProbe(probe)
	w.activityProbe = classifier.NewActivityProbe(classifier.NewCursorActivityAdapter())
	io.paneContentValue = "Cursor Agent\n→ Add a follow-up\nctrl+c to stop\n"
	identity := testSessionInputIdentity("cursor-agent --force")
	turn := testTurnDraft("ignored-submit", time.Now().UTC(), identity)
	result, err := newLedgerSessionInputOwner(io, newFakeTurnLedger()).submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "same old input", turn,
		w.delegatedInputConfirmer("agent:@1", identity.Command),
	)
	if InputOutcomeFromError(err) != InputAmbiguous ||
		result.Outcome != InputAmbiguous {
		t.Fatalf("stale activity/pane running was accepted: (%+v, %v)", result, err)
	}
}

func TestWatcherDelegatedAdmissionSerializesConcurrentSamePayloadBaselines(t *testing.T) {
	io := newFakeSessionInputIO()
	payload := "same payload"
	probe := &transportAdmissionProbe{
		io:      io,
		inputs:  map[int]string{0: "old", 1: payload, 2: payload},
		missing: map[int]bool{},
		staleAt: map[int]time.Time{},
	}
	w := watcherWithAdmissionProbe(probe)
	owner := newSessionInputOwner(io)
	owner.ledger = newFakeTurnLedger()
	identity := testSessionInputIdentity("cursor-agent --force")
	results := make(chan InputResult, 2)
	errs := make(chan error, 2)
	for _, id := range []string{"concurrent-a", "concurrent-b"} {
		go func(id string) {
			result, err := owner.submitDelegated(
				"agent:@1", identity, fixedSessionInputResolver(identity),
				identity.Command, payload,
				testTurnDraft(id, time.Now().UTC(), identity),
				w.delegatedInputConfirmer("agent:@1", identity.Command),
			)
			results <- result
			errs <- err
		}(id)
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if result := <-results; result.Outcome != InputAccepted {
			t.Fatalf("concurrent result = %+v", result)
		}
	}
	if len(io.queues) != 2 {
		t.Fatalf("concurrent same-payload submissions = %d, want two distinct receipts", len(io.queues))
	}
	if !reflect.DeepEqual(probe.steps, []int{0, 1, 1, 2}) {
		t.Fatalf("concurrent callers shared a pre-lock baseline: %v", probe.steps)
	}
}

func TestSessionInputCursorFollowUpIsNotAcceptedWithoutProviderStart(t *testing.T) {
	io := newFakeSessionInputIO()
	io.paneContentValue = "Cursor Agent\n→ Add a follow-up\nRun Everything\n"
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("cursor-agent --force")
	turn := delegatedTurnDraft{
		ID:              "cursor-follow-up",
		AcceptedAt:      time.Date(2026, 8, 5, 1, 2, 0, 0, time.UTC),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}

	owner := newSessionInputOwner(io)
	owner.ledger = ledger
	result, err := owner.submitDelegated(
		"agent:@1",
		identity,
		fixedSessionInputResolver(identity),
		identity.Command,
		"short follow-up",
		turn,
		scriptedAmbiguousAdmission(),
	)
	if err == nil || result.Outcome == InputAccepted {
		t.Fatalf("idle composer submit = (%+v, %v), must not be accepted without provider start", result, err)
	}
	// The canonical turn stays Admitted (uncertain, never failed).
	if ledger.snapshot("agent:@1").Status != TurnAdmitted {
		t.Fatalf("ambiguous follow-up must stay Admitted, got %+v", ledger.snapshot("agent:@1"))
	}
}

func TestSessionInputDefiniteQueueNonStartRollsBackAndIsRetryable(t *testing.T) {
	io := newFakeSessionInputIO()
	io.runErr = errors.New("tmux queue did not start")
	io.runStarted = false
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("cursor-agent --force")
	turn := delegatedTurnDraft{
		ID:              "cursor-retryable",
		AcceptedAt:      time.Date(2026, 8, 5, 1, 3, 0, 0, time.UTC),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	owner := newSessionInputOwner(io)
	owner.ledger = ledger

	result, err := owner.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "retry me", turn, scriptedCorrelatedAdmission("retry me"),
	)
	if InputOutcomeFromError(err) != InputNotSubmitted ||
		result.Outcome != InputNotSubmitted {
		t.Fatalf("definite non-submit = (%+v, %v)", result, err)
	}
	if _, found := io.ledger.entry(turn.ID); found {
		t.Fatalf("definite non-submit retained durable ambiguity: ledger=%+v", io.ledger)
	}
	// The canonical Admitted record stays (never replayed, never removed);
	// the retry below steers into it per the durable uncertainty rules.
	if ledger.snapshot("agent:@1").TurnID != turn.ID {
		t.Fatalf("definite non-submit lost canonical turn: %+v", ledger.snapshot("agent:@1"))
	}

	io.runErr = nil
	result, err = owner.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "retry me", turn, scriptedCorrelatedAdmission("retry me"),
	)
	if err != nil || result.Outcome != InputAccepted || len(io.queues) != 2 {
		t.Fatalf("retry after definite non-submit = (%+v, %v), queues=%d", result, err, len(io.queues))
	}
}

func TestSessionInputDuplicateNewTurnReceiptReturnsExistingLifecycleIdentity(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("future-agent")
	resolver := fixedSessionInputResolver(identity)
	acceptedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	turn := delegatedTurnDraft{
		ID:              "turn-idempotent",
		AcceptedAt:      acceptedAt,
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	first := newSessionInputOwner(io)
	first.ledger = ledger
	if _, err := first.submitDelegated(
		"agent:@1", identity, resolver, identity.Command, "task", turn,
		scriptedCorrelatedAdmission("task"),
	); err != nil {
		t.Fatal(err)
	}
	restarted := newSessionInputOwner(io)
	restarted.ledger = ledger
	result, err := restarted.submitDelegated(
		"agent:@1", identity, resolver, identity.Command, "task", turn,
		scriptedCorrelatedAdmission("task"),
	)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != turn.ID || !result.Duplicate {
		t.Fatalf("duplicate new-turn receipt = (%+v, %v)", result, err)
	}
	if len(io.queues) != 1 || ledger.snapshot("agent:@1").TurnID != turn.ID {
		t.Fatalf("duplicate new-turn receipt replayed/reset lifecycle: queues=%d turn=%+v", len(io.queues), ledger.snapshot("agent:@1"))
	}
}

func TestSessionInputAcceptedDuplicateWithoutTurnMarkerIsAmbiguousAndNotReplayed(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("future-agent")
	resolver := fixedSessionInputResolver(identity)
	turn := delegatedTurnDraft{
		ID:              "turn-marker-lost",
		AcceptedAt:      time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	first := newSessionInputOwner(io)
	first.ledger = ledger
	if _, err := first.submitDelegated(
		"agent:@1", identity, resolver, identity.Command, "task", turn,
		scriptedCorrelatedAdmission("task"),
	); err != nil {
		t.Fatal(err)
	}

	// The durable ledger record is the identity: an accepted receipt with no
	// canonical turn is ambiguous and never replayed.
	restarted := newSessionInputOwner(io)
	restarted.ledger = newFakeTurnLedger()
	_, err := restarted.submitDelegated(
		"agent:@1", identity, resolver, identity.Command, "task", turn,
		scriptedCorrelatedAdmission("task"),
	)
	if InputOutcomeFromError(err) != InputAmbiguous ||
		!strings.Contains(err.Error(), "canonical turn is missing") {
		t.Fatalf("accepted duplicate without canonical turn = %v", err)
	}
	if len(io.queues) != 1 {
		t.Fatalf("accepted duplicate without canonical turn replayed input: queues=%d", len(io.queues))
	}
}

func TestSessionInputSteeringWhileRunningRetainsDelegatedTurnIdentity(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("future-agent")
	ledger.seed("agent:@1", TurnSnapshot{
		SessionID:  "agent:@1",
		TurnID:     "active-turn",
		Status:     TurnRunning,
		AcceptedAt: time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC),
	})
	owner := newSessionInputOwner(io)
	owner.ledger = ledger
	next := delegatedTurnDraft{
		ID:              "too-early",
		AcceptedAt:      time.Date(2026, 8, 5, 1, 0, 1, 0, time.UTC),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	result, err := owner.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "follow-up", next, scriptedCorrelatedAdmission("follow-up"),
	)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != "active-turn" {
		t.Fatalf("running-turn steering = (%+v, %v)", result, err)
	}
	if len(io.queues) != 1 || len(io.submissions) != 1 || io.submissions[0] != "follow-up" {
		t.Fatalf("steering submissions=%#v queues=%d", io.submissions, len(io.queues))
	}
	if ledger.snapshot("agent:@1").TurnID != "active-turn" ||
		ledger.snapshot("agent:@1").Status != TurnRunning {
		t.Fatalf("steering replaced lifecycle owner: %+v", ledger.snapshot("agent:@1"))
	}
}

func TestSessionInputActiveSteeringWithoutAdmissionIsAmbiguousAndNeverReplayed(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("cursor-agent --force")
	ledger.seed("agent:@1", TurnSnapshot{
		SessionID:  "agent:@1",
		TurnID:     "active-turn",
		Status:     TurnRunning,
		AcceptedAt: time.Now().UTC().Add(-time.Minute),
	})
	steering := delegatedTurnDraft{
		ID:              "steering-ambiguous",
		AcceptedAt:      time.Now().UTC(),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}

	first := newSessionInputOwner(io)
	first.ledger = ledger
	result, err := first.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "steer once", steering,
		scriptedAmbiguousAdmission(),
	)
	if InputOutcomeFromError(err) != InputAmbiguous ||
		result.Outcome != InputAmbiguous || result.TurnID != "active-turn" {
		t.Fatalf("ambiguous active steering = (%+v, %v)", result, err)
	}
	if ledger.snapshot("agent:@1").TurnID != "active-turn" || len(io.queues) != 1 {
		t.Fatalf("ambiguous steering changed lifecycle or queue count: turn=%+v queues=%d", ledger.snapshot("agent:@1"), len(io.queues))
	}

	restarted := newSessionInputOwner(io)
	restarted.ledger = ledger
	result, err = restarted.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "steer once", steering,
		scriptedCorrelatedAdmission("steer once"),
	)
	if InputOutcomeFromError(err) != InputAmbiguous ||
		result.Outcome != InputAmbiguous || result.TurnID != "active-turn" ||
		len(io.queues) != 1 {
		t.Fatalf("restart replayed ambiguous steering = (%+v, %v), queues=%d", result, err, len(io.queues))
	}
	if ledger.snapshot("agent:@1").TurnID != "active-turn" {
		t.Fatalf("ambiguous steering duplicate reset lifecycle: %+v", ledger.snapshot("agent:@1"))
	}
}

func TestSessionInputDuplicateSteeringReceiptRetainsActiveTurn(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("future-agent")
	ledger.seed("agent:@1", TurnSnapshot{
		SessionID:  "agent:@1",
		TurnID:     "active-turn",
		Status:     TurnRunning,
		AcceptedAt: time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC),
	})
	steering := delegatedTurnDraft{
		ID:              "steering-delivery",
		AcceptedAt:      time.Date(2026, 8, 5, 1, 0, 1, 0, time.UTC),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	first := newSessionInputOwner(io)
	first.ledger = ledger
	result, err := first.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "steer", steering, scriptedCorrelatedAdmission("steer"),
	)
	if err != nil || result.TurnID != "active-turn" {
		t.Fatalf("first steering = (%+v, %v)", result, err)
	}
	restarted := newSessionInputOwner(io)
	restarted.ledger = ledger
	result, err = restarted.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "steer", steering, scriptedCorrelatedAdmission("steer"),
	)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != "active-turn" || !result.Duplicate {
		t.Fatalf("duplicate steering receipt = (%+v, %v)", result, err)
	}
	if len(io.queues) != 1 || ledger.snapshot("agent:@1").TurnID != "active-turn" {
		t.Fatalf("duplicate steering replayed/reset lifecycle: queues=%d turn=%+v", len(io.queues), ledger.snapshot("agent:@1"))
	}
}

func TestSessionInputDefinitelyNotSubmittedKeepsDurableAdmittedIdentity(t *testing.T) {
	io := newFakeSessionInputIO()
	ledger := newFakeTurnLedger()
	identity := testSessionInputIdentity("future-agent")
	settledAt := time.Date(2026, 8, 5, 1, 1, 0, 0, time.UTC)
	ledger.seed("agent:@1", TurnSnapshot{
		SessionID:  "agent:@1",
		TurnID:     "settled-prior",
		Status:     TurnDone,
		AcceptedAt: settledAt.Add(-time.Minute),
		SettledAt:  &settledAt,
	})
	io.runErr = errors.New("queue did not start")
	io.runStarted = false
	owner := newSessionInputOwner(io)
	owner.ledger = ledger
	next := delegatedTurnDraft{
		ID:              "next",
		AcceptedAt:      settledAt.Add(time.Second),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	_, err := owner.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "next task", next, scriptedCorrelatedAdmission("next task"),
	)
	if InputOutcomeFromError(err) != InputNotSubmitted {
		t.Fatalf("queue start failure outcome = %s, err=%v", InputOutcomeFromError(err), err)
	}
	// The durable Admitted record stays (C.6): the mutation provably never
	// started, the receipt was rolled back, and the turn is never replayed.
	turn := ledger.snapshot("agent:@1")
	if turn.TurnID != next.ID || turn.Status != TurnAdmitted {
		t.Fatalf("definite pre-submit failure lost the durable Admitted identity: %+v", turn)
	}
	if _, found := io.ledger.entry(next.ID); found {
		t.Fatalf("definite non-submit retained durable ambiguity: %+v", io.ledger)
	}
}

func TestSessionInputReceiptLedgerIsBoundedAndNeverEvictsAmbiguity(t *testing.T) {
	accepted := emptySessionInputReceiptLedger()
	for index := 0; index < sessionInputReceiptLedgerLimit; index++ {
		accepted.Entries = append(accepted.Entries, sessionInputReceiptEntry{
			Receipt:       fmt.Sprintf("accepted-%02d", index),
			PayloadSHA256: strings.Repeat(fmt.Sprintf("%x", index%16), sha256.Size*2),
			Outcome:       InputAccepted,
		})
	}
	next, err := accepted.withAmbiguous("new", strings.Repeat("a", sha256.Size*2))
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Entries) != sessionInputReceiptLedgerLimit {
		t.Fatalf("bounded ledger entries=%d", len(next.Entries))
	}
	if _, found := next.entry("accepted-00"); found {
		t.Fatal("oldest accepted receipt was not evicted at the bound")
	}
	if entry, found := next.entry("new"); !found || entry.Outcome != InputAmbiguous {
		t.Fatalf("new ambiguity missing: %#v found=%v", entry, found)
	}

	ambiguous := emptySessionInputReceiptLedger()
	for index := 0; index < sessionInputReceiptLedgerLimit; index++ {
		ambiguous.Entries = append(ambiguous.Entries, sessionInputReceiptEntry{
			Receipt:       fmt.Sprintf("ambiguous-%02d", index),
			PayloadSHA256: strings.Repeat(fmt.Sprintf("%x", index%16), sha256.Size*2),
			Outcome:       InputAmbiguous,
		})
	}
	if _, err := ambiguous.withAmbiguous("must-not-evict", strings.Repeat("b", sha256.Size*2)); err == nil {
		t.Fatal("ledger full of ambiguity accepted a new receipt")
	}
}

func TestSessionInputReceiptLedgerRetainsAcceptedABAInProcessAndAfterRestart(t *testing.T) {
	for _, restartBeforeRetry := range []bool{false, true} {
		name := "in process"
		if restartBeforeRetry {
			name = "after owner restart"
		}
		t.Run(name, func(t *testing.T) {
			io := newFakeSessionInputIO()
			identity := testSessionInputIdentity("codex")
			resolver := fixedSessionInputResolver(identity)
			owner := newSessionInputOwner(io)
			if _, err := owner.submit("agent:@1", identity, resolver, identity.Command, "payload A", "receipt-A"); err != nil {
				t.Fatal(err)
			}
			if _, err := owner.submit("agent:@1", identity, resolver, identity.Command, "payload B", "receipt-B"); err != nil {
				t.Fatal(err)
			}
			if restartBeforeRetry {
				owner = newSessionInputOwner(io)
			}
			result, err := owner.submit("agent:@1", identity, resolver, identity.Command, "payload A", "receipt-A")
			if err != nil || result.Outcome != InputAccepted {
				t.Fatalf("A/B/A retry = (%+v, %v)", result, err)
			}
			if len(io.queues) != 2 || len(io.ledger.Entries) != 2 {
				t.Fatalf("A/B/A replayed or lost ledger: queues=%d ledger=%#v", len(io.queues), io.ledger)
			}
		})
	}
}

func TestSessionInputReceiptPayloadMismatchFailsAfterRestart(t *testing.T) {
	io := newFakeSessionInputIO()
	identity := testSessionInputIdentity("claude")
	resolver := fixedSessionInputResolver(identity)
	if _, err := newSessionInputOwner(io).submit(
		"agent:@1", identity, resolver, identity.Command, "original", "same-receipt",
	); err != nil {
		t.Fatal(err)
	}
	restarted := newSessionInputOwner(io)
	_, err := restarted.submit(
		"agent:@1", identity, resolver, identity.Command, "different", "same-receipt",
	)
	if InputOutcomeFromError(err) != InputNotSubmitted ||
		!strings.Contains(err.Error(), "different input") ||
		len(io.queues) != 1 {
		t.Fatalf("mismatched restart retry: outcome=%s queues=%d err=%v", InputOutcomeFromError(err), len(io.queues), err)
	}
}

func TestSessionInputDistinguishesPreMutationAndAmbiguousWithoutReplay(t *testing.T) {
	t.Run("pre mutation", func(t *testing.T) {
		io := newFakeSessionInputIO()
		io.paneValue.alive = false
		owner := newSessionInputOwner(io)
		identity := testSessionInputIdentity("codex")
		_, err := owner.submit("agent:@1", identity, fixedSessionInputResolver(identity), identity.Command, "message", "receipt")
		if InputOutcomeFromError(err) != InputNotSubmitted || len(io.queues) != 0 {
			t.Fatalf("outcome=%s queues=%d err=%v", InputOutcomeFromError(err), len(io.queues), err)
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		io := newFakeSessionInputIO()
		io.runStarted = true
		io.runErr = errors.New("connection lost after queue start")
		identity := testSessionInputIdentity("claude")
		resolver := fixedSessionInputResolver(identity)
		_, firstErr := newSessionInputOwner(io).submit(
			"agent:@1", identity, resolver, identity.Command, "message", "receipt",
		)
		if InputOutcomeFromError(firstErr) != InputAmbiguous {
			t.Fatalf("first outcome=%s err=%v", InputOutcomeFromError(firstErr), firstErr)
		}
		restarted := newSessionInputOwner(io)
		_, secondErr := restarted.submit("agent:@1", identity, resolver, identity.Command, "message", "receipt")
		if InputOutcomeFromError(secondErr) != InputAmbiguous {
			t.Fatalf("second outcome=%s err=%v", InputOutcomeFromError(secondErr), secondErr)
		}
		if len(io.queues) != 1 {
			t.Fatalf("ambiguous receipt replayed: queues=%d", len(io.queues))
		}
	})
}

func TestSessionInputAcceptanceWriteFailureLeavesDurableAmbiguityAcrossRestart(t *testing.T) {
	io := newFakeSessionInputIO()
	io.writeErrors[2] = errors.New("daemon stopped before accepted receipt write")
	identity := testSessionInputIdentity("grok")
	resolver := fixedSessionInputResolver(identity)
	_, firstErr := newSessionInputOwner(io).submit(
		"agent:@1", identity, resolver, identity.Command, "message", "receipt",
	)
	if InputOutcomeFromError(firstErr) != InputAmbiguous {
		t.Fatalf("first outcome=%s err=%v", InputOutcomeFromError(firstErr), firstErr)
	}
	entry, found := io.ledger.entry("receipt")
	if !found || entry.Outcome != InputAmbiguous || len(io.queues) != 1 {
		t.Fatalf("pre-acceptance crash state: entry=%#v found=%v queues=%d", entry, found, len(io.queues))
	}
	io.writeErrors = map[int]error{}
	restarted := newSessionInputOwner(io)
	_, retryErr := restarted.submit(
		"agent:@1", identity, resolver, identity.Command, "message", "receipt",
	)
	if InputOutcomeFromError(retryErr) != InputAmbiguous || len(io.queues) != 1 {
		t.Fatalf("durable ambiguity replayed after restart: outcome=%s queues=%d err=%v",
			InputOutcomeFromError(retryErr), len(io.queues), retryErr)
	}
}

func TestSessionInputRollbackReadbackFailureRemainsSafeToRetryAfterRestart(t *testing.T) {
	io := newFakeSessionInputIO()
	io.runErr = errors.New("tmux queue did not start")
	io.readErrors[3] = errors.New("rollback readback unavailable")
	identity := testSessionInputIdentity("claude")
	resolver := fixedSessionInputResolver(identity)

	_, firstErr := newSessionInputOwner(io).submit(
		"agent:@1", identity, resolver, identity.Command, "message", "receipt",
	)
	if InputOutcomeFromError(firstErr) != InputNotSubmitted {
		t.Fatalf("first outcome=%s err=%v", InputOutcomeFromError(firstErr), firstErr)
	}
	if !strings.Contains(firstErr.Error(), "rollback readback unavailable") {
		t.Fatalf("first error omitted rollback confirmation failure: %v", firstErr)
	}
	if _, found := io.ledger.entry("receipt"); found {
		t.Fatalf("rollback write did not erase ambiguity marker: ledger=%#v", io.ledger)
	}
	if io.startedQueues != 0 {
		t.Fatalf("provider queues started before retry=%d, want zero", io.startedQueues)
	}

	io.runErr = nil
	io.readErrors = map[int]error{}
	result, retryErr := newSessionInputOwner(io).submit(
		"agent:@1", identity, resolver, identity.Command, "message", "receipt",
	)
	if retryErr != nil || result.Outcome != InputAccepted {
		t.Fatalf("restart retry = (%+v, %v), want accepted", result, retryErr)
	}
	if io.startedQueues != 1 {
		t.Fatalf("provider queues started across both attempts=%d, want one", io.startedQueues)
	}
	entry, found := io.ledger.entry("receipt")
	if !found || entry.Outcome != InputAccepted {
		t.Fatalf("retry receipt was not durably accepted: entry=%#v found=%v", entry, found)
	}
}

func TestCodexInitialReadinessIsSeparateFromOrdinarySubmit(t *testing.T) {
	startup := "│ >_ OpenAI Codex │\nSelect a model\n"
	ready := "│ >_ OpenAI Codex │\n\n› Ask Codex to work on something\n"
	if isCodexStartupReady(startup) {
		t.Fatal("model-selection startup screen must not accept initial input")
	}
	if !isCodexStartupReady(ready) {
		t.Fatal("ready initial Codex screen should accept input")
	}

	// The ordinary owner receives no rendered provider content at all.
	io := newFakeSessionInputIO()
	owner := newSessionInputOwner(io)
	identity := testSessionInputIdentity("codex")
	if _, err := owner.submit("agent:@1", identity, fixedSessionInputResolver(identity), identity.Command, "follow-up", ""); err != nil {
		t.Fatal(err)
	}
	if len(io.queues) != 1 {
		t.Fatalf("ordinary submit queues=%d, want one", len(io.queues))
	}
}

func TestSessionInputProviderAdapterSurfaceStaysThin(t *testing.T) {
	adapter := sessionInputProviderForCommand("claude")
	if adapter.submitKey != "Enter" || adapter.prepare != 0 ||
		adapter.settle != 250*time.Millisecond {
		t.Fatalf("Claude adapter = %+v", adapter)
	}
	cursor := sessionInputProviderForCommand("cursor-agent --force")
	if cursor.submitKey != "Enter" || cursor.prepare != 400*time.Millisecond ||
		cursor.settle != 2*time.Second {
		t.Fatalf("Cursor adapter = %+v", cursor)
	}
}
