package terminal

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestTmuxNativeLatestLetsDesktopAndPhoneActivityOwnSharedSize(t *testing.T) {
	requireTmux(t)
	tmuxTmpDir := isolateTmuxServer(t)
	runTmuxTestCommand(t,
		"-f", "/dev/null", "new-session", "-d", "-s", "source", "-x", "100", "-y", "30", "cat",
	)

	if socketPath := tmuxTestOutput(t, "display-message", "-p", "#{socket_path}"); !strings.HasPrefix(socketPath, tmuxTmpDir+string(filepath.Separator)) {
		t.Fatalf("isolated tmux socket = %q, want it below %q", socketPath, tmuxTmpDir)
	}

	desktopCommand := exec.Command("tmux", "attach-session", "-t", "source")
	desktopCommand.Env = tmuxClientEnv(os.Environ())
	desktopPTY, err := pty.StartWithSize(desktopCommand, &pty.Winsize{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("attach isolated desktop client: %v", err)
	}
	drainPTY(t, desktopCommand, desktopPTY)
	desktopClient := waitForTmuxTestClient(t, "source")
	if strings.Contains(desktopClient.flags, "ignore-size") {
		t.Fatalf("desktop client flags = %q, must be a normal sizing client", desktopClient.flags)
	}

	windowID := tmuxTestOutput(t, "display-message", "-p", "-t", "source", "#{window_id}")
	opened, err := (&TmuxBackend{}).Open("source:"+windowID, OpenOptions{Cols: 44, Rows: 18})
	if err != nil {
		t.Fatalf("open phone tmux client: %v", err)
	}
	phone := opened.(*tmuxSession)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = phone.Close()
		}
	})

	if err := phone.Start(ctx); err != nil {
		t.Fatalf("start phone tmux client: %v", err)
	}
	helper := phone.linkedSession
	phoneClient := waitForTmuxTestClient(t, helper)
	if strings.Contains(phoneClient.flags, "ignore-size") {
		t.Fatalf("phone client flags = %q, must be a normal sizing client", phoneClient.flags)
	}
	if got := phone.Size(); got != (Size{Cols: 44, Rows: 18}) {
		t.Fatalf("phone session grid = %+v, want 44x18", got)
	}
	waitForTmuxTestGeometry(t, helper, windowID, "44x18", "44x17")
	if strategy := tmuxTestOutput(
		t,
		"show-options", "-w", "-A", "-v", "-t", windowID, "window-size",
	); strategy != "latest" {
		t.Fatalf("shared window-size = %q, want latest", strategy)
	}
	if localStrategy := tmuxTestOutput(
		t,
		"show-options", "-w", "-q", "-v", "-t", windowID, "window-size",
	); localStrategy != "" {
		t.Fatalf("shared window has local window-size override %q, want inherited native default", localStrategy)
	}

	if _, err := desktopPTY.Write([]byte("\x1b[I")); err != nil {
		t.Fatalf("send desktop client FocusIn: %v", err)
	}
	waitForTmuxTestGeometry(t, "source", windowID, "100x30", "100x29")

	if err := phone.Scroll(-1); err != nil {
		t.Fatalf("send server-side phone copy-mode command: %v", err)
	}
	if geometry := tmuxTestOutput(
		t,
		"display-message", "-p", "-t", windowID, "#{window_width}x#{window_height}",
	); geometry != "100x29" {
		t.Fatalf("server-side copy-mode command changed native latest geometry to %q, want desktop-owned 100x29", geometry)
	}
	if err := phone.CancelScroll(); err != nil {
		t.Fatalf("cancel boundary copy-mode fixture: %v", err)
	}

	if err := phone.Write("\x1b[I"); err != nil {
		t.Fatalf("send phone client FocusIn: %v", err)
	}
	waitForTmuxTestGeometry(t, helper, windowID, "44x18", "44x17")

	if err := phone.Resize(60, 22); err != nil {
		t.Fatalf("resize phone client: %v", err)
	}
	if got := phone.Size(); got != (Size{Cols: 60, Rows: 22}) {
		t.Fatalf("resized phone session grid = %+v, want 60x22", got)
	}
	waitForTmuxTestGeometry(t, helper, windowID, "60x22", "60x21")

	if err := phone.Close(); err != nil {
		t.Fatalf("close phone client: %v", err)
	}
	closed = true
	cancel()
	if err := exec.Command("tmux", "has-session", "-t", helper).Run(); err == nil {
		t.Fatalf("disposable helper session %q remains after close", helper)
	}
	if err := exec.Command("tmux", "has-session", "-t", "source").Run(); err != nil {
		t.Fatalf("source session was removed with helper: %v", err)
	}
}

