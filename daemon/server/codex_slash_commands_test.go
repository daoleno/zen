package server

import (
	"encoding/binary"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParseCodexSlashDescriptionsFindsBinaryBlock(t *testing.T) {
	data := []byte("noise" +
		"choose what model and reasoning effort to use" +
		"include current selection, open files, and other context from your IDE" +
		"choose what Codex is allowed to do" +
		"remap TUI shortcuts" +
		"set up elevated agent sandbox" +
		"let sandbox read a directory: /sandbox-add-read-dir <absolute_path>" +
		"toggle experimental features" +
		"approve one retry of a recent auto-review denial" +
		"configure memory use and generation" +
		"use skills to improve how Codex performs specific tasks" +
		"view and manage lifecycle hooks" +
		"review my current changes and find issues" +
		"rename the current thread" +
		"start a new chat during a conversation" +
		"resume a saved chat" +
		"fork the current chat" +
		"create an AGENTS.md file with instructions for Codex" +
		"summarize conversation to prevent hitting the context limit" +
		"switch to Plan mode" +
		"set or view the goal for a long-running task" +
		"start a side conversation in an ephemeral fork" +
		"copy last response as markdown" +
		"toggle raw scrollback mode for copy-friendly terminal selection" +
		"show git diff (including untracked files)" +
		"mention a file" +
		"show current session configuration and token usage" +
		"show config layers and requirement sources for debugging" +
		"configure which items appear in the terminal title" +
		"configure which items appear in the status line" +
		"choose a syntax highlighting theme" +
		"choose or hide the terminal pet" +
		"list configured MCP tools; use /mcp verbose for details" +
		"manage apps" +
		"browse plugins" +
		"exit Codex" +
		"send logs to maintainers" +
		"print the rollout file path" +
		"list background terminals" +
		"stop all background terminals" +
		"clear the terminal and start a new chat" +
		"choose a communication style for Codex" +
		"toggle realtime voice mode (experimental)" +
		"configure realtime microphone/speaker" +
		"test approval request" +
		"switch the active agent thread" +
		"DO NOT USE" +
		"tail")

	descriptions := parseCodexSlashDescriptions(data)
	if len(descriptions) < 40 {
		t.Fatalf("description count = %d, want at least 40", len(descriptions))
	}
	for command, want := range map[string]string{
		"model":       "choose what model and reasoning effort to use",
		"permissions": "choose what Codex is allowed to do",
		"status":      "show current session configuration and token usage",
		"agent":       "switch the active agent thread",
	} {
		if got := descriptions[command]; got != want {
			t.Fatalf("description for %s = %q, want %q", command, got, want)
		}
	}
}

func TestDefaultCodexSlashCommandsContainsRealCommandSet(t *testing.T) {
	commands := defaultCodexSlashCommands("test")
	if len(commands) < 40 {
		t.Fatalf("command count = %d, want at least 40", len(commands))
	}

	values := map[string]bool{}
	for _, command := range commands {
		values[command.Value] = true
		if command.Source != "test" {
			t.Fatalf("command %s source = %q, want test", command.Value, command.Source)
		}
		if command.Category == "" || command.Execution == "" || command.Input.Kind == "" || command.Output.Kind == "" {
			t.Fatalf("command %s missing capability metadata: %#v", command.Value, command)
		}
	}
	for _, value := range []string{"/model", "/permissions", "/diff", "/status", "/agent", "/setup-default-sandbox", "/settings"} {
		if !values[value] {
			t.Fatalf("missing command %s", value)
		}
	}
	for _, value := range []string{"/test", "/help", "/sandbox", "/voice"} {
		if values[value] {
			t.Fatalf("unexpected non-Codex command %s", value)
		}
	}
}

func TestSlashCommandCapabilitiesAreConservative(t *testing.T) {
	commands := defaultCodexSlashCommands("test")
	values := map[string]CodexSlashCommand{}
	for _, command := range commands {
		values[command.Value] = command
	}

	for _, value := range []string{"/status", "/diff", "/copy"} {
		command := values[value]
		if command.Execution != "chat-native" || !command.ChatSupported || !command.TerminalSupported {
			t.Fatalf("%s capability = %#v, want chat-native and supported", value, command)
		}
	}

	for _, value := range []string{"/model", "/permissions", "/mention", "/mcp"} {
		command := values[value]
		if command.Execution != "terminal-required" || command.ChatSupported || !command.TerminalSupported || !command.Interactive {
			t.Fatalf("%s capability = %#v, want interactive terminal-required", value, command)
		}
	}

	command := values["/debug-m-drop"]
	if command.Execution != "unsupported" || command.ChatSupported || command.TerminalSupported {
		t.Fatalf("/debug-m-drop capability = %#v, want unsupported", command)
	}
}

func TestCommandsFromDiscoveryIncludesNewBinaryCommands(t *testing.T) {
	commands := commandsFromDiscovery(codexSlashCommandDiscovery{
		names: []string{"model", "new-upstream-command"},
		descriptions: map[string]string{
			"model":                "model description from binary",
			"new-upstream-command": "new command description",
		},
	})

	values := map[string]CodexSlashCommand{}
	for _, command := range commands {
		values[command.Value] = command
	}
	if got := values["/model"].Description; got != "model description from binary" {
		t.Fatalf("/model description = %q, want binary override", got)
	}
	if got := values["/new-upstream-command"].Description; got != "new command description" {
		t.Fatalf("/new-upstream-command description = %q, want discovered command", got)
	}
	if command := values["/new-upstream-command"]; command.Execution != "terminal-required" || command.Category != "unknown" || command.ChatSupported {
		t.Fatalf("/new-upstream-command capability = %#v, want conservative unknown terminal command", command)
	}
}

func TestParseCodexSlashCommandNamesFindsPointerBackedCluster(t *testing.T) {
	data := make([]byte, 512)
	copy(data[128:], []byte("modelnew-upstream-commanddebug-m-dropdebug-m-update"))
	for index, offset := range []uint64{128, 133, 153, 165} {
		binary.LittleEndian.PutUint64(data[index*8:], offset)
	}

	names := parseCodexSlashCommandNames(data)
	values := map[string]bool{}
	for _, name := range names {
		values[name] = true
	}
	for _, name := range []string{"model", "new-upstream-command", "debug-m-drop", "debug-m-update"} {
		if !values[name] {
			t.Fatalf("missing discovered command %s from %#v", name, names)
		}
	}
}

func TestParseCodexFastSlashDescription(t *testing.T) {
	data := []byte(`{
		"service_tiers": [
			{"id": "priority", "name": "Fast", "description": "1.5x speed, increased usage"}
		]
	}`)

	if got := parseCodexFastSlashDescription(data); got != "1.5x speed, increased usage" {
		t.Fatalf("parseCodexFastSlashDescription() = %q", got)
	}
}

func TestDiscoverCodexSlashCommandsFromInstalledCodex(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex not installed")
	}

	commands, source := discoverCodexSlashCommandsUncached()
	if source != "codex-binary" {
		t.Skipf("local Codex binary discovery unavailable; source = %s", source)
	}
	if len(commands) < 40 {
		t.Fatalf("command count = %d, want at least 40", len(commands))
	}

	values := map[string]CodexSlashCommand{}
	for _, command := range commands {
		values[command.Value] = command
	}
	for _, value := range []string{"/model", "/fast", "/permissions", "/diff", "/status", "/settings"} {
		if !strings.HasPrefix(values[value].Value, "/") {
			t.Fatalf("missing discovered command %s", value)
		}
	}
	if fast := values["/fast"].Description; fast == "" || fast == "use fewer credits for upcoming turns" {
		t.Fatalf("/fast description = %q, want installed Codex metadata", fast)
	}
}

