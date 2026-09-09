package watcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// No tmux command is needed: stale ownership must fail before any transport IO.
// ZEN007: Given old cleanup waiting for the input lock, when a new turn is
// admitted first, then cleanup rejects the old identity before transport IO.
func TestBDD_ZEN007_CompletedCleanupSerializesWithNewInput(t *testing.T) {
	w := New(time.Second)
	ledger := &fakeTurnLedger{turns: map[string]TurnSnapshot{
		"worker": {SessionID: "worker", TurnID: "old", Status: TurnDone, SignalProtocol: true},
	}}
	w.SetTurnLedger(ledger)
	owner := w.sessionInputOwner()
	session := owner.session("worker")
	session.mu.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(started); done <- w.KillCompletedSession("worker", "old") }()
	<-started
	// Represents the new admission committed inside the shared input lock.
	ledger.turns["worker"] = TurnSnapshot{SessionID: "worker", TurnID: "new", Status: TurnRunning, SignalProtocol: true}
	session.mu.Unlock()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("old cleanup accepted a newer turn")
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup did not release input serialization")
	}
}

func TestBDD_ZEN008_AlreadyReclaimedSessionCleanupIsIdempotent(t *testing.T) {
	// Given exact completed ownership in the ledger but a reclaimed tmux window.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte("#!/bin/sh\necho \"can't find window: missing:@1\" >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	w := New(0)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"missing:@1": {SessionID: "missing:@1", TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	// When cleanup is reconciled repeatedly, proven absence is success, not an
	// ownership conflict. Other ownership checks remain covered separately.
	for range 2 {
		if err := w.KillCompletedSession("missing:@1", "completed"); err != nil {
			t.Fatalf("already reclaimed: %v", err)
		}
	}
}

func TestCompletedSessionCleanupRejectsNonterminalAndUncontractedTurns(t *testing.T) {
	for _, turn := range []TurnSnapshot{
		{SessionID: "worker", TurnID: "turn", Status: TurnRunning, SignalProtocol: true},
		{SessionID: "worker", TurnID: "turn", Status: TurnUnknown, SignalProtocol: true},
		{SessionID: "worker", TurnID: "turn", Status: TurnDone},
	} {
		w := New(time.Second)
		w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{"worker": turn}})
		if err := w.KillCompletedSession("worker", "turn"); err == nil {
			t.Fatalf("unsafe cleanup accepted: %+v", turn)
		}
	}
}

func TestBDD_ZEN014_QuietMissingSessionCleanupIsAbsentNotUnowned(t *testing.T) {
	// Given tmux 3.6a quiet show-options success for a session that list-panes
	// proves is gone, completed cleanup must be idempotent absence — not Unowned.
	dir := writeFakeTmux(t, `cmd=
for arg in "$@"; do
  case "$arg" in list-panes|show-options|kill-window) cmd=$arg ;; esac
done
if [ "$cmd" = "list-panes" ]; then
  echo "can't find session: gone:@1" >&2
  exit 1
fi
if [ "$cmd" = "show-options" ]; then
  exit 0
fi
if [ "$cmd" = "kill-window" ]; then
  echo "kill-window must not run for a proven-absent target" >&2
  exit 1
fi
echo "unexpected tmux invocation: $*" >&2
exit 1
`)
	t.Setenv("PATH", dir)
	w := New(0)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"gone:@1": {SessionID: "gone:@1", TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	for range 2 {
		if err := w.KillCompletedSession("gone:@1", "completed"); err != nil {
			t.Fatalf("quiet missing session: %v", err)
		}
	}
	present, owned, err := probeTmuxTargetOwnership("", "gone:@1")
	if err != nil || present || owned {
		t.Fatalf("quiet missing classified as present=%v owned=%v err=%v", present, owned, err)
	}
}

func TestBDD_ZEN015_UnownedPresentCompletedCleanupIsProtected(t *testing.T) {
	dir := writeFakeTmux(t, `log=${ZEN_TEST_TMUX_LOG:?}
printf '%s\n' "$*" >>"$log"
target=
prev=
for arg in "$@"; do
  if [ "$prev" = "-t" ]; then target=$arg; fi
  prev=$arg
done
cmd=
for arg in "$@"; do
  case "$arg" in list-panes|show-options|kill-window) cmd=$arg ;; esac
done
if [ "$cmd" = "list-panes" ]; then
  echo "ambient:@1"
  exit 0
fi
if [ "$cmd" = "show-options" ]; then
  exit 0
fi
if [ "$cmd" = "kill-window" ]; then
  echo "killed $target" >>"$log"
  exit 0
fi
exit 0
`)
	logPath := filepath.Join(dir, "tmux.log")
	t.Setenv("PATH", dir)
	t.Setenv("ZEN_TEST_TMUX_LOG", logPath)
	w := New(0)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"ambient:@1": {SessionID: "ambient:@1", TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	for range 2 {
		err := w.KillCompletedSession("ambient:@1", "completed")
		if !errors.Is(err, ErrUnownedTmuxTarget) {
			t.Fatalf("unowned present err=%v", err)
		}
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "killed ") || strings.Contains(string(raw), "kill-window") {
		t.Fatalf("unowned target was mutated:\n%s", raw)
	}
}

func TestCompletedCleanupRejectsReusedOwnedGeneration(t *testing.T) {
	dir := writeFakeTmux(t, ownedPresentTmuxScript())
	t.Setenv("PATH", dir)
	w := New(0)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"worker:@1": {
			SessionID: "worker:@1", TurnID: "old", Status: TurnDone, SignalProtocol: true,
			ProcessIdentity: "old-process", PaneGeneration: "old-pane",
		},
	}})
	w.targetProcessResolver = func(string) (targetProcessIdentity, bool) {
		return targetProcessIdentity{Command: "cursor-agent", ProcessID: 99, ProcessStart: 99}, true
	}
	w.SetPollSources(PollSources{PaneGeneration: func(string) string { return "new-pane" }})
	if err := w.KillCompletedSession("worker:@1", "old"); err == nil || !strings.Contains(err.Error(), "generation changed") {
		t.Fatalf("reused identity err=%v", err)
	}
}

