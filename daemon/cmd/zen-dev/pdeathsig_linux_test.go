//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestWatcherAbruptDeathStopsDaemonChild proves the kernel parent-death binding:
// a SIGKILLed watcher must not leave its daemon child running and holding the
// single-owner state lock. This is the failure mode a process-scoped systemd
// KillMode would otherwise expose.
func TestWatcherAbruptDeathStopsDaemonChild(t *testing.T) {
	if os.Getenv("ZEN_DEV_PDEATH_HELPER") == "1" {
		// Helper mode: this re-executed test binary acts as a watcher whose
		// only job is to hold one child process.
		runner := &devRunner{
			root:       os.Getenv("ZEN_DEV_PDEATH_ROOT"),
			binary:     "/bin/sh",
			daemonArgs: []string{"-c", "echo $$ > " + os.Getenv("ZEN_DEV_PDEATH_PIDFILE") + "; exec sleep 300"},
			stdout:     os.Stdout,
			stderr:     os.Stderr,
		}
		if err := runner.start(); err != nil {
			os.Exit(2)
		}
		select {}
	}

	root := t.TempDir()
	pidfile := filepath.Join(t.TempDir(), "child.pid")
	helper := exec.Command(os.Args[0], "-test.run", "^TestWatcherAbruptDeathStopsDaemonChild$")
	helper.Env = append(os.Environ(),
		"ZEN_DEV_PDEATH_HELPER=1",
		"ZEN_DEV_PDEATH_ROOT="+root,
		"ZEN_DEV_PDEATH_PIDFILE="+pidfile,
	)
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	var childPID int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidfile); err == nil {
			if value, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && value > 0 {
				childPID = value
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if childPID == 0 {
		_ = helper.Process.Kill()
		_ = helper.Wait()
		t.Fatal("daemon child pid was never observed")
	}

	// Abrupt watcher death, no watcher-side cleanup at all.
	if err := helper.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = helper.Wait()

	gone := false
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		if syscall.Kill(childPID, 0) != nil {
			gone = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !gone {
		_ = syscall.Kill(childPID, syscall.SIGKILL)
		t.Fatalf("daemon child %d survived an abrupt watcher SIGKILL", childPID)
	}
}
