package watcher

import "testing"

func TestPiAndOpenCodeInputReadyPredicates(t *testing.T) {
	piReady := `
 pi v0.73.1
 escape interrupt · ctrl+c/ctrl+d clear/exit · / commands · ! bash · ctrl+o

────────────────────────────────────────────────────────────────────────────────

────────────────────────────────────────────────────────────────────────────────
~/project
0.0%/272k (auto)                                                gpt-5.4 • medium
`
	if !isPiInputReady(piReady) {
		t.Fatal("expected pi ready pane")
	}
	piDraft := `
 pi v0.73.1
────────────────────────────────────────────────────────────────────────────────
draft still sitting here
────────────────────────────────────────────────────────────────────────────────
`
	if isPiInputReady(piDraft) {
		t.Fatal("nonempty editor must not be ready")
	}

	ocReady := `
   ┃
   ┃  Ask anything... "Fix broken tests"
   ┃
   ┃  Build auto · GPT-5.3 Chat (latest) OpenAI
   ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
   tab agents  ctrl+p commands
  ~/project                                  1.18.13
`
	if !isOpenCodeInputReady(ocReady) {
		t.Fatal("expected opencode ready pane")
	}
	ocPlain := `
   ┃  Ask anything...
   ┃  Build · GPT-5.3
  ~/project                                  1.18.13
`
	if !isOpenCodeInputReady(ocPlain) {
		t.Fatal("expected plain Build · ready pane")
	}
	ocBlocked := ocReady + "\nSelect a model\n"
	if isOpenCodeInputReady(ocBlocked) {
		t.Fatal("model picker must not be ready")
	}
	ocFalseSemver := `
   ┃  Ask anything...
   ┃  Build · GPT-5.3
   installed package 2.3.4 successfully
   tab agents  ctrl+p commands
`
	if isOpenCodeInputReady(ocFalseSemver) {
		t.Fatal("arbitrary pane semver must not satisfy OpenCode footer")
	}
	ocToolOutput := `
   ┃  Ask anything...
   ┃  Build · GPT-5.3
   Node.js 20.11.0
   typescript 5.4.5
`
	if isOpenCodeInputReady(ocToolOutput) {
		t.Fatal("tool-output semver must not satisfy OpenCode footer")
	}
	if !needsInputReadinessWait("pi", "") || !needsInputReadinessWait("opencode", "") {
		t.Fatal("pi/opencode must wait for readiness")
	}
	if !isPiCommand("env PATH=/x -- pi --session /tmp/a.jsonl") {
		t.Fatal("env-wrapped pi detection failed")
	}
	if !isOpenCodeCommand("opencode --auto") {
		t.Fatal("opencode detection failed")
	}
	if agentProviderFamily("pi") != "pi" || agentProviderFamily("opencode --auto") != "opencode" {
		t.Fatal("provider family mismatch")
	}
	if got := agentCommandFromProcess(processInfo{comm: "pi", args: "pi --session /tmp/x.jsonl"}); got != "pi" {
		t.Fatalf("pi process = %q", got)
	}
	if got := agentCommandFromProcess(processInfo{comm: "pip", args: "/usr/bin/pip install x"}); got != "" {
		t.Fatalf("pip must not look like pi: %q", got)
	}
	if got := agentCommandFromProcess(processInfo{comm: "node", args: "node /opt/bin/opencode --auto"}); got != "opencode" {
		t.Fatalf("opencode node wrapper = %q", got)
	}
	if !isAgentCommand("pi --session /tmp/x.jsonl") || !isAgentCommand("opencode --auto") {
		t.Fatal("isAgentCommand must recognize pi/opencode")
	}
}
