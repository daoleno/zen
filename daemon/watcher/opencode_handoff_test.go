package watcher

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// openCodeIdleReadyContent is the OpenCode 1.18.15 idle composer view captured
// live on 2026-08-08 (footer without semver).
const openCodeIdleReadyContent = `   ┃
   ┃  Ask anything...
   ┃
   ┃  Build auto · DeepSeek V4 Flash (2x usage) OpenCode Go · max
   ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
   /home/daoleno/project  0.0% · $0.00  ctrl+p commands
`

// openCodeStartingContent is a pre-ready startup view: no composer
// placeholder, no idle footer.
const openCodeStartingContent = `   ┃
   ┃  Loading...
   ┃  Build auto · DeepSeek V4 Flash (2x usage) OpenCode Go · max
   ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
   esc interrupt         0.0% · $0.00  ctrl+p commands
`

// openCodeHomeIdleContent is the exact live capture from the real Calendar
// occurrence d9ff47a4 (Session @71, 2026-08-08 12:48 Asia/Shanghai): the home
// cwd renders as a bare "~" with the 1.18.15 semver at the right.
const openCodeHomeIdleContent = `   ┃  Ask anything... "What is the tech stack of this project?"
   ┃  Build auto · DeepSeek V4 Flash (2x usage) OpenCode Go · max
   tab agents  ctrl+p commands
  ~                                                                    1.18.15
`

// scriptedOpenCodeHandoff builds a Watcher whose pane content, target
// identity, and input submission are all deterministic. Panes stay alive;
// content is drawn from the scripted sequence per capture call.
func scriptedOpenCodeHandoff(t *testing.T, contents []string) (*Watcher, *fakeSessionInputIO, *sync.Mutex, *int) {
	t.Helper()
	io := newFakeSessionInputIO()
	owner := newSessionInputOwner(io)
	w := New(time.Second)
	w.sessionInput = owner
	w.targetProcessResolver = fixedSessionInputResolver(testSessionInputIdentity("opencode"))
	w.targetOwnershipResolver = func(string) (bool, error) { return true, nil }
	w.agents["opencode-handoff:@1"] = &classifier.Agent{
		ID:        "opencode-handoff:@1",
		Command:   "opencode",
		Cwd:       "/repo/zen",
		PaneAlive: true,
		Delegated: true,
	}
	mu := &sync.Mutex{}
	calls := 0
	previous := capturePaneContentFunc
	capturePaneContentFunc = func(string) (string, bool, int) {
		mu.Lock()
		defer mu.Unlock()
		index := calls
		if index >= len(contents) {
			index = len(contents) - 1
		}
		calls++
		return contents[index], true, -1
	}
	t.Cleanup(func() { capturePaneContentFunc = previous })
	return w, io, mu, &calls
}

