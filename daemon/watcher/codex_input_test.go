package watcher

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

type fakeCodexInputIO struct {
	captureFn           func(*fakeCodexInputIO) codexPaneCapture
	captures            []string
	alive               []bool
	states              []codexComposerState
	generations         []string
	index               int
	clock               time.Time
	pastes              []string
	enters              int
	pasteErr            error
	submitErrors        []error
	submitAttempts      int
	enterErr            error
	beforeEnter         func(*fakeCodexInputIO)
	suppressPersistence bool
	persistedMessages   []string
	persistedByRollout  map[string][]string
	submitted           chan struct{}
}

type failCodexStoreAtPhase struct {
	codexTransactionStore
	phase  codexTransactionPhase
	failed bool
}

func (store *failCodexStoreAtPhase) Save(record codexTransactionRecord) error {
	if !store.failed && record.Phase == store.phase {
		store.failed = true
		return errors.New("injected durable save failure")
	}
	return store.codexTransactionStore.Save(record)
}

func (f *fakeCodexInputIO) capture(string) codexPaneCapture {
	if f.captureFn != nil {
		capture := f.captureFn(f)
		if !capture.rollout.valid() && capture.generation != "" {
			capture.rollout = fakeCodexRollout(capture.generation)
		}
		return capture
	}
	if len(f.captures) == 0 {
		return codexPaneCapture{}
	}
	index := f.index
	if index >= len(f.captures) {
		index = len(f.captures) - 1
	}
	f.index++
	alive := true
	if len(f.alive) > 0 {
		aliveIndex := index
		if aliveIndex >= len(f.alive) {
			aliveIndex = len(f.alive) - 1
		}
		alive = f.alive[aliveIndex]
	}
	state := codexComposerUnknown
	if len(f.states) > 0 {
		stateIndex := index
		if stateIndex >= len(f.states) {
			stateIndex = len(f.states) - 1
		}
		state = f.states[stateIndex]
	} else if len(f.pastes) == 0 {
		state = codexComposerEmpty
	} else {
		state = codexComposerHasDraft
	}
	generation := "generation-1"
	if len(f.generations) > 0 {
		generationIndex := index
		if generationIndex >= len(f.generations) {
			generationIndex = len(f.generations) - 1
		}
		generation = f.generations[generationIndex]
	}
	capture := codexPaneCapture{
		content:    f.captures[index],
		alive:      alive,
		composer:   state,
		generation: generation,
	}
	capture.rollout = fakeCodexRollout(generation)
	return capture
}

func (f *fakeCodexInputIO) submitIfEmpty(
	sessionID string,
	generation string,
	rollout codexRolloutIdentity,
	_ string,
	body string,
) error {
	f.submitAttempts++
	if f.beforeEnter != nil {
		f.beforeEnter(f)
		current := f.capture(sessionID)
		if !current.alive ||
			current.generation != generation ||
			current.composer != codexComposerEmpty ||
			!current.rollout.equal(rollout) ||
			!isAgentInputReady("codex", current.content) {
			return fmt.Errorf("%w: foreign content was preserved", errCodexMutationConflict)
		}
	}
	if len(f.submitErrors) > 0 {
		err := f.submitErrors[0]
		f.submitErrors = f.submitErrors[1:]
		if err != nil {
			return err
		}
	}
	if f.pasteErr != nil {
		return f.pasteErr
	}
	f.pastes = append(f.pastes, body)
	if f.enterErr != nil {
		return f.enterErr
	}
	f.enters++
	if !f.suppressPersistence {
		if f.persistedByRollout != nil {
			f.persistedByRollout[rollout.Path] = append(f.persistedByRollout[rollout.Path], body)
		} else {
			f.persistedMessages = append(f.persistedMessages, body)
		}
	}
	if f.submitted != nil {
		select {
		case f.submitted <- struct{}{}:
		default:
		}
	}
	return nil
}

func (f *fakeCodexInputIO) advanceStartup(string, string) error {
	if f.enterErr != nil {
		return f.enterErr
	}
	f.enters++
	return nil
}

func (f *fakeCodexInputIO) releaseStaleInputSuppression(
	string,
	codexPaneInputSuppression,
) error {
	return nil
}

func (f *fakeCodexInputIO) persistedUserMessage(
	rollout codexRolloutIdentity,
	instruction string,
	_ time.Time,
) (bool, error) {
	messages := f.persistedMessages
	if f.persistedByRollout != nil {
		messages = f.persistedByRollout[rollout.Path]
	}
	for _, message := range messages {
		if message == instruction {
			return true, nil
		}
	}
	return false, nil
}

func fakeCodexRollout(generation string) codexRolloutIdentity {
	return codexRolloutIdentity{
		Path:      "/fake/codex/sessions/rollout-" + generation + ".jsonl",
		SessionID: "rollout-" + generation,
	}
}

func (f *fakeCodexInputIO) sleep(delay time.Duration) { f.clock = f.clock.Add(delay) }
func (f *fakeCodexInputIO) now() time.Time            { return f.clock }

func testCodexSubmitConfig() codexSubmitConfig {
	return codexSubmitConfig{
		startupStallTimeout: 3 * time.Second,
		confirmationReserve: 2 * time.Second,
		confirmationWindow:  time.Second,
		totalTimeout:        20 * time.Second,
		pollInterval:        time.Second,
		stableReadyPolls:    2,
	}
}

func codexReadyPane(extra string) string {
	return "╭────╮\n│ >_ OpenAI Codex (v0.144.3) │\n│ model: gpt-5.6 medium │\n╰────╯\n" + extra + "\n› Find and fix a bug in @filename\n\n  gpt-5.6 medium · /tmp\n"
}

func TestCodexRolloutIdentityAllowsPathToAppearOnlyForSameSession(t *testing.T) {
	beforeRollout := codexRolloutIdentity{SessionID: "session-target"}
	afterRollout := codexRolloutIdentity{
		Path:      "/codex/sessions/rollout-target.jsonl",
		SessionID: "session-target",
	}
	if !beforeRollout.equal(afterRollout) || !afterRollout.equal(beforeRollout) {
		t.Fatal("the exact pre-rollout session must remain identical when its path appears")
	}
	if beforeRollout.equal(codexRolloutIdentity{
		Path:      afterRollout.Path,
		SessionID: "session-foreign",
	}) {
		t.Fatal("a foreign Codex session must never match the target identity")
	}
	if afterRollout.equal(codexRolloutIdentity{
		Path:      "/codex/sessions/rollout-other.jsonl",
		SessionID: afterRollout.SessionID,
	}) {
		t.Fatal("two observable rollout paths must agree exactly")
	}
}

func TestCodexPaneInputLockExcludesForeignKeysUntilOwnedMutation(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required for the provider-boundary lock test")
	}
	sessionID := fmt.Sprintf("zen-codex-lock-test-%d", time.Now().UnixNano())
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", sessionID, "cat").CombinedOutput(); err != nil {
		t.Fatalf("create tmux lock test: %v%s", err, commandOutputSuffix(out))
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionID).Run()
	})
	metadata := readCodexTmuxPaneMetadata(sessionID)
	if !metadata.alive || metadata.generation == "" {
		t.Fatalf("tmux metadata = %#v", metadata)
	}
	lock, err := lockCodexPaneInput(sessionID, metadata.generation)
	if err != nil {
		t.Fatalf("lock pane input: %v", err)
	}
	defer lock.release()
	if out, err := exec.Command("tmux", "send-keys", "-t", sessionID, "FOREIGN").CombinedOutput(); err != nil {
		t.Fatalf("attempt foreign key delivery: %v%s", err, commandOutputSuffix(out))
	}
	if out, err := exec.Command("tmux", "capture-pane", "-p", "-t", sessionID).Output(); err != nil {
		t.Fatalf("capture locked pane: %v", err)
	} else if strings.Contains(string(out), "FOREIGN") {
		t.Fatal("foreign keys reached the exclusively locked pane")
	}
	if err := lock.mutate("send-keys", "-t", lock.paneID, "OWNED"); err != nil {
		t.Fatalf("owned mutation: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		out, captureErr := exec.Command("tmux", "capture-pane", "-p", "-t", sessionID).Output()
		if captureErr == nil && strings.Contains(string(out), "OWNED") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("owned keys were not delivered: output=%q err=%v", out, captureErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	after := readCodexTmuxPaneMetadata(sessionID)
	if after.inputOff {
		t.Fatal("owned mutation did not release the pane input lock")
	}
}

func TestCodexAtomicSubmitUsesOnePasteEnterCommandQueue(t *testing.T) {
	got := codexAtomicSubmitCommand("zen-buffer", "%42")
	want := []string{
		"paste-buffer", "-p", "-b", "zen-buffer", "-t", "%42",
		";", "send-keys", "-t", "%42", "Enter",
		";", "delete-buffer", "-b", "zen-buffer",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("atomic submit command = %#v, want %#v", got, want)
	}
	if strings.Count(strings.Join(got, "\x00"), "Enter") != 1 {
		t.Fatalf("atomic submit must contain exactly one Enter: %#v", got)
	}
}

func TestCodexPaneInputLockHelperCrashRecoversWithoutReplay(t *testing.T) {
	if os.Getenv("ZEN_CODEX_LOCK_CRASH_HELPER") == "1" {
		sessionID := os.Getenv("ZEN_CODEX_LOCK_SESSION")
		generation := os.Getenv("ZEN_CODEX_LOCK_GENERATION")
		transactionID := os.Getenv("ZEN_CODEX_LOCK_TRANSACTION")
		_, err := lockCodexPaneInputOwned(
			sessionID,
			generation,
			transactionID,
			codexPaneLockOperationSubmit,
		)
		if err != nil {
			t.Fatalf("helper lock: %v", err)
		}
		os.Exit(0)
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required for the crash-recovery test")
	}

	sessionID := fmt.Sprintf("zen-codex-crash-lock-%d", time.Now().UnixNano())
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", sessionID, "cat").CombinedOutput(); err != nil {
		t.Fatalf("create crash-lock pane: %v%s", err, commandOutputSuffix(out))
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "select-pane", "-e", "-t", sessionID).Run()
		_ = exec.Command("tmux", "kill-session", "-t", sessionID).Run()
	})
	io := realCodexInputIO{}
	capture := io.capture(sessionID)
	if !capture.alive || capture.generation == "" {
		t.Fatalf("initial capture = %#v", capture)
	}
	before, err := exec.Command("tmux", "capture-pane", "-p", "-t", sessionID).Output()
	if err != nil {
		t.Fatalf("capture before helper: %v", err)
	}
	stateDir := t.TempDir()
	first, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("first coordinator: %v", err)
	}
	transactionID := "crash-owned-enter-pending"
	payload := "crash-safe exact instruction"
	now := time.Now().UTC()
	rolloutPath := filepath.Join(t.TempDir(), "rollout-crash-recovery.jsonl")
	record := codexTransactionRecord{
		SchemaVersion:     codexTransactionSchemaVersion,
		TransactionID:     transactionID,
		SessionID:         sessionID,
		SessionGeneration: capture.generation,
		Action:            "submit_codex_input",
		Phase:             codexTransactionEnterPending,
		PayloadSHA256:     codexSHA256(payload),
		Instruction:       payload,
		InstructionSHA256: codexSHA256(payload),
		RolloutPath:       rolloutPath,
		RolloutSessionID:  "crash-recovery-rollout",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := first.store.Save(record); err != nil {
		t.Fatalf("save enter-pending owner: %v", err)
	}

	helper := exec.Command(os.Args[0], "-test.run=^TestCodexPaneInputLockHelperCrashRecoversWithoutReplay$")
	helper.Env = append(
		os.Environ(),
		"ZEN_CODEX_LOCK_CRASH_HELPER=1",
		"ZEN_CODEX_LOCK_SESSION="+sessionID,
		"ZEN_CODEX_LOCK_GENERATION="+capture.generation,
		"ZEN_CODEX_LOCK_TRANSACTION="+transactionID,
	)
	if output, err := helper.CombinedOutput(); err != nil {
		t.Fatalf("crash helper: %v\n%s", err, output)
	}
	if metadata := readCodexTmuxPaneMetadata(sessionID); !metadata.inputOff {
		t.Fatal("helper did not leave pane input suppressed")
	}

	restarted, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("restarted coordinator: %v", err)
	}
	rawActions := 0
	err = restarted.mutateIfUnowned(io, sessionID, func() error {
		rawActions++
		return nil
	})
	if err == nil || rawActions != 0 {
		t.Fatalf("unrelated raw mutation error=%v actions=%d", err, rawActions)
	}
	if metadata := readCodexTmuxPaneMetadata(sessionID); metadata.inputOff {
		t.Fatal("restart did not release the proven stale pane suppression")
	}
	after, err := exec.Command("tmux", "capture-pane", "-p", "-t", sessionID).Output()
	if err != nil {
		t.Fatalf("capture after recovery: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("crash recovery changed composer bytes\nbefore=%q\nafter=%q", before, after)
	}

	writeCodexJournalTestRows(t, rolloutPath, []map[string]any{
		codexJournalSessionMeta(now, record.RolloutSessionID),
		{
			"timestamp": now.Add(time.Second).Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": payload,
			},
		},
	})
	if err := restarted.submit(io, sessionID, payload, testCodexSubmitConfig()); err != nil {
		t.Fatalf("exact rollout reconciliation: %v", err)
	}
	final, err := exec.Command("tmux", "capture-pane", "-p", "-t", sessionID).Output()
	if err != nil || !bytes.Equal(before, final) {
		t.Fatalf("reconciliation replayed mutation output=%q err=%v", final, err)
	}
}

