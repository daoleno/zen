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
	mu               sync.Mutex
	paneValue        sessionInputPane
	buffers          map[string]string
	loadedPayloads   []string
	queues           [][]string
	submissions      []string
	ledger           sessionInputReceiptLedger
	ledgerWrites     []sessionInputReceiptLedger
	writeErrors      map[int]error
	ledgerReads      int
	readErrors       map[int]error
	operations       []string
	turn             delegatedTurnRecord
	hasTurn          bool
	runStarted       bool
	runErr           error
	startedQueues    int
	afterLoad        func()
	activeQueues     int
	maxQueues        int
	paneContentValue string
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

func (io *fakeSessionInputIO) pane(string) sessionInputPane {
	io.mu.Lock()
	defer io.mu.Unlock()
	return io.paneValue
}

func (io *fakeSessionInputIO) loadBuffer(buffer, payload string) error {
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

func (io *fakeSessionInputIO) deleteBuffer(buffer string) {
	io.mu.Lock()
	delete(io.buffers, buffer)
	io.mu.Unlock()
}

func (io *fakeSessionInputIO) runQueue(args []string) (bool, error) {
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

func (io *fakeSessionInputIO) receiptLedger(string) (sessionInputReceiptLedger, error) {
	io.mu.Lock()
	defer io.mu.Unlock()
	io.ledgerReads++
	if err := io.readErrors[io.ledgerReads]; err != nil {
		return sessionInputReceiptLedger{}, err
	}
	return cloneSessionInputReceiptLedger(io.ledger), nil
}

func (io *fakeSessionInputIO) writeReceiptLedger(_ string, ledger sessionInputReceiptLedger) error {
	io.mu.Lock()
	defer io.mu.Unlock()
	call := len(io.ledgerWrites) + 1
	io.ledgerWrites = append(io.ledgerWrites, cloneSessionInputReceiptLedger(ledger))
	io.operations = append(io.operations, "ledger_write")
	if err := io.writeErrors[call]; err != nil {
		return err
	}
	io.ledger = cloneSessionInputReceiptLedger(ledger)
	return nil
}

func (io *fakeSessionInputIO) delegatedTurn(string) (delegatedTurnRecord, bool, error) {
	io.mu.Lock()
	defer io.mu.Unlock()
	return io.turn, io.hasTurn, nil
}

func (io *fakeSessionInputIO) writeDelegatedTurn(_ string, turn delegatedTurnRecord) error {
	io.mu.Lock()
	defer io.mu.Unlock()
	io.turn = turn
	io.hasTurn = true
	io.operations = append(io.operations, "turn_write")
	return nil
}

func (io *fakeSessionInputIO) clearDelegatedTurn(string) error {
	io.mu.Lock()
	defer io.mu.Unlock()
	io.turn = delegatedTurnRecord{}
	io.hasTurn = false
	io.operations = append(io.operations, "turn_clear")
	return nil
}

func (io *fakeSessionInputIO) paneContent(string) (string, error) {
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
			return delegatedInputConfirmation{Outcome: InputAccepted}, nil
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
	identity := testSessionInputIdentity("future-agent")
	resolver := fixedSessionInputResolver(identity)
	owner := newSessionInputOwner(io)
	acceptedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	first := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "turn-initial",
		Status:          delegatedTurnDispatched,
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
	if !io.hasTurn || io.turn.ID != first.ID || io.turn.Status != delegatedTurnRunning ||
		len(io.submissions) != 1 {
		t.Fatalf("initial durable turn=%+v submissions=%#v", io.turn, io.submissions)
	}

	settledAt := acceptedAt.Add(time.Minute)
	io.turn.Status = delegatedTurnDone
	io.turn.SettledAt = &settledAt
	second := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "turn-follow-up",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      settledAt.Add(time.Second),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	restartedOwner := newSessionInputOwner(io)
	result, err = restartedOwner.submitDelegated(
		"agent:@1", identity, resolver, identity.Command, "follow-up", second,
		scriptedCorrelatedAdmission("follow-up"),
	)
	if err != nil || result.Outcome != InputAccepted || result.Receipt != second.ID {
		t.Fatalf("follow-up delegated submit = (%+v, %v)", result, err)
	}
	if io.turn.ID != second.ID || io.turn.Status != delegatedTurnRunning ||
		len(io.submissions) != 2 {
		t.Fatalf("follow-up durable turn=%+v submissions=%#v", io.turn, io.submissions)
	}
}

func TestSessionInputCursorInitialPreservesExactUTF8AndAcceptsAfterProviderStart(t *testing.T) {
	io := newFakeSessionInputIO()
	identity := testSessionInputIdentity("cursor-agent --force")
	payload := "Concrete task prefix 你好\n\nZen lifecycle protocol:\n- preserve the task\n"
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "cursor-initial",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      time.Date(2026, 8, 5, 1, 1, 0, 0, time.UTC),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}

	result, err := newSessionInputOwner(io).submitDelegated(
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
	if !io.hasTurn || io.turn.Status != delegatedTurnRunning {
		t.Fatalf("Cursor initial accepted before provider start: %+v", io.turn)
	}
}

func TestWatcherDelegatedAdmissionConfirmsCursorInitialAndActiveSteering(t *testing.T) {
	io := newFakeSessionInputIO()
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
	identity := testSessionInputIdentity("cursor-agent --force")
	initial := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "initial-turn",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      time.Now().UTC(),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}

	result, err := owner.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, initialPayload, initial,
		w.delegatedInputConfirmer("agent:@1", identity.Command),
	)
	if err != nil || result.Outcome != InputAccepted ||
		result.TurnID != initial.ID || io.turn.Status != delegatedTurnRunning {
		t.Fatalf("production initial admission = (%+v, %v), turn=%+v", result, err, io.turn)
	}

	steering := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "steering-receipt",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      time.Now().UTC(),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	beforeTurn := io.turn
	result, err = owner.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, followPayload, steering,
		w.delegatedInputConfirmer("agent:@1", identity.Command),
	)
	if err != nil || result.Outcome != InputAccepted ||
		result.TurnID != initial.ID {
		t.Fatalf("production steering admission = (%+v, %v)", result, err)
	}
	if io.turn != beforeTurn {
		t.Fatalf("accepted steering created or reset lifecycle turn: before=%+v after=%+v", beforeTurn, io.turn)
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
			turn := delegatedTurnRecord{
				SchemaVersion:   delegatedTurnSchema,
				ID:              "receipt-" + test.name,
				Status:          delegatedTurnDispatched,
				AcceptedAt:      time.Now().UTC(),
				ProcessIdentity: delegatedTurnIdentity(identity),
			}
			result, err := newSessionInputOwner(io).submitDelegated(
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
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "embedded-marker-prefix-collision",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      time.Now().UTC(),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	result, err := newSessionInputOwner(io).submitDelegated(
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

	result, err = newSessionInputOwner(io).submitDelegated(
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
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "ignored-submit",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      time.Now().UTC(),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	result, err := newSessionInputOwner(io).submitDelegated(
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
	identity := testSessionInputIdentity("cursor-agent --force")
	results := make(chan InputResult, 2)
	errs := make(chan error, 2)
	for _, id := range []string{"concurrent-a", "concurrent-b"} {
		go func(id string) {
			result, err := owner.submitDelegated(
				"agent:@1", identity, fixedSessionInputResolver(identity),
				identity.Command, payload,
				delegatedTurnRecord{
					SchemaVersion:   delegatedTurnSchema,
					ID:              id,
					Status:          delegatedTurnDispatched,
					AcceptedAt:      time.Now().UTC(),
					ProcessIdentity: delegatedTurnIdentity(identity),
				},
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
	identity := testSessionInputIdentity("cursor-agent --force")
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "cursor-follow-up",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      time.Date(2026, 8, 5, 1, 2, 0, 0, time.UTC),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}

	result, err := newSessionInputOwner(io).submitDelegated(
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
}

func TestSessionInputDefiniteQueueNonStartRollsBackAndIsRetryable(t *testing.T) {
	io := newFakeSessionInputIO()
	io.runErr = errors.New("tmux queue did not start")
	io.runStarted = false
	identity := testSessionInputIdentity("cursor-agent --force")
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "cursor-retryable",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      time.Date(2026, 8, 5, 1, 3, 0, 0, time.UTC),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	owner := newSessionInputOwner(io)

	result, err := owner.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "retry me", turn, scriptedCorrelatedAdmission("retry me"),
	)
	if InputOutcomeFromError(err) != InputNotSubmitted ||
		result.Outcome != InputNotSubmitted {
		t.Fatalf("definite non-submit = (%+v, %v)", result, err)
	}
	if _, found := io.ledger.entry(turn.ID); found || io.hasTurn {
		t.Fatalf("definite non-submit retained durable ambiguity: ledger=%+v turn=%+v", io.ledger, io.turn)
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
	identity := testSessionInputIdentity("future-agent")
	resolver := fixedSessionInputResolver(identity)
	acceptedAt := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "turn-idempotent",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      acceptedAt,
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	first := newSessionInputOwner(io)
	if _, err := first.submitDelegated(
		"agent:@1", identity, resolver, identity.Command, "task", turn,
		scriptedCorrelatedAdmission("task"),
	); err != nil {
		t.Fatal(err)
	}
	restarted := newSessionInputOwner(io)
	result, err := restarted.submitDelegated(
		"agent:@1", identity, resolver, identity.Command, "task", turn,
		scriptedCorrelatedAdmission("task"),
	)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != turn.ID || !result.Duplicate {
		t.Fatalf("duplicate new-turn receipt = (%+v, %v)", result, err)
	}
	if len(io.queues) != 1 || io.turn.ID != turn.ID {
		t.Fatalf("duplicate new-turn receipt replayed/reset lifecycle: queues=%d turn=%+v", len(io.queues), io.turn)
	}
}

func TestSessionInputAcceptedDuplicateWithoutTurnMarkerIsAmbiguousAndNotReplayed(t *testing.T) {
	io := newFakeSessionInputIO()
	identity := testSessionInputIdentity("future-agent")
	resolver := fixedSessionInputResolver(identity)
	turn := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "turn-marker-lost",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	first := newSessionInputOwner(io)
	if _, err := first.submitDelegated(
		"agent:@1", identity, resolver, identity.Command, "task", turn,
		scriptedCorrelatedAdmission("task"),
	); err != nil {
		t.Fatal(err)
	}
	io.hasTurn = false
	io.turn = delegatedTurnRecord{}

	restarted := newSessionInputOwner(io)
	_, err := restarted.submitDelegated(
		"agent:@1", identity, resolver, identity.Command, "task", turn,
		scriptedCorrelatedAdmission("task"),
	)
	if InputOutcomeFromError(err) != InputAmbiguous ||
		!strings.Contains(err.Error(), "turn marker is missing") {
		t.Fatalf("accepted duplicate without marker = %v", err)
	}
	if len(io.queues) != 1 {
		t.Fatalf("accepted duplicate without marker replayed input: queues=%d", len(io.queues))
	}
}

func TestSessionInputSteeringWhileRunningRetainsDelegatedTurnIdentity(t *testing.T) {
	io := newFakeSessionInputIO()
	identity := testSessionInputIdentity("future-agent")
	io.hasTurn = true
	io.turn = delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "active-turn",
		Status:          delegatedTurnRunning,
		AcceptedAt:      time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	owner := newSessionInputOwner(io)
	next := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "too-early",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      io.turn.AcceptedAt.Add(time.Second),
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
	if io.turn.ID != "active-turn" || io.turn.Status != delegatedTurnRunning {
		t.Fatalf("steering replaced lifecycle owner: %+v", io.turn)
	}
}

func TestSessionInputActiveSteeringWithoutAdmissionIsAmbiguousAndNeverReplayed(t *testing.T) {
	io := newFakeSessionInputIO()
	identity := testSessionInputIdentity("cursor-agent --force")
	active := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "active-turn",
		Status:          delegatedTurnRunning,
		AcceptedAt:      time.Now().UTC().Add(-time.Minute),
		ProcessIdentity: delegatedTurnIdentity(identity),
		PaneBaseline:    delegatedTurnPaneIdentity("active"),
	}
	io.hasTurn = true
	io.turn = active
	steering := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "steering-ambiguous",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      time.Now().UTC(),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}

	first := newSessionInputOwner(io)
	result, err := first.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "steer once", steering,
		scriptedAmbiguousAdmission(),
	)
	if InputOutcomeFromError(err) != InputAmbiguous ||
		result.Outcome != InputAmbiguous || result.TurnID != active.ID {
		t.Fatalf("ambiguous active steering = (%+v, %v)", result, err)
	}
	if io.turn != active || len(io.queues) != 1 {
		t.Fatalf("ambiguous steering changed lifecycle or queue count: turn=%+v queues=%d", io.turn, len(io.queues))
	}

	restarted := newSessionInputOwner(io)
	result, err = restarted.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "steer once", steering,
		scriptedCorrelatedAdmission("steer once"),
	)
	if InputOutcomeFromError(err) != InputAmbiguous ||
		result.Outcome != InputAmbiguous || result.TurnID != active.ID ||
		len(io.queues) != 1 {
		t.Fatalf("restart replayed ambiguous steering = (%+v, %v), queues=%d", result, err, len(io.queues))
	}
	if io.turn != active {
		t.Fatalf("ambiguous steering duplicate reset lifecycle: %+v", io.turn)
	}
}

func TestSessionInputDuplicateSteeringReceiptRetainsActiveTurn(t *testing.T) {
	io := newFakeSessionInputIO()
	identity := testSessionInputIdentity("future-agent")
	active := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "active-turn",
		Status:          delegatedTurnRunning,
		AcceptedAt:      time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC),
		ProcessIdentity: delegatedTurnIdentity(identity),
		PaneBaseline:    delegatedTurnPaneIdentity("active"),
	}
	io.hasTurn = true
	io.turn = active
	steering := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "steering-delivery",
		Status:          delegatedTurnDispatched,
		AcceptedAt:      active.AcceptedAt.Add(time.Second),
		ProcessIdentity: delegatedTurnIdentity(identity),
	}
	first := newSessionInputOwner(io)
	result, err := first.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "steer", steering, scriptedCorrelatedAdmission("steer"),
	)
	if err != nil || result.TurnID != active.ID {
		t.Fatalf("first steering = (%+v, %v)", result, err)
	}
	restarted := newSessionInputOwner(io)
	result, err = restarted.submitDelegated(
		"agent:@1", identity, fixedSessionInputResolver(identity),
		identity.Command, "steer", steering, scriptedCorrelatedAdmission("steer"),
	)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != active.ID || !result.Duplicate {
		t.Fatalf("duplicate steering receipt = (%+v, %v)", result, err)
	}
	if len(io.queues) != 1 || io.turn != active {
		t.Fatalf("duplicate steering replayed/reset lifecycle: queues=%d turn=%+v", len(io.queues), io.turn)
	}
}

func TestSessionInputDefinitelyNotSubmittedRestoresPriorDelegatedTurn(t *testing.T) {
	io := newFakeSessionInputIO()
	identity := testSessionInputIdentity("future-agent")
	settledAt := time.Date(2026, 8, 5, 1, 1, 0, 0, time.UTC)
	prior := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "settled-prior",
		Status:          delegatedTurnDone,
		AcceptedAt:      settledAt.Add(-time.Minute),
		ProcessIdentity: delegatedTurnIdentity(identity),
		PaneBaseline:    delegatedTurnPaneIdentity("prior"),
		SettledAt:       &settledAt,
	}
	io.hasTurn = true
	io.turn = prior
	io.runErr = errors.New("queue did not start")
	io.runStarted = false
	owner := newSessionInputOwner(io)
	next := delegatedTurnRecord{
		SchemaVersion:   delegatedTurnSchema,
		ID:              "next",
		Status:          delegatedTurnDispatched,
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
	if !io.hasTurn || io.turn.ID != prior.ID || io.turn.Status != prior.Status {
		t.Fatalf("definite pre-submit failure did not restore prior turn: %+v", io.turn)
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
