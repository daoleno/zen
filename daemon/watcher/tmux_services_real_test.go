package watcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestRealTmuxServicePanesFromBothServers covers service inventory on both
// tmux servers: panes started under Zen-owned sessions on the daemon socket
// and panes on the user's default server are both enumerated, so TCP services
// under delegated Agents are no longer silently omitted.
func TestRealTmuxServicePanesFromBothServers(t *testing.T) {
	w, daemonSocket, _ := realTmuxHarness(t)
	daemonTarget := createHarnessPane(t, daemonSocket, "srv-daemon", "/bin/sh")
	userTarget := createHarnessPane(t, "", "srv-user", "/bin/sh")
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", daemonSocket, "kill-server").Run()
	})

	panes, err := w.listServicePanes()
	if err != nil {
		t.Fatalf("listServicePanes: %v", err)
	}
	if !servicePanesContainTarget(panes, daemonTarget) {
		t.Fatalf("daemon-socket pane missing from service inventory: %#v", panes)
	}
	if !servicePanesContainTarget(panes, userTarget) {
		t.Fatalf("user-default pane missing from service inventory: %#v", panes)
	}
}

// TestRealTmuxServicePanesShadowSameNameDaemonFirst covers deterministic
// shadowing for duplicate target identities across servers: a Zen-owned
// daemon-socket pane shadows the same-named user pane, exactly like the
// window inventory, so services are never attributed to the wrong server's
// pane.
func TestRealTmuxServicePanesShadowSameNameDaemonFirst(t *testing.T) {
	w, daemonSocket, _ := realTmuxHarness(t)
	daemonTarget := createHarnessPane(t, daemonSocket, "ambig", "/bin/sh")
	userTarget := createHarnessPane(t, "", "ambig", "/bin/sh")
	if daemonTarget != userTarget {
		t.Fatalf("fixture targets differ: %q vs %q", daemonTarget, userTarget)
	}
	daemonPID := harnessPanePID(t, daemonSocket, daemonTarget)
	userPID := harnessPanePID(t, "", userTarget)
	if daemonPID == userPID {
		t.Fatal("fixture pane PIDs must differ across servers")
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", daemonSocket, "kill-server").Run()
		_ = tmuxHarnessCommand("", "kill-server").Run()
	})

	panes, err := w.listServicePanes()
	if err != nil {
		t.Fatalf("listServicePanes: %v", err)
	}
	matches := 0
	for _, pane := range panes {
		if pane.target != daemonTarget {
			continue
		}
		matches++
		if pane.panePID != daemonPID {
			t.Fatalf("same-name target resolved to user pane pid %d, want daemon pid %d", pane.panePID, daemonPID)
		}
	}
	if matches != 1 {
		t.Fatalf("same-name target entries = %d, want exactly one (daemon shadows user)", matches)
	}
}

// TestRealTmuxServicePanesSurvivesMissingOrWrongServer covers the tolerance
// contract: a wrong daemon socket path and a missing user default server are
// both treated as empty, and neither erases the other server's inventory.
func TestRealTmuxServicePanesSurvivesMissingOrWrongServer(t *testing.T) {
	w, daemonSocket, _ := realTmuxHarness(t)
	realTmux := os.Getenv("ZEN_TEST_REAL_TMUX")
	defaultSocket := os.Getenv("ZEN_TEST_DEFAULT_TMUX_SOCKET")
	daemonTarget := createHarnessPane(t, daemonSocket, "srv-daemon", "/bin/sh")
	userTarget := createHarnessPane(t, "", "srv-user", "/bin/sh")
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-S", daemonSocket, "kill-server").Run()
		_ = exec.Command("tmux", "-S", defaultSocket, "kill-server").Run()
	})

	// A wrong daemon socket (no server there) is empty; the user inventory
	// survives and no hard error is reported.
	w.mu.Lock()
	w.daemonSocketPath = filepath.Join(filepath.Dir(daemonSocket), "missing.sock")
	w.mu.Unlock()
	panes, err := w.listServicePanes()
	if err != nil {
		t.Fatalf("wrong daemon socket: %v", err)
	}
	if !servicePanesContainTarget(panes, userTarget) {
		t.Fatalf("wrong daemon socket erased user inventory: %#v", panes)
	}

	// A missing user default server is empty; the daemon inventory survives.
	w.mu.Lock()
	w.daemonSocketPath = daemonSocket
	w.mu.Unlock()
	stopHarnessTmuxServer(t, realTmux, defaultSocket)
	panes, err = w.listServicePanes()
	if err != nil {
		t.Fatalf("missing user server: %v", err)
	}
	if !servicePanesContainTarget(panes, daemonTarget) {
		t.Fatalf("missing user server erased daemon inventory: %#v", panes)
	}
	if servicePanesContainTarget(panes, userTarget) {
		t.Fatalf("missing user server still contributed panes: %#v", panes)
	}
}

func servicePanesContainTarget(panes []servicePane, target string) bool {
	for _, pane := range panes {
		if pane.target == target {
			return true
		}
	}
	return false
}

func harnessPanePID(t *testing.T, socket, target string) int {
	t.Helper()
	out, err := tmuxHarnessCommand(socket, "display-message", "-p", "-t", target, "#{pane_pid}").Output()
	if err != nil {
		t.Fatalf("resolve pane pid for %s on %q: %v", target, socket, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 0 {
		t.Fatalf("pane pid for %s on %q = %q", target, socket, strings.TrimSpace(string(out)))
	}
	return pid
}