func TestCodexCaptureIncludesEditableRowsBelowCursor(t *testing.T) {
	const foreignSuffix = "ROUND6_FOREIGN_SUFFIX_BELOW_CURSOR_终_Ω"
	binDir := t.TempDir()
	tmuxPath := filepath.Join(binDir, "tmux")
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  display-message)
    printf '0\tround6-deterministic\t1785682000\t@1\t%%%%1\t%d\tcodex\t120\t0\t\t\t\t0\t0\t/dev/null\t36\t1\n'
    ;;
  capture-pane)
    bounded=0
    for arg in "$@"; do
      if [ "$arg" = "-E" ]; then
        bounded=1
      fi
    done
    if [ "$bounded" = "1" ]; then
      printf '│ >_ OpenAI Codex (round6 deterministic capture) │\n› ROUND6_EXACT_OWNED_INSTRUCTION'
    else
      printf '│ >_ OpenAI Codex (round6 deterministic capture) │\n› ROUND6_EXACT_OWNED_INSTRUCTION\n  %s\n\n  \033[38;2;246;226;183mgpt-5.6-sol xhigh\033[2m\033[39m · \033[0m\033[38;2;171;223;167m~/workspace\033[0m\n'
    fi
    ;;
esac
exit 0
`, os.Getpid(), foreignSuffix)
	if err := os.WriteFile(tmuxPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write deterministic tmux: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	capture := (realCodexInputIO{}).capture("round6-deterministic:@1")
	if !capture.alive || capture.generation == "" {
		t.Fatalf("deterministic capture = %#v", capture)
	}
	if !strings.Contains(capture.content, foreignSuffix) {
		t.Fatalf(
			"complete editable composer omitted the row below the cursor: %q",
			capture.content,
		)
	}
}

func TestUntrackedCodexMutationsUseAuthoritativeResolverAndArbiter(t *testing.T) {
	sessionID := "external:@untracked-codex"
	generation := "generation-untracked-codex"
	io := &fakeCodexInputIO{
		captures:    []string{"plain pane without provider header"},
		generations: []string{generation},
	}
	coordinator := newCodexInputCoordinator()
	record := codexTransactionRecord{
		SchemaVersion:     codexTransactionSchemaVersion,
		TransactionID:     "untracked-codex-ambiguous",
		SessionID:         sessionID,
		SessionGeneration: generation,
		Action:            "submit_codex_input",
		Phase:             codexTransactionAmbiguous,
		PayloadSHA256:     codexSHA256("owned"),
		Instruction:       "owned",
		InstructionSHA256: codexSHA256("owned"),
		RolloutPath:       fakeCodexRollout(generation).Path,
		RolloutSessionID:  fakeCodexRollout(generation).SessionID,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := coordinator.store.Save(record); err != nil {
		t.Fatalf("save ambiguous owner: %v", err)
	}
	w := New(time.Second)
	w.codexInput = coordinator
	w.codexInputIO = io
	resolverCalls := 0
	w.targetCommandResolver = func(target string) (string, bool) {
		resolverCalls++
		if target != sessionID {
			t.Fatalf("resolver target = %q", target)
		}
		return "codex --no-alt-screen", true
	}

	mutations := []struct {
		name string
		run  func() error
	}{
		{name: "input", run: func() error { return w.SendInput(sessionID, "foreign") }},
		{name: "enter key", run: func() error { return w.SendKey(sessionID, "Enter") }},
		{name: "enter action", run: func() error { return w.SendAction(sessionID, "show_diff") }},
		{name: "control-c action", run: func() error { return w.SendAction(sessionID, "pause") }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			err := mutation.run()
			if err == nil || !strings.Contains(err.Error(), "unresolved") {
				t.Fatalf("mutation error = %v", err)
			}
		})
	}
	if resolverCalls < len(mutations)*2 {
		t.Fatalf("authoritative resolver calls=%d want at least %d", resolverCalls, len(mutations)*2)
	}
	if len(io.pastes) != 0 || io.enters != 0 {
		t.Fatalf("untracked Codex mutated provider pastes=%#v enters=%d", io.pastes, io.enters)
	}
}

func TestUntrackedTargetWithUnresolvedIdentityFailsClosed(t *testing.T) {
	const sessionID = "external:@unresolved-provider"
	w := New(time.Second)
	resolverCalls := 0
	w.targetCommandResolver = func(target string) (string, bool) {
		resolverCalls++
		if target != sessionID {
			t.Fatalf("resolver target = %q", target)
		}
		return "", false
	}

	err := w.SendInput(sessionID, "must not reach tmux")
	if err == nil || !strings.Contains(err.Error(), "provider could not be proven") {
		t.Fatalf("unresolved provider error = %v", err)
	}
	if resolverCalls != 1 {
		t.Fatalf("authoritative resolver calls=%d want=1", resolverCalls)
	}
}

func TestCachedAndExplicitCommandsCannotOverrideAuthoritativeCodexTarget(t *testing.T) {
	const (
		sessionID  = "agent:@cached-claude-actual-codex"
		generation = "generation-cached-claude-actual-codex"
	)
	binDir := t.TempDir()
	rawLog := filepath.Join(t.TempDir(), "raw-actions.log")
	tmuxPath := filepath.Join(binDir, "tmux")
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  send-keys|paste-buffer)
    printf '%%s\n' "$*" >> %q
    ;;
  capture-pane)
    printf 'Claude Code v2.1.214\nCursor Agent\nGrok 1\n❯ \nbypass permissions on\nEnter: send\n'
    ;;
  list-panes)
    printf '0\n'
    ;;
esac
exit 0
`, rawLog)
	if err := os.WriteFile(tmuxPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write recording tmux: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	io := &fakeCodexInputIO{
		captures:    []string{"plain current Codex pane"},
		generations: []string{generation},
	}
	coordinator := newCodexInputCoordinator()
	record := codexTransactionRecord{
		SchemaVersion:     codexTransactionSchemaVersion,
		TransactionID:     "cached-command-ambiguous",
		SessionID:         sessionID,
		SessionGeneration: generation,
		Action:            "submit_codex_input",
		Phase:             codexTransactionAmbiguous,
		PayloadSHA256:     codexSHA256("owned"),
		Instruction:       "owned",
		InstructionSHA256: codexSHA256("owned"),
		RolloutPath:       fakeCodexRollout(generation).Path,
		RolloutSessionID:  fakeCodexRollout(generation).SessionID,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := coordinator.store.Save(record); err != nil {
		t.Fatalf("save ambiguous owner: %v", err)
	}
	resolverCalls := 0
	resolver := func(target string) (targetProcessIdentity, bool) {
		resolverCalls++
		if target != sessionID {
			t.Fatalf("resolver target = %q", target)
		}
		return targetProcessIdentity{
			Command:         "codex --no-alt-screen",
			PanePID:         41001,
			PaneStart:       1785681000000000000,
			ForegroundID:    41009,
			ForegroundStart: 1785681001000000000,
			ProcessID:       41009,
			ProcessStart:    1785681001000000000,
		}, true
	}
	w := New(time.Second)
	w.codexInput = coordinator
	w.codexInputIO = io
	w.targetProcessResolver = resolver
	w.agents[sessionID] = &classifier.Agent{
		ID:      sessionID,
		Command: "claude --permission-mode bypassPermissions",
	}

	defaultCodexInputCoordinatorMu.Lock()
	previousCoordinator := defaultCodexInputCoordinator
	defaultCodexInputCoordinator = coordinator
	defaultCodexInputCoordinatorMu.Unlock()
	targetCommandResolverMu.Lock()
	previousProcessResolver := targetProcessResolver
	previousResolver := targetCommandResolver
	targetProcessResolver = resolver
	targetCommandResolver = nil
	targetCommandResolverMu.Unlock()
	packageCodexInputIOMu.Lock()
	previousIO := packageCodexInputIO
	packageCodexInputIO = io
	packageCodexInputIOMu.Unlock()
	t.Cleanup(func() {
		defaultCodexInputCoordinatorMu.Lock()
		defaultCodexInputCoordinator = previousCoordinator
		defaultCodexInputCoordinatorMu.Unlock()
		targetCommandResolverMu.Lock()
		targetProcessResolver = previousProcessResolver
		targetCommandResolver = previousResolver
		targetCommandResolverMu.Unlock()
		packageCodexInputIOMu.Lock()
		packageCodexInputIO = previousIO
		packageCodexInputIOMu.Unlock()
	})

	mutations := []struct {
		name string
		run  func() error
	}{
		{name: "instance input", run: func() error { return w.SendInput(sessionID, "foreign") }},
		{name: "instance key", run: func() error { return w.SendKey(sessionID, "Enter") }},
		{name: "instance action", run: func() error { return w.SendAction(sessionID, "pause") }},
		{
			name: "instance explicit ready",
			run: func() error {
				return w.SendInputWhenReady(sessionID, "custom-agent", "foreign")
			},
		},
		{
			name: "instance explicit structured submit",
			run: func() error {
				return w.SubmitInputWhenReady(sessionID, "claude", "foreign")
			},
		},
		{
			name: "package explicit input",
			run: func() error {
				return SendInputForCommand(sessionID, "claude", "foreign")
			},
		},
		{
			name: "package explicit ready",
			run: func() error {
				return SendInputWhenReady(sessionID, "custom-agent", "foreign")
			},
		},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			err := mutation.run()
			if err == nil ||
				(!strings.Contains(err.Error(), "unresolved") &&
					!strings.Contains(err.Error(), "different input")) {
				t.Fatalf("authoritative Codex mutation error = %v", err)
			}
		})
	}
	if raw, err := os.ReadFile(rawLog); err == nil && len(raw) > 0 {
		t.Fatalf("cached or explicit command bypassed Codex arbiter: %q", raw)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read raw action log: %v", err)
	}
	if len(io.pastes) != 0 || io.enters != 0 {
		t.Fatalf("provider mutations pastes=%#v enters=%d", io.pastes, io.enters)
	}
	if resolverCalls < len(mutations)*2 {
		t.Fatalf("authoritative resolver calls=%d want at least %d", resolverCalls, len(mutations)*2)
	}
}

