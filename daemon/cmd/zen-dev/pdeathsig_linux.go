//go:build linux

package main

import "syscall"

// parentDeathSignal ties the daemon child's lifetime to the watcher through the
// kernel's PR_SET_PDEATHSIG. If the watcher dies abruptly (crash, SIGKILL, or a
// process-scoped supervisor stop), the kernel sends the child SIGTERM so it
// exits and releases the single-owner state lock instead of surviving as an
// orphan. The child is still stopped explicitly and gracefully during normal
// rebuild/exit, and the external tmux server is never part of this binding.
func parentDeathSignal() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