func TestTmuxPhoneClientPreservesExplicitWindowSizePolicy(t *testing.T) {
	requireTmux(t)
	isolateTmuxServer(t)
	runTmuxTestCommand(t,
		"-f", "/dev/null", "new-session", "-d", "-s", "source", "-x", "100", "-y", "30", "cat",
	)

	windowID := tmuxTestOutput(t, "display-message", "-p", "-t", "source", "#{window_id}")
	runTmuxTestCommand(t, "set-window-option", "-t", windowID, "window-size", "smallest")
	if policy := tmuxTestOutput(
		t,
		"show-options", "-w", "-q", "-v", "-t", windowID, "window-size",
	); policy != "smallest" {
		t.Fatalf("fixture local window-size = %q, want smallest", policy)
	}

	opened, err := (&TmuxBackend{}).Open("source:"+windowID, OpenOptions{Cols: 44, Rows: 18})
	if err != nil {
		t.Fatalf("open phone tmux client: %v", err)
	}
	phone := opened.(*tmuxSession)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() { _ = phone.Close() })
	if err := phone.Start(ctx); err != nil {
		t.Fatalf("start phone tmux client: %v", err)
	}
	waitForTmuxTestClient(t, phone.linkedSession)

	if policy := tmuxTestOutput(
		t,
		"show-options", "-w", "-q", "-v", "-t", windowID, "window-size",
	); policy != "smallest" {
		t.Fatalf("phone attach changed local window-size to %q, want preserved smallest", policy)
	}

	if err := phone.Close(); err != nil {
		t.Fatalf("close phone tmux client: %v", err)
	}
	if policy := tmuxTestOutput(
		t,
		"show-options", "-w", "-q", "-v", "-t", windowID, "window-size",
	); policy != "smallest" {
		t.Fatalf("phone cleanup changed local window-size to %q, want preserved smallest", policy)
	}
}

func TestTmuxCopyModeRedrawFlowsThroughTheLivePTY(t *testing.T) {
	requireTmux(t)
	isolateTmuxServer(t)
	runTmuxTestCommand(t,
		"-f", "/dev/null", "new-session", "-d", "-s", "source", "-x", "44", "-y", "18",
		"sh", "-c", "seq -f fixture-%03g 1 160; exec cat",
	)
	waitForTmuxTestHistory(t, "source")

	windowID := tmuxTestOutput(t, "display-message", "-p", "-t", "source", "#{window_id}")
	opened, err := (&TmuxBackend{}).Open("source:"+windowID, OpenOptions{Cols: 44, Rows: 18})
	if err != nil {
		t.Fatal(err)
	}
	phone := opened.(*tmuxSession)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() { _ = phone.Close() })
	if err := phone.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitForTmuxTestClient(t, phone.linkedSession)
	waitForTmuxOutputContaining(t, phone.Events(), "fixture-160")
	drainTmuxEvents(phone.Events(), 40*time.Millisecond)

	if err := phone.Scroll(-6); err != nil {
		t.Fatalf("scroll toward history: %v", err)
	}
	waitForTmuxTestPaneMode(t, "source", "1")
	waitForTmuxTestPaneMode(t, phone.linkedSession, "1")
	olderRedraw := waitForTmuxOutput(t, phone.Events())
	if olderRedraw == "" {
		t.Fatal("copy-mode entry produced no attached-client PTY redraw")
	}
	position, err := strconv.Atoi(tmuxTestOutput(
		t, "display-message", "-p", "-t", phone.linkedSession, "#{scroll_position}",
	))
	if err != nil || position <= 0 {
		t.Fatalf("native copy-mode scroll_position = %d, error %v; want > 0", position, err)
	}

	for step := 0; step < 40; step++ {
		if err := phone.Scroll(12); err != nil {
			t.Fatalf("scroll toward live bottom at step %d: %v", step, err)
		}
		if paneMode(t, phone.linkedSession) == "0" {
			break
		}
		if step == 39 {
			t.Fatal("scroll-down-and-cancel did not exit copy-mode at the bottom")
		}
	}
	waitForTmuxTestPaneMode(t, "source", "0")
	newerRedraw := waitForTmuxOutput(t, phone.Events())
	if newerRedraw == "" || newerRedraw == olderRedraw {
		t.Fatal("return to live bottom did not produce a distinct PTY redraw")
	}
	for batch := 0; batch < 3; batch++ {
		if err := phone.Scroll(2); err != nil {
			t.Fatalf("bounded inertial batch %d after native bottom exit: %v", batch, err)
		}
	}
	if err := phone.Scroll(-2); err != nil {
		t.Fatalf("fast direction reversal after native bottom exit: %v", err)
	}
	waitForTmuxTestPaneMode(t, phone.linkedSession, "1")
	if err := phone.CancelScroll(); err != nil {
		t.Fatalf("idempotent input transition after bottom exit: %v", err)
	}
	waitForTmuxTestPaneMode(t, phone.linkedSession, "0")
	const marker = "native-input-after-scroll"
	if err := phone.Write(marker + "\r"); err != nil {
		t.Fatalf("write once after leaving copy-mode: %v", err)
	}
	waitForTmuxOutputContaining(t, phone.Events(), marker)
}