func TestCommandlessPackageSendUsesAuthoritativeCodexResolver(t *testing.T) {
	sessionID := "external:@package-commandless"
	generation := "generation-package-commandless"
	io := &fakeCodexInputIO{
		captures:    []string{"plain pane with no retained Codex header"},
		generations: []string{generation},
	}
	coordinator := newCodexInputCoordinator()
	record := codexTransactionRecord{
		SchemaVersion:     codexTransactionSchemaVersion,
		TransactionID:     "package-commandless-ambiguous",
		SessionID:         sessionID,
		SessionGeneration: generation,
		Action:            "submit_codex_input",
		Phase:             codexTransactionAmbiguous,
		PayloadSHA256:     codexSHA256("owned"),
		Instruction:       "owned",
		InstructionSHA256: codexSHA256("owned"),
		RolloutPath:       fakeCodexRollout(generation).Path,
		RolloutSessionID:  fakeCodexRollout(generation).SessionID,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := coordinator.store.Save(record); err != nil {
		t.Fatalf("save package ambiguous owner: %v", err)
	}

	defaultCodexInputCoordinatorMu.Lock()
	previousCoordinator := defaultCodexInputCoordinator
	defaultCodexInputCoordinator = coordinator
	defaultCodexInputCoordinatorMu.Unlock()
	targetCommandResolverMu.Lock()
	previousResolver := targetCommandResolver
	resolverCalls := 0
	targetCommandResolver = func(target string) (string, bool) {
		resolverCalls++
		return "codex", target == sessionID
	}
	targetCommandResolverMu.Unlock()
	packageCodexInputIOMu.Lock()
	previousIO := packageCodexInputIO
	packageCodexInputIO = io
	packageCodexInputIOMu.Unlock()
	t.Cleanup(func() {
		defaultCodexInputCoordinatorMu.Lock()
		defaultCodexInputCoordinator = previousCoordinator
		defaultCodexInputCoordinatorMu.Unlock()
		targetCommandResolverMu.Lock()
		targetCommandResolver = previousResolver
		targetCommandResolverMu.Unlock()
		packageCodexInputIOMu.Lock()
		packageCodexInputIO = previousIO
		packageCodexInputIOMu.Unlock()
	})

	err := SendInput(sessionID, "foreign package input")
	if err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("commandless package mutation error = %v", err)
	}
	if resolverCalls < 2 {
		t.Fatalf("authoritative resolver calls=%d want at least 2", resolverCalls)
	}
	if len(io.pastes) != 0 || io.enters != 0 {
		t.Fatalf("package commandless mutation reached provider pastes=%#v enters=%d", io.pastes, io.enters)
	}
}

func TestUntrackedKnownClaudeKeepsBaselineRawInput(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required for the non-Codex control")
	}
	sessionID := fmt.Sprintf("zen-claude-control-%d", time.Now().UnixNano())
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", sessionID, "cat").CombinedOutput(); err != nil {
		t.Fatalf("create Claude control pane: %v%s", err, commandOutputSuffix(out))
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionID).Run()
	})
	w := New(time.Second)
	resolverCalls := 0
	w.targetCommandResolver = func(target string) (string, bool) {
		resolverCalls++
		return "claude", target == sessionID
	}
	if err := w.SendInput(sessionID, "CLAUDE_BASELINE_ONCE"); err != nil {
		t.Fatalf("Claude baseline input: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		out, err := exec.Command("tmux", "capture-pane", "-p", "-t", sessionID).Output()
		if err == nil && strings.Count(string(out), "CLAUDE_BASELINE_ONCE") == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Claude baseline output=%q err=%v", out, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if resolverCalls < 2 {
		t.Fatalf("Claude resolver calls=%d want at least 2", resolverCalls)
	}
}

func TestCodexPayloadPreservesExactBytesAndSubmitBoundary(t *testing.T) {
	body, submit := splitCodexSubmitInput("line one\r\nline two\r\n\r\n")
	if !submit || body != "line one\r\nline two\r\n" {
		t.Fatalf("split body=%q submit=%v", body, submit)
	}
	normalized, err := normalizeCodexPayload(body)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized != body {
		t.Fatalf("payload = %q, want exact %q", normalized, body)
	}
	if exact, err := normalizeCodexPayload("binary-safe\x00payload"); err != nil || exact != "binary-safe\x00payload" {
		t.Fatalf("exact control payload=%q err=%v", exact, err)
	}
	if untouched, submit := splitCodexSubmitInput("no submit delimiter"); submit || untouched != "no submit delimiter" {
		t.Fatalf("non-submit body=%q submit=%v", untouched, submit)
	}
}

func TestCodexStructuredSubmitPreservesCallerOwnedFinalLineEnding(t *testing.T) {
	normalized, err := normalizeCodexPayload("alpha\r\nβ\n")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized != "alpha\r\nβ\n" {
		t.Fatalf("structured payload = %q, want exact caller bytes", normalized)
	}
	if got, want := codexSHA256(normalized), codexSHA256("alpha\r\nβ\n"); got != want {
		t.Fatalf("payload digest = %s, want %s", got, want)
	}
}

func TestWatcherStructuredSubmitPreservesExactFinalLineEndingInJournal(t *testing.T) {
	payload := "alpha\r\nβ\n"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-structured-final-lf",
		}
		if len(current.pastes) > 0 {
			capture.content = ready + "\n› " + current.pastes[0] + "\n"
			capture.composer = codexComposerHasDraft
		}
		return capture
	}
	stateDir := t.TempDir()
	coordinator, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	w := New(time.Second)
	w.codexInput = coordinator
	w.codexInputIO = io
	w.targetCommandResolver = func(target string) (string, bool) {
		return "codex --no-alt-screen", target == "agent:@structured-final-lf"
	}

	if err := w.SubmitInputWhenReady(
		"agent:@structured-final-lf",
		"codex --no-alt-screen",
		payload,
	); err != nil {
		t.Fatalf("structured submit: %v", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("actions pastes=%#v enters=%d", io.pastes, io.enters)
	}
	transactions, err := os.ReadDir(filepath.Join(stateDir, "codex-input", "transactions"))
	if err != nil || len(transactions) != 1 {
		t.Fatalf("transactions=%v err=%v", transactions, err)
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, "codex-input", "transactions", transactions[0].Name()))
	if err != nil {
		t.Fatalf("read exact payload transaction: %v", err)
	}
	var record codexTransactionRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("decode exact payload transaction: %v", err)
	}
	if record.Instruction != payload || record.InstructionSHA256 != codexSHA256(payload) || record.EnvelopePath != "" {
		t.Fatalf("exact payload transaction = %#v", record)
	}
}

