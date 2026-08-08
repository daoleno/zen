package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestDaemonTmuxPathsNamespacesByDaemonIdentity covers the daemon tmux
// isolation path construction: Zen-owned sockets and scratch live under the
// daemon identity namespace inside the user home, and the identity is
// truncated to a short digest to stay well under sockaddr_un.
func TestDaemonTmuxPathsNamespacesByDaemonIdentity(t *testing.T) {
	home := "/home/user"
	daemonID := strings.Repeat("ab", 32) // 64 hex chars, as minted by the auth manager
	socket, scratch, err := daemonTmuxPaths(home, daemonID)
	if err != nil {
		t.Fatal(err)
	}
	wantSocket := filepath.Join(home, ".zen", "run", "tmux", "zen-"+daemonID[:24]+".sock")
	wantScratch := filepath.Join(home, ".zen", "run", "tmux-scratch", daemonID[:24])
	if socket != wantSocket {
		t.Fatalf("socket = %q, want %q", socket, wantSocket)
	}
	if scratch != wantScratch {
		t.Fatalf("scratch = %q, want %q", scratch, wantScratch)
	}
	if len(socket) > 100 {
		t.Fatalf("socket path %q risks exceeding sockaddr_un", socket)
	}

	// A short identity is used verbatim; both paths stay under the same home.
	shortSocket, shortScratch, err := daemonTmuxPaths(home, "short-id")
	if err != nil {
		t.Fatal(err)
	}
	if shortSocket != filepath.Join(home, ".zen", "run", "tmux", "zen-short-id.sock") {
		t.Fatalf("short socket = %q", shortSocket)
	}
	if shortScratch != filepath.Join(home, ".zen", "run", "tmux-scratch", "short-id") {
		t.Fatalf("short scratch = %q", shortScratch)
	}
}

// TestDaemonTmuxPathsFailClosedOnEmptyIdentity covers the fail-closed guard:
// an empty or whitespace daemon identity must never construct a shared or
// unnamespaced socket, because that would mix Zen-owned Sessions with another
// daemon's server.
func TestDaemonTmuxPathsFailClosedOnEmptyIdentity(t *testing.T) {
	for _, id := range []string{"", "   "} {
		if socket, scratch, err := daemonTmuxPaths("/home/user", id); err == nil {
			t.Fatalf("empty identity %q produced socket %q scratch %q", id, socket, scratch)
		}
	}
}

// TestTmuxClientSocketResolvesCallersOwnServer covers the pane identity hint
// socket resolution: the first TMUX field is the caller's own server socket,
// including the daemon-namespaced server for delegated panes; outside tmux
// the hint stays on the user default server.
func TestTmuxClientSocketResolvesCallersOwnServer(t *testing.T) {
	t.Setenv("TMUX", "")
	if got := tmuxClientSocket(); got != "" {
		t.Fatalf("outside tmux socket = %q, want empty", got)
	}
	t.Setenv("TMUX", "/tmp/tmux-1000/default,2519350,0")
	if got := tmuxClientSocket(); got != "/tmp/tmux-1000/default" {
		t.Fatalf("user default client socket = %q", got)
	}
	t.Setenv("TMUX", "/home/user/.zen/run/tmux/zen-abc.sock,12345,0")
	if got := tmuxClientSocket(); got != "/home/user/.zen/run/tmux/zen-abc.sock" {
		t.Fatalf("daemon client socket = %q", got)
	}
	t.Setenv("TMUX", "comma-less-value")
	if got := tmuxClientSocket(); got != "comma-less-value" {
		t.Fatalf("comma-less TMUX socket = %q", got)
	}
}
