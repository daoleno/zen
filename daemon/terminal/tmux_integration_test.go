package terminal

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestTmuxViewLifecycleDoesNotOwnSharedWindowSize(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}

	tmuxTmpDir := isolateTmuxServer(t)
	runTmuxTestCommand(t,
		"-f", "/dev/null",
		"new-session", "-d",
		"-s", "source",
		"-x", "180",
		"-y", "55",
		"cat",
	)

	socketPath := tmuxTestOutput(t, "display-message", "-p", "#{socket_path}")
	if !strings.HasPrefix(socketPath, tmuxTmpDir+string(filepath.Separator)) {
		t.Fatalf("isolated tmux socket = %q, want it below %q", socketPath, tmuxTmpDir)
	}

	sourceClient := exec.Command("tmux", "attach-session", "-t", "source")
	sourceClient.Env = tmuxClientEnv(os.Environ())
	sourcePTY, err := pty.StartWithSize(sourceClient, &pty.Winsize{Cols: 180, Rows: 55})
	if err != nil {
		t.Fatalf("attach source tmux client: %v", err)
	}
	sourceDrainDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, sourcePTY)
		close(sourceDrainDone)
	}()
	t.Cleanup(func() {
		_ = sourcePTY.Close()
		if sourceClient.Process != nil {
			_ = sourceClient.Process.Kill()
		}
		_ = sourceClient.Wait()
		select {
		case <-sourceDrainDone:
		case <-time.After(time.Second):
		}
	})
	waitForTmuxTestClient(t, "source")

	windowID := tmuxTestOutput(t, "display-message", "-p", "-t", "source:0", "#{window_id}")
	runTmuxTestCommand(t, "set-window-option", "-g", "window-size", "smallest")
	runTmuxTestCommand(t, "set-window-option", "-g", "aggressive-resize", "off")
	runTmuxTestCommand(t, "resize-window", "-t", windowID, "-x", "180", "-y", "55")
	runTmuxTestCommand(t, "set-window-option", "-t", windowID, "window-size", "smallest")
	runTmuxTestCommand(t, "set-window-option", "-t", windowID, "aggressive-resize", "off")

	sourceState := readTmuxTestWindowState(t, windowID)
	if sourceState.sizeStrategy != "smallest" || sourceState.aggressiveResize != "off" {
		t.Fatalf("source sizing setup = %+v, want window-size smallest and aggressive-resize off", sourceState)
	}
	sourceStatus := tmuxTestOutput(t, "show-options", "-v", "-t", "source", "status")
	baselineWindowID := tmuxTestOutput(
		t,
		"new-window", "-d", "-P", "-F", "#{window_id}",
		"-t", "source", "-n", "baseline-probe", "cat",
	)
	baselineWindowState := readTmuxTestWindowState(t, baselineWindowID)
	runTmuxTestCommand(t, "kill-window", "-t", baselineWindowID)

	opened, err := (&TmuxBackend{}).Open("source:"+windowID, OpenOptions{Cols: 1, Rows: 1})
	if err != nil {
		t.Fatalf("open 1x1 tmux view: %v", err)
	}
	view := opened.(*tmuxSession)
	if got := view.Size(); got != (Size{Cols: 1, Rows: 1}) {
		t.Fatalf("opened view size = %+v, want mobile projection size 1x1", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	viewClosed := false
	t.Cleanup(func() {
		if !viewClosed {
			_ = view.Close()
		}
	})

	if err := view.Start(ctx); err != nil {
		t.Fatalf("start 1x1 tmux view: %v", err)
	}
	viewSession := view.linkedSession
	flags := waitForTmuxTestClient(t, viewSession)
	if !strings.Contains(flags, "ignore-size") {
		t.Fatalf("view client flags = %q, want ignore-size", flags)
	}
	if got := tmuxTestOutput(
		t,
		"display-message", "-p", "-t", viewSession,
		"#{session_grouped}:#{session_windows}",
	); got != "0:1" {
		t.Fatalf("view session ownership = %q, want independent session with one linked window", got)
	}
	assertTmuxTestClientMatchesWindowGeometry(t, viewSession, "after 1x1 Open")
	assertTmuxTestWindowState(t, windowID, sourceState, "after 1x1 Open")

	newWindowID := tmuxTestOutput(
		t,
		"new-window", "-d", "-P", "-F", "#{window_id}",
		"-t", "source", "-n", "open-probe", "cat",
	)
	newWindowState := readTmuxTestWindowState(t, newWindowID)
	if newWindowState != baselineWindowState {
		t.Fatalf(
			"source window created while view is attached = %+v, want pre-view source state %+v",
			newWindowState,
			baselineWindowState,
		)
	}
	if got := tmuxTestOutput(
		t,
		"list-windows", "-t", viewSession, "-F", "#{window_id}",
	); got != windowID {
		t.Fatalf("view session windows after source created %s = %q, want only %q", newWindowID, got, windowID)
	}

	// Simulate the source owner choosing an explicit geometry while the view is
	// attached. Later view resizes and Close must leave that ownership intact.
	runTmuxTestCommand(t, "resize-window", "-t", windowID, "-x", "180", "-y", "55")
	runTmuxTestCommand(t, "set-window-option", "-t", windowID, "window-size", "manual")
	runTmuxTestCommand(t, "set-window-option", "-t", windowID, "aggressive-resize", "off")
	ownerState := readTmuxTestWindowState(t, windowID)

	if err := view.Resize(100, 30); err != nil {
		t.Fatalf("resize tmux view to normal dimensions: %v", err)
	}
	if got := view.Size(); got != (Size{Cols: 100, Rows: 30}) {
		t.Fatalf("resized view size = %+v, want mobile projection size 100x30", got)
	}
	assertTmuxTestClientMatchesWindowGeometry(t, viewSession, "after normal Resize")
	assertTmuxTestWindowState(t, windowID, ownerState, "after normal Resize")
	normalResizeWindowID := assertTmuxTestNewSourceWindowState(
		t,
		"normal-resize-probe",
		baselineWindowState,
	)
	runTmuxTestCommand(t, "kill-window", "-t", normalResizeWindowID)

	marker := fmt.Sprintf("zen-view-io-%d", time.Now().UnixNano())
	if err := view.Write(marker + "\r"); err != nil {
		t.Fatalf("write through tmux view: %v", err)
	}
	waitForTmuxTestOutput(t, view.Events(), marker)

	if err := view.Resize(1, 1); err != nil {
		t.Fatalf("resize tmux view to 1x1: %v", err)
	}
	if got := view.Size(); got != (Size{Cols: 1, Rows: 1}) {
		t.Fatalf("resized view size = %+v, want mobile projection size 1x1", got)
	}
	assertTmuxTestClientMatchesWindowGeometry(t, viewSession, "after 1x1 Resize")
	assertTmuxTestWindowState(t, windowID, ownerState, "after 1x1 Resize")
	oneByOneResizeWindowID := assertTmuxTestNewSourceWindowState(
		t,
		"one-by-one-resize-probe",
		baselineWindowState,
	)
	runTmuxTestCommand(t, "kill-window", "-t", oneByOneResizeWindowID)

	if err := view.Close(); err != nil {
		t.Fatalf("close tmux view: %v", err)
	}
	viewClosed = true
	cancel()
	if err := exec.Command("tmux", "has-session", "-t", viewSession).Run(); err == nil {
		t.Fatalf("linked view session %q still exists after Close", viewSession)
	}
	if err := exec.Command("tmux", "has-session", "-t", "source").Run(); err != nil {
		t.Fatalf("source session was lost when view closed: %v", err)
	}
	if got := tmuxTestOutput(t, "show-options", "-v", "-t", "source", "status"); got != sourceStatus {
		t.Fatalf("source session status after Close = %q, want unchanged %q", got, sourceStatus)
	}
	assertTmuxTestWindowState(t, windowID, ownerState, "after Close")
	assertTmuxTestWindowState(t, newWindowID, newWindowState, "on source-only window after Close")
}

type tmuxTestWindowState struct {
	geometry         string
	sizeStrategy     string
	aggressiveResize string
}

func isolateTmuxServer(t *testing.T) string {
	t.Helper()

	tmuxValue, hadTmux := os.LookupEnv("TMUX")
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatalf("unset TMUX for isolated server: %v", err)
	}
	t.Cleanup(func() {
		if hadTmux {
			_ = os.Setenv("TMUX", tmuxValue)
			return
		}
		_ = os.Unsetenv("TMUX")
	})

	tmuxTmpDir := t.TempDir()
	t.Setenv("TMUX_TMPDIR", tmuxTmpDir)
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-server").Run()
	})
	return tmuxTmpDir
}

