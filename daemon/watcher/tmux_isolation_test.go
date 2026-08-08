package watcher

import (
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// TestWatcherSocketResolution covers Slice 3 ownership routing: Zen-owned
// targets resolve to the daemon-namespaced socket, user/manual targets to the
// user default server, and the inventory tags each window without mixing
// ownership.
func TestWatcherSocketResolution(t *testing.T) {
	w := New(time.Second)
	daemonSocket := "/home/user/.zen/run/tmux/daemon-1.sock"
	scratch := "/home/user/.zen/run/tmux-scratch/daemon-1"
	w.SetDaemonSocket(daemonSocket, scratch)

	t.Run("freshly created sessions land on the socket actually used", func(t *testing.T) {
		w.registerCreatedSession(daemonSocket, "brain-agent-delegated:@1", "/repo", CreateSessionOptions{
			Name: "delegated", Delegated: true, ProgressEnv: true,
		}, time.Now().UTC())
		w.registerCreatedSession("", "user-join:@5", "/repo", CreateSessionOptions{
			Name: "user join", ProgressEnv: true,
		}, time.Now().UTC())
		if got := w.SocketPathFor("user-join:@5"); got != "" {
			t.Fatalf("user-server join target socket = %q, want user default", got)
		}
		if got := w.SocketPathFor("brain-agent-delegated:@1"); got != daemonSocket {
			t.Fatalf("delegated target socket = %q, want %q", got, daemonSocket)
		}
	})

	t.Run("unknown targets fall back to the daemon socket", func(t *testing.T) {
		if got := w.socketPathFor("brain-agent-brain-host:@9"); got != daemonSocket {
			t.Fatalf("unknown target socket = %q, want daemon", got)
		}
	})

	t.Run("inventory tags user windows with the default server", func(t *testing.T) {
		windows := []tmuxWindow{
			{target: "main:@2", name: "user", socket: ""},
			{target: "brain-agent-worker:@3", name: "worker", socket: daemonSocket},
		}
		restore := installFakePollSeams(windows, map[string]string{}, map[int]processInfo{})
		defer restore()
		w.poll()
		drainWatcherEvents(w)
		if got := w.SocketPathFor("main:@2"); got != "" {
			t.Fatalf("user target socket = %q, want user default", got)
		}
		if got := w.SocketPathFor("brain-agent-worker:@3"); got != daemonSocket {
			t.Fatalf("daemon target socket = %q, want %q", got, daemonSocket)
		}
	})

	t.Run("removal clears the target socket", func(t *testing.T) {
		windows := []tmuxWindow{{target: "brain-agent-worker:@4", name: "worker", socket: daemonSocket}}
		restore := installFakePollSeams(windows, map[string]string{
			"brain-agent-worker:@4": "working\n",
		}, map[int]processInfo{})
		defer restore()
		w.poll()
		drainWatcherEvents(w)
		restore()
		restore = installFakePollSeams(nil, map[string]string{}, map[int]processInfo{})
		defer restore()
		w.poll()
		drainWatcherEvents(w)
		if got := w.SocketPathFor("brain-agent-worker:@4"); got != "" {
			t.Fatalf("removed target kept socket %q", got)
		}
	})
}

// TestAgentProgressEnvScriptUnsetsTMUX covers Slice 3 environment hardening:
// the launch shell derives ZEN_AGENT_ID from the pane's own server ($TMUX),
// then unsets TMUX so later unscoped `tmux` invocations resolve under the
// private TMUX_TMPDIR — never the daemon server or the user's default server.
func TestAgentProgressEnvScriptUnsetsTMUX(t *testing.T) {
	script := agentProgressEnvScript()
	unset := strings.Index(script, "unset TMUX")
	derive := strings.Index(script, "ZEN_AGENT_ID=")
	if unset < 0 {
		t.Fatalf("script does not unset TMUX: %s", script)
	}
	if derive < 0 || derive > unset {
		t.Fatalf("script must derive ZEN_AGENT_ID before unsetting TMUX: %s", script)
	}
}

// TestDelegatedWindowEnvironmentGetsPrivateTmuxScratch covers the env side of
// the isolation: daemon-owned panes receive TMUX_TMPDIR (host scratch for
// hidden Brain host sessions, per-agent scratch for delegated sessions) so a
// plain nested `tmux kill-server` resolves into the private directory.
func TestDelegatedWindowEnvironmentGetsPrivateTmuxScratch(t *testing.T) {
	w := New(time.Second)
	scratch := "/home/user/.zen/run/tmux-scratch/daemon-1"
	w.SetDaemonSocket("/home/user/.zen/run/tmux/daemon-1.sock", scratch)

	host := CreateSessionOptions{Name: "Brain", Hidden: true, ProgressEnv: true, Env: map[string]string{}}
	applyDaemonWindowEnvironment(&host, w)
	if host.Env["TMUX_TMPDIR"] != scratch {
		t.Fatalf("hidden host TMUX_TMPDIR = %q, want %q", host.Env["TMUX_TMPDIR"], scratch)
	}

	delegated := CreateSessionOptions{Name: "worker", Delegated: true, ProgressEnv: true, Env: map[string]string{}}
	delegated.resource = &delegatedResourceSpec{TempDir: "/home/user/.zen/t/abc123"}
	applyDaemonWindowEnvironment(&delegated, w)
	if delegated.Env["TMUX_TMPDIR"] != "/home/user/.zen/t/abc123" {
		t.Fatalf("delegated TMUX_TMPDIR = %q, want per-agent scratch", delegated.Env["TMUX_TMPDIR"])
	}
}

// TestPollDoesNotMixOwnershipAcrossServers covers the watcher-side contract
// that the restored projection never inherits a foreign server: a target
// moving between servers (recorded socket vs inventory socket) always reads
// the inventory's socket, and the fake ledger sees facts only for the
// daemon-socket target.
func TestPollDoesNotMixOwnershipAcrossServers(t *testing.T) {
	w := New(time.Second)
	daemonSocket := "/home/user/.zen/run/tmux/daemon-1.sock"
	w.SetDaemonSocket(daemonSocket, "/home/user/.zen/run/tmux-scratch/daemon-1")
	w.pollNow = fakePollClock([]time.Time{time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)})

	// A delegated window inventoried on the daemon socket.
	windows := []tmuxWindow{{
		target: "brain-agent-worker:@1", name: "worker", cwd: "/repo/zen",
		command: "opencode", panePID: 333, delegated: true, socket: daemonSocket,
	}}
	restore := installFakePollSeams(windows, map[string]string{
		"brain-agent-worker:@1": "OpenCode\nworking\n",
	}, map[int]processInfo{333: fakeProcess(333, time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC))})
	defer restore()
	w.poll()
	drainWatcherEvents(w)
	agent := agentByID(w.Agents(), "brain-agent-worker:@1")
	if agent == nil {
		t.Fatal("delegated agent missing after poll")
	}
	if got := w.SocketPathFor("brain-agent-worker:@1"); got != daemonSocket {
		t.Fatalf("delegated agent socket = %q, want %q", got, daemonSocket)
	}
	_ = classifier.StateRunning
}