func TestParseCodexSlashCommandDiscoveryFromInstalledBinary(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex not installed")
	}
	binaryPath, ok := resolveCodexNativeBinary()
	if !ok {
		t.Skip("native Codex binary unavailable")
	}
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read native Codex binary: %v", err)
	}

	discovery := parseCodexSlashCommandDiscovery(data)
	names := map[string]bool{}
	for _, name := range discovery.names {
		names[name] = true
	}
	for _, name := range []string{"setup-default-sandbox", "sandbox-add-read-dir", "debug-m-drop", "debug-m-update"} {
		if !names[name] {
			t.Fatalf("missing binary-discovered command name %s from %#v", name, discovery.names)
		}
	}
	if got := discovery.descriptions["fast"]; got == "" || got == "use fewer credits for upcoming turns" {
		t.Fatalf("fast description = %q, want installed Codex metadata", got)
	}
}

func TestSlashCommandTitlePreservesKnownInitialisms(t *testing.T) {
	for input, want := range map[string]string{
		"ide":                   "IDE",
		"mcp":                   "MCP",
		"sandbox-add-read-dir":  "Sandbox Add Read Dir",
		"setup-default-sandbox": "Setup Default Sandbox",
	} {
		if got := slashCommandTitle(input); got != want {
			t.Fatalf("slashCommandTitle(%q) = %q, want %q", input, got, want)
		}
	}
}
