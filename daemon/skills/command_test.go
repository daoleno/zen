package skills

import (
	"os"
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

func TestBuildMutationCommandRejectsUnsupportedOperations(t *testing.T) {
	for _, operation := range []MutationOperation{"upgrade", "check", "plugin-install", "plugin-uninstall", ""} {
		request := MutationRequest{
			Operation: operation,
			SkillID:   "acme/skills/good",
			Source:    "acme/skills",
			SkillName: "good",
			Scope:     ScopeGlobal,
			Agents:    []Agent{AgentCodex},
		}
		if got, buildErr := BuildMutationCommand(InventoryOptions{}, request); buildErr == nil || !strings.Contains(buildErr.Error(), "unsupported Skill operation") {
			t.Fatalf("operation %q built %#v with error %v, want unsupported Skill operation", operation, got, buildErr)
		}
	}
}

func TestBuildUpdateCommandBuildsExactScopeLevelCommands(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	writeTestSkill(t, filepath.Join(home, ".agents", "skills", "global-skill"), "global-skill", "Global")
	writeTestLock(t, filepath.Join(home, ".agents", ".skill-lock.json"), 3, map[string]lockEntry{
		"global-skill": {
			Source:       "acme/skills",
			SourceType:   "github",
			SourceURL:    "https://github.com/acme/skills",
			SkillPath:    "skills/global-skill/SKILL.md",
			ComputedHash: "abc",
		},
	})
	writeTestSkill(t, filepath.Join(project, ".agents", "skills", "project-skill"), "project-skill", "Project")
	writeTestLock(t, filepath.Join(project, "skills-lock.json"), 1, map[string]lockEntry{
		"project-skill": {
			Source:       "acme/skills",
			SourceType:   "github",
			SourceURL:    "https://github.com/acme/skills",
			SkillPath:    "skills/project-skill/SKILL.md",
			ComputedHash: "def",
		},
	})

	global, err := BuildMutationCommand(InventoryOptions{Home: home}, MutationRequest{
		Operation: OperationUpdate,
		Scope:     ScopeGlobal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if global.Command != "npx skills update --global --yes" || len(global.Agents) != 0 || global.SkillName != "" {
		t.Fatalf("global update = %#v", global)
	}

	projectUpdate, err := BuildMutationCommand(InventoryOptions{Home: home}, MutationRequest{
		Operation: OperationUpdate,
		CWD:       project,
		Scope:     ScopeProject,
	})
	if err != nil {
		t.Fatal(err)
	}
	if projectUpdate.Command != "npx skills update --project --yes" {
		t.Fatalf("project update = %#v", projectUpdate)
	}

	if _, err := BuildMutationCommand(InventoryOptions{Home: home}, MutationRequest{
		Operation: OperationUpdate,
		Scope:     ScopeProject,
	}); err == nil || !strings.Contains(err.Error(), "working directory") {
		t.Fatalf("project update without CWD error = %v", err)
	}
}

func TestBuildUpdateCommandFailsClosedWithoutCLIManagedSkills(t *testing.T) {
	home := t.TempDir()
	writeTestSkill(t, filepath.Join(home, ".codex", "skills", ".system", "builtin-skill"), "builtin-skill", "Builtin")
	if _, err := BuildMutationCommand(InventoryOptions{Home: home}, MutationRequest{
		Operation: OperationUpdate,
		Scope:     ScopeGlobal,
	}); err == nil || !strings.Contains(err.Error(), "no skills-cli managed Skill") {
		t.Fatalf("update without CLI skills error = %v", err)
	}
}
func TestBuildMutationCommandRejectsPluginScopeMutations(t *testing.T) {
	for _, operation := range []MutationOperation{OperationInstall, OperationRemove, "update"} {
		request := MutationRequest{
			Operation: operation,
			CWD:       "",
			SkillID:   "acme/skills/good",
			Source:    "acme/skills",
			SkillName: "good",
			Scope:     ScopePlugin,
			Agents:    []Agent{AgentCodex},
		}
		if got, buildErr := BuildMutationCommand(InventoryOptions{}, request); buildErr == nil || !strings.Contains(buildErr.Error(), "unsupported managed Skill scope") {
			t.Fatalf("plugin-scope %q built %#v with error %v, want unsupported managed Skill scope", operation, got, buildErr)
		}
	}
}

func TestBuildInstalledCommandRefusesPluginOwnedSkill(t *testing.T) {
	home := t.TempDir()
	directory := filepath.Join(home, ".codex", "plugins", "cache", "vendor", "sample-plugin", "1.0.0", "skills", "plugin-skill")
	writeTestSkill(t, directory, "plugin-skill", "Plugin")

	inventory, err := DiscoverInventory(InventoryOptions{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	var pluginSkill *InstalledSkill
	for index := range inventory.Skills {
		if inventory.Skills[index].Manager == ManagerPlugin {
			pluginSkill = &inventory.Skills[index]
			break
		}
	}
	if pluginSkill == nil {
		t.Fatal("plugin-owned Skill not discovered")
	}
	if pluginSkill.Plugin != "sample-plugin" || pluginSkill.Scope != ScopePlugin {
		t.Fatalf("plugin provenance = %q scope = %q", pluginSkill.Plugin, pluginSkill.Scope)
	}
	if pluginSkill.Capability.CanRemove {
		t.Fatal("plugin-owned Skill must never be removable")
	}

	base := MutationRequest{Operation: OperationRemove, CWD: "", SkillID: pluginSkill.ID, Scope: ScopePlugin, Agents: []Agent{AgentCodex}}
	if _, buildErr := BuildMutationCommand(InventoryOptions{Home: home}, base); buildErr == nil || !strings.Contains(buildErr.Error(), "unsupported managed Skill scope") {
		t.Fatalf("plugin-scope remove error = %v, want unsupported managed Skill scope", buildErr)
	}
	base.Scope = ScopeGlobal
	if _, buildErr := BuildMutationCommand(InventoryOptions{Home: home}, base); buildErr == nil || !strings.Contains(buildErr.Error(), "not present in the requested scope") {
		t.Fatalf("global-scope remove error = %v, want installed Skill not present in the requested scope", buildErr)
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
	wantRemove := "npx skills remove managed-skill --agent codex --agent cursor --agent opencode --yes"
	if remove.Command != wantRemove {
		t.Fatalf("remove = %q, want %q", remove.Command, wantRemove)
	}
}

func TestBuildRemoveUsesOnlyDaemonProvenAgentRemovalPlans(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	canonical := filepath.Join(project, ".agents", "skills", "shared-skill")
	writeTestSkill(t, canonical, "shared-skill", "Shared")
	claudeLink := filepath.Join(project, ".claude", "skills", "shared-skill")
	if err := os.MkdirAll(filepath.Dir(claudeLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonical, claudeLink); err != nil {
		t.Fatal(err)
	}
	writeTestLock(t, filepath.Join(project, "skills-lock.json"), 1, map[string]lockEntry{
		"shared-skill": {Source: "acme/skills", SourceType: "github"},
	})

	options := InventoryOptions{CWD: project, Home: home}
	inventory, err := DiscoverInventory(options)
	if err != nil || len(inventory.Skills) != 1 {
		t.Fatalf("inventory = %#v, error = %v", inventory, err)
	}
	skill := inventory.Skills[0]
	if len(skill.Capability.RemovalPlans) != 4 {
		t.Fatalf("removal plans = %#v", skill.Capability.RemovalPlans)
	}

	claude, err := BuildMutationCommand(options, MutationRequest{
		Operation: OperationRemove,
		CWD:       project,
		SkillID:   skill.ID,
		Scope:     ScopeProject,
		Agents:    []Agent{AgentClaudeCode},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "npx skills remove shared-skill --agent claude-code --yes"; claude.Command != want {
		t.Fatalf("Claude removal = %q, want %q", claude.Command, want)
	}

	for _, unsafe := range [][]Agent{{AgentCodex}, {AgentCursor}, {AgentOpenCode}, {AgentCodex, AgentCursor}} {
		if _, err := BuildMutationCommand(options, MutationRequest{
			Operation: OperationRemove,
			CWD:       project,
			SkillID:   skill.ID,
			Scope:     ScopeProject,
			Agents:    unsafe,
		}); err == nil || !strings.Contains(err.Error(), "provable removal plan") {
			t.Fatalf("unsafe target %v error = %v", unsafe, err)
		}
	}

	shared, err := BuildMutationCommand(options, MutationRequest{
		Operation: OperationRemove,
		CWD:       project,
		SkillID:   skill.ID,
		Scope:     ScopeProject,
		Agents:    []Agent{AgentCodex, AgentClaudeCode, AgentCursor, AgentOpenCode},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "npx skills remove shared-skill --agent codex --agent claude-code --agent cursor --agent opencode --yes"; shared.Command != want {
		t.Fatalf("shared removal = %q, want %q", shared.Command, want)
	}
}

func TestBuildRemoveKeepsGlobalSharedBindingsInseparable(t *testing.T) {
	home := t.TempDir()
	canonical := filepath.Join(home, ".agents", "skills", "shared-skill")
	writeTestSkill(t, canonical, "shared-skill", "Shared global")
	for _, link := range []string{
		filepath.Join(home, ".codex", "skills", "shared-skill"),
		filepath.Join(home, ".cursor", "skills", "shared-skill"),
	} {
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(canonical, link); err != nil {
			t.Fatal(err)
		}
	}
	writeTestLock(t, filepath.Join(home, ".agents", ".skill-lock.json"), 3, map[string]lockEntry{
		"shared-skill": {Source: "acme/skills", SourceType: "github"},
	})

	options := InventoryOptions{Home: home}
	inventory, err := DiscoverInventory(options)
	if err != nil || len(inventory.Skills) != 1 {
		t.Fatalf("inventory = %#v, error = %v", inventory, err)
	}
	skill := inventory.Skills[0]
	if len(skill.Capability.RemovalPlans) != 2 {
		t.Fatalf("removal plans = %#v", skill.Capability.RemovalPlans)
	}
	for _, plan := range skill.Capability.RemovalPlans {
		if !sameAgentSet(plan.AffectedAgents, []Agent{AgentCodex, AgentCursor}) {
			t.Fatalf("global plan = %#v, want both bound Agents", plan)
		}
	}
	if _, err := BuildMutationCommand(options, MutationRequest{
		Operation: OperationRemove,
		SkillID:   skill.ID,
		Scope:     ScopeGlobal,
		Agents:    []Agent{AgentCodex},
	}); err == nil || !strings.Contains(err.Error(), "provable removal plan") {
		t.Fatalf("partial global removal error = %v", err)
	}

	command, err := BuildMutationCommand(options, MutationRequest{
		Operation: OperationRemove,
		SkillID:   skill.ID,
		Scope:     ScopeGlobal,
		Agents:    []Agent{AgentCodex, AgentCursor},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "npx skills remove shared-skill --global --agent codex --agent cursor --yes"; command.Command != want {
		t.Fatalf("global removal = %q, want %q", command.Command, want)
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
