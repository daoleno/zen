package watcher

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// codexNativeFixtureSource is the fake installed-Codex native binary. It
// mirrors the real @openai/codex 0.147 split: the TUI client half reads the
// pane's stdin and records every received line; the headless app-server half
// never reads stdin (its output is redirected to a log by the launch).
const codexNativeFixtureSource = `package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "app-server" {
		time.Sleep(24 * time.Hour)
		return
	}
	// Render a codex-ready composer + model footer so the readiness gate
	// converges before the first send.
	go func() {
		for {
			fmt.Print("\x1b[2J\x1b[H› \nmodel: gpt-5.6-sol · 100% context left\n")
			time.Sleep(100 * time.Millisecond)
		}
	}()
	record := os.Getenv("ZEN_TEST_CODEX_RECORD")
	f, err := os.OpenFile(record, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		fmt.Fprintln(f, line)
	}
}
`

// codexWrapperFixtureSource is the fake installed-Codex node wrapper
// (bin/codex.js in the real npm package): comm=node, argv carries the wrapper
// path, and it spawns the native binary descendant with argv passed through.
const codexWrapperFixtureSource = `#!/usr/bin/env node
const { spawn } = require("node:child_process");
const child = spawn(process.env.ZEN_TEST_CODEX_NATIVE, process.argv.slice(2), {
  stdio: "inherit",
  env: process.env,
});
child.on("exit", (code, signal) => {
  if (signal) {
    try { process.kill(process.pid, signal); } catch { process.exit(1); }
  } else {
    process.exit(code ?? 1);
  }
});
["SIGINT", "SIGTERM", "SIGHUP"].forEach((sig) => {
  process.on(sig, () => { try { child.kill(sig); } catch {} });
});
`

// buildFakeCodexInstall materializes the real installed-Codex process shape:
// a node wrapper at <root>/bin/codex (comm=node, argv contains "/bin/codex")
// that spawns a compiled native binary at <root>/native/codex (comm=codex).
// It returns the wrapper path, the native path, and the stdin record path.
func buildFakeCodexInstall(t *testing.T, root string) (wrapper, native, record string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; cannot build the installed-Codex wrapper shape")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available; cannot build the native codex fixture")
	}
	_ = node
	binDir := filepath.Join(root, "bin")
	nativeDir := filepath.Join(root, "native")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nativeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	wrapper = filepath.Join(binDir, "codex")
	if err := os.WriteFile(wrapper, []byte(codexWrapperFixtureSource), 0o700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(nativeDir, "main.go")
	if err := os.WriteFile(src, []byte(codexNativeFixtureSource), 0o600); err != nil {
		t.Fatal(err)
	}
	native = filepath.Join(nativeDir, "codex")
	build := exec.Command("go", "build", "-o", native, src)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native codex fixture: %v: %s", err, out)
	}
	record = filepath.Join(root, "codex-stdin.rec")
	return wrapper, native, record
}

// liveControlProcessTreeComplete reports whether the pane's foreground group
// holds the full real live-control tree: the node-wrapper TUI (pane process),
// its native --remote descendant, the node-wrapper app-server sibling, and its
// native app-server descendant. Node 24 reports the wrapper comm as
// "MainThread", so the wrapper halves are recognized by argv role only.
func liveControlProcessTreeComplete(panePID int, processes map[int]processInfo) bool {
	nativeTUI := false
	nativeServer := false
	siblingCount := 0
	for _, proc := range processes {
		if proc.pgid != panePID || proc.tpgid != panePID || proc.pid == panePID || proc.ppid != panePID {
			continue
		}
		if agentCommandFromProcess(proc) == "" {
			return false
		}
		siblingCount++
		if isCodexAppServerProcess(proc.args) {
			nativeServer = true
		}
		if proc.comm == "codex" && isCodexTUIClientProcess(proc.args) {
			nativeTUI = true
		}
	}
	// The two sibling subtrees: native TUI client and app-server wrapper
	// (whose own native descendant is a grandchild, not a pane child).
	return siblingCount >= 2 && nativeTUI && nativeServer
}

