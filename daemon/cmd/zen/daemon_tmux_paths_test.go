package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDaemonTmuxRuntimeSelectsCallerVisibleServer(t *testing.T) {
	home := "/home/user"
	daemonID := strings.Repeat("ab", 32)

	t.Run("inside tmux reuses exact inherited socket", func(t *testing.T) {
		inherited := "/run/user/1000/tmux,custom/server with space.sock "
		socket, scratch, err := daemonTmuxRuntime(
			home,
			daemonID,
			inherited+",2519350,7",
		)
		if err != nil {
			t.Fatal(err)
		}
		if socket != inherited {
			t.Fatalf("socket = %q, want exact inherited %q", socket, inherited)
		}
		wantScratch := filepath.Join(home, ".zen", "run", "tmux-scratch", daemonID[:24])
		if scratch != wantScratch {
			t.Fatalf("scratch = %q, want %q", scratch, wantScratch)
		}
	})

	t.Run("outside tmux uses ordinary default server", func(t *testing.T) {
		socket, scratch, err := daemonTmuxRuntime(home, "short-id", "")
		if err != nil {
			t.Fatal(err)
		}
		if socket != "" {
			t.Fatalf("socket = %q, want empty default-server route", socket)
		}
		if scratch != filepath.Join(home, ".zen", "run", "tmux-scratch", "short-id") {
			t.Fatalf("scratch = %q", scratch)
		}
	})
}

func TestDaemonTmuxRuntimeFailClosedOnEmptyIdentity(t *testing.T) {
	for _, id := range []string{"", "   "} {
		if socket, scratch, err := daemonTmuxRuntime("/home/user", id, ""); err == nil {
			t.Fatalf("empty identity %q produced socket %q scratch %q", id, socket, scratch)
		}
	}
}

func TestTmuxClientSocketResolvesCallersOwnServer(t *testing.T) {
	t.Setenv("TMUX", "")
	if got := tmuxClientSocket(); got != "" {
		t.Fatalf("outside tmux socket = %q, want empty", got)
	}
	t.Setenv("TMUX", "/tmp/tmux-1000/default,2519350,0")
	if got := tmuxClientSocket(); got != "/tmp/tmux-1000/default" {
		t.Fatalf("default client socket = %q", got)
	}
	t.Setenv("TMUX", "/tmp/with,comma/server.sock,12345,0")
	if got := tmuxClientSocket(); got != "/tmp/with,comma/server.sock" {
		t.Fatalf("comma-bearing client socket = %q", got)
	}
	t.Setenv("TMUX", "comma-less-value")
	if got := tmuxClientSocket(); got != "comma-less-value" {
		t.Fatalf("malformed TMUX fallback socket = %q", got)
	}
}

func TestCurrentWorkerIDQueryNeverAutostartsServer(t *testing.T) {
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv")
	shim := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + argvPath + "\"\nexit 1\n"
	if err := os.WriteFile(shim, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ZEN_WORKER_ID", "")
	t.Setenv("TMUX_PANE", "%7")
	t.Setenv("TMUX", "/tmp/tmux-1000/default,2519350,0")
	if got := currentWorkerID(); got != "" {
		t.Fatalf("currentWorkerID() = %q, want empty on failing query", got)
	}
	argv, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "-S\n/tmp/tmux-1000/default\n-N\ndisplay-message\n-p\n-t\n%7\n#{session_name}:#{window_id}\n"
	if string(argv) != want {
		t.Fatalf("worker query argv = %q, want %q", argv, want)
	}
}