func TestCodexCoordinatorSubmitsExactLongUnicodeAndKeepsDurableSpool(t *testing.T) {
	body := strings.Repeat(
		"修复一个已经在 Brain delegated follow-up 中真实复现的 Codex 输入事务 bug。\n完整 Unicode 草稿保持精确字节。\n",
		8,
	)
	if len(body) <= codexPayloadSpoolThreshold {
		t.Fatalf("test payload length=%d must exceed spool threshold=%d", len(body), codexPayloadSpoolThreshold)
	}
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-envelope",
		}
		if len(current.pastes) == 0 {
			return capture
		}
		draft := ready + "\n› " + current.pastes[0] + "\n"
		capture.content = draft
		capture.composer = codexComposerHasDraft
		if current.enters > 0 {
			capture.content = draft + "\n• Working (1s • esc to interrupt)\n"
			capture.composer = codexComposerEmpty
		}
		return capture
	}
	stateDir := t.TempDir()
	coordinator, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	if err := coordinator.submit(io, "agent:@341", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submit returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body || strings.Contains(io.pastes[0], "ZEN_TX=") {
		t.Fatalf("pastes = %#v, want exact original Unicode payload", io.pastes)
	}
	if io.enters != 1 {
		t.Fatalf("Enter count = %d, want exactly 1", io.enters)
	}
	envelopes, err := os.ReadDir(filepath.Join(stateDir, "codex-input", "envelopes"))
	if err != nil || len(envelopes) != 1 {
		t.Fatalf("envelopes=%v err=%v", envelopes, err)
	}
	envelopePath := filepath.Join(stateDir, "codex-input", "envelopes", envelopes[0].Name())
	if envelopes[0].Name() != codexSHA256(body) {
		t.Fatalf("envelope name = %q, want payload SHA-256", envelopes[0].Name())
	}
	raw, err := os.ReadFile(envelopePath)
	if err != nil || string(raw) != body {
		t.Fatalf("envelope bytes exact=%v err=%v", string(raw) == body, err)
	}
	if info, err := os.Stat(envelopePath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("envelope mode info=%v err=%v", info, err)
	}
	transactions, err := os.ReadDir(filepath.Join(stateDir, "codex-input", "transactions"))
	if err != nil || len(transactions) != 1 {
		t.Fatalf("transactions=%v err=%v", transactions, err)
	}
	transactionPath := filepath.Join(stateDir, "codex-input", "transactions", transactions[0].Name())
	transactionRaw, err := os.ReadFile(transactionPath)
	if err != nil {
		t.Fatalf("read transaction: %v", err)
	}
	var record codexTransactionRecord
	if err := json.Unmarshal(transactionRaw, &record); err != nil {
		t.Fatalf("decode transaction: %v", err)
	}
	if record.Phase != codexTransactionConfirmed ||
		record.SessionGeneration != "generation-envelope" ||
		record.PayloadSHA256 != codexSHA256(body) ||
		record.Instruction != body ||
		record.InstructionSHA256 != codexSHA256(body) ||
		record.EnvelopePath != envelopePath ||
		strings.Contains(io.pastes[0], record.TransactionID) ||
		strings.Contains(io.pastes[0], record.PayloadSHA256) {
		t.Fatalf("transaction record = %#v instruction=%q", record, io.pastes[0])
	}
	if info, err := os.Stat(transactionPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("transaction mode info=%v err=%v", info, err)
	}
}

func TestPersistentCodexCoordinatorUsesDirectPathForObservableShortPrompt(t *testing.T) {
	body := "short exact Unicode 中文"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-short-direct",
		}
		if len(current.pastes) > 0 {
			capture.content = ready + "\n› " + current.pastes[0] + "\n"
			capture.composer = codexComposerHasDraft
			if current.enters > 0 {
				capture.content += "\n• Working (1s • esc to interrupt)\n"
				capture.composer = codexComposerEmpty
			}
		}
		return capture
	}
	coordinator, err := newPersistentCodexInputCoordinator(t.TempDir())
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	if err := coordinator.submit(io, "agent:@short-direct", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body || io.enters != 1 {
		t.Fatalf("short production actions pastes=%#v enters=%d", io.pastes, io.enters)
	}
}

