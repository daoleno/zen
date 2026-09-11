//go:build !linux

package main

import "syscall"

// Non-Linux platforms have no parent-death signal. The watcher's explicit stop
// path still terminates the child on rebuild and normal exit.
func parentDeathSignal() *syscall.SysProcAttr {
	return nil
}