func TestSendInputWhenReadyBudgetedDelayedReadinessSubmitsOnce(t *testing.T) {
	// First readiness probes fail (startup screen); later the 1.18.15 idle
	// composer appears; the one submit succeeds and no duplicate is sent.
	w, io, _, _ := scriptedOpenCodeHandoff(t, []string{
		openCodeStartingContent,
		openCodeStartingContent,
		openCodeIdleReadyContent,
	})
	start := time.Now()
	err := w.SendInputWhenReadyBudgeted("opencode-handoff:@1", "opencode", "Your work item: /tmp/scheduled.md\n", 3*time.Second)
	if err != nil {
		t.Fatalf("budgeted handoff = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("budgeted handoff took %s", elapsed)
	}
	if len(io.submissions) != 1 || io.submissions[0] != "Your work item: /tmp/scheduled.md" {
		t.Fatalf("submissions = %#v, want exactly one prompt", io.submissions)
	}
}

func TestSendInputWhenReadyBudgetedRetriesAfterFullAttemptTimeoutThenSubmitsOnce(t *testing.T) {
	// The pane stays on the startup screen for a full per-attempt timeout
	// (truncated to the occurrence budget), so attempt 1 is definitely-not-
	// submitted; the retry then finds the ready composer and submits exactly
	// once. This is the deterministic delayed-readiness fixture: first probes
	// fail, one later submit succeeds, task runs once.
	w, io, mu, calls := scriptedOpenCodeHandoff(t, nil)
	readyAt := 9 // first attempt makes up to 8 probes (~1.05s of 900ms budget)
	previous := capturePaneContentFunc
	capturePaneContentFunc = func(string) (string, bool, int) {
		mu.Lock()
		defer mu.Unlock()
		*calls++
		if *calls >= readyAt {
			return openCodeIdleReadyContent, true, -1
		}
		return openCodeStartingContent, true, -1
	}
	defer func() { capturePaneContentFunc = previous }()

	start := time.Now()
	err := w.SendInputWhenReadyBudgeted("opencode-handoff:@1", "opencode", "task\n", 1400*time.Millisecond)
	if err != nil {
		t.Fatalf("budgeted handoff = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("budgeted handoff took %s", elapsed)
	}
	if len(io.submissions) != 1 {
		t.Fatalf("submissions = %d, want exactly one", len(io.submissions))
	}
	if len(io.queues) != 1 {
		t.Fatalf("queues = %d, want exactly one", len(io.queues))
	}
}

func TestSubmitDelegatedInputWhenReadyBudgetedRetriesReadinessAndAdmitsOneTurn(t *testing.T) {
	payload := "execute this delegated task exactly once"
	w, io, mu, calls := scriptedOpenCodeHandoff(t, nil)
	readyAt := 9
	previous := capturePaneContentFunc
	capturePaneContentFunc = func(string) (string, bool, int) {
		mu.Lock()
		defer mu.Unlock()
		*calls++
		if *calls >= readyAt {
			return openCodeIdleReadyContent, true, -1
		}
		return openCodeStartingContent, true, -1
	}
	defer func() { capturePaneContentFunc = previous }()
	ledger := newFakeTurnLedger()
	w.turnLedger = ledger
	w.sessionInput.ledger = ledger
	w.providerActivityProbe = &transportAdmissionProbe{
		io: io,
		inputs: map[int]string{
			0: "older input",
			1: payload,
		},
		missing: map[int]bool{},
		staleAt: map[int]time.Time{},
		activity: map[int]string{
			0: "older-activity",
			1: "delegated-activity",
		},
	}
	acceptedAt := time.Now().UTC()
	result, err := w.SubmitDelegatedInputWhenReadyBudgeted(
		"opencode-handoff:@1", "opencode", payload,
		"work-budgeted-delegated", "turn-budgeted-delegated", acceptedAt,
		1400*time.Millisecond,
	)
	if err != nil || result.Outcome != InputAccepted || result.TurnID != "turn-budgeted-delegated" {
		t.Fatalf("budgeted delegated result=%+v err=%v", result, err)
	}
	if len(io.submissions) != 1 || len(io.queues) != 1 || io.submissions[0] != payload {
		t.Fatalf("budgeted delegated effects submissions=%+v queues=%d", io.submissions, len(io.queues))
	}
	if turn := ledger.snapshot("opencode-handoff:@1"); turn.TurnID != "turn-budgeted-delegated" ||
		turn.Status != TurnAccepted {
		t.Fatalf("budgeted delegated canonical turn=%+v", turn)
	}
}

func TestSubmitDelegatedInputWhenReadyBudgetedExhaustionCreatesNoTurn(t *testing.T) {
	w, io, _, _ := scriptedOpenCodeHandoff(t, []string{openCodeStartingContent})
	ledger := newFakeTurnLedger()
	w.turnLedger = ledger
	w.sessionInput.ledger = ledger
	result, err := w.SubmitDelegatedInputWhenReadyBudgeted(
		"opencode-handoff:@1", "opencode", "never submitted",
		"work-never-ready", "turn-never-ready", time.Now().UTC(),
		400*time.Millisecond,
	)
	if err == nil || !errors.Is(err, ErrAgentInputNotReady) || result.Outcome != InputNotSubmitted {
		t.Fatalf("never-ready delegated result=%+v err=%v", result, err)
	}
	if _, found, err := ledger.Turn("opencode-handoff:@1"); err != nil || found {
		t.Fatalf("never-ready delegated created a Turn found=%v err=%v", found, err)
	}
	if _, found, err := ledger.TurnSubmission("opencode-handoff:@1", "turn-never-ready"); err != nil || found {
		t.Fatalf("never-ready delegated created a submission found=%v err=%v", found, err)
	}
	if len(io.submissions) != 0 || len(io.queues) != 0 {
		t.Fatalf("never-ready delegated mutated provider submissions=%+v queues=%d", io.submissions, len(io.queues))
	}
}

func TestSendInputWhenReadyBudgetedTimeoutIsBoundedRetryableNotSubmitted(t *testing.T) {
	w, io, _, _ := scriptedOpenCodeHandoff(t, []string{openCodeStartingContent})
	start := time.Now()
	err := w.SendInputWhenReadyBudgeted("opencode-handoff:@1", "opencode", "task\n", 400*time.Millisecond)
	if err == nil {
		t.Fatal("budgeted handoff succeeded with a never-ready pane")
	}
	if !errors.Is(err, ErrAgentInputNotReady) {
		t.Fatalf("error = %v, want ErrAgentInputNotReady", err)
	}
	if outcome := InputOutcomeFromError(err); outcome != InputNotSubmitted {
		t.Fatalf("outcome = %s, want definitely not submitted", outcome)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("never-ready handoff was not bounded: %s", elapsed)
	}
	if len(io.submissions) != 0 || len(io.queues) != 0 {
		t.Fatalf("never-ready handoff submitted: submissions=%#v queues=%d", io.submissions, len(io.queues))
	}
}

func TestSendInputWhenReadyBudgetedAmbiguousAdmissionNeverReplays(t *testing.T) {
	w, io, _, _ := scriptedOpenCodeHandoff(t, []string{openCodeIdleReadyContent})
	io.runErr = errors.New("provider queue failed")
	io.runStarted = true // the queue started and may have submitted
	start := time.Now()
	err := w.SendInputWhenReadyBudgeted("opencode-handoff:@1", "opencode", "task\n", 5*time.Second)
	if err == nil {
		t.Fatal("budgeted handoff succeeded under ambiguous admission")
	}
	if outcome := InputOutcomeFromError(err); outcome != InputAmbiguous {
		t.Fatalf("outcome = %s, want ambiguous", outcome)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("ambiguous handoff was not fail-fast: %s", elapsed)
	}
	if len(io.queues) != 1 {
		t.Fatalf("ambiguous admission was replayed: queues=%d", len(io.queues))
	}
}

func TestSendInputWhenReadyBudgetedSessionEndedWithoutNotificationFailsOnce(t *testing.T) {
	// The spawned provider ended (process/pane gone) without ever notifying:
	// the exact spawned identity is no longer attributable, so the handoff
	// fails closed within the bounded identity window instead of retrying a
	// dead session or leaving the occurrence running.
	w, io, _, _ := scriptedOpenCodeHandoff(t, []string{openCodeIdleReadyContent})
	w.targetProcessResolver = func(string) (targetProcessIdentity, bool) {
		return targetProcessIdentity{}, false
	}
	start := time.Now()
	err := w.SendInputWhenReadyBudgeted("opencode-handoff:@1", "opencode", "task\n", 400*time.Millisecond)
	if err == nil {
		t.Fatal("budgeted handoff succeeded after the spawned session ended")
	}
	if errors.Is(err, ErrAgentInputNotReady) {
		t.Fatalf("ended session was treated as retryable not-ready: %v", err)
	}
	if !strings.Contains(err.Error(), "target provider could not be proven") {
		t.Fatalf("error = %v, want unprovable target identity", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("ended-session handoff was not fail-fast: %s", elapsed)
	}
	if len(io.submissions) != 0 || len(io.queues) != 0 {
		t.Fatalf("ended-session handoff submitted: submissions=%#v queues=%d", io.submissions, len(io.queues))
	}
}

func TestSendInputWhenReadyBudgetedExactHomeCaptureSubmitsOnce(t *testing.T) {
	// Regression for the rejected real occurrence d9ff47a4: the exact @71
	// live capture (home cwd "~" + anchored semver) must reach readiness and
	// the bounded handoff must submit exactly once after the startup probes
	// fail.
	w, io, _, _ := scriptedOpenCodeHandoff(t, []string{
		openCodeStartingContent,
		openCodeStartingContent,
		openCodeHomeIdleContent,
	})
	err := w.SendInputWhenReadyBudgeted("opencode-handoff:@1", "opencode", "Your work item: /tmp/scheduled.md\n", 3*time.Second)
	if err != nil {
		t.Fatalf("budgeted handoff = %v, want success on the exact @71 idle capture", err)
	}
	if len(io.submissions) != 1 || io.submissions[0] != "Your work item: /tmp/scheduled.md" {
		t.Fatalf("submissions = %#v, want exactly one prompt", io.submissions)
	}
	if len(io.queues) != 1 {
		t.Fatalf("queues = %d, want exactly one submit queue", len(io.queues))
	}
}

func TestAgentInputNotReadyIsDistinctFromOtherNotSubmitted(t *testing.T) {
	retryable := agentInputNotReady("opencode")
	if !errors.Is(retryable, ErrAgentInputNotReady) {
		t.Fatal("agentInputNotReady must unwrap ErrAgentInputNotReady")
	}
	if outcome := InputOutcomeFromError(retryable); outcome != InputNotSubmitted {
		t.Fatalf("outcome = %s", outcome)
	}
	terminal := definitelyNotSubmitted("", errors.New("target provider could not be proven"))
	if errors.Is(terminal, ErrAgentInputNotReady) {
		t.Fatal("unprovable target identity must not be retryable")
	}
	if !strings.Contains(retryable.Error(), `agent input not ready for "opencode"`) {
		t.Fatalf("error text changed: %v", retryable)
	}
}
