package watcher

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

type fakeCodexInputIO struct {
	captureFn           func(*fakeCodexInputIO) codexPaneCapture
	captures            []string
	styledCaptures      []string
	alive               []bool
	states              []codexComposerState
	generations         []string
	width               int
	index               int
	clock               time.Time
	pastes              []string
	enters              int
	clears              int
	cleared             bool
	pasteErr            error
	clearErr            error
	enterErr            error
	beforeClear         func(*fakeCodexInputIO)
	beforeEnter         func(*fakeCodexInputIO)
	suppressPersistence bool
	persistedMessages   []string
	persistedByRollout  map[string][]string
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
	} else if f.cleared || len(f.pastes) == 0 {
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
	width := f.width
	if width == 0 {
		width = 240
	}
	capture := codexPaneCapture{
		content:    f.captures[index],
		alive:      alive,
		composer:   state,
		generation: generation,
		width:      width,
	}
	if len(f.styledCaptures) > 0 {
		styledIndex := index
		if styledIndex >= len(f.styledCaptures) {
			styledIndex = len(f.styledCaptures) - 1
		}
		capture.styled = f.styledCaptures[styledIndex]
	}
	capture.rollout = fakeCodexRollout(generation)
	return capture
}

func (f *fakeCodexInputIO) pasteIfEmpty(
	_ string,
	_ string,
	_ codexRolloutIdentity,
	_ string,
	body string,
) error {
	f.pastes = append(f.pastes, body)
	f.cleared = false
	return f.pasteErr
}

