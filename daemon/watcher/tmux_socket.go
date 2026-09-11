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

// tmuxSocketArgs returns the global flags binding an invocation to an
// externally owned server: -S selects the exact socket, -N forbids the
// client from starting a server when that socket is absent or dead, so a
// missing server fails instead of forking a fallback into our own cgroup.
// An empty socketPath means the user's default server (unchanged behavior).
func tmuxSocketArgs(socketPath string) []string {
	if socketPath == "" {
		return nil
	}
	return []string{"-S", socketPath, "-N"}
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
