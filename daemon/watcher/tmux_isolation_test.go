package watcher

import (
	"strings"
	"testing"
	"time"
)

func TestWatcherUsesOneSelectedHostServerForKnownTargets(t *testing.T) {
	w := New(time.Second)
	socket := "/run/user/1000/custom tmux.sock "
	w.SetTmuxServer(socket, "/home/user/.zen/run/tmux-scratch/daemon-1")
	w.registerCreatedSession("zen-worker-worker:@3", "/repo", CreateSessionOptions{
		Name: "worker", Delegated: true, ProgressEnv: true,
	}, time.Now().UTC())

	if got := w.socketPathFor("zen-worker-worker:@3"); got != socket {
		t.Fatalf("internal selected socket = %q, want %q", got, socket)
	}
	if got := w.SocketPathFor("zen-worker-worker:@3"); got != socket {
		t.Fatalf("known target socket = %q, want %q", got, socket)
	}
	if got := w.SocketPathFor("ambient:@9"); got != "" {
		t.Fatalf("unknown target exposed custom socket %q", got)
	}
	if args := tmuxSocketArgs(socket); len(args) != 3 || args[0] != "-S" || args[1] != socket || args[2] != "-N" {
		t.Fatalf("tmux socket args = %#v, want [-S byte-exact-socket -N]", args)
	}
	if args := tmuxSocketArgs(""); args != nil {
		t.Fatalf("empty tmux socket args = %#v, want nil (default server)", args)
	}
}

func TestWorkerProgressEnvScriptDropsHostTmuxCapabilityAfterIdentity(t *testing.T) {
	script := workerProgressEnvScript()
	unset := strings.Index(script, "unset TMUX")
	derive := strings.Index(script, "ZEN_WORKER_ID=")
	if unset < 0 {
		t.Fatalf("script does not unset TMUX: %s", script)
	}
	if derive < 0 || derive > unset {
		t.Fatalf("script must derive ZEN_WORKER_ID before unsetting TMUX: %s", script)
	}
}

func TestProviderWindowEnvironmentGetsPrivateTmuxScratch(t *testing.T) {
	w := New(time.Second)
	scratch := "/home/user/.zen/run/tmux-scratch/daemon-1"
	w.SetTmuxServer("/run/user/1000/custom-tmux.sock", scratch)

	host := CreateSessionOptions{Name: "Brain", Hidden: true, ProgressEnv: true, Env: map[string]string{}}
	applyProviderTmuxIsolation(&host, w)
	if host.Env["TMUX_TMPDIR"] != scratch {
		t.Fatalf("hidden host TMUX_TMPDIR = %q, want %q", host.Env["TMUX_TMPDIR"], scratch)
	}

	delegated := CreateSessionOptions{Name: "worker", Delegated: true, ProgressEnv: true, Env: map[string]string{}}
	delegated.resource = &delegatedResourceSpec{TempDir: "/home/user/.zen/t/abc123"}
	applyProviderTmuxIsolation(&delegated, w)
	if delegated.Env["TMUX_TMPDIR"] != "/home/user/.zen/t/abc123" {
		t.Fatalf("delegated TMUX_TMPDIR = %q, want per-agent scratch", delegated.Env["TMUX_TMPDIR"])
	}
	if _, present := host.Env["TMUX"]; present {
		t.Fatal("host environment injected TMUX")
	}
}