func (f *fakeCodexInputIO) mutateOwned(
	sessionID string,
	prepared codexPreparedInput,
	mutation codexOwnedMutation,
) error {
	switch mutation {
	case codexOwnedMutationClear:
		if f.beforeClear != nil {
			f.beforeClear(f)
			if !codexCaptureMatchesPrepared(f.capture(sessionID), prepared, prepared.generation) {
				return fmt.Errorf("%w: foreign content was preserved", errCodexMutationConflict)
			}
		}
		if f.clearErr == nil {
			f.cleared = true
		}
		if f.clearErr == nil {
			f.clears++
		}
		return f.clearErr
	default:
		if f.beforeEnter != nil {
			f.beforeEnter(f)
			if !codexCaptureMatchesPrepared(f.capture(sessionID), prepared, prepared.generation) {
				return fmt.Errorf("%w: foreign content was preserved", errCodexMutationConflict)
			}
		}
		if f.enterErr != nil {
			return f.enterErr
		}
		f.enters++
		if !f.suppressPersistence && len(f.pastes) > 0 {
			message := f.pastes[len(f.pastes)-1]
			if f.persistedByRollout != nil {
				f.persistedByRollout[prepared.rollout.Path] = append(
					f.persistedByRollout[prepared.rollout.Path],
					message,
				)
			} else {
				f.persistedMessages = append(f.persistedMessages, message)
			}
		}
		return nil
	}
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
		draftTimeout:        time.Second,
		draftProgressWindow: time.Second,
		draftMaxTimeout:     6 * time.Second,
		draftBytesPerSecond: 1 << 30,
		recoveryTimeout:     2 * time.Second,
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

func TestCodexPaneInputLockHelperCrashRecoversWithoutReplay(t *testing.T) {
	if os.Getenv("ZEN_CODEX_LOCK_CRASH_HELPER") == "1" {
		sessionID := os.Getenv("ZEN_CODEX_LOCK_SESSION")
		generation := os.Getenv("ZEN_CODEX_LOCK_GENERATION")
		transactionID := os.Getenv("ZEN_CODEX_LOCK_TRANSACTION")
		_, err := lockCodexPaneInputOwned(
			sessionID,
			generation,
			transactionID,
			codexPaneLockOperationEnter,
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

func TestCodexConditionalMutationRejectsInputQueuedBeforePaneLock(t *testing.T) {
	if os.Getenv("ZEN_CODEX_PTY_BARRIER_HELPER") == "1" {
		rolloutFile, err := os.Open(os.Getenv("ZEN_CODEX_PTY_ROLLOUT"))
		if err != nil {
			t.Fatalf("open helper rollout: %v", err)
		}
		defer rolloutFile.Close()
		instruction := os.Getenv("ZEN_CODEX_PTY_INSTRUCTION")
		if _, err := fmt.Fprintf(
			os.Stdout,
			"\x1b[2J\x1b[H│ >_ OpenAI Codex (round5 PTY helper) │\r\n› %s\r\n\r\n  \x1b[38;2;246;226;183mgpt-5.6-sol xhigh\x1b[2m\x1b[39m · \x1b[0m\x1b[38;2;171;223;167m~/workspace\x1b[0m\x1b[2;%dH",
			instruction,
			len(instruction)+3,
		); err != nil {
			t.Fatalf("render helper composer: %v", err)
		}
		if err := os.WriteFile(os.Getenv("ZEN_CODEX_PTY_READY"), []byte("ready"), 0o600); err != nil {
			t.Fatalf("signal helper ready: %v", err)
		}
		for {
			if _, err := os.Stat(os.Getenv("ZEN_CODEX_PTY_CONSUME")); err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		buffer := make([]byte, 4096)
		count, err := os.Stdin.Read(buffer)
		if err != nil {
			t.Fatalf("read queued PTY input: %v", err)
		}
		received := append([]byte(nil), buffer[:count]...)
		if err := os.WriteFile(os.Getenv("ZEN_CODEX_PTY_RECEIVED"), received, 0o600); err != nil {
			t.Fatalf("record queued PTY input: %v", err)
		}
		if _, err := os.Stdout.Write(received); err != nil {
			t.Fatalf("render queued PTY input: %v", err)
		}
		select {}
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required for the real PTY ordering-barrier test")
	}

	const (
		instruction   = "ROUND5_EXACT_OWNED_INSTRUCTION"
		foreignSuffix = "FOREIGN_SUFFIX_终_Ω"
		rolloutID     = "019fc5e5-0000-7000-8000-000000000005"
	)
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	rolloutPath := filepath.Join(
		codexHome,
		"sessions",
		"2026",
		"08",
		"03",
		"rollout-round5-pty.jsonl",
	)
	if err := os.MkdirAll(filepath.Dir(rolloutPath), 0o700); err != nil {
		t.Fatalf("create helper rollout directory: %v", err)
	}
	writeCodexJournalTestRows(t, rolloutPath, []map[string]any{
		codexJournalSessionMeta(time.Now().UTC(), rolloutID),
	})
	t.Setenv("CODEX_HOME", codexHome)
	readyPath := filepath.Join(root, "ready")
	consumePath := filepath.Join(root, "consume")
	receivedPath := filepath.Join(root, "received")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	sessionID := fmt.Sprintf("zen-codex-pty-barrier-%d", time.Now().UnixNano())
	command := strings.Join([]string{
		"stty raw -echo;",
		"exec",
		"env",
		"ZEN_CODEX_PTY_BARRIER_HELPER=1",
		"ZEN_CODEX_PTY_ROLLOUT=" + shellQuote(rolloutPath),
		"ZEN_CODEX_PTY_INSTRUCTION=" + shellQuote(instruction),
		"ZEN_CODEX_PTY_READY=" + shellQuote(readyPath),
		"ZEN_CODEX_PTY_CONSUME=" + shellQuote(consumePath),
		"ZEN_CODEX_PTY_RECEIVED=" + shellQuote(receivedPath),
		shellQuote(executable),
		"-test.run=^TestCodexConditionalMutationRejectsInputQueuedBeforePaneLock$",
	}, " ")
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", sessionID, command).CombinedOutput(); err != nil {
		t.Fatalf("create PTY barrier helper: %v%s", err, commandOutputSuffix(out))
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "select-pane", "-e", "-t", sessionID).Run()
		_ = exec.Command("tmux", "kill-session", "-t", sessionID).Run()
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("PTY barrier helper did not render its exact composer")
		}
		time.Sleep(5 * time.Millisecond)
	}

	io := realCodexInputIO{}
	initial := io.capture(sessionID)
	if !initial.alive ||
		initial.generation == "" ||
		initial.composer != codexComposerHasDraft ||
		!initial.rollout.valid() {
		t.Fatalf("initial PTY capture = %#v", initial)
	}
	prepared := codexPreparedInput{
		transactionID: "round5-pty-barrier",
		instruction:   instruction,
		proofMode:     codexDraftProofExactLine,
		rollout:       initial.rollout,
		generation:    initial.generation,
	}
	if !codexCaptureMatchesPrepared(initial, prepared, initial.generation) {
		t.Fatalf("helper did not expose exact owned composer: %q", initial.content)
	}
	if out, err := exec.Command(
		"tmux",
		"send-keys",
		"-l",
		"-t",
		sessionID,
		"--",
		foreignSuffix,
	).CombinedOutput(); err != nil {
		t.Fatalf("queue foreign PTY suffix: %v%s", err, commandOutputSuffix(out))
	}

	mutationErr := io.mutateOwned(sessionID, prepared, codexOwnedMutationEnter)
	if err := os.WriteFile(consumePath, []byte("consume"), 0o600); err != nil {
		t.Fatalf("release helper PTY read: %v", err)
	}
	received := []byte(readFileWithMinSize(t, receivedPath, len(foreignSuffix), 3*time.Second))
	if mutationErr == nil || !errors.Is(mutationErr, errCodexMutationConflict) {
		t.Fatalf("queued foreign input conditional Enter error = %v", mutationErr)
	}
	if !bytes.Contains(received, []byte(foreignSuffix)) {
		t.Fatalf("foreign suffix was not retained in PTY bytes: %q", received)
	}
	if bytes.Contains(received, []byte{'\r'}) || bytes.Contains(received, []byte{'\n'}) {
		t.Fatalf("conditional Enter reached PTY after queued foreign suffix: %q", received)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		out, captureErr := exec.Command("tmux", "capture-pane", "-p", "-t", sessionID).Output()
		if captureErr == nil && strings.Contains(string(out), foreignSuffix) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("foreign suffix not preserved on screen output=%q err=%v", out, captureErr)
		}
		time.Sleep(5 * time.Millisecond)
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

func TestCodexComposerIdentityRejectsStyledForeignSuffix(t *testing.T) {
	const (
		generation  = "generation-round6-colored-footer"
		instruction = "ZEN_TX=round6;PATH_B64URL=L3JvdW5kNg;PAYLOAD_SHA256=abcdef;ACTION=decode_PATH_B64URL_then_read_exact_UTF8_verify_PAYLOAD_SHA256_and_follow_task"
	)
	const coloredForeign = "  \x1b[38;2;246;226;183mROUND7_FOREIGN_SUFFIX_终_Ω\x1b[0m\n"
	const dimForeign = "  \x1b[2mROUND7_FOREIGN_SUFFIX_终_Ω\x1b[0m\n"
	const genuineFooter = "  \x1b[38;2;246;226;183mgpt-5.6-sol xhigh\x1b[2m\x1b[39m · \x1b[0m\x1b[38;2;171;223;167m~/workspace\x1b[0m\n"
	tests := []struct {
		name string
		tail string
		want bool
	}{
		{
			name: "colored continuation",
			tail: coloredForeign,
		},
		{
			name: "blank then colored continuation",
			tail: "\n" + coloredForeign,
		},
		{
			name: "dim continuation",
			tail: dimForeign,
		},
		{
			name: "styled at-file continuation",
			tail: "  \x1b[38;2;171;223;167m@filename\x1b[0m\n",
		},
		{
			name: "styled image attachment continuation",
			tail: "  \x1b[2m[Image #1]\x1b[0m\n",
		},
		{
			name: "dedented colored suffix",
			tail: "\x1b[38;2;246;226;183mROUND7_FOREIGN_SUFFIX_终_Ω\x1b[0m\n",
		},
		{
			name: "genuine footer then extra suffix",
			tail: "\n" + genuineFooter + coloredForeign,
		},
		{
			name: "genuine real provider footer",
			tail: "\n" + genuineFooter,
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			styled := "\x1b[1m›\x1b[0m ZEN_TX=round6;PATH_B64URL=L3JvdW5kNg;PAYLOAD_SHA256=abcdef;ACTION=\n" +
				"  decode_PATH_B64URL_then_read_exact_UTF8_verify_PAYLOAD_SHA256_and_follow_task\n" +
				tc.tail
			rollout := fakeCodexRollout(generation)
			capture := codexPaneCapture{
				content:    stripCodexTerminalEscapes(styled),
				styled:     styled,
				alive:      true,
				composer:   codexComposerHasDraft,
				generation: generation,
				rollout:    rollout,
			}
			prepared := codexPreparedInput{
				instruction: instruction,
				proofMode:   codexDraftProofEnvelopeInstruction,
				rollout:     rollout,
				generation:  generation,
			}
			if got := codexCaptureMatchesPrepared(capture, prepared, generation); got != tc.want {
				t.Fatalf(
					"codexCaptureMatchesPrepared() = %t, want %t for styled tail %q",
					got,
					tc.want,
					stripCodexTerminalEscapes(tc.tail),
				)
			}
		})
	}
}

func TestCodexComposerIdentityAcceptsResetPrefixedRealFooter(t *testing.T) {
	const (
		generation  = "generation-round8-reset-prefixed-footer"
		instruction = "ZEN_TX=round8;PATH_B64URL=L3JvdW5kOA;PAYLOAD_SHA256=abcdef;ACTION=decode_PATH_B64URL_then_read_exact_UTF8_verify_PAYLOAD_SHA256_and_follow_task"
	)
	styled := "\x1b[1m›\x1b[0m ZEN_TX=round8;PATH_B64URL=L3JvdW5kOA;PAYLOAD_SHA256=abcdef;ACTION=\n" +
		"  decode_PATH_B64URL_then_read_exact_UTF8_verify_PAYLOAD_SHA256_and_follow_task\n\n" +
		"\x1b[0m  \x1b[38;2;246;226;183mgpt-5.6-sol xhigh\x1b[2m\x1b[39m · \x1b[0m\x1b[38;2;171;223;167m~/workspace\x1b[0m\n"
	rollout := fakeCodexRollout(generation)
	capture := codexPaneCapture{
		content:    stripCodexTerminalEscapes(styled),
		styled:     styled,
		alive:      true,
		composer:   codexComposerHasDraft,
		generation: generation,
		rollout:    rollout,
	}
	prepared := codexPreparedInput{
		instruction: instruction,
		proofMode:   codexDraftProofEnvelopeInstruction,
		rollout:     rollout,
		generation:  generation,
	}
	if !codexCaptureMatchesPrepared(capture, prepared, generation) {
		t.Fatalf(
			"reset-prefixed real Provider footer obscured exact composer identity: %q",
			capture.content,
		)
	}
}

func TestCodexConditionalMutationRejectsResetPrefixedForeignSuffixes(t *testing.T) {
	const (
		generation   = "generation-round8-reset-prefixed-foreign"
		styledFooter = "  \x1b[38;2;246;226;183mgpt-5.6-sol xhigh\x1b[2m\x1b[39m · \x1b[0m\x1b[38;2;171;223;167m~/workspace\x1b[0m"
	)
	tests := []struct {
		name       string
		styledTail string
	}{
		{
			name:       "colored",
			styledTail: "\x1b[0m  \x1b[38;2;246;226;183mROUND8_COLORED_FOREIGN_SUFFIX_终_Ω\x1b[0m",
		},
		{
			name:       "dim",
			styledTail: "\x1b[0m  \x1b[2mROUND8_DIM_FOREIGN_SUFFIX_终_Ω\x1b[0m",
		},
		{
			name:       "blank separated colored",
			styledTail: "\n\x1b[0m  \x1b[38;2;246;226;183mROUND8_BLANK_FOREIGN_SUFFIX_终_Ω\x1b[0m",
		},
		{
			name:       "arbitrary",
			styledTail: "\x1b[0m  ROUND8_ARBITRARY_FOREIGN_SUFFIX_终_Ω",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := "Round 8 exact durable envelope first line\nUnicode second line 终 Ω"
			ready := codexReadyPane("")
			foreignAtMutation := false
			expectedContent := ""
			expectedStyled := ""
			io := &fakeCodexInputIO{width: 240}
			io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
				capture := codexPaneCapture{
					content:    ready,
					styled:     ready,
					alive:      true,
					composer:   codexComposerEmpty,
					generation: generation,
					width:      current.width,
				}
				if len(current.pastes) == 0 {
					return capture
				}
				capture.styled = ready + "\n› " + current.pastes[0]
				if foreignAtMutation {
					capture.styled += "\n" + tc.styledTail
				}
				capture.styled += "\n\n" + styledFooter + "\n"
				capture.content = stripCodexTerminalEscapes(capture.styled)
				capture.composer = codexComposerHasDraft
				if foreignAtMutation {
					expectedContent = capture.content
					expectedStyled = capture.styled
				}
				return capture
			}
			io.beforeEnter = func(*fakeCodexInputIO) {
				foreignAtMutation = true
			}
			coordinator, err := newPersistentCodexInputCoordinator(t.TempDir())
			if err != nil {
				t.Fatalf("new coordinator: %v", err)
			}

			err = coordinator.submit(
				io,
				"agent:@round8-reset-prefixed-foreign",
				body,
				testCodexSubmitConfig(),
			)
			if err == nil || !strings.Contains(err.Error(), "foreign content was preserved") {
				t.Fatalf("reset-prefixed foreign suffix error = %v", err)
			}
			if len(io.pastes) != 1 || io.enters != 0 || io.clears != 0 {
				t.Fatalf(
					"mutations pastes=%#v enters=%d clears=%d, want one paste and zero Enter/C-c",
					io.pastes,
					io.enters,
					io.clears,
				)
			}
			preserved := io.capture("agent:@round8-reset-prefixed-foreign")
			if expectedContent == "" ||
				preserved.content != expectedContent ||
				preserved.styled != expectedStyled {
				t.Fatalf(
					"foreign composer changed\nwant content=%q styled=%q\ngot content=%q styled=%q",
					expectedContent,
					expectedStyled,
					preserved.content,
					preserved.styled,
				)
			}
			if !strings.Contains(preserved.content, "ROUND8_") {
				t.Fatalf("distinctive foreign bytes were not preserved: %q", preserved.content)
			}
		})
	}
}

