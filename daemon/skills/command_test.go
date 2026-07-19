package skills

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildInstallCommandUsesOnlyValidatedStructuredIdentity(t *testing.T) {
	command, err := BuildMutationCommand(InventoryOptions{}, MutationRequest{
		Operation: OperationInstall,
		SkillID:   "vercel-labs/agent-skills/vercel-react-native-skills",
		Source:    "vercel-labs/agent-skills",
		SkillName: "vercel-react-native-skills",
		Scope:     ScopeGlobal,
		Agents:    []Agent{AgentCodex, AgentClaudeCode, AgentCursor},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "npx skills add https://github.com/vercel-labs/agent-skills --skill vercel-react-native-skills --global --agent codex --agent claude-code --agent cursor --yes"
	if command.Command != want {
		t.Fatalf("command = %q, want %q", command.Command, want)
	}
	if command.CatalogID != "vercel-labs/agent-skills/vercel-react-native-skills" || command.Source != "vercel-labs/agent-skills" {
		t.Fatalf("install identity = %q/%q", command.CatalogID, command.Source)
	}

	for _, invalid := range []MutationRequest{
		{Operation: OperationInstall, SkillID: "acme/skills/good", Source: "acme/skills;echo", SkillName: "good", Scope: ScopeGlobal, Agents: []Agent{AgentCodex}},
		{Operation: OperationInstall, SkillID: "acme/skills/good", Source: "acme/skills", SkillName: "bad;echo", Scope: ScopeGlobal, Agents: []Agent{AgentCodex}},
		{Operation: OperationInstall, SkillID: "acme/skills/other", Source: "acme/skills", SkillName: "good", Scope: ScopeGlobal, Agents: []Agent{AgentCodex}},
		{Operation: OperationInstall, SkillID: "acme/skills/good", Source: "acme/skills", SkillName: "good", Scope: ScopeGlobal, Agents: []Agent{AgentGrok}},
	} {
		if got, buildErr := BuildMutationCommand(InventoryOptions{}, invalid); buildErr == nil || got.Command != "" {
			t.Fatalf("invalid request built %#v with error %v", got, buildErr)
		}
	}
}

func TestBuildRemoveRediscoversExactOfficialCLIProvenance(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	directory := filepath.Join(project, ".agents", "skills", "managed-skill")
	writeTestSkill(t, directory, "managed-skill", "Managed")
	writeTestLock(t, filepath.Join(project, "skills-lock.json"), 1, map[string]lockEntry{
		"managed-skill": {
			Source:       "acme/skills",
			SourceType:   "github",
			SourceURL:    "https://github.com/acme/skills",
			SkillPath:    "skills/managed-skill/SKILL.md",
			ComputedHash: "abc",
		},
	})
	inventoryOptions := InventoryOptions{CWD: project, Home: home}
	inventory, err := DiscoverInventory(inventoryOptions)
	if err != nil || len(inventory.Skills) != 1 {
		t.Fatalf("inventory = %#v, error = %v", inventory, err)
	}
	skill := inventory.Skills[0]
	remove, err := BuildMutationCommand(inventoryOptions, MutationRequest{
		Operation: OperationRemove,
		CWD:       project,
		SkillID:   skill.ID,
		Scope:     ScopeProject,
		Agents:    skill.Agents,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRemove := "npx skills remove managed-skill --agent codex --agent cursor --yes"
	if remove.Command != wantRemove {
		t.Fatalf("remove = %q, want %q", remove.Command, wantRemove)
	}
}

func TestBuildInstalledCommandRejectsUnmanagedMissingAndRetargetedRows(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	writeTestSkill(t, filepath.Join(project, ".agents", "skills", "unknown-skill"), "unknown-skill", "Unknown")
	inventory, err := DiscoverInventory(InventoryOptions{CWD: project, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	skill := inventory.Skills[0]
	base := MutationRequest{Operation: OperationRemove, CWD: project, SkillID: skill.ID, Scope: ScopeProject, Agents: skill.Agents}
	if _, err := BuildMutationCommand(InventoryOptions{Home: home}, base); err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("unmanaged error = %v", err)
	}
	base.SkillID = strings.Repeat("a", 24)
	if _, err := BuildMutationCommand(InventoryOptions{Home: home}, base); err == nil || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("missing error = %v", err)
	}
	base.SkillID = skill.ID
	base.Agents = []Agent{AgentCodex}
	if _, err := BuildMutationCommand(InventoryOptions{Home: home}, base); err == nil {
		t.Fatal("retargeted unmanaged request unexpectedly succeeded")
	}
}

func TestLiteralValidatorsRejectShellAndTraversalGrammars(t *testing.T) {
	for _, value := range []string{"good-skill", "skill_2", "skill.v3"} {
		if err := ValidateSkillName(value); err != nil {
			t.Fatalf("ValidateSkillName(%q): %v", value, err)
		}
	}
	for _, value := range []string{"../skill", "skill name", "skill;echo", "-flag", "UPPER"} {
		if err := ValidateSkillName(value); err == nil {
			t.Fatalf("ValidateSkillName(%q) succeeded", value)
		}
	}
	for _, value := range []string{"acme/skills", "vercel-labs/agent_skills"} {
		if err := ValidateRepository(value); err != nil {
			t.Fatalf("ValidateRepository(%q): %v", value, err)
		}
	}
	for _, value := range []string{"https://github.com/acme/skills", "acme/skills.git", "acme/skills;echo", "../skills"} {
		if err := ValidateRepository(value); err == nil {
			t.Fatalf("ValidateRepository(%q) succeeded", value)
		}
	}
}