func TestWatcherCodexReceiptSurvivesDirectAndEnvelopeTransactionsRestartWithoutReplay(t *testing.T) {
	binDir := t.TempDir()
	tmuxMutationPath := filepath.Join(t.TempDir(), "tmux-mutations")
	tmuxScript := fmt.Sprintf(`#!/bin/sh
case "$1" in
  show-options)
    exit 0
    ;;
  set-option)
    printf 'set-option\n' >> %q
    exit 0
    ;;
  *)
    exit 1
    ;;
esac
`, tmuxMutationPath)
	if err := os.WriteFile(filepath.Join(binDir, "tmux"), []byte(tmuxScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	tests := []struct {
		name         string
		body         string
		wantEnvelope bool
	}{
		{
			name: "short direct input",
			body: "short receipt-bound input",
		},
		{
			name:         "long content-addressed envelope",
			body:         "caller Work Event payload first line\nUnicode payload 终 Ω\n" + strings.Repeat("exact-long-body-", 20),
			wantEnvelope: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const (
				sessionID  = "brain-host:@receipt"
				receipt    = "event-stable-id:claim-stable-token"
				generation = "generation-receipt"
			)
			ready := codexReadyPane("")
			io := &fakeCodexInputIO{
				clock: time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC),
			}
			io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
				capture := codexPaneCapture{
					content:    ready,
					alive:      true,
					composer:   codexComposerEmpty,
					generation: generation,
				}
				if len(current.pastes) > 0 {
					capture.content = ready + "\n› " + current.pastes[0] + "\n"
					capture.composer = codexComposerHasDraft
					if current.enters > 0 {
						capture.content += "\n• Working (1s • esc to interrupt)\n"
						capture.composer = codexComposerEmpty
					}
				}
				return capture
			}
			stateDir := t.TempDir()
			coordinator, err := newPersistentCodexInputCoordinator(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			newWatcher := func(input *codexInputCoordinator) *Watcher {
				w := New(time.Second)
				w.codexInput = input
				w.codexInputIO = io
				w.targetCommandResolver = func(target string) (string, bool) {
					return "codex --no-alt-screen", target == sessionID
				}
				return w
			}
			first := newWatcher(coordinator)
			if err := first.SendInputWithReceipt(sessionID, test.body+"\n", receipt); err != nil {
				t.Fatalf("first receipt-bound send: %v", err)
			}
			accepted, err := first.HasInputReceipt(sessionID, receipt)
			if err != nil || !accepted {
				t.Fatalf("first accepted=%v err=%v", accepted, err)
			}
			if len(io.pastes) != 1 || io.enters != 1 {
				t.Fatalf("first actions pastes=%#v enters=%d", io.pastes, io.enters)
			}

			entries, err := os.ReadDir(filepath.Join(stateDir, "codex-input", "transactions"))
			if err != nil || len(entries) != 1 {
				t.Fatalf("transactions=%v err=%v", entries, err)
			}
			raw, err := os.ReadFile(filepath.Join(stateDir, "codex-input", "transactions", entries[0].Name()))
			if err != nil {
				t.Fatal(err)
			}
			var record codexTransactionRecord
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			if record.Phase != codexTransactionConfirmed ||
				record.AcceptanceReceipt != receipt ||
				record.PayloadSHA256 != codexSHA256(test.body) {
				t.Fatalf("receipt transaction = %#v", record)
			}
			if test.wantEnvelope {
				if record.EnvelopePath == "" {
					t.Fatal("long receipt input did not use an envelope")
				}
				envelope, err := os.ReadFile(record.EnvelopePath)
				if err != nil || string(envelope) != test.body {
					t.Fatalf("envelope exact=%v err=%v", string(envelope) == test.body, err)
				}
			} else if record.EnvelopePath != "" || io.pastes[0] != test.body {
				t.Fatalf("short path record=%#v paste=%q", record, io.pastes[0])
			}

			restartedCoordinator, err := newPersistentCodexInputCoordinator(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			restarted := newWatcher(restartedCoordinator)
			if err := restarted.SendInputWithReceipt(sessionID, test.body+"\n", receipt); err != nil {
				t.Fatalf("restart receipt recovery: %v", err)
			}
			if len(io.pastes) != 1 || io.enters != 1 {
				t.Fatalf("restart replayed input: pastes=%#v enters=%d", io.pastes, io.enters)
			}
		})
	}
	raw, err := os.ReadFile(tmuxMutationPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(raw), "set-option\n"); got != len(tests) {
		t.Fatalf("Session receipt metadata writes = %d, want one per accepted transaction", got)
	}
}

func TestWatcherCodexReceiptClosesConfirmedEnvelopeMetadataCrashWindow(t *testing.T) {
	binDir := t.TempDir()
	tmuxScript := `#!/bin/sh
case "$1" in
  show-options)
    exit 0
    ;;
  set-option)
    exit 1
    ;;
  *)
    exit 1
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "tmux"), []byte(tmuxScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	const (
		sessionID  = "brain-host:@metadata-crash"
		receipt    = "event-metadata-crash:claim-metadata-crash"
		generation = "generation-metadata-crash"
	)
	body := "long actionable Work Event\n" + strings.Repeat("receipt-bound-envelope-", 20)
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{
		clock: time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC),
	}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: generation,
		}
		if len(current.pastes) > 0 {
			capture.content = ready + "\n› " + current.pastes[0] + "\n"
			capture.composer = codexComposerHasDraft
			if current.enters > 0 {
				capture.content += "\n• Working (1s • esc to interrupt)\n"
				capture.composer = codexComposerEmpty
			}
		}
		return capture
	}
	stateDir := t.TempDir()
	newWatcher := func() *Watcher {
		coordinator, err := newPersistentCodexInputCoordinator(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		w := New(time.Second)
		w.codexInput = coordinator
		w.codexInputIO = io
		w.targetCommandResolver = func(target string) (string, bool) {
			return "codex --no-alt-screen", target == sessionID
		}
		return w
	}

	first := newWatcher()
	if err := first.SendInputWithReceipt(sessionID, body+"\n", receipt); err == nil {
		t.Fatal("injected Session metadata failure was not reported")
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("first acceptance actions pastes=%#v enters=%d", io.pastes, io.enters)
	}

	restarted := newWatcher()
	accepted, err := restarted.HasInputReceipt(sessionID, receipt)
	if err != nil || !accepted {
		t.Fatalf("transactional receipt recovery accepted=%v err=%v", accepted, err)
	}
	if err := restarted.SendInputWithReceipt(sessionID, body+"\n", receipt); err != nil {
		t.Fatalf("restarted dedupe: %v", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("restart replayed confirmed envelope: pastes=%#v enters=%d", io.pastes, io.enters)
	}
}

func TestCodexComposerStateUsesProviderStyling(t *testing.T) {
	empty := "\x1b[1m›\x1b[0m \x1b[2mSummarize recent commits\n"
	draft := empty +
		"\x1b[1m›\x1b[0m exact Unicode 中文 first line\n" +
		"  exact second line\n"
	coloredDraft := empty +
		"\x1b[1m›\x1b[0m \x1b[38;2;12;34;56mcolored draft\n"

	if got := codexComposerStateFromStyledPane(empty); got != codexComposerEmpty {
		t.Fatalf("empty composer state = %v, want empty", got)
	}
	if got := codexComposerStateFromStyledPane(draft); got != codexComposerHasDraft {
		t.Fatalf("draft composer state = %v, want has-draft", got)
	}
	if got := codexComposerStateFromStyledPane(coloredDraft); got != codexComposerHasDraft {
		t.Fatalf("colored draft composer state = %v, want has-draft", got)
	}
	if got := stripCodexTerminalEscapes(draft); strings.ContainsRune(got, '\x1b') ||
		!strings.Contains(got, "exact Unicode 中文 first line") {
		t.Fatalf("stripped pane = %q", got)
	}
}

func TestSubmitCodexInputPreservesForeignDraftAtEntry(t *testing.T) {
	body := "replacement line one\nreplacement Unicode 第二行"
	ready := codexReadyPane("")
	stale := ready + "\n› stale draft that must never be appended\n"
	io := &fakeCodexInputIO{
		captures: []string{stale},
		states:   []codexComposerState{codexComposerHasDraft},
	}

	err := submitCodexInput(io, "agent:@stale", body, testCodexSubmitConfig())
	if err == nil || !strings.Contains(err.Error(), "occupied") {
		t.Fatalf("error = %v, want occupied composer conflict", err)
	}
	if len(io.pastes) != 0 || io.enters != 0 {
		t.Fatalf("foreign draft mutated: pastes=%#v enters=%d", io.pastes, io.enters)
	}
}

func TestCodexCoordinatorRequiresDurableSubmitIntentBeforeAtomicMutation(t *testing.T) {
	body := "journal-before-enter"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-enter-intent",
		}
		if len(current.pastes) > 0 {
			capture.content = ready + "\n› " + current.pastes[0] + "\n"
			capture.composer = codexComposerHasDraft
		}
		return capture
	}
	store := &failCodexStoreAtPhase{
		codexTransactionStore: newMemoryCodexTransactionStore(),
		phase:                 codexTransactionEnterPending,
	}
	coordinator := newCodexInputCoordinatorWithStore(store)

	err := coordinator.submit(io, "agent:@enter-intent", body, testCodexSubmitConfig())
	if err == nil || !strings.Contains(err.Error(), "provider input was not changed") {
		t.Fatalf("error = %v, want durable submit-intent failure", err)
	}
	if len(io.pastes) != 0 || io.enters != 0 {
		t.Fatalf("actions pastes=%#v enters=%d", io.pastes, io.enters)
	}
}

func TestSubmitCodexInputAcceptsProviderNativeQueuedMessage(t *testing.T) {
	body := "实现并验证完整的 Codex queued follow-up；忙碌时交给 provider-native queue，绝不由 Zen 重贴、重发或另建队列。"
	busy := codexReadyPane("• Working (4m 10s • esc to interrupt)")
	draft := busy + "\n› " + body + "\n"
	queued := busy + "\n• Messages to be submitted after next tool\n  call (press esc to interrupt and send immediately)\n" +
		"  ↳ 实现并验证完整的 Codex queued\n" +
		"    follow-up；忙碌时交给 provider-native queue，绝不由\n" +
		"    Zen 重贴、重发或另建\n" +
		"    …\n\n› Summarize recent commits\n"
	io := &fakeCodexInputIO{captures: []string{busy, busy, draft, draft, queued}}

	if err := submitCodexInput(io, "agent:@341", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submitCodexInput returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body {
		t.Fatalf("pastes = %#v, want queued body exactly once", io.pastes)
	}
	if io.enters != 1 {
		t.Fatalf("Enter count = %d, want exactly 1 for native queue submission", io.enters)
	}
}

func TestSubmitCodexInputKeepsPendingPTYInputQueuedUntilAtomicPreflightIsSafe(t *testing.T) {
	body := "queued behind native input without losing or duplicating either producer"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{
		captures: []string{ready, ready, ready, ready},
		submitErrors: []error{
			fmt.Errorf("%w: target application has 3 unconsumed PTY input bytes", errCodexMutationConflict),
			nil,
		},
	}

	if err := submitCodexInput(io, "agent:@pending-pty", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("pending input should remain queued until safe preflight: %v", err)
	}
	if io.submitAttempts != 2 || len(io.pastes) != 1 || io.pastes[0] != body || io.enters != 1 {
		t.Fatalf(
			"attempts=%d pastes=%#v enters=%d, want one deferred preflight and one exact atomic submit",
			io.submitAttempts,
			io.pastes,
			io.enters,
		)
	}
}

func TestCodexCoordinatorPersistentPTYContentionReturnsPendingThenResumesSameReceipt(t *testing.T) {
	const (
		sessionID = "agent:@persistent-pty"
		body      = "one durable payload behind persistent PTY contention"
		receipt   = "chat-request-persistent-pty"
	)
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{}
	io.captureFn = func(*fakeCodexInputIO) codexPaneCapture {
		return codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-persistent-pty",
		}
	}
	for range 32 {
		io.submitErrors = append(io.submitErrors, fmt.Errorf(
			"%w: target application has 3 unconsumed PTY input bytes",
			errCodexMutationConflict,
		))
	}
	store := newMemoryCodexTransactionStore()
	coordinator := newCodexInputCoordinatorWithStore(store)
	cfg := testCodexSubmitConfig()

	err := coordinator.submitWithReceipt(io, sessionID, body, receipt, cfg)
	if !IsInputPending(err) {
		t.Fatalf("persistent contention error = %v, want durable pending", err)
	}
	active, activeErr := store.Active(sessionID, "generation-persistent-pty")
	if activeErr != nil {
		t.Fatalf("read active transaction: %v", activeErr)
	}
	if len(active) != 1 || active[0].Phase != codexTransactionPrepared ||
		active[0].AcceptanceReceipt != receipt {
		t.Fatalf("active transactions = %#v, want one prepared receipt owner", active)
	}
	transactionID := active[0].TransactionID
	if len(io.pastes) != 0 || io.enters != 0 {
		t.Fatalf("pending contention mutated provider: pastes=%#v enters=%d", io.pastes, io.enters)
	}

	io.submitErrors = nil
	if err := coordinator.submitWithReceipt(io, sessionID, body, receipt, cfg); err != nil {
		t.Fatalf("resume pending receipt: %v", err)
	}
	record, found, recordErr := store.Receipt(sessionID, "generation-persistent-pty", receipt)
	if recordErr != nil || !found {
		t.Fatalf("read settled receipt found=%v err=%v", found, recordErr)
	}
	if record.TransactionID != transactionID || record.Phase != codexTransactionConfirmed {
		t.Fatalf("settled record = %#v, want same confirmed transaction %s", record, transactionID)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body || io.enters != 1 {
		t.Fatalf("resumed actions pastes=%#v enters=%d, want one paste and one Enter", io.pastes, io.enters)
	}
}

func TestCodexCoordinatorForeignDraftSurvivesRestartAndResumesSameReceipt(t *testing.T) {
	const (
		sessionID  = "agent:@foreign-restart"
		generation = "generation-foreign-restart"
		body       = "durable input queued behind a known foreign draft"
		receipt    = "chat-request-foreign-restart"
	)
	ready := codexReadyPane("")
	foreign := ready + "\n› user-owned draft\n"
	stateDir := t.TempDir()
	first, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("create first persistent coordinator: %v", err)
	}
	blockedIO := &fakeCodexInputIO{
		captures:    []string{foreign},
		states:      []codexComposerState{codexComposerHasDraft},
		generations: []string{generation},
		clock:       time.Now().UTC(),
	}
	if err := first.submitWithReceipt(blockedIO, sessionID, body, receipt, testCodexSubmitConfig()); !IsInputPending(err) {
		t.Fatalf("foreign draft error = %v, want durable pending", err)
	}
	if len(blockedIO.pastes) != 0 || blockedIO.enters != 0 {
		t.Fatalf("foreign draft mutated: pastes=%#v enters=%d", blockedIO.pastes, blockedIO.enters)
	}
	before, found, err := first.store.Receipt(sessionID, generation, receipt)
	if err != nil || !found || before.Phase != codexTransactionPrepared {
		t.Fatalf("pending record found=%v err=%v record=%#v", found, err, before)
	}

	restarted, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("create restarted coordinator: %v", err)
	}
	clearIO := &fakeCodexInputIO{
		captures:    []string{ready, ready, ready},
		states:      []codexComposerState{codexComposerEmpty},
		generations: []string{generation},
		clock:       blockedIO.clock,
	}
	if err := restarted.submitWithReceipt(clearIO, sessionID, body, receipt, testCodexSubmitConfig()); err != nil {
		t.Fatalf("restart resume: %v", err)
	}
	after, found, err := restarted.store.Receipt(sessionID, generation, receipt)
	if err != nil || !found {
		t.Fatalf("settled record found=%v err=%v", found, err)
	}
	if after.TransactionID != before.TransactionID || after.Phase != codexTransactionConfirmed {
		t.Fatalf("after restart record=%#v, want same confirmed transaction %s", after, before.TransactionID)
	}
	if len(clearIO.pastes) != 1 || clearIO.pastes[0] != body || clearIO.enters != 1 {
		t.Fatalf("restart actions pastes=%#v enters=%d", clearIO.pastes, clearIO.enters)
	}
}

func TestCodexCoordinatorConcurrentResumeOfPendingReceiptSubmitsExactlyOnce(t *testing.T) {
	const (
		sessionID  = "agent:@pending-concurrent"
		generation = "generation-pending-concurrent"
		body       = "resume this one durable transaction from concurrent callers"
		receipt    = "chat-request-pending-concurrent"
	)
	ready := codexReadyPane("")
	foreign := ready + "\n› preserve me\n"
	store := newMemoryCodexTransactionStore()
	coordinator := newCodexInputCoordinatorWithStore(store)
	blockedIO := &fakeCodexInputIO{
		captures:    []string{foreign},
		states:      []codexComposerState{codexComposerHasDraft},
		generations: []string{generation},
	}
	if err := coordinator.submitWithReceipt(blockedIO, sessionID, body, receipt, testCodexSubmitConfig()); !IsInputPending(err) {
		t.Fatalf("seed pending transaction: %v", err)
	}
	clearIO := &fakeCodexInputIO{}
	clearIO.captureFn = func(*fakeCodexInputIO) codexPaneCapture {
		return codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: generation,
		}
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var callers sync.WaitGroup
	for range 2 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			<-start
			errs <- coordinator.submitWithReceipt(
				clearIO,
				sessionID,
				body,
				receipt,
				testCodexSubmitConfig(),
			)
		}()
	}
	close(start)
	callers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent resume: %v", err)
		}
	}
	if len(clearIO.pastes) != 1 || clearIO.pastes[0] != body || clearIO.enters != 1 {
		t.Fatalf("concurrent resume pastes=%#v enters=%d", clearIO.pastes, clearIO.enters)
	}
}

func TestWatcherPendingDriverResumesSolePreparedReceiptExactlyOnce(t *testing.T) {
	const (
		sessionID  = "agent:@pending-driver"
		generation = "generation-pending-driver"
		body       = "the durable driver resumes this exact payload"
		receipt    = "chat-request-pending-driver"
	)
	ready := codexReadyPane("")
	store := newMemoryCodexTransactionStore()
	record := codexTransactionRecord{
		SchemaVersion:     codexTransactionSchemaVersion,
		TransactionID:     "transaction-pending-driver",
		SessionID:         sessionID,
		SessionGeneration: generation,
		AcceptanceReceipt: receipt,
		Action:            "submit_codex_input",
		Phase:             codexTransactionPrepared,
		PayloadSHA256:     codexSHA256(body),
		Instruction:       body,
		InstructionSHA256: codexSHA256(body),
		RolloutPath:       fakeCodexRollout(generation).Path,
		RolloutSessionID:  fakeCodexRollout(generation).SessionID,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := store.Save(record); err != nil {
		t.Fatalf("seed pending record: %v", err)
	}
	submitted := make(chan struct{}, 1)
	io := &fakeCodexInputIO{submitted: submitted}
	io.captureFn = func(*fakeCodexInputIO) codexPaneCapture {
		return codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: generation,
		}
	}
	w := New(time.Second)
	w.codexInput = newCodexInputCoordinatorWithStore(store)
	w.codexInputIO = io
	w.targetCommandResolver = func(target string) (string, bool) {
		return "codex", target == sessionID
	}

	w.startCodexPendingResume(sessionID)
	select {
	case <-submitted:
	case <-time.After(2 * time.Second):
		t.Fatal("durable pending driver did not resume after ownership cleared")
	}
	var settled codexTransactionRecord
	var found bool
	deadline := time.Now().Add(time.Second)
	for {
		var err error
		settled, found, err = store.Receipt(sessionID, generation, receipt)
		if err != nil {
			t.Fatalf("read settled receipt: %v", err)
		}
		if found && settled.Phase == codexTransactionConfirmed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending receipt did not settle: found=%v record=%#v", found, settled)
		}
		time.Sleep(time.Millisecond)
	}
	if settled.TransactionID != record.TransactionID ||
		settled.Phase != codexTransactionConfirmed {
		t.Fatalf("settled record=%#v, want same confirmed transaction", settled)
	}
	w.startCodexPendingResume(sessionID)
	select {
	case <-submitted:
		t.Fatal("confirmed pending receipt was submitted a second time")
	case <-time.After(25 * time.Millisecond):
	}
	if len(io.pastes) != 1 || io.pastes[0] != body || io.enters != 1 {
		t.Fatalf("driver actions pastes=%#v enters=%d", io.pastes, io.enters)
	}
}

func TestCodexCoordinatorSerializesConcurrentProducersAndDedupesReceipt(t *testing.T) {
	const (
		sessionID = "agent:@concurrent"
		payload   = "one receipt-bound payload from concurrent Zen producers"
		receipt   = "event-concurrent:claim-stable"
	)
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{}
	io.captureFn = func(*fakeCodexInputIO) codexPaneCapture {
		return codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-concurrent",
		}
	}
	coordinator := newCodexInputCoordinator()
	cfg := testCodexSubmitConfig()
	start := make(chan struct{})
	errs := make(chan error, 2)
	var producers sync.WaitGroup
	for range 2 {
		producers.Add(1)
		go func() {
			defer producers.Done()
			<-start
			errs <- coordinator.submitWithReceipt(io, sessionID, payload, receipt, cfg)
		}()
	}
	close(start)
	producers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent producer: %v", err)
		}
	}
	if len(io.pastes) != 1 || io.pastes[0] != payload || io.enters != 1 {
		t.Fatalf("pastes=%#v enters=%d, want one serialized receipt-bound submission", io.pastes, io.enters)
	}
}

func TestCodexCoordinatorSerializesDistinctConcurrentPayloadsWithoutInterleaving(t *testing.T) {
	const sessionID = "agent:@concurrent-distinct"
	payloads := []string{
		"first exact concurrent payload\nwith its own trailing line\n",
		"第二个并发 payload 保持完整 🧭\n\n",
	}
	ready := codexReadyPane("• Working (1s • esc to interrupt)")
	io := &fakeCodexInputIO{}
	io.captureFn = func(*fakeCodexInputIO) codexPaneCapture {
		return codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-concurrent-distinct",
		}
	}
	coordinator := newCodexInputCoordinator()
	cfg := testCodexSubmitConfig()
	start := make(chan struct{})
	errs := make(chan error, len(payloads))
	var producers sync.WaitGroup
	for index, payload := range payloads {
		producers.Add(1)
		go func() {
			defer producers.Done()
			<-start
			errs <- coordinator.submitWithReceipt(
				io,
				sessionID,
				payload,
				fmt.Sprintf("event-distinct:%d", index),
				cfg,
			)
		}()
	}
	close(start)
	producers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("distinct concurrent producer: %v", err)
		}
	}
	if io.enters != len(payloads) || len(io.pastes) != len(payloads) {
		t.Fatalf("pastes=%#v enters=%d, want one atomic submit per producer", io.pastes, io.enters)
	}
	got := slices.Clone(io.pastes)
	want := slices.Clone(payloads)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("submitted payloads=%#v want exact payloads=%#v", got, want)
	}
}

func TestSubmitCodexInputPreservesUnicodeMultilineAndTrailingNewlinesExactly(t *testing.T) {
	body := "第一行\r\nemoji 🧭\ncombining e\u0301\n\n"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{captures: []string{ready, ready}}

	if err := submitCodexInput(io, "agent:@exact-unicode", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submit exact Unicode payload: %v", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body || io.enters != 1 {
		t.Fatalf("pastes=%#v enters=%d, want byte-exact payload and one Enter", io.pastes, io.enters)
	}
	if got, want := codexSHA256(io.pastes[0]), codexSHA256(body); got != want {
		t.Fatalf("submitted payload digest=%s want=%s", got, want)
	}
}

func TestSubmitCodexInputAcceptsComposerAcrossOptionalMCPThirtySecondBoundary(t *testing.T) {
	body := "execute unique marker ZEN_INITIAL_12345"
	starting := codexReadyPane("• Starting MCP servers (0/3): context7, playwright")
	draft := starting + "\n› " + body + "\n\n  gpt-5.6 medium · /tmp\n"
	submitted := starting + "\n› " + body + "\n\n• Working (1s • esc to interrupt)\n\n› Find and fix a bug in @filename\n"
	io := &fakeCodexInputIO{
		captures: []string{starting, starting, draft, draft, submitted},
	}
	cfg := testCodexSubmitConfig()
	cfg.startupStallTimeout = 30 * time.Second

	if err := submitCodexInput(io, "agent:@1", body, cfg); err != nil {
		t.Fatalf("submitCodexInput returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body {
		t.Fatalf("pastes = %#v, want body exactly once", io.pastes)
	}
	if io.enters != 1 {
		t.Fatalf("Enter count = %d, want 1", io.enters)
	}
	if elapsed := io.now().Sub(time.Time{}); elapsed >= 30*time.Second {
		t.Fatalf("handoff waited %s for optional MCP outcome; composer was already usable", elapsed)
	}
}

func TestSubmitCodexInputAcceptsComposerAfterOptionalMCPTerminalFailure(t *testing.T) {
	body := "execute unique marker ZEN_MCP_FAILED_24680"
	failed := codexReadyPane("⚠ MCP client for `codex_apps` timed out after 30 seconds.\n⚠ MCP startup incomplete (failed: codex_apps)")
	draft := failed + "\n› " + body + "\n"
	submitted := failed + "\n› " + body + "\n\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{captures: []string{failed, failed, draft, draft, submitted}}

	if err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submitCodexInput returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("pastes=%d enters=%d, want one of each after optional MCP failure", len(io.pastes), io.enters)
	}
}

func TestSubmitCodexInputAcceptsComposerWhileSlowOptionalMCPLaterSucceeds(t *testing.T) {
	body := "execute unique marker ZEN_MCP_LATE_SUCCESS_97531"
	starting := codexReadyPane("• Starting MCP servers (1/2): slow_optional")
	ready := codexReadyPane("")
	draft := starting + "\n› " + body + "\n"
	submittedAfterMCP := ready + "\n› " + body + "\n\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{captures: []string{starting, starting, draft, draft, submittedAfterMCP}}

	if err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submitCodexInput returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("pastes=%d enters=%d, want one handoff while optional MCP later succeeds", len(io.pastes), io.enters)
	}
}

func TestSubmitCodexInputExtendsOnlyForFiniteRecognizedStartupProgress(t *testing.T) {
	body := "execute unique marker ZEN_PROGRESS_13579"
	splash := "Welcome to Codex\n"
	trust := "│ >_ OpenAI Codex │\nDo you trust the contents of this directory?\n› 1. Yes, continue\n  Press enter to continue\n"
	loading := "│ >_ OpenAI Codex │\n│ model: loading │\n› Write tests for @filename\n"
	ready := codexReadyPane("")
	draft := ready + "\n› " + body + "\n"
	submitted := ready + "\n› " + body + "\n\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{captures: []string{splash, splash, trust, loading, loading, ready, ready, draft, draft, submitted}}
	cfg := testCodexSubmitConfig()
	cfg.startupStallTimeout = 2 * time.Second

	if err := submitCodexInput(io, "agent:@1", body, cfg); err != nil {
		t.Fatalf("submitCodexInput returned error after recognized progress: %v", err)
	}
	if elapsed := io.now().Sub(time.Time{}); elapsed <= cfg.startupStallTimeout {
		t.Fatalf("elapsed = %s, want proof that recognized stage progress extended the initial stall window", elapsed)
	}
	if len(io.pastes) != 1 || io.enters != 2 {
		t.Fatalf("pastes=%d enters=%d, want startup Enter plus exactly one submission Enter", len(io.pastes), io.enters)
	}
}

func TestSubmitCodexInputFailsBoundedlyWhenCoreModelStalls(t *testing.T) {
	loading := "│ >_ OpenAI Codex │\n│ model: loading │\n› Write tests for @filename\n"
	io := &fakeCodexInputIO{captures: []string{loading}}
	cfg := testCodexSubmitConfig()
	cfg.startupStallTimeout = 2 * time.Second

	err := submitCodexInput(io, "agent:@1", "must not be pasted", cfg)
	if err == nil || !strings.Contains(err.Error(), "composer") {
		t.Fatalf("error = %v, want bounded composer stall failure", err)
	}
	if len(io.pastes) != 0 || io.enters != 0 {
		t.Fatalf("pastes=%d enters=%d, stalled core model must receive no task input", len(io.pastes), io.enters)
	}
	if elapsed := io.now().Sub(time.Time{}); elapsed > 2*cfg.startupStallTimeout {
		t.Fatalf("core model stall took %s; one recognized model-loading transition must remain bounded", elapsed)
	}
}

func TestSubmitCodexInputAppliesOneBoundedTotalDeadline(t *testing.T) {
	loading := "│ >_ OpenAI Codex │\n│ model: loading │\n› Write tests for @filename\n"
	io := &fakeCodexInputIO{captures: []string{loading}}
	cfg := testCodexSubmitConfig()
	cfg.startupStallTimeout = 30 * time.Second
	cfg.totalTimeout = 5 * time.Second
	cfg.confirmationReserve = time.Second

	err := submitCodexInput(io, "agent:@total-timeout", "must remain unsent", cfg)
	if err == nil || !strings.Contains(err.Error(), "composer") {
		t.Fatalf("error = %v, want bounded composer failure", err)
	}
	if elapsed := io.now().Sub(time.Time{}); elapsed > cfg.totalTimeout-cfg.confirmationReserve {
		t.Fatalf("transaction elapsed = %s, exceeded reserved pre-submit total deadline %s", elapsed, cfg.totalTimeout-cfg.confirmationReserve)
	}
	if len(io.pastes) != 0 || io.enters != 0 {
		t.Fatalf("pastes=%d enters=%d, want no input actions", len(io.pastes), io.enters)
	}
}

func TestSubmitCodexInputFailsBoundedlyWhenComposerNeverAppears(t *testing.T) {
	loadedWithoutComposer := "│ >_ OpenAI Codex │\n│ model: gpt-5.6-sol │\nStarting terminal UI\n"
	io := &fakeCodexInputIO{captures: []string{loadedWithoutComposer}}
	cfg := testCodexSubmitConfig()
	cfg.startupStallTimeout = 2 * time.Second

	err := submitCodexInput(io, "agent:@1", "must not be pasted", cfg)
	if err == nil || !strings.Contains(err.Error(), "no recognized startup progress") {
		t.Fatalf("error = %v, want explicit no-progress failure", err)
	}
	if len(io.pastes) != 0 || io.enters != 0 {
		t.Fatalf("pastes=%d enters=%d, missing composer must receive no task input", len(io.pastes), io.enters)
	}
	if elapsed := io.now().Sub(time.Time{}); elapsed > 2*cfg.startupStallTimeout {
		t.Fatalf("missing composer took %s; finite identity progress must remain bounded", elapsed)
	}
}

func TestSubmitCodexInputFailsImmediatelyForDeadPane(t *testing.T) {
	io := &fakeCodexInputIO{
		captures: []string{"│ >_ OpenAI Codex │\n│ model: loading │\n"},
		alive:    []bool{false},
	}

	err := submitCodexInput(io, "agent:@1", "must not be pasted", testCodexSubmitConfig())
	if err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("error = %v, want dead-pane failure", err)
	}
	if len(io.pastes) != 0 || io.enters != 0 {
		t.Fatalf("pastes=%d enters=%d, dead pane must receive no input", len(io.pastes), io.enters)
	}
}

func TestSubmitCodexInputAfterLongTurnWithoutHeader(t *testing.T) {
	body := "follow-up unique marker ZEN_LONG_TURN_24680"
	ready := strings.Repeat("completed delegated output line\n", 1100) +
		"\n› Find and fix a bug in @filename\n\n  gpt-5.6 medium · /tmp\n"
	draft := ready + "\n› " + body + "\n\n  gpt-5.6 medium · /tmp\n"
	submitted := ready + "\n› " + body + "\n\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{
		captures: []string{ready, ready, draft, draft, submitted},
	}

	if err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submitCodexInput returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body {
		t.Fatalf("pastes = %#v, want headerless follow-up body exactly once", io.pastes)
	}
	if io.enters != 1 {
		t.Fatalf("Enter count = %d, want 1", io.enters)
	}
}

func TestSubmitCodexInputAdvancesStartupTrustBeforePasting(t *testing.T) {
	body := "execute unique marker ZEN_TRUST_12345"
	trust := "│ >_ OpenAI Codex │\nDo you trust the contents of this directory?\n› 1. Yes, continue\n  Press enter to continue\n"
	ready := codexReadyPane("")
	draft := ready + "\n› " + body + "\n"
	submitted := ready + "\n› " + body + "\n\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{captures: []string{trust, ready, ready, draft, draft, submitted}}

	if err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig()); err != nil {
		t.Fatalf("submitCodexInput returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.enters != 2 {
		t.Fatalf("pastes=%d enters=%d, want startup Enter plus submit Enter", len(io.pastes), io.enters)
	}
}

func TestSubmitCodexInputSubmitsLongPayloadWithoutRenderedIdentity(t *testing.T) {
	body := strings.Repeat("long delegated task line\n", 100) + "unique final marker\n"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{captures: []string{ready, ready}}

	err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig())
	if err != nil {
		t.Fatalf("submit long exact payload: %v", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body || strings.Contains(io.pastes[0], "ZEN_TX=") || io.enters != 1 {
		t.Fatalf("pastes=%#v enters=%d, want one exact atomic submission", io.pastes, io.enters)
	}
}

func TestSubmitCodexInputDoesNotRetryEnterWhenConfirmationIsDelayed(t *testing.T) {
	body := "follow-up unique marker ZEN_FOLLOWUP_67890"
	ready := codexReadyPane("")
	draft := ready + "\n› " + body + "\n"
	submitted := ready + "\n› " + body + "\n\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{
		captures:            []string{ready, ready, draft, draft, draft, draft, submitted},
		suppressPersistence: true,
	}

	err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig())
	if err == nil || !strings.Contains(err.Error(), "durably ambiguous") {
		t.Fatalf("error = %v, want durable ambiguity after one unconfirmed Enter", err)
	}
	if len(io.pastes) != 1 {
		t.Fatalf("paste count = %d, want one paste", len(io.pastes))
	}
	if io.enters != 1 {
		t.Fatalf("Enter count = %d, an unconfirmed transaction must not retry Enter", io.enters)
	}
}

func TestSubmitCodexInputReturnsAttentionFailureAfterSingleEnter(t *testing.T) {
	body := "prompt remains in composer"
	ready := codexReadyPane("")
	draft := ready + "\n› " + body + "\n"
	io := &fakeCodexInputIO{
		captures:            []string{ready, ready, draft, draft},
		suppressPersistence: true,
	}

	err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig())
	if err == nil || !strings.Contains(err.Error(), "durably ambiguous") {
		t.Fatalf("error = %v, want durable ambiguity", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("pastes=%d enters=%d, want one paste and one Enter", len(io.pastes), io.enters)
	}
}

func TestCodexCoordinatorNeverReplaysAmbiguousEnter(t *testing.T) {
	body := "ambiguous exact-once payload ZEN_AMBIGUOUS_326"
	ready := codexReadyPane("")
	draft := ready + "\n› " + body + "\n"
	submitted := draft + "\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{
		captures:            []string{ready, ready, draft, draft, draft},
		suppressPersistence: true,
	}
	coordinator := newCodexInputCoordinator()
	cfg := testCodexSubmitConfig()

	err := coordinator.submit(io, "agent:@ambiguous", body, cfg)
	if err == nil || !strings.Contains(err.Error(), "durably ambiguous") {
		t.Fatalf("first error = %v, want durable ambiguous submission", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body || io.enters != 1 {
		t.Fatalf("first attempt pastes=%#v enters=%d", io.pastes, io.enters)
	}

	if err := coordinator.submit(io, "agent:@ambiguous", "different payload", cfg); err == nil ||
		!strings.Contains(err.Error(), "different input") {
		t.Fatalf("different-payload retry error = %v", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("different retry replayed input: pastes=%#v enters=%d", io.pastes, io.enters)
	}

	io.captures = []string{draft, draft}
	io.index = 0
	err = coordinator.submit(io, "agent:@ambiguous", body, cfg)
	if err == nil || !strings.Contains(err.Error(), "durably ambiguous") {
		t.Fatalf("same-payload unresolved retry error = %v", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("unresolved retry replayed input: pastes=%#v enters=%d", io.pastes, io.enters)
	}

	io.captures = []string{submitted}
	io.persistedMessages = []string{body}
	io.index = 0
	if err := coordinator.submit(io, "agent:@ambiguous", body, cfg); err != nil {
		t.Fatalf("eventually observable retry returned error: %v", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body || io.enters != 1 {
		t.Fatalf("confirmed retry changed exact-once actions: pastes=%#v enters=%d", io.pastes, io.enters)
	}
}

func TestCodexCoordinatorAmbiguitySurvivesCoordinatorRecreation(t *testing.T) {
	body := "durable ambiguous payload ZEN_RESTART_75319"
	receipt := "event-restart:claim-restart"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{
		suppressPersistence: true,
		clock:               time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC),
	}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-restart",
		}
		if len(current.pastes) > 0 {
			capture.content = ready + "\n› " + current.pastes[0] + "\n"
			capture.composer = codexComposerHasDraft
		}
		return capture
	}
	cfg := testCodexSubmitConfig()
	stateDir := t.TempDir()

	first, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("first coordinator: %v", err)
	}
	err = first.submitWithReceipt(io, "agent:@restart", body, receipt, cfg)
	if err == nil {
		t.Fatal("first coordinator must report ambiguous Enter")
	}
	if len(io.pastes) != 1 || io.pastes[0] != body || io.enters != 1 {
		t.Fatalf("first actions pastes=%#v enters=%d", io.pastes, io.enters)
	}

	second, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("second coordinator: %v", err)
	}
	accepted, receiptErr := second.hasAcceptedReceipt(io, "agent:@restart", receipt)
	if receiptErr != nil || accepted {
		t.Fatalf("ambiguous receipt accepted=%v err=%v", accepted, receiptErr)
	}
	err = second.submitWithReceipt(io, "agent:@restart", body, receipt, cfg)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("recreated coordinator error = %v, want durable ambiguity", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("recreated coordinator replayed input: pastes=%#v enters=%d", io.pastes, io.enters)
	}

	io.persistedMessages = []string{io.pastes[0]}
	third, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("third coordinator: %v", err)
	}
	if err := third.submitWithReceipt(io, "agent:@restart", body, receipt, cfg); err != nil {
		t.Fatalf("persisted user-message reconciliation: %v", err)
	}
	accepted, receiptErr = third.hasAcceptedReceipt(io, "agent:@restart", receipt)
	if receiptErr != nil || !accepted {
		t.Fatalf("reconciled receipt accepted=%v err=%v", accepted, receiptErr)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("reconciliation replayed input: pastes=%#v enters=%d", io.pastes, io.enters)
	}
}

func TestCodexCoordinatorReconcilesOnlyTargetRollout(t *testing.T) {
	body := "target-rollout-bound instruction"
	ready := codexReadyPane("")
	rolloutA := codexRolloutIdentity{
		Path:      "/fake/codex/sessions/rollout-A.jsonl",
		SessionID: "session-A",
	}
	rolloutB := codexRolloutIdentity{
		Path:      "/fake/codex/sessions/rollout-B.jsonl",
		SessionID: "session-B",
	}
	io := &fakeCodexInputIO{
		suppressPersistence: true,
		persistedByRollout: map[string][]string{
			rolloutB.Path: {body},
		},
	}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-target-rollout",
			rollout:    rolloutA,
		}
		if len(current.pastes) > 0 {
			capture.composer = codexComposerHasDraft
			if current.enters == 0 {
				capture.content = ready + "\n› " + current.pastes[0] +
					"\n\n  gpt-5.6 medium · /tmp\n"
			} else {
				capture.content = ready + "\n› " + current.pastes[0] +
					"\n\n• Working (generic progress only)\n"
			}
		}
		return capture
	}
	stateDir := t.TempDir()
	cfg := testCodexSubmitConfig()

	first, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("first coordinator: %v", err)
	}
	err = first.submit(io, "agent:@target-rollout", body, cfg)
	if err == nil || !strings.Contains(err.Error(), "durably ambiguous") {
		t.Fatalf("foreign-rollout confirmation error = %v", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("first actions pastes=%#v enters=%d", io.pastes, io.enters)
	}

	second, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("second coordinator: %v", err)
	}
	err = second.submit(io, "agent:@target-rollout", body, cfg)
	if err == nil || !strings.Contains(err.Error(), "durably ambiguous") {
		t.Fatalf("restart with foreign evidence error = %v", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("restart replayed actions pastes=%#v enters=%d", io.pastes, io.enters)
	}

	io.persistedByRollout[rolloutA.Path] = []string{body}
	third, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("third coordinator: %v", err)
	}
	if err := third.submit(io, "agent:@target-rollout", body, cfg); err != nil {
		t.Fatalf("target-rollout reconciliation: %v", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 {
		t.Fatalf("target reconciliation replayed actions pastes=%#v enters=%d", io.pastes, io.enters)
	}
}

func TestWatcherPublicCodexMutationsRejectAmbiguousOwner(t *testing.T) {
	sessionID := "agent:@public-ambiguous"
	generation := "generation-public-ambiguous"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{
		captures:    []string{ready},
		generations: []string{generation},
	}
	coordinator := newCodexInputCoordinator()
	record := codexTransactionRecord{
		SchemaVersion:     codexTransactionSchemaVersion,
		TransactionID:     "public-ambiguous-transaction",
		SessionID:         sessionID,
		SessionGeneration: generation,
		Action:            "submit_codex_input",
		Phase:             codexTransactionAmbiguous,
		PayloadSHA256:     codexSHA256("owned payload"),
		Instruction:       "owned payload",
		InstructionSHA256: codexSHA256("owned payload"),
		RolloutPath:       fakeCodexRollout(generation).Path,
		RolloutSessionID:  fakeCodexRollout(generation).SessionID,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := coordinator.store.Save(record); err != nil {
		t.Fatalf("save ambiguous owner: %v", err)
	}
	w := New(time.Second)
	w.codexInput = coordinator
	w.codexInputIO = io
	w.agents[sessionID] = &classifier.Agent{ID: sessionID, Command: "codex --no-alt-screen"}
	w.targetCommandResolver = func(target string) (string, bool) {
		return "codex --no-alt-screen", target == sessionID
	}

	mutations := []struct {
		name string
		run  func() error
	}{
		{name: "raw input", run: func() error { return w.SendInput(sessionID, "foreign raw text") }},
		{name: "key", run: func() error { return w.SendKey(sessionID, "Enter") }},
		{name: "action", run: func() error { return w.SendAction(sessionID, "pause") }},
		{
			name: "readiness raw input",
			run: func() error {
				return w.SendInputWhenReady(sessionID, "codex --no-alt-screen", "foreign raw text")
			},
		},
		{
			name: "structured unrelated submit",
			run: func() error {
				return w.SubmitInputWhenReady(sessionID, "codex --no-alt-screen", "unrelated payload")
			},
		},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			err := mutation.run()
			if err == nil ||
				(!strings.Contains(err.Error(), "unresolved") &&
					!strings.Contains(err.Error(), "different input")) {
				t.Fatalf("mutation error = %v", err)
			}
		})
	}
	if io.enters != 0 || len(io.pastes) != 0 {
		t.Fatalf("ambiguous owner mutated provider: pastes=%#v enters=%d", io.pastes, io.enters)
	}
}

func TestCodexCoordinatorOldGenerationDoesNotBlockReusedSessionID(t *testing.T) {
	oldBody := "old generation ambiguous ZEN_OLD_13579"
	newBody := "new generation independent ZEN_NEW_24680"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{suppressPersistence: true}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-old",
		}
		if len(current.pastes) > 0 {
			capture.content = ready + "\n› " + current.pastes[0] + "\n"
			capture.composer = codexComposerHasDraft
		}
		return capture
	}
	cfg := testCodexSubmitConfig()
	stateDir := t.TempDir()
	first, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("first coordinator: %v", err)
	}
	if err := first.submit(io, "agent:@reused", oldBody, cfg); err == nil ||
		!strings.Contains(err.Error(), "durably ambiguous") {
		t.Fatalf("old generation error = %v", err)
	}

	oldPasteCount := len(io.pastes)
	io.suppressPersistence = false
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-new",
		}
		if len(current.pastes) > oldPasteCount {
			draft := ready + "\n› " + current.pastes[len(current.pastes)-1] + "\n"
			capture.content = draft
			capture.composer = codexComposerHasDraft
			if current.enters > 1 {
				capture.content = draft + "\n• Working (1s • esc to interrupt)\n"
				capture.composer = codexComposerEmpty
			}
		}
		return capture
	}
	io.index = 0
	second, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("second coordinator: %v", err)
	}
	if err := second.submit(io, "agent:@reused", newBody, cfg); err != nil {
		t.Fatalf("new generation submit: %v", err)
	}
	if len(io.pastes) != 2 || io.pastes[0] != oldBody || io.pastes[1] != newBody || io.enters != 2 {
		t.Fatalf("generation actions pastes=%#v enters=%d", io.pastes, io.enters)
	}
}

func TestSubmitCodexInputReturnsPasteFailureWithoutEnter(t *testing.T) {
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{captures: []string{ready, ready}, pasteErr: errors.New("paste failed")}
	err := submitCodexInput(io, "agent:@1", "unique prompt body", testCodexSubmitConfig())
	if err == nil || !strings.Contains(err.Error(), "paste failed") ||
		!strings.Contains(err.Error(), "durably ambiguous") || io.enters != 0 {
		t.Fatalf("error=%v enters=%d", err, io.enters)
	}
}