func TestCodexConditionalMutationRejectsEditableSuffixBelowCursor(t *testing.T) {
	if os.Getenv("ZEN_CODEX_BELOW_CURSOR_HELPER") == "1" {
		rolloutFile, err := os.Open(os.Getenv("ZEN_CODEX_BELOW_CURSOR_ROLLOUT"))
		if err != nil {
			t.Fatalf("open helper rollout: %v", err)
		}
		defer rolloutFile.Close()
		instruction := os.Getenv("ZEN_CODEX_BELOW_CURSOR_INSTRUCTION")
		foreignSuffix := os.Getenv("ZEN_CODEX_BELOW_CURSOR_SUFFIX")
		if _, err := fmt.Fprintf(
			os.Stdout,
			"\x1b[2J\x1b[H│ >_ OpenAI Codex (round6 below-cursor helper) │\r\n› %s\r\n  %s\r\n\r\n  \x1b[38;2;246;226;183mgpt-5.6-sol xhigh\x1b[2m\x1b[39m · \x1b[0m\x1b[38;2;171;223;167m~/workspace\x1b[0m\x1b[2;%dH",
			instruction,
			foreignSuffix,
			len(instruction)+3,
		); err != nil {
			t.Fatalf("render below-cursor helper composer: %v", err)
		}
		if err := os.WriteFile(os.Getenv("ZEN_CODEX_BELOW_CURSOR_READY"), []byte("ready"), 0o600); err != nil {
			t.Fatalf("signal below-cursor helper ready: %v", err)
		}
		var sequence []byte
		buffer := make([]byte, 64)
		for {
			count, err := os.Stdin.Read(buffer)
			if err != nil {
				t.Fatalf("read below-cursor helper input: %v", err)
			}
			sequence = append(sequence, buffer[:count]...)
			for len(sequence) > 0 {
				switch {
				case sequence[0] == '\r' || sequence[0] == '\n':
					file, err := os.OpenFile(
						os.Getenv("ZEN_CODEX_BELOW_CURSOR_MUTATIONS"),
						os.O_CREATE|os.O_WRONLY|os.O_APPEND,
						0o600,
					)
					if err != nil {
						t.Fatalf("open below-cursor mutation log: %v", err)
					}
					if _, err := file.WriteString("ENTER\n"); err != nil {
						_ = file.Close()
						t.Fatalf("record below-cursor Enter: %v", err)
					}
					if err := file.Close(); err != nil {
						t.Fatalf("close below-cursor mutation log: %v", err)
					}
					sequence = sequence[1:]
				case len(sequence) >= 3 && bytes.Equal(sequence[:3], []byte{0x1b, '[', 'D'}):
					if _, err := os.Stdout.Write([]byte("\x1b[D")); err != nil {
						t.Fatalf("acknowledge Left probe: %v", err)
					}
					sequence = sequence[3:]
				case len(sequence) >= 3 && bytes.Equal(sequence[:3], []byte{0x1b, '[', 'C'}):
					if _, err := os.Stdout.Write([]byte("\x1b[C")); err != nil {
						t.Fatalf("acknowledge Right probe: %v", err)
					}
					sequence = sequence[3:]
				case sequence[0] == 0x1b && len(sequence) < 3:
					break
				default:
					sequence = sequence[1:]
				}
				if len(sequence) > 0 && sequence[0] == 0x1b && len(sequence) < 3 {
					break
				}
			}
		}
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required for the real below-cursor composer test")
	}

	const (
		instruction   = "ROUND6_EXACT_OWNED_INSTRUCTION"
		foreignSuffix = "ROUND6_FOREIGN_SUFFIX_BELOW_CURSOR_终_Ω"
		rolloutID     = "019fc6a0-0000-7000-8000-000000000006"
	)
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	rolloutPath := filepath.Join(
		codexHome,
		"sessions",
		"2026",
		"08",
		"03",
		"rollout-round6-below-cursor.jsonl",
	)
	if err := os.MkdirAll(filepath.Dir(rolloutPath), 0o700); err != nil {
		t.Fatalf("create below-cursor rollout directory: %v", err)
	}
	writeCodexJournalTestRows(t, rolloutPath, []map[string]any{
		codexJournalSessionMeta(time.Now().UTC(), rolloutID),
	})
	t.Setenv("CODEX_HOME", codexHome)
	readyPath := filepath.Join(root, "ready")
	mutationPath := filepath.Join(root, "mutations")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	sessionID := fmt.Sprintf("zen-codex-below-cursor-%d", time.Now().UnixNano())
	command := strings.Join([]string{
		"stty raw -echo;",
		"exec",
		"env",
		"ZEN_CODEX_BELOW_CURSOR_HELPER=1",
		"ZEN_CODEX_BELOW_CURSOR_ROLLOUT=" + shellQuote(rolloutPath),
		"ZEN_CODEX_BELOW_CURSOR_INSTRUCTION=" + shellQuote(instruction),
		"ZEN_CODEX_BELOW_CURSOR_SUFFIX=" + shellQuote(foreignSuffix),
		"ZEN_CODEX_BELOW_CURSOR_READY=" + shellQuote(readyPath),
		"ZEN_CODEX_BELOW_CURSOR_MUTATIONS=" + shellQuote(mutationPath),
		shellQuote(executable),
		"-test.run=^TestCodexConditionalMutationRejectsEditableSuffixBelowCursor$",
	}, " ")
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", sessionID, command).CombinedOutput(); err != nil {
		t.Fatalf("create below-cursor helper: %v%s", err, commandOutputSuffix(out))
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "select-pane", "-e", "-t", sessionID).Run()
		_ = exec.Command("tmux", "kill-session", "-t", sessionID).Run()
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("below-cursor helper did not render")
		}
		time.Sleep(5 * time.Millisecond)
	}

	fullPane, err := exec.Command(
		"tmux",
		"capture-pane",
		"-e",
		"-J",
		"-p",
		"-S",
		"-1000",
		"-t",
		sessionID,
	).Output()
	if err != nil {
		t.Fatalf("capture complete helper pane: %v", err)
	}
	if !strings.Contains(stripCodexTerminalEscapes(string(fullPane)), foreignSuffix) {
		t.Fatalf("semantic precondition: full pane omitted foreign suffix: %q", fullPane)
	}
	io := realCodexInputIO{}
	initial := io.capture(sessionID)
	if !initial.alive || initial.generation == "" || !initial.rollout.valid() {
		t.Fatalf("initial below-cursor capture = %#v", initial)
	}
	prepared := codexPreparedInput{
		transactionID: "round6-below-cursor",
		instruction:   instruction,
		proofMode:     codexDraftProofExactLine,
		rollout:       initial.rollout,
		generation:    initial.generation,
	}
	mutationErr := io.mutateOwned(sessionID, prepared, codexOwnedMutationEnter)
	mutations, readErr := os.ReadFile(mutationPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read below-cursor mutation log: %v", readErr)
	}
	if mutationErr == nil || !errors.Is(mutationErr, errCodexMutationConflict) {
		t.Fatalf(
			"below-cursor foreign suffix conditional Enter error=%v mutations=%q bounded_capture=%q full_capture=%q",
			mutationErr,
			mutations,
			initial.content,
			stripCodexTerminalEscapes(string(fullPane)),
		)
	}
	if bytes.Contains(mutations, []byte("ENTER")) {
		t.Fatalf("conditional Enter reached helper despite foreign suffix: %q", mutations)
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
	if len(io.pastes) != 0 || io.enters != 0 || io.clears != 0 {
		t.Fatalf("untracked Codex mutated provider pastes=%#v enters=%d clears=%d", io.pastes, io.enters, io.clears)
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
	if len(io.pastes) != 0 || io.enters != 0 || io.clears != 0 {
		t.Fatalf("provider mutations pastes=%#v enters=%d clears=%d", io.pastes, io.enters, io.clears)
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
	if len(io.pastes) != 0 || io.enters != 0 || io.clears != 0 {
		t.Fatalf("package commandless mutation reached provider pastes=%#v enters=%d clears=%d", io.pastes, io.enters, io.clears)
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

func TestCodexPayloadNormalizationAndSubmitBoundary(t *testing.T) {
	body, submit := splitCodexSubmitInput("line one\r\nline two\r\n\r\n")
	if !submit || body != "line one\r\nline two\r\n" {
		t.Fatalf("split body=%q submit=%v", body, submit)
	}
	normalized, err := normalizeCodexPayload(body)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized != "line one\nline two\n" {
		t.Fatalf("normalized = %q", normalized)
	}
	if _, err := normalizeCodexPayload("unsafe\x00payload"); err == nil {
		t.Fatal("NUL must be rejected before hashing or paste")
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
	if normalized != "alpha\nβ\n" {
		t.Fatalf("normalized structured payload = %q, want caller-owned final LF", normalized)
	}
	if got, want := codexSHA256(normalized), codexSHA256("alpha\nβ\n"); got != want {
		t.Fatalf("payload digest = %s, want %s", got, want)
	}
}

func TestWatcherStructuredSubmitPreservesNormalizedFinalLineEndingInEnvelope(t *testing.T) {
	payload := "alpha\r\nβ\n"
	normalized := "alpha\nβ\n"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{width: 240}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-structured-final-lf",
			width:      current.width,
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
	envelopePath := filepath.Join(
		stateDir,
		"codex-input",
		"envelopes",
		codexSHA256(normalized),
	)
	raw, err := os.ReadFile(envelopePath)
	if err != nil {
		t.Fatalf("read normalized envelope: %v", err)
	}
	if string(raw) != normalized {
		t.Fatalf("envelope payload = %q, want %q", raw, normalized)
	}
}

func TestCodexDraftVisibleRequiresExactSingleRowIdentity(t *testing.T) {
	longChinese := "修复一个已经在 Brain delegated follow-up 中真实复现的 Codex 输入事务 bug。事实：Zen agent send 向 @341 粘贴了一条较长的单行中文任务文本；完整 Unicode 草稿不得丢失。"
	tests := []struct {
		name    string
		body    string
		draft   string
		visible bool
	}{
		{
			name:    "long Chinese single line visually wrapped",
			body:    longChinese,
			draft:   "› 修复一个已经在 Brain delegated\n  follow-up 中真实复现的 Codex 输入事务 bug。事实：\n  Zen agent send 向 @341 粘贴了一条较长的单行中文任务文本；\n  完整 Unicode 草稿不得丢失。\n",
			visible: false,
		},
		{
			name:    "ordinary ASCII",
			body:    "fix the Codex input transaction marker ZEN_ASCII_24680",
			draft:   "› fix the Codex input transaction marker ZEN_ASCII_24680\n",
			visible: true,
		},
		{
			name:    "multiline body with visual wrapping",
			body:    "inspect the watcher transaction\npreserve the provider-native queue\n验证完整 Unicode 草稿",
			draft:   "› inspect the watcher transaction\n  preserve the provider-native\n  queue\n  验证完整 Unicode 草稿\n",
			visible: false,
		},
		{
			name: "similar old text is not the requested draft",
			body: "shared delegated follow-up prefix deliberately longer than thirty-six runes with NEW_REQUIRED_TAIL",
			draft: "› shared delegated follow-up prefix deliberately longer than thirty-six runes with " +
				"OLD_STALE_TAIL\n",
			visible: false,
		},
		{
			name:    "duplicated body is not an exact draft",
			body:    "one exact payload with duplicate-sensitive marker ZEN_ONCE_97531",
			draft:   "› one exact payload with duplicate-sensitive marker ZEN_ONCE_97531one exact payload with duplicate-sensitive marker ZEN_ONCE_97531\n",
			visible: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := codexReadyPane("")
			after := before + "\n" + tc.draft
			if got := codexDraftVisible(before, after, tc.body); got != tc.visible {
				t.Fatalf("codexDraftVisible() = %v, want %v", got, tc.visible)
			}
		})
	}
}

func TestCodexCoordinatorUsesExactDurableEnvelopeForWrappedLongUnicode(t *testing.T) {
	body := "修复一个已经在 Brain delegated follow-up 中真实复现的 Codex 输入事务 bug。\n完整 Unicode 草稿由精确的内容寻址信封承载。"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{width: 100}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-envelope",
			width:      current.width,
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
	if len(io.pastes) != 1 || io.pastes[0] == body ||
		!strings.Contains(io.pastes[0], "PATH_B64URL=") ||
		!strings.Contains(io.pastes[0], "PAYLOAD_SHA256=") {
		t.Fatalf("pastes = %#v, want one narrow digest-bound envelope instruction", io.pastes)
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
		record.InstructionSHA256 != codexSHA256(io.pastes[0]) ||
		record.EnvelopePath != envelopePath ||
		!strings.Contains(io.pastes[0], record.TransactionID) ||
		!strings.Contains(io.pastes[0], base64.RawURLEncoding.EncodeToString([]byte(envelopePath))) ||
		!strings.Contains(io.pastes[0], record.PayloadSHA256) {
		t.Fatalf("transaction record = %#v instruction=%q", record, io.pastes[0])
	}
	if info, err := os.Stat(transactionPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("transaction mode info=%v err=%v", info, err)
	}
}

func TestPersistentCodexCoordinatorUsesDirectPathForObservableShortPrompt(t *testing.T) {
	body := "short exact Unicode 中文"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{width: 240}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-short-direct",
			width:      current.width,
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

func TestCodexEnvelopeComposerIdentityRequiresExactWrappedBytes(t *testing.T) {
	instruction := codexEnvelopeInstruction(
		"0123456789abcdef0123",
		"/durable/state/codex-input/envelopes/0123456789abcdef",
		strings.Repeat("a", 64),
	)
	split := len(instruction) / 2
	prepared := codexPreparedInput{
		instruction: instruction,
		proofMode:   codexDraftProofEnvelopeInstruction,
		rollout:     fakeCodexRollout("generation-wrapped-envelope"),
		generation:  "generation-wrapped-envelope",
	}
	capture := codexPaneCapture{
		content:    "› " + instruction[:split] + "\n  " + instruction[split:] + "\n",
		alive:      true,
		composer:   codexComposerHasDraft,
		generation: "generation-wrapped-envelope",
		rollout:    fakeCodexRollout("generation-wrapped-envelope"),
	}
	if !codexCaptureMatchesPrepared(capture, prepared, capture.generation) {
		t.Fatal("exact visual wrapping must preserve the complete envelope identity")
	}

	for name, mutate := range map[string]func(string) string{
		"changed byte": func(value string) string {
			return strings.Replace(value, "PAYLOAD_SHA256=", "PAYLOAD_SHA256=b", 1)
		},
		"inserted whitespace": func(value string) string {
			return strings.Replace(value, ";ACTION=", " ;ACTION=", 1)
		},
		"foreign suffix": func(value string) string {
			return value + "-foreign"
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := mutate(instruction)
			changedSplit := min(split, len(changed))
			capture.content = "› " + changed[:changedSplit] + "\n  " + changed[changedSplit:] + "\n"
			if codexCaptureMatchesPrepared(capture, prepared, capture.generation) {
				t.Fatalf("mutated wrapped composer authorized Enter: %q", changed)
			}
		})
	}
}

func TestCodexMutationPointRejectsComposerOwnedBlankBulletAndForeignTail(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		width    int
		envelope bool
	}{
		{
			name:  "direct",
			body:  "short exact instruction",
			width: 240,
		},
		{
			name:     "wrapped envelope",
			body:     "first payload line\nsecond Unicode payload line 中文",
			width:    76,
			envelope: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const generation = "generation-foreign-multiline-tail"
			ready := codexReadyPane("")
			foreignAtMutation := false
			foreignComposer := ""
			io := &fakeCodexInputIO{width: tc.width}
			renderComposer := func(instruction string, foreign bool) string {
				first := instruction
				continuation := ""
				if tc.envelope {
					split := len(instruction) / 2
					first = instruction[:split]
					continuation = "\n  " + instruction[split:]
				}
				content := ready + "\n› " + first + continuation
				if foreign {
					content += "\n\n  • user-entered bullet is composer content\n  ROUND5_FOREIGN_TAIL_终_Ω"
				}
				return content + "\n"
			}
			io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
				capture := codexPaneCapture{
					content:    ready,
					alive:      true,
					composer:   codexComposerEmpty,
					generation: generation,
					width:      current.width,
				}
				if len(current.pastes) > 0 {
					capture.content = renderComposer(current.pastes[0], foreignAtMutation)
					capture.composer = codexComposerHasDraft
					if foreignAtMutation {
						foreignComposer = capture.content
					}
				}
				return capture
			}
			io.beforeEnter = func(*fakeCodexInputIO) {
				foreignAtMutation = true
			}
			coordinator, err := newPersistentCodexInputCoordinator(t.TempDir())
			if err != nil {
				t.Fatalf("new coordinator: %v", err)
			}

			err = coordinator.submit(io, "agent:@foreign-multiline-tail", tc.body, testCodexSubmitConfig())
			if err == nil || !strings.Contains(err.Error(), "foreign content was preserved") {
				t.Fatalf("mutation-point foreign tail error = %v", err)
			}
			if len(io.pastes) != 1 || io.enters != 0 || io.clears != 0 {
				t.Fatalf("mutations pastes=%#v enters=%d clears=%d", io.pastes, io.enters, io.clears)
			}
			preserved := io.capture("agent:@foreign-multiline-tail")
			if foreignComposer == "" || preserved.content != foreignComposer {
				t.Fatalf(
					"foreign composer changed\nwant=%q\ngot=%q",
					foreignComposer,
					preserved.content,
				)
			}
		})
	}
}

func TestCodexComposerIdentityRejectsExtraBlankBeforeStyledProviderChrome(t *testing.T) {
	const (
		generation  = "generation-styled-blank-boundary"
		instruction = "ZEN_TX=styled-blank-boundary;PAYLOAD_SHA256=0123456789abcdef"
	)
	prepared := codexPreparedInput{
		instruction: instruction,
		proofMode:   codexDraftProofEnvelopeInstruction,
		rollout:     fakeCodexRollout(generation),
		generation:  generation,
	}
	split := len(instruction) / 2
	composer := "› " + instruction[:split] + "\n  " + instruction[split:]
	plainFooter := "  gpt-5.6 medium · /tmp"
	styledFooter := "  \x1b[38;2;246;226;183mgpt-5.6 medium\x1b[2m\x1b[39m · \x1b[0m\x1b[38;2;171;223;167m/tmp\x1b[0m"
	capture := codexPaneCapture{
		content:    composer + "\n\n" + plainFooter + "\n",
		styled:     composer + "\n\n" + styledFooter + "\n",
		alive:      true,
		composer:   codexComposerHasDraft,
		generation: generation,
		rollout:    prepared.rollout,
	}
	if !codexCaptureMatchesPrepared(capture, prepared, generation) {
		t.Fatal("one provider-owned layout separator must preserve exact wrapped identity")
	}
	capture.content = composer + "\n\n\n" + plainFooter + "\n"
	capture.styled = composer + "\n\n\n" + styledFooter + "\n"
	if codexCaptureMatchesPrepared(capture, prepared, generation) {
		t.Fatal("extra user-enterable blank before provider chrome authorized mutation")
	}
}

func TestSubmitCodexInputExtendsDraftDeadlineOnObservableProgress(t *testing.T) {
	body := "progressively-rendered-Unicode-中文-line-one-exact-tail"
	ready := codexReadyPane("")
	partialOne := ready + "\n› progressively-rendered-Unicode-中文\n"
	partialTwo := ready + "\n› progressively-rendered-Unicode-中文-line-one\n"
	draft := ready + "\n› " + body + "\n"
	submitted := draft + "\n• Working (1s • esc to interrupt)\n"
	io := &fakeCodexInputIO{
		captures: []string{
			ready,
			ready,
			partialOne,
			partialOne,
			partialTwo,
			partialTwo,
			draft,
			draft,
			submitted,
		},
	}
	cfg := testCodexSubmitConfig()
	cfg.draftTimeout = 2 * time.Second
	cfg.draftProgressWindow = 2 * time.Second

	if err := submitCodexInput(io, "agent:@slow-render", body, cfg); err != nil {
		t.Fatalf("submitCodexInput returned error after observable draft progress: %v", err)
	}
	if len(io.pastes) != 1 || io.pastes[0] != body {
		t.Fatalf("pastes = %#v, want exact progressive body once", io.pastes)
	}
	if io.enters != 1 {
		t.Fatalf("Enter count = %d, want exactly 1", io.enters)
	}
}

func TestCodexDraftVisibleRejectsWrongCollapsedPasteLength(t *testing.T) {
	body := strings.Repeat("x", 2048)
	before := codexReadyPane("")
	after := before + "\n› [Pasted Content 1024 chars]\n"

	if codexDraftVisible(before, after, body) {
		t.Fatal("a placeholder for a different paste length must not prove the intended draft")
	}
}

func TestCodexDraftVisibleRejectsCollapsedMarkerWithoutExactIdentity(t *testing.T) {
	body := strings.Repeat("a", 2048)
	before := codexReadyPane("")
	after := before + "\n› [Pasted Content 2048 chars]\n"

	if codexDraftVisible(before, after, body) {
		t.Fatal("a matching rune-count marker does not acknowledge the intended bytes")
	}
}

func TestCodexDraftVisibleRejectsForeignAndWhitespaceCorruptedComposer(t *testing.T) {
	before := codexReadyPane("")
	tests := []struct {
		name  string
		body  string
		draft string
	}{
		{
			name:  "foreign prefix and suffix",
			body:  "exact-payload-ZEN-IDENTITY",
			draft: "› foreign-prefix-exact-payload-ZEN-IDENTITY-foreign-suffix\n",
		},
		{
			name:  "Unicode whitespace changed",
			body:  "alpha\u2003beta",
			draft: "› alpha beta\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if codexDraftVisible(before, before+"\n"+tc.draft, tc.body) {
				t.Fatal("the entire logical composer must equal the intended normalized bytes")
			}
		})
	}
}

