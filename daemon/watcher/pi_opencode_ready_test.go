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
	// OpenCode 1.18.15 (captured live 2026-08-08): the idle footer dropped the
	// semver; cwd stays left and "ctrl+p commands" stays right. The busy
	// footer ("esc interrupt ... ctrl+p commands") must not pass because it
	// does not start with a filesystem path.
	ocIdleV11815 := `
   ┃
   ┃  Ask anything...
   ┃
   ┃  Build auto · DeepSeek V4 Flash (2x usage) OpenCode Go · max
   ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
   /home/daoleno/project  220.5K (22%) · $0.01  ctrl+p commands
`
	if !isOpenCodeInputReady(ocIdleV11815) {
		t.Fatal("expected opencode 1.18.15 idle footer ready pane")
	}
	// Exact live capture from real Calendar occurrence d9ff47a4 (Session @71,
	// 2026-08-08): the home cwd /home/daoleno renders as a bare "~" and the
	// 1.18.15 idle footer still carries the semver. This pane must be ready.
	ocHomeV11815 := `
   ┃  Ask anything... "What is the tech stack of this project?"
   ┃  Build auto · DeepSeek V4 Flash (2x usage) OpenCode Go · max
   tab agents  ctrl+p commands
  ~                                                                    1.18.15
`
	if !isOpenCodeInputReady(ocHomeV11815) {
		t.Fatal("expected opencode 1.18.15 home-cwd tilde footer ready pane")
	}
	// Same home-cwd "~" abbreviation on the path/usage/ctrl+p footer layout.
	ocHomeCtrlPV11815 := `
   ┃
   ┃  Ask anything...
   ┃
   ┃  Build auto · DeepSeek V4 Flash (2x usage) OpenCode Go · max
   ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
  ~  220.5K (22%) · $0.01  ctrl+p commands
`
	if !isOpenCodeInputReady(ocHomeCtrlPV11815) {
		t.Fatal("expected opencode 1.18.15 home-cwd tilde ctrl+p footer ready pane")
	}
	// A real draft replaces the "Ask anything..." placeholder; even with the
	// exact tilde footer the pane must not be treated as ready.
	ocDraftV11815 := `
   ┃  What is the tech stack of this project?
   ┃  Build auto · DeepSeek V4 Flash (2x usage) OpenCode Go · max
   tab agents  ctrl+p commands
  ~                                                                    1.18.15
`
	if isOpenCodeInputReady(ocDraftV11815) {
		t.Fatal("draft composer without the Ask anything placeholder must not be ready")
	}
	ocBusyV11815 := `
   ┃
   ┃  Ask anything... "Fix broken tests"
   ┃  Build auto · GPT-5.3 Chat (latest) OpenAI
   ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
   esc interrupt         109.7K (11%) · $0.01  ctrl+p commands
`
	if isOpenCodeInputReady(ocBusyV11815) {
		t.Fatal("busy esc interrupt footer must not be ready")
	}
	ocFalseCtrlP := `
   ┃  Ask anything...
   ┃  Build · GPT-5.3
   some tool output  ctrl+p commands
`
	if isOpenCodeInputReady(ocFalseCtrlP) {
		t.Fatal("non-footer ctrl+p commands text must not satisfy OpenCode idle footer")
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
	// A tilde-prefixed transcript line must still anchor the semver: prose
	// between the tilde and a later version is not a footer.
	ocTildeTranscriptSemver := `
   ┃  Ask anything...
   ┃  Build · GPT-5.3
   ~  npm installed 2.3.4
`
	if isOpenCodeInputReady(ocTildeTranscriptSemver) {
		t.Fatal("tilde transcript line must not satisfy OpenCode footer")
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