// TestRealTmuxCodexLiveControlNodeWrapperFirstSendOnce is the first-send
// regression proof for the real installed-Codex process shape: a node wrapper
// plus native descendant for each of the two live-control halves (headless
// app-server sibling subtree + --remote TUI subtree) sharing the pane's
// foreground process group. The freshly created Session must prove its target
// provider identity immediately and accept its first Interface message exactly
// once, through the production identity/send path.
func TestRealTmuxCodexLiveControlNodeWrapperFirstSendOnce(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	wrapper, native, record := buildFakeCodexInstall(t, h.root)
	socket := filepath.Join(h.root, "codex-ctl.sock")
	logPath := filepath.Join(h.root, "app-server.log")
	pidPath := filepath.Join(h.root, "app-server.pid")

	target, err := h.w.CreateSession("", CreateSessionOptions{
		Name: "live-control-first-send",
		Cwd:  h.root,
		Env: map[string]string{
			"ZEN_TEST_CODEX_NATIVE": native,
			"ZEN_TEST_CODEX_RECORD": record,
		},
		Command: "set +m; " + wrapper + " app-server --listen unix://" + socket +
			" > " + logPath + " 2>&1 & echo $! > " + pidPath +
			"; exec " + wrapper + " --remote unix://" + socket + " --model gpt-5",
		Detached: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = h.w.KillSession(target)
	})

	// The pane PID is stable across the exec chain (login shell -> wrapper).
	var panePID int
	waitForHarness(t, "live-control pane process", func() bool {
		out, displayErr := tmuxHarnessCommand(h.selected, "display-message", "-p", "-t", target, "#{pane_pid}").Output()
		if displayErr != nil {
			return false
		}
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(out)))
		if parseErr != nil || pid <= 0 {
			return false
		}
		panePID = pid
		return true
	})

	// Wait for the full real tree (both node wrappers and both native
	// descendants in the pane's foreground group), then prove the production
	// resolver picks the native --remote TUI client.
	waitForHarness(t, "full live-control process tree", func() bool {
		processes := snapshotProcesses()
		if !liveControlProcessTreeComplete(panePID, processes) {
			return false
		}
		authority, ok := resolveForegroundTargetProcess(panePID, processes)
		return ok && authority.command == "codex" && authority.provider.pid != panePID &&
			isCodexTUIClientProcess(authority.provider.args)
	})

	// First Interface message: the exact product path (SendInputWithReceipt
	// -> targetForSession -> submit) must accept once without weakening the
	// fail-closed identity guard.
	payload := "zen live-control first send proof"
	result, err := h.w.SendInputWithReceiptResult(target, payload, "receipt-live-control-first")
	if err != nil {
		t.Fatalf("first send failed: %v", err)
	}
	if result.Outcome != InputAccepted {
		t.Fatalf("first send outcome = %q, want %q", result.Outcome, InputAccepted)
	}

	// The provider received the message exactly once.
	waitForHarness(t, "provider receipt of first send", func() bool {
		return countRecordedLines(record, payload) >= 1
	})
	time.Sleep(500 * time.Millisecond)
	if count := countRecordedLines(record, payload); count != 1 {
		t.Fatalf("provider received %d copies of %q, want exactly 1", count, payload)
	}
}

// TestRealTmuxCodexLiveControlImmediateFirstSendWhileTreeForms drives the
// spawn handoff path (SendInputWhenReadyBudgeted, the exact identity-wait +
// mutation-boundary re-proof used for the initial delegated prompt) starting
// immediately after CreateSession, while the node-wrapper/app-server process
// tree is still forming. The frozen identity must converge on the native TUI
// client and the re-proof must reproduce it, so the first message submits
// exactly once.
func TestRealTmuxCodexLiveControlImmediateFirstSendWhileTreeForms(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	wrapper, native, record := buildFakeCodexInstall(t, h.root)
	socket := filepath.Join(h.root, "codex-ctl.sock")
	logPath := filepath.Join(h.root, "app-server.log")
	pidPath := filepath.Join(h.root, "app-server.pid")

	launch := "set +m; " + wrapper + " app-server --listen unix://" + socket +
		" > " + logPath + " 2>&1 & echo $! > " + pidPath +
		"; exec " + wrapper + " --remote unix://" + socket + " --model gpt-5"
	target, err := h.w.CreateSession("", CreateSessionOptions{
		Name: "live-control-immediate",
		Cwd:  h.root,
		Env: map[string]string{
			"ZEN_TEST_CODEX_NATIVE": native,
			"ZEN_TEST_CODEX_RECORD": record,
		},
		Command:  launch,
		Detached: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = h.w.KillSession(target)
	})

	// Send before the tree is complete: the identity wait must poll through
	// the launcher/app-server transitions and converge on the native TUI.
	// The trailing newline makes this a submit (the same shape the delegated
	// initial handoff submits); a draft without Enter would never be sent.
	payload := "zen immediate first send proof"
	if err := h.w.SendInputWhenReadyBudgeted(target, launch, payload+"\n", 20*time.Second); err != nil {
		t.Fatalf("immediate first send failed: %v", err)
	}
	waitForHarness(t, "provider receipt of immediate first send", func() bool {
		return countRecordedLines(record, payload) >= 1
	})
	time.Sleep(500 * time.Millisecond)
	if count := countRecordedLines(record, payload); count != 1 {
		t.Fatalf("provider received %d copies of %q, want exactly 1", count, payload)
	}
}

func countRecordedLines(path, payload string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == payload {
			count++
		}
	}
	return count
}