func TestSubmitCodexInputRejectsSameLengthWrongIdentityAndMarker(t *testing.T) {
	intended := "AAAA-exact-identity"
	ready := codexReadyPane("")
	tests := []struct {
		name  string
		draft string
	}{
		{name: "same rune count different bytes", draft: ready + "\n› BBBB-wrong-identity\n"},
		{name: "same rune count marker only", draft: ready + "\n› [Pasted Content 19 chars]\n"},
		{name: "foreign wrapper", draft: ready + "\n› prefix-AAAA-exact-identity-suffix\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			io := &fakeCodexInputIO{
				captures: []string{ready, ready, tc.draft},
				states: []codexComposerState{
					codexComposerEmpty,
					codexComposerEmpty,
					codexComposerHasDraft,
				},
			}
			err := submitCodexInput(io, "agent:@wrong-identity", intended, testCodexSubmitConfig())
			if err == nil || !strings.Contains(err.Error(), "Enter was not sent") {
				t.Fatalf("error = %v, want exact-proof failure", err)
			}
			if len(io.pastes) != 1 || io.pastes[0] != intended || io.enters != 0 || io.clears != 0 {
				t.Fatalf("actions pastes=%#v enters=%d clears=%d", io.pastes, io.enters, io.clears)
			}
		})
	}
}

