package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestRunSurfacesUnexpectedDaemonExit(t *testing.T) {
	// Keep this focused on the lifecycle contract rather than building the real
	// daemon: a finished child must be observable as a failure, never leave the
	// watcher presenting itself as a live daemon.
	cmd := exec.Command("sh", "-c", "exit 23")
	child := &runningProcess{cmd: cmd, done: make(chan error, 1)}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { child.done <- cmd.Wait() }()
	err := <-child.done
	if err == nil || !strings.Contains(err.Error(), "exit status 23") {
		t.Fatalf("child exit err=%v", err)
	}
	if got := unexpectedDaemonExit(err); !errors.Is(got, err) ||
		!strings.Contains(got.Error(), "daemon exited unexpectedly") {
		t.Fatalf("diagnostic=%v", got)
	}
	if got := unexpectedDaemonExit(nil); got == nil ||
		got.Error() != "daemon exited unexpectedly" {
		t.Fatalf("clean child exit diagnostic=%v", got)
	}
}
