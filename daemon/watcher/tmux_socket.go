package watcher

import (
	"context"
	"os/exec"
)

// tmux server layout:
//
// Zen-owned Brain and delegated Sessions live on the ONE caller-visible tmux
// server selected at daemon startup: the exact inherited socket when the
// daemon starts inside tmux, or the user's default server otherwise. Provider
// panes lose that host capability after deriving their target identity: TMUX
// is unset and TMUX_TMPDIR points at private scratch, so their later unscoped
// tmux commands cannot reach the shared host server.

// tmuxSocketArgs returns the -S flag pair for a non-default tmux server; an
// empty socketPath means the user's default server.
func tmuxSocketArgs(socketPath string) []string {
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