func TestCodexDraftObservationBudgetScalesAndRemainsBounded(t *testing.T) {
	cfg := defaultCodexSubmitConfig()
	short := codexDraftObservationBudget("short prompt", cfg)
	long := codexDraftObservationBudget(
		strings.Repeat("多字节 Unicode line\n", 400),
		cfg,
	)

	if short != cfg.draftTimeout {
		t.Fatalf("short budget = %s, want base %s", short, cfg.draftTimeout)
	}
	if long <= short {
		t.Fatalf("long budget = %s, want greater than short budget %s", long, short)
	}
	if long > cfg.draftMaxTimeout {
		t.Fatalf("long budget = %s, exceeds hard limit %s", long, cfg.draftMaxTimeout)
	}
	if got := codexDraftObservationBudget(strings.Repeat("x", 10_000_000), cfg); got != cfg.draftMaxTimeout {
		t.Fatalf("huge budget = %s, want hard limit %s", got, cfg.draftMaxTimeout)
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
	if io.clears != 0 || len(io.pastes) != 0 || io.enters != 0 {
		t.Fatalf("foreign draft mutated: clears=%d pastes=%#v enters=%d", io.clears, io.pastes, io.enters)
	}
}

func TestSubmitCodexInputPreservesForeignReplacementBeforeRecovery(t *testing.T) {
	body := "owned exact payload"
	ready := codexReadyPane("")
	partial := ready + "\n› owned exact\n"
	foreign := ready + "\n› foreign replacement must survive\n"
	io := &fakeCodexInputIO{
		captures: []string{ready, ready, ready, partial, foreign, foreign, foreign},
		states: []codexComposerState{
			codexComposerEmpty,
			codexComposerEmpty,
			codexComposerEmpty,
			codexComposerHasDraft,
			codexComposerHasDraft,
			codexComposerHasDraft,
			codexComposerHasDraft,
		},
	}
	cfg := testCodexSubmitConfig()

	err := submitCodexInput(io, "agent:@foreign-replacement", body, cfg)
	if err == nil {
		t.Fatal("expected exact-proof conflict")
	}
	if io.clears != 0 || io.enters != 0 || len(io.pastes) != 1 {
		t.Fatalf("foreign replacement mutated: clears=%d pastes=%#v enters=%d", io.clears, io.pastes, io.enters)
	}
}

func TestCodexCoordinatorClearsOnlyAcknowledgedUnchangedOwnedDraft(t *testing.T) {
	body := "owned-draft-for-cleanup"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-owned-cleanup",
			width:      240,
		}
		if len(current.pastes) > 0 && !current.cleared {
			capture.content = ready + "\n› " + current.pastes[0] + "\n"
			capture.composer = codexComposerHasDraft
		}
		return capture
	}
	store := &failCodexStoreAtPhase{
		codexTransactionStore: newMemoryCodexTransactionStore(),
		phase:                 codexTransactionDraftAcknowledged,
	}
	coordinator := newCodexInputCoordinatorWithStore(store)

	err := coordinator.submit(io, "agent:@owned-cleanup", body, testCodexSubmitConfig())
	if err == nil || !strings.Contains(err.Error(), "unchanged owned composer was cleared") {
		t.Fatalf("error = %v, want owned cleanup result", err)
	}
	if io.clears != 1 || len(io.pastes) != 1 || io.pastes[0] != body || io.enters != 0 {
		t.Fatalf("actions clears=%d pastes=%#v enters=%d", io.clears, io.pastes, io.enters)
	}
}

