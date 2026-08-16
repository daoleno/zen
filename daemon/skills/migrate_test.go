package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationTracksExternalWithoutTakeover(t *testing.T) {
	f := newFixture(t)
	// External skills across several agent global dirs, including a symlink
	// shared across Pi and Codex (one canonical identity).
	f.writeSkill(f.agentGlobalDir(AgentCodex), "shared", "shared body")
	if err := os.MkdirAll(f.agentGlobalDir(AgentPi), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.agentGlobalDir(AgentCodex), "shared"), filepath.Join(f.agentGlobalDir(AgentPi), "shared")); err != nil {
		t.Fatal(err)
	}
	f.writeSkill(f.agentGlobalDir(AgentGrok), "grok-native", "grok content")

	report, err := ExecuteMigration(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	if report.Tracked < 2 {
		t.Fatalf("expected at least two tracked external rows, got %+v", report)
	}
	store := f.store()
	file, err := store.LoadInventory(false)
	if err != nil {
		t.Fatal(err)
	}
	shared := file.Packages["shared"]
	if shared.Owned {
		t.Fatal("migration must never mark external rows as owned")
	}
	if len(shared.DiscoveredAgents) != 1 {
		t.Fatalf("shared symlink must discover one canonical agent, got %+v", shared.DiscoveredAgents)
	}
	// External files are untouched.
	content, err := os.ReadFile(filepath.Join(f.agentGlobalDir(AgentCodex), "shared", "SKILL.md"))
	if err != nil || !strings.Contains(string(content), "shared body") {
		t.Fatalf("migration must preserve external files: %v", err)
	}
	// Duplicate same-name different-content detection.
	f.writeSkill(f.agentGlobalDir(AgentCodex), "dup", "codex copy")
	f.writeSkill(f.agentGlobalDir(AgentOpenCode), "dup", "opencode copy")
	report2, err := ExecuteMigration(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	if report2.Duplicate == 0 && report2.Conflict == 0 {
		t.Fatalf("duplicate detection must report, got %+v", report2)
	}
	// A same-name conflict against an owned package must be skipped loudly.
	source := f.writeSkill(f.Home, "conflict-name", "imported content")
	mustRunMutation(t, f, MutationRequest{Operation: OperationImport, SkillName: "conflict-name", InfoPath: source, Scope: ScopeGlobal, Agents: []Agent{AgentCodex}})
	f.writeSkill(f.agentGlobalDir(AgentGrok), "conflict-name", "different external content")
	report3, err := ExecuteMigration(f.options(""))
	if err != nil {
		t.Fatal(err)
	}
	if report3.Conflict == 0 {
		t.Fatalf("conflict must be counted, got %+v", report3)
	}
	// The external conflict file stays untouched.
	if _, err := os.Stat(filepath.Join(f.agentGlobalDir(AgentGrok), "conflict-name", "SKILL.md")); err != nil {
		t.Fatal("conflicting external file must be preserved")
	}
}

func TestMigrationProjectScopeAndPinnedState(t *testing.T) {
	f := newFixture(t)
	f.writeSkill(f.agentProjectDir(AgentCodex, f.Project), "local-skill", "project body")
	report, err := ExecuteMigration(f.options(f.Project))
	if err != nil {
		t.Fatal(err)
	}
	store := f.store()
	file, _ := store.LoadInventory(false)
	entry := file.Packages["local-skill"]
	if entry.DiscoveredScope != ScopeProject {
		t.Fatalf("project discovery scope wrong: %+v", entry)
	}
	if len(entry.DiscoveredAgents) != 1 || entry.DiscoveredAgents[0] != AgentCodex {
		t.Fatalf("project discovered agent wrong: %+v", entry.DiscoveredAgents)
	}
	if report.Tracked < 1 {
		t.Fatalf("project migration must track, got %+v", report)
	}
}

func TestAdaptersRegistryContracts(t *testing.T) {
	env := func(key string) string {
		switch key {
		case "CODEX_HOME":
			return "/env/codex"
		case "CLAUDE_CONFIG_DIR":
			return "/env/claude"
		case "XDG_CONFIG_HOME":
			return "/env/config"
		default:
			return ""
		}
	}
	assert := func(agent Agent, wantGlobal, wantProject string, mode BindingMode) {
		t.Helper()
		adapter, err := adapterFor(agent)
		if err != nil {
			t.Fatal(err)
		}
		if got := globalSkillsDir(adapter, "/home/u", env); got != wantGlobal {
			t.Fatalf("%s global dir = %s, want %s", agent, got, wantGlobal)
		}
		if got := projectSkillsDir(adapter, "/repo"); got != wantProject {
			t.Fatalf("%s project dir = %s, want %s", agent, got, wantProject)
		}
		if adapter.Mode != mode {
			t.Fatalf("%s binding mode = %s, want %s", agent, adapter.Mode, mode)
		}
	}
	assert(AgentCodex, "/env/codex/skills", "/repo/.agents/skills", BindingSymlink)
	assert(AgentClaudeCode, "/env/claude/skills", "/repo/.claude/skills", BindingSymlink)
	assert(AgentCursor, "/home/u/.cursor/skills", "/repo/.cursor/skills", BindingCopy)
	assert(AgentGrok, "/home/u/.grok/skills", "/repo/.grok/skills", BindingCopy)
	assert(AgentOpenCode, "/env/config/opencode/skills", "/repo/.opencode/skills", BindingSymlink)
	assert(AgentPi, "/home/u/.pi/agent/skills", "/repo/.pi/skills", BindingSymlink)
}

func TestExecutorAliasResolution(t *testing.T) {
	cases := []struct {
		kind, command, name string
		want                Agent
	}{
		{"cursor", "cursor-agent", "agent", AgentCursor},
		{"claude", "claude", "claude", AgentClaudeCode},
		{"", "/usr/local/bin/grok", "grok", AgentGrok},
		{"", "codex --dangerously-bypass", "codeman", AgentCodex},
		{"", "pi", "pi", AgentPi},
		{"", "opencode", "opencode", AgentOpenCode},
		{"", "pip install", "pip", ""},
		{"", "myopencode", "custom-oc", ""},
		{"", "", "random", ""},
	}
	for _, tc := range cases {
		if got := resolveExecutorAgent(tc.kind, tc.command, tc.name); got != tc.want {
			t.Errorf("resolveExecutorAgent(%q,%q,%q) = %q, want %q", tc.kind, tc.command, tc.name, got, tc.want)
		}
	}
}