func TestIsolatedTmuxCleanupCannotKillAmbientServer(t *testing.T) {
	requireTmux(t)

	ambientTmpDir, err := os.MkdirTemp("/tmp", "zt-ambient-")
	if err != nil {
		t.Fatalf("create ambient tmux tmpdir: %v", err)
	}
	ambientEnv := isolatedTmuxEnv(os.Environ(), ambientTmpDir)
	t.Cleanup(func() {
		cmd := exec.Command("tmux", "kill-server")
		cmd.Env = ambientEnv
		_ = cmd.Run()
		_ = os.RemoveAll(ambientTmpDir)
	})

	ambient := exec.Command(
		"tmux", "-f", "/dev/null", "new-session", "-d", "-s", "ambient", "cat",
	)
	ambient.Env = ambientEnv
	if output, err := ambient.CombinedOutput(); err != nil {
		t.Fatalf("start ambient tmux server: %v\n%s", err, output)
	}
	t.Setenv("TMUX_TMPDIR", ambientTmpDir)

	var isolatedTmpDir string
	t.Run("isolated", func(t *testing.T) {
		isolatedTmpDir = isolateTmuxServer(t)
		runTmuxTestCommand(
			t, "-f", "/dev/null", "new-session", "-d", "-s", "isolated", "cat",
		)
	})

	probe := exec.Command("tmux", "has-session", "-t", "ambient")
	probe.Env = ambientEnv
	if output, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("isolated cleanup killed ambient tmux server: %v\n%s", err, output)
	}
	if _, err := os.Stat(isolatedTmpDir); !os.IsNotExist(err) {
		t.Fatalf("isolated tmux tmpdir still exists after cleanup: %q, stat error %v", isolatedTmpDir, err)
	}
}

func requireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
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
		} else {
			_ = os.Unsetenv("TMUX")
		}
	})

	// Keep the tmux UNIX socket path under sockaddr_un (~108 bytes).
	// t.TempDir() embeds the full test name and can overflow when TMPDIR is nested.
	tmuxTmpDir, err := os.MkdirTemp("/tmp", "zt-")
	if err != nil {
		t.Fatalf("create isolated tmux tmpdir: %v", err)
	}
	t.Setenv("TMUX_TMPDIR", tmuxTmpDir)
	cleanupEnv := isolatedTmuxEnv(os.Environ(), tmuxTmpDir)
	t.Cleanup(func() {
		// Never use ambient TMUX/TMUX_TMPDIR here. Test cleanup may run after
		// t.Setenv has restored the parent environment; a bare kill-server would
		// then kill the user's tmux server and strand the isolated one.
		cmd := exec.Command("tmux", "kill-server")
		cmd.Env = cleanupEnv
		_ = cmd.Run()
		_ = os.RemoveAll(tmuxTmpDir)
	})
	return tmuxTmpDir
}