func TestCodexCoordinatorRequiresDurableEnterIntentBeforeEnter(t *testing.T) {
	body := "journal-before-enter"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-enter-intent",
			width:      240,
		}
		if len(current.pastes) > 0 && !current.cleared {
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
	if err == nil || !strings.Contains(err.Error(), "Enter was not sent") {
		t.Fatalf("error = %v, want durable Enter-intent failure", err)
	}
	if io.clears != 1 || len(io.pastes) != 1 || io.pastes[0] != body || io.enters != 0 {
		t.Fatalf("actions clears=%d pastes=%#v enters=%d", io.clears, io.pastes, io.enters)
	}
}

func TestCodexCoordinatorRejectsForeignMutationAtEnterMutationPoint(t *testing.T) {
	body := "owned-enter-draft"
	ready := codexReadyPane("")
	foreign := false
	io := &fakeCodexInputIO{width: 240}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-enter-race",
			width:      current.width,
		}
		if len(current.pastes) > 0 {
			draft := current.pastes[0]
			if foreign {
				draft += "-foreign"
			}
			capture.content = ready + "\n› " + draft + "\n"
			capture.composer = codexComposerHasDraft
		}
		return capture
	}
	io.beforeEnter = func(*fakeCodexInputIO) { foreign = true }

	err := submitCodexInput(io, "agent:@enter-race", body, testCodexSubmitConfig())
	if err == nil || !strings.Contains(err.Error(), "foreign") {
		t.Fatalf("error = %v, want mutation-point conflict", err)
	}
	if io.enters != 0 || io.clears != 0 || len(io.pastes) != 1 {
		t.Fatalf("actions pastes=%#v enters=%d clears=%d", io.pastes, io.enters, io.clears)
	}
}

