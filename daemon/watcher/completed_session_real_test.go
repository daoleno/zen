package watcher

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBDD_ZEN016_RealTmuxCompletedCleanupDistinguishesAbsenceAndUnowned(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	ambient := createHarnessPane(t, h.selected, "ambient-keep", "exec /bin/sh")

	owned, err := h.w.CreateSession("", CreateSessionOptions{
		Name: "reclaimed", Command: "exec /bin/sh", Detached: true, Delegated: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionName := baseSessionName(owned)
	h.w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		owned: {SessionID: owned, TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	if out, killErr := tmuxHarnessCommand(h.selected, "kill-window", "-t", owned).CombinedOutput(); killErr != nil {
		t.Fatalf("reclaim owned window: %v: %s", killErr, out)
	}

	for range 2 {
		if err := h.w.KillCompletedSession(owned, "completed"); err != nil {
			t.Fatalf("absent completed target: %v", err)
		}
	}
	if err := tmuxHarnessCommand(h.selected, "has-session", "-t", ambient).Run(); err != nil {
		t.Fatalf("ambient was removed during absent cleanup: %v", err)
	}

	reused := createHarnessPane(t, h.selected, sessionName, "exec /bin/sh")
	h.w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		reused: {SessionID: reused, TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	if err := h.w.KillCompletedSession(reused, "completed"); !errors.Is(err, ErrUnownedTmuxTarget) {
		t.Fatalf("reused identity err=%v, want ErrUnownedTmuxTarget", err)
	}
	if err := tmuxHarnessCommand(h.selected, "has-session", "-t", reused).Run(); err != nil {
		t.Fatalf("unowned reused target was killed: %v", err)
	}
}

func TestBDD_ZEN017_RealTmuxWrongSocketAndRebootOwnership(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	foreign := createHarnessPane(t, h.defaultSocket, "gone-worker", "exec /bin/sh")
	h.w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		foreign: {SessionID: foreign, TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	for range 2 {
		if err := h.w.KillCompletedSession(foreign, "completed"); err != nil {
			t.Fatalf("selected socket absence: %v", err)
		}
	}
	if err := tmuxHarnessCommand(h.defaultSocket, "has-session", "-t", foreign).Run(); err != nil {
		t.Fatalf("wrong-socket ambient was mutated: %v", err)
	}

	owned, err := h.w.CreateSession("", CreateSessionOptions{
		Name: "reboot-owned", Command: "exec /bin/sh", Detached: true, Delegated: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	recovered := New(10 * time.Millisecond)
	recovered.SetTmuxServer(h.selected, h.scratch)
	recovered.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		owned: {SessionID: owned, TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	if err := recovered.KillCompletedSession(owned, "completed"); err != nil {
		t.Fatalf("reboot owned leftover: %v", err)
	}
	if err := tmuxHarnessCommand(h.selected, "has-session", "-t", owned).Run(); err == nil {
		t.Fatal("owned leftover survived reboot cleanup")
	}

	recovered.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		owned: {SessionID: owned, TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	if err := recovered.KillCompletedSession(owned, "completed"); err != nil {
		t.Fatalf("reboot absent retry: %v", err)
	}
}

func TestCompletedCleanupRealTmuxUnreachableSocket(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	blocked := filepath.Join(h.root, "blocked.sock")
	if err := os.WriteFile(blocked, []byte("not a tmux socket"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o600) })
	h.w.SetTmuxServer(blocked, h.scratch)
	h.w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		"worker:@1": {SessionID: "worker:@1", TurnID: "done", Status: TurnDone, SignalProtocol: true},
	}})
	err := h.w.KillCompletedSession("worker:@1", "done")
	if err == nil || errors.Is(err, ErrUnownedTmuxTarget) {
		t.Fatalf("blocked socket err=%v", err)
	}
}

func TestCompletedCleanupRealTmuxPartialRetryAfterKill(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	ambient := createHarnessPane(t, h.selected, "partial-keep", "exec /bin/sh")
	owned, err := h.w.CreateSession("", CreateSessionOptions{
		Name: "partial", Command: "exec /bin/sh", Detached: true, Delegated: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	unit := delegatedResourceUnit("abc123", "0123456789abcdef0123456789abcdef")
	manager := &fakeDelegatedResourceManager{
		boundTarget: owned,
		boundUnit:   unit,
		releaseErr:  errors.New("first resource release fail"),
	}
	h.w.resources = manager
	h.w.SetTurnLedger(&fakeTurnLedger{turns: map[string]TurnSnapshot{
		owned: {SessionID: owned, TurnID: "completed", Status: TurnDone, SignalProtocol: true},
	}})
	if err := h.w.KillCompletedSession(owned, "completed"); !errors.Is(err, ErrDelegatedResourceRelease) {
		t.Fatalf("partial cleanup err=%v", err)
	}
	manager.releaseErr = nil
	if err := h.w.KillCompletedSession(owned, "completed"); err != nil {
		t.Fatalf("retry after resource failure: %v", err)
	}
	if len(manager.released) != 2 {
		t.Fatalf("released=%#v", manager.released)
	}
	if err := tmuxHarnessCommand(h.selected, "has-session", "-t", ambient).Run(); err != nil {
		t.Fatalf("ambient was removed during partial cleanup: %v", err)
	}
}
