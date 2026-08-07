package watcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKillSessionMissingIsIdempotentSuccess(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	tmuxPath := filepath.Join(dir, "tmux")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_TMUX_LOG"
echo "can't find window: missing:@1" >&2
exit 1
`
	if err := os.WriteFile(tmuxPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ZEN_TEST_TMUX_LOG", logPath)
	w := New(0)
	if err := w.KillSession("missing:@1"); err != nil {
		t.Fatalf("missing target must be idempotent success: %v", err)
	}
}

func TestKillSessionResourceReleaseFailureAfterSuccessfulKill(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	tmuxPath := filepath.Join(dir, "tmux")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_TMUX_LOG"
exit 0
`
	if err := os.WriteFile(tmuxPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ZEN_TEST_TMUX_LOG", logPath)
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	manager := &fakeDelegatedResourceManager{
		boundTarget: "main:@42",
		boundUnit:   unit,
		releaseErr:  errors.New("injected cgroup release failure"),
	}
	w := New(0)
	w.resources = manager
	err := w.KillSession("main:@42")
	if err == nil || !errors.Is(err, ErrDelegatedResourceRelease) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "injected cgroup release failure") {
		t.Fatalf("err=%v", err)
	}
	if len(manager.released) != 1 {
		t.Fatalf("released=%#v", manager.released)
	}
}

func TestKillSessionMissingStillReleasesBoundUnit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	tmuxPath := filepath.Join(dir, "tmux")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_TMUX_LOG"
echo "can't find window" >&2
exit 1
`
	if err := os.WriteFile(tmuxPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ZEN_TEST_TMUX_LOG", logPath)
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	manager := &fakeDelegatedResourceManager{boundTarget: "main:@42", boundUnit: unit}
	w := New(0)
	w.resources = manager
	if err := w.KillSession("main:@42"); err != nil {
		t.Fatal(err)
	}
	if len(manager.released) != 1 {
		t.Fatalf("released=%#v", manager.released)
	}
}

func TestKillSessionRetryAfterResourceFailureConverges(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "tmux.log")
	tmuxPath := filepath.Join(dir, "tmux")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_TMUX_LOG"
if echo "$*" | grep -q 'kill-window'; then
  echo "can't find window" >&2
  exit 1
fi
exit 0
`
	if err := os.WriteFile(tmuxPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ZEN_TEST_TMUX_LOG", logPath)
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	manager := &fakeDelegatedResourceManager{
		boundTarget: "main:@42",
		boundUnit:   unit,
		releaseErr:  errors.New("first release fail"),
	}
	w := New(0)
	w.resources = manager
	if err := w.KillSession("main:@42"); !errors.Is(err, ErrDelegatedResourceRelease) {
		t.Fatalf("first=%v", err)
	}
	manager.releaseErr = nil
	if err := w.KillSession("main:@42"); err != nil {
		t.Fatalf("retry=%v", err)
	}
	if len(manager.released) != 2 {
		t.Fatalf("released=%#v", manager.released)
	}
}

func TestProbeSessionDistinguishesAbsentFromTransportError(t *testing.T) {
	dir := t.TempDir()
	tmuxPath := filepath.Join(dir, "tmux")
	writeTmux := func(body string) {
		t.Helper()
		if err := os.WriteFile(tmuxPath, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)

	writeTmux(`echo "failed to connect to server" >&2
exit 1`)
	w := New(0)
	presence, err := w.ProbeSession("main:@1")
	if err != nil || presence != SessionPresenceAbsent {
		t.Fatalf("no-server presence=%v err=%v", presence, err)
	}

	writeTmux(`echo "can't find session: main:@1" >&2
exit 1`)
	presence, err = w.ProbeSession("main:@1")
	if err != nil || presence != SessionPresenceAbsent {
		t.Fatalf("missing presence=%v err=%v", presence, err)
	}

	writeTmux(`echo "permission denied reading tmux socket" >&2
exit 1`)
	presence, err = w.ProbeSession("main:@1")
	if presence != SessionPresenceUnknown || err == nil {
		t.Fatalf("transport presence=%v err=%v", presence, err)
	}
}