func TestCodexCoordinatorRejectsForeignMutationAtClearMutationPoint(t *testing.T) {
	body := "owned-clear-draft"
	ready := codexReadyPane("")
	foreign := false
	io := &fakeCodexInputIO{width: 240}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-clear-race",
			width:      current.width,
		}
		if len(current.pastes) > 0 && !current.cleared {
			draft := current.pastes[0]
			if foreign {
				draft += "-foreign"
			}
			capture.content = ready + "\n› " + draft + "\n"
			capture.composer = codexComposerHasDraft
		}
		return capture
	}
	io.beforeClear = func(*fakeCodexInputIO) { foreign = true }
	store := &failCodexStoreAtPhase{
		codexTransactionStore: newMemoryCodexTransactionStore(),
		phase:                 codexTransactionDraftAcknowledged,
	}

	err := newCodexInputCoordinatorWithStore(store).submit(
		io,
		"agent:@clear-race",
		body,
		testCodexSubmitConfig(),
	)
	if err == nil || !strings.Contains(err.Error(), "foreign") {
		t.Fatalf("error = %v, want mutation-point cleanup conflict", err)
	}
	if io.clears != 0 || io.enters != 0 || len(io.pastes) != 1 {
		t.Fatalf("actions pastes=%#v enters=%d clears=%d", io.pastes, io.enters, io.clears)
	}
}