func isolatedTmuxEnv(env []string, tmuxTmpDir string) []string {
	isolated := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, "TMUX=") || strings.HasPrefix(item, "TMUX_TMPDIR=") {
			continue
		}
		isolated = append(isolated, item)
	}
	return append(isolated, "TMUX_TMPDIR="+tmuxTmpDir)
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

func drainPTY(t *testing.T, command *exec.Cmd, ptmx *os.File) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, ptmx)
		close(done)
	}()
	t.Cleanup(func() {
		_ = ptmx.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})
}

type tmuxTestClient struct {
	name  string
	flags string
}

func waitForTmuxTestClient(t *testing.T, sessionName string) tmuxTestClient {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		output, err := exec.Command(
			"tmux", "list-clients", "-t", sessionName,
			"-F", "#{client_name}\t#{client_flags}",
		).CombinedOutput()
		if err == nil {
			line := strings.TrimSpace(string(output))
			if line != "" {
				parts := strings.SplitN(line, "\t", 2)
				if len(parts) == 2 {
					return tmuxTestClient{name: parts[0], flags: parts[1]}
				}
				return tmuxTestClient{name: line}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for tmux client on %q", sessionName)
	return tmuxTestClient{}
}

func waitForTmuxTestGeometry(
	t *testing.T,
	sessionName string,
	windowID string,
	wantClient string,
	wantWindow string,
) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		clientOutput, clientErr := exec.Command(
			"tmux", "list-clients", "-t", sessionName,
			"-F", "#{client_width}x#{client_height}",
		).CombinedOutput()
		windowOutput, windowErr := exec.Command(
			"tmux", "display-message", "-p", "-t", windowID,
			"#{window_width}x#{window_height}",
		).CombinedOutput()
		if clientErr == nil && windowErr == nil &&
			strings.TrimSpace(string(clientOutput)) == wantClient &&
			strings.TrimSpace(string(windowOutput)) == wantWindow {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf(
		"phone client/shared window did not converge to client %s and native usable window %s",
		wantClient,
		wantWindow,
	)
}

func waitForTmuxTestHistory(t *testing.T, target string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		output, err := exec.Command(
			"tmux", "display-message", "-p", "-t", target, "#{history_size}",
		).CombinedOutput()
		if err == nil {
			historySize, parseErr := strconv.Atoi(strings.TrimSpace(string(output)))
			if parseErr == nil && historySize > 0 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for fixture history on %q", target)
}

func paneMode(t *testing.T, target string) string {
	t.Helper()
	return tmuxTestOutput(t, "display-message", "-p", "-t", target, "#{pane_in_mode}")
}

func waitForTmuxTestPaneMode(t *testing.T, target, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		output, err := exec.Command(
			"tmux", "display-message", "-p", "-t", target, "#{pane_in_mode}",
		).CombinedOutput()
		if err == nil && strings.TrimSpace(string(output)) == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("pane_in_mode for %q did not become %q", target, want)
}

func waitForTmuxOutputContaining(
	t *testing.T,
	events <-chan Event,
	marker string,
) string {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	var received strings.Builder
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("terminal events closed before %q", marker)
			}
			if event.Type == EventOutput {
				received.WriteString(event.Data)
				if strings.Contains(received.String(), marker) {
					return received.String()
				}
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for %q in PTY output", marker)
		}
	}
}

func waitForTmuxOutput(t *testing.T, events <-chan Event) string {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("terminal events closed while waiting for PTY redraw")
			}
			if event.Type == EventOutput && event.Data != "" {
				return event.Data
			}
		case <-timer.C:
			t.Fatal("timed out waiting for attached-client PTY redraw")
		}
	}
}

func drainTmuxEvents(events <-chan Event, quiet time.Duration) {
	timer := time.NewTimer(quiet)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(quiet)
		case <-timer.C:
			return
		}
	}
}