func runTmuxTestCommand(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("tmux", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tmux %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func tmuxTestOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("tmux", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func waitForTmuxTestClient(t *testing.T, sessionName string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var lastOutput string
	var lastErr error
	for time.Now().Before(deadline) {
		cmd := exec.Command(
			"tmux",
			"list-clients",
			"-t",
			sessionName,
			"-F",
			"#{client_flags}",
		)
		output, err := cmd.CombinedOutput()
		lastOutput = strings.TrimSpace(string(output))
		lastErr = err
		if err == nil && lastOutput != "" {
			return lastOutput
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("wait for tmux client on %q: last error %v, last output %q", sessionName, lastErr, lastOutput)
	return ""
}

func readTmuxTestWindowState(t *testing.T, windowID string) tmuxTestWindowState {
	t.Helper()
	return tmuxTestWindowState{
		geometry: tmuxTestOutput(
			t,
			"display-message", "-p", "-t", windowID,
			"#{window_width}x#{window_height}",
		),
		sizeStrategy: tmuxTestOutput(
			t,
			"show-options", "-w", "-A", "-v", "-t", windowID, "window-size",
		),
		aggressiveResize: tmuxTestOutput(
			t,
			"show-options", "-w", "-A", "-v", "-t", windowID, "aggressive-resize",
		),
	}
}

func assertTmuxTestWindowState(
	t *testing.T,
	windowID string,
	want tmuxTestWindowState,
	stage string,
) {
	t.Helper()
	if got := readTmuxTestWindowState(t, windowID); got != want {
		t.Fatalf("source window state %s = %+v, want unchanged %+v", stage, got, want)
	}
}

func assertTmuxTestClientMatchesWindowGeometry(t *testing.T, sessionName, stage string) {
	t.Helper()
	wantSize, err := tmuxViewPTYSize(sessionName)
	if err != nil {
		t.Fatalf("read expected tmux view client geometry %s: %v", stage, err)
	}
	want := fmt.Sprintf("%dx%d", wantSize.Cols, wantSize.Rows)
	if got := tmuxTestOutput(
		t,
		"list-clients", "-t", sessionName, "-F", "#{client_width}x#{client_height}",
	); got != want {
		t.Fatalf("tmux view client geometry %s = %q, want source-derived geometry %q", stage, got, want)
	}
}

func assertTmuxTestNewSourceWindowState(
	t *testing.T,
	name string,
	want tmuxTestWindowState,
) string {
	t.Helper()
	windowID := tmuxTestOutput(
		t,
		"new-window", "-d", "-P", "-F", "#{window_id}",
		"-t", "source", "-n", name, "cat",
	)
	if got := readTmuxTestWindowState(t, windowID); got != want {
		t.Fatalf("new source window %q = %+v, want pre-view state %+v", name, got, want)
	}
	return windowID
}

func waitForTmuxTestOutput(t *testing.T, events <-chan Event, marker string) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	var received strings.Builder
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("tmux view events closed before receiving %q", marker)
			}
			if event.Err != nil {
				t.Fatalf("tmux view emitted an error before receiving %q: %v", marker, event.Err)
			}
			if event.Type == EventOutput || event.Type == EventHistory {
				received.WriteString(event.Data)
				if strings.Contains(received.String(), marker) {
					return
				}
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %q in tmux view output; received %q", marker, received.String())
		}
	}
}