func TestSubmitCodexInputTimeoutPreservesUnacknowledgedPartialAndRetryDoesNotAppend(t *testing.T) {
	body := "retry-safe-partial-exact-tail"
	ready := codexReadyPane("")
	partial := ready + "\n› retry-safe-partial\n"
	io := &fakeCodexInputIO{
		captures: []string{ready, ready, partial, partial, partial},
		states: []codexComposerState{
			codexComposerEmpty,
			codexComposerEmpty,
			codexComposerHasDraft,
			codexComposerHasDraft,
			codexComposerHasDraft,
		},
	}
	cfg := testCodexSubmitConfig()
	coordinator := newCodexInputCoordinator()

	err := coordinator.submit(io, "agent:@retry", body, cfg)
	if err == nil || !strings.Contains(err.Error(), "preserved") {
		t.Fatalf("first error = %v, want preserved unacknowledged draft", err)
	}
	if io.clears != 0 || io.enters != 0 || len(io.pastes) != 1 {
		t.Fatalf("clears=%d pastes=%#v enters=%d, want one paste and no mutation", io.clears, io.pastes, io.enters)
	}

	io.captures = []string{partial}
	io.states = []codexComposerState{codexComposerHasDraft}
	io.index = 0

	err = coordinator.submit(io, "agent:@retry", body, cfg)
	if err == nil || !strings.Contains(err.Error(), "occupied") {
		t.Fatalf("retry error = %v, want occupied composer conflict", err)
	}
	if len(io.pastes) != 1 || io.clears != 0 || io.enters != 0 {
		t.Fatalf("retry mutated partial draft: clears=%d pastes=%#v enters=%d", io.clears, io.pastes, io.enters)
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

func TestCodexSubmissionAdvancedRequiresPostDraftTransition(t *testing.T) {
	body := "follow-up body with complete marker ZEN_TRANSITION_86420"
	preExistingQueue := "• Messages to be submitted after next tool call\n  ↳ older queued task\n"
	draft := "• Working (4m 10s • esc to interrupt)\n" + preExistingQueue + "\n› " + body + "\n"

	tests := []struct {
		name     string
		current  string
		advanced bool
	}{
		{
			name:     "pre-existing busy marker redraw",
			current:  "• Working (4m 11s • esc to interrupt)\n" + preExistingQueue + "\n› " + body + "\n",
			advanced: false,
		},
		{
			name: "changed pre-existing queue without marker increase",
			current: "• Working (4m 11s • esc to interrupt)\n" +
				"• Messages to be submitted after next tool call\n  ↳ older queued task redrawn\n\n› " + body + "\n",
			advanced: false,
		},
		{
			name:     "increased native queue marker",
			current:  draft + "\n• Messages to be submitted after next tool call\n  ↳ newly queued task\n",
			advanced: true,
		},
		{
			name:     "working marker after complete body",
			current:  "› " + body + "\n\n• Working (1s • esc to interrupt)\n",
			advanced: true,
		},
		{
			name:     "empty composer after complete body",
			current:  "› " + body + "\n\n› Summarize recent commits\n",
			advanced: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexSubmissionAdvanced(draft, tc.current, body); got != tc.advanced {
				t.Fatalf("codexSubmissionAdvanced() = %v, want %v", got, tc.advanced)
			}
		})
	}

	t.Run("stale complete body followed by old working marker", func(t *testing.T) {
		withOldHistory := "› " + body + "\n\n• Working (old turn)\n"
		draftWithNewComposer := withOldHistory + "\n› " + body + "\n"
		currentWithoutNewDraft := "› " + body + "\n\n• Working (old turn redrawn)\n"
		if codexSubmissionAdvanced(draftWithNewComposer, currentWithoutNewDraft, body) {
			t.Fatal("a surviving stale body must not stand in for the post-draft transition")
		}
	})

	t.Run("collapsed draft does not reuse pre-existing busy marker", func(t *testing.T) {
		collapsedDraft := "• Working (4m 10s • esc to interrupt)\n› [Pasted Content 2048 chars]\n"
		busyRedraw := "• Working (4m 11s • esc to interrupt)\n"
		if codexSubmissionAdvanced(collapsedDraft, busyRedraw, body) {
			t.Fatal("a pre-existing busy marker must not confirm a collapsed draft")
		}
	})
}

func TestSubmitCodexInputAcceptsComposerAcrossOptionalMCPThirtySecondBoundary(t *testing.T) {
	body := "execute unique marker ZEN_INITIAL_12345"
	starting := codexReadyPane("• Starting MCP servers (0/3): context7, playwright")
	draft := starting + "\n› " + body + "\n\n  gpt-5.6 medium · /tmp\n"
	submitted := starting + "\n› " + body + "\n\n• Working (1s • esc to interrupt)\n\n› Find and fix a bug in @filename\n"
	styledDraft := strings.ReplaceAll(
		draft,
		"  gpt-5.6 medium · /tmp",
		"  \x1b[38;2;246;226;183mgpt-5.6 medium\x1b[2m\x1b[39m · \x1b[0m\x1b[38;2;171;223;167m/tmp\x1b[0m",
	)
	io := &fakeCodexInputIO{
		captures:       []string{starting, starting, draft, draft, submitted},
		styledCaptures: []string{starting, starting, styledDraft, styledDraft, submitted},
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
	cfg.recoveryTimeout = time.Second

	err := submitCodexInput(io, "agent:@total-timeout", "must remain unsent", cfg)
	if err == nil || !strings.Contains(err.Error(), "composer") {
		t.Fatalf("error = %v, want bounded composer failure", err)
	}
	if elapsed := io.now().Sub(time.Time{}); elapsed > cfg.totalTimeout-cfg.recoveryTimeout {
		t.Fatalf("transaction elapsed = %s, exceeded reserved pre-Enter total deadline %s", elapsed, cfg.totalTimeout-cfg.recoveryTimeout)
	}
	if len(io.pastes) != 0 || io.enters != 0 || io.clears != 0 {
		t.Fatalf("pastes=%d enters=%d clears=%d, want no input actions", len(io.pastes), io.enters, io.clears)
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
	styledDraft := strings.ReplaceAll(
		draft,
		"  gpt-5.6 medium · /tmp",
		"  \x1b[38;2;246;226;183mgpt-5.6 medium\x1b[2m\x1b[39m · \x1b[0m\x1b[38;2;171;223;167m/tmp\x1b[0m",
	)
	io := &fakeCodexInputIO{
		captures:       []string{ready, ready, draft, draft, submitted},
		styledCaptures: []string{ready, ready, styledDraft, styledDraft, submitted},
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

func TestSubmitCodexInputRejectsCollapsedLongPasteWithoutExactIdentity(t *testing.T) {
	body := strings.Repeat("long delegated task line\n", 100) + "unique final marker"
	ready := codexReadyPane("")
	draft := ready + "\n› [Pasted Content 2519 chars]\n"
	io := &fakeCodexInputIO{captures: []string{ready, ready, draft}}

	err := submitCodexInput(io, "agent:@1", body, testCodexSubmitConfig())
	if err == nil || !strings.Contains(err.Error(), "durable Codex envelope storage is not configured") {
		t.Fatalf("error = %v, want fail-closed envelope requirement", err)
	}
	if len(io.pastes) != 0 || io.enters != 0 {
		t.Fatalf("pastes=%d enters=%d, collapsed direct input must not be attempted", len(io.pastes), io.enters)
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

func TestSubmitCodexInputDoesNotPressEnterWhenDraftCannotBeObserved(t *testing.T) {
	body := "prompt that never appears"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{captures: []string{ready, ready, ready, ready}}
	cfg := testCodexSubmitConfig()

	err := submitCodexInput(io, "agent:@1", body, cfg)
	if err == nil || !strings.Contains(err.Error(), "Enter was not sent") {
		t.Fatalf("error = %v, want explicit unobserved-draft failure", err)
	}
	if len(io.pastes) != 1 || io.enters != 0 {
		t.Fatalf("pastes=%d enters=%d, want one paste and no Enter", len(io.pastes), io.enters)
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
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{width: 120, suppressPersistence: true}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-restart",
			width:      current.width,
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
	err = first.submit(io, "agent:@restart", body, cfg)
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
	err = second.submit(io, "agent:@restart", body, cfg)
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
	if err := third.submit(io, "agent:@restart", body, cfg); err != nil {
		t.Fatalf("persisted user-message reconciliation: %v", err)
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
		width:               240,
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
			width:      current.width,
			rollout:    rolloutA,
		}
		if len(current.pastes) > 0 {
			capture.composer = codexComposerHasDraft
			if current.enters == 0 {
				capture.content = ready + "\n› " + current.pastes[0] +
					"\n\n  gpt-5.6 medium · /tmp\n"
				capture.styled = ready + "\n› " + current.pastes[0] +
					"\n\n  \x1b[38;2;246;226;183mgpt-5.6 medium\x1b[2m\x1b[39m · \x1b[0m\x1b[38;2;171;223;167m/tmp\x1b[0m\n"
			} else {
				capture.content = ready + "\n› " + current.pastes[0] +
					"\n\n• Working (generic progress only)\n"
				capture.styled = ready + "\n› " + current.pastes[0] +
					"\n\n\x1b[2m• Working (generic progress only)\x1b[0m\n"
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
	if len(io.pastes) != 1 || io.enters != 1 || io.clears != 0 {
		t.Fatalf("restart replayed actions pastes=%#v enters=%d clears=%d", io.pastes, io.enters, io.clears)
	}

	io.persistedByRollout[rolloutA.Path] = []string{body}
	third, err := newPersistentCodexInputCoordinator(stateDir)
	if err != nil {
		t.Fatalf("third coordinator: %v", err)
	}
	if err := third.submit(io, "agent:@target-rollout", body, cfg); err != nil {
		t.Fatalf("target-rollout reconciliation: %v", err)
	}
	if len(io.pastes) != 1 || io.enters != 1 || io.clears != 0 {
		t.Fatalf("target reconciliation replayed actions pastes=%#v enters=%d clears=%d", io.pastes, io.enters, io.clears)
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
	if io.enters != 0 || io.clears != 0 || len(io.pastes) != 0 {
		t.Fatalf("ambiguous owner mutated provider: pastes=%#v enters=%d clears=%d", io.pastes, io.enters, io.clears)
	}
}

func TestCodexCoordinatorOldGenerationDoesNotBlockReusedSessionID(t *testing.T) {
	oldBody := "old generation ambiguous ZEN_OLD_13579"
	newBody := "new generation independent ZEN_NEW_24680"
	ready := codexReadyPane("")
	io := &fakeCodexInputIO{width: 120, suppressPersistence: true}
	io.captureFn = func(current *fakeCodexInputIO) codexPaneCapture {
		capture := codexPaneCapture{
			content:    ready,
			alive:      true,
			composer:   codexComposerEmpty,
			generation: "generation-old",
			width:      current.width,
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
			width:      current.width,
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
		!strings.Contains(err.Error(), "ownership was not acknowledged") || io.enters != 0 || io.clears != 0 {
		t.Fatalf("error=%v enters=%d", err, io.enters)
	}
}
