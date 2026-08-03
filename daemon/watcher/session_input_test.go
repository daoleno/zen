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
)

type fakeSessionInputIO struct {
	mu             sync.Mutex
	paneValue      sessionInputPane
	buffers        map[string]string
	loadedPayloads []string
	queues         [][]string
	submissions    []string
	ledger         sessionInputReceiptLedger
	ledgerWrites   []sessionInputReceiptLedger
	writeErrors    map[int]error
	operations     []string
	runStarted     bool
	runErr         error
	afterLoad      func()
	activeQueues   int
	maxQueues      int
}

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
	io.submissions = append(io.submissions, io.buffers[buffer])
	started, err := io.runStarted, io.runErr
	io.activeQueues--
	io.mu.Unlock()
	return started, err
}

func (io *fakeSessionInputIO) receiptLedger(string) (sessionInputReceiptLedger, error) {
	io.mu.Lock()
	defer io.mu.Unlock()
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

func TestSessionInputCodexAndClaudeUseExactCommonSubmit(t *testing.T) {
	for _, command := range []string{"codex --no-alt-screen", "claude --permission-mode bypassPermissions"} {
		t.Run(command, func(t *testing.T) {
			io := newFakeSessionInputIO()
			owner := newSessionInputOwner(io)
			identity := testSessionInputIdentity(command)
			payload := "你好, Claude/Codex 👋\nline two\n\n"

			result, err := owner.submit("agent:@1", identity, fixedSessionInputResolver(identity), command, payload, "")
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
		})
	}
}

func TestSessionInputCursorAndGrokRouteThroughSharedOwner(t *testing.T) {
	tests := []struct {
		command string
		settle  string
	}{
		{command: "cursor-agent --force", settle: "sleep 0.400"},
		{command: "grok --no-alt-screen", settle: "sleep 0.300"},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			io := newFakeSessionInputIO()
			owner := newSessionInputOwner(io)
			identity := testSessionInputIdentity(test.command)
			if _, err := owner.submit("agent:@1", identity, fixedSessionInputResolver(identity), test.command, "hello", ""); err != nil {
				t.Fatal(err)
			}
			assertSingleSubmitQueue(t, io.queues[0], "Enter")
			if indexOf(io.queues[0], test.settle) < 0 {
				t.Fatalf("queue %q does not contain provider settle %q", io.queues[0], test.settle)
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
	if adapter.submitKey != "Enter" || adapter.settle != 250*time.Millisecond {
		t.Fatalf("Claude adapter = %+v", adapter)
	}
}
