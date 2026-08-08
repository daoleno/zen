package watcher

import (
	"context"
	"os/exec"
	"strings"
)

// tmux socket layout (Slice 3, daemon tmux isolation):
//
// Zen-owned Brain and delegated Sessions live on ONE daemon-namespaced tmux
// server, addressed by an explicit -S socket path under ~/.zen/run/tmux.
// User-visible/manual Terminal Sessions stay on the user's default tmux
// server. Every watcher tmux invocation resolves the target's server first
// (targetSockets), so ownership is never mixed; an unscoped nested
// `tmux kill-server` inside a delegated pane cannot reach the daemon server
// because the pane's environment unsets TMUX and points TMUX_TMPDIR at the
// agent's private scratch directory.

// tmuxSocketArgs returns the -S flag pair for a non-default tmux server; an
// empty socketPath means the user's default server.
func tmuxSocketArgs(socketPath string) []string {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil
	}
	return []string{"-S", socketPath}
}

// tmuxCommand builds a tmux invocation bound to the given server socket.
// Empty socketPath targets the user's default server.
func tmuxCommand(socketPath string, args ...string) *exec.Cmd {
	return exec.Command("tmux", append(tmuxSocketArgs(socketPath), args...)...)
}

// tmuxCommandContext is the context-bound variant.
func tmuxCommandContext(ctx context.Context, socketPath string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "tmux", append(tmuxSocketArgs(socketPath), args...)...)
}