func TestCompletedCleanupUnreachableSocketIsNotAbsence(t *testing.T) {
	dir := writeFakeTmux(t, `echo "permission denied reading tmux socket" >&2
exit 1
`)
	t.Setenv("PATH", dir)
	w := New(0)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"worker:@1": {SessionID: "worker:@1", TurnID: "done", Status: TurnDone, SignalProtocol: true},
	}})
	err := w.KillCompletedSession("worker:@1", "done")
	if err == nil || errors.Is(err, ErrUnownedTmuxTarget) {
		t.Fatalf("unreachable classified as success or unowned: %v", err)
	}
	if presence, probeErr := w.ProbeSession("worker:@1"); presence != SessionPresenceUnknown || probeErr == nil {
		t.Fatalf("unreachable probe presence=%v err=%v", presence, probeErr)
	}
}

func TestCompletedCleanupMissingScratchAfterAbsenceSucceeds(t *testing.T) {
	dir := writeFakeTmux(t, `echo "can't find session: missing:@1" >&2
exit 1
`)
	t.Setenv("PATH", dir)
	manager := newTestPortableResourceManager(t, "abc123", delegatedResourceLimits{TasksMax: 1024})
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	tempDir, err := manager.createOwnedTempDir(unit)
	if err != nil {
		t.Fatal(err)
	}
	manager.Bind("missing:@1", unit)
	if err := os.RemoveAll(tempDir); err != nil {
		t.Fatal(err)
	}
	w := New(0)
	w.resources = manager
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"missing:@1": {SessionID: "missing:@1", TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	if err := w.KillCompletedSession("missing:@1", "completed"); err != nil {
		t.Fatalf("temp dir already gone: %v", err)
	}
}

func TestCompletedCleanupRaceDoesNotKillAppearingUnownedTarget(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	scriptPath := filepath.Join(dir, "tmux")
	statePath := filepath.Join(dir, "state")
	if err := os.WriteFile(statePath, []byte("absent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_TMUX_LOG"
state=$(cat "$ZEN_TEST_TMUX_STATE")
cmd=
for arg in "$@"; do
  case "$arg" in list-panes|show-options|kill-window) cmd=$arg ;; esac
done
if [ "$state" = "absent" ]; then
  echo "can't find session: race:@1" >&2
  exit 1
fi
if [ "$cmd" = "list-panes" ]; then
  echo "race:@1"
  exit 0
fi
if [ "$cmd" = "show-options" ]; then
  exit 0
fi
if [ "$cmd" = "kill-window" ]; then
  echo killed >> "$ZEN_TEST_TMUX_LOG"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ZEN_TEST_TMUX_LOG", logPath)
	t.Setenv("ZEN_TEST_TMUX_STATE", statePath)
	w := New(0)
	w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"race:@1": {SessionID: "race:@1", TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- w.KillCompletedSession("race:@1", "completed")
	}()
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		_ = os.WriteFile(statePath, []byte("unowned\n"), 0o600)
		errs <- w.KillCompletedSession("race:@1", "completed")
	}()
	wg.Wait()
	close(errs)
	sawUnowned := false
	for err := range errs {
		if err == nil {
			continue
		}
		if errors.Is(err, ErrUnownedTmuxTarget) {
			sawUnowned = true
			continue
		}
		t.Fatalf("unexpected race err=%v", err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "killed") {
		t.Fatalf("race killed unowned target:\n%s", raw)
	}
	if !sawUnowned {
		t.Fatal("appearing unowned target was not protected")
	}
}

func writeFakeTmux(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func ownedPresentTmuxScript() string {
	return `target=
prev=
for arg in "$@"; do
  if [ "$prev" = "-t" ]; then target=$arg; fi
  prev=$arg
done
cmd=
for arg in "$@"; do
  case "$arg" in list-panes|show-options|kill-window) cmd=$arg ;; esac
done
if [ "$cmd" = "list-panes" ]; then
  echo "$target"
  exit 0
fi
if [ "$cmd" = "show-options" ]; then
  echo 1
  exit 0
fi
exit 0
`
}
