package skills

import "time"

type Agent string

const (
	AgentCodex      Agent = "codex"
	AgentClaudeCode Agent = "claude-code"
	AgentCursor     Agent = "cursor"
	AgentGrok       Agent = "grok"
)

type Scope string

const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
	ScopeMixed   Scope = "mixed"
	ScopePlugin  Scope = "plugin"
	ScopeBuiltin Scope = "builtin"
	ScopeUnknown Scope = "unknown"
)

type Manager string

const (
	ManagerSkillsCLI Manager = "skills-cli"
	ManagerPlugin    Manager = "plugin"
	ManagerBuiltin   Manager = "builtin"
	ManagerUnknown   Manager = "unknown"
)

type AgentSupport struct {
	Agent      Agent  `json:"agent"`
	Name       string `json:"name"`
	Supported  bool   `json:"supported"`
	CLIManaged bool   `json:"cli_managed"`
	Reason     string `json:"reason,omitempty"`
}

type ManagementCapability struct {
	CanRemove bool   `json:"can_remove"`
	Reason    string `json:"reason,omitempty"`
}

type SkillBinding struct {
	SourcePath string  `json:"source_path"`
	Scope      Scope   `json:"scope"`
	Agents     []Agent `json:"agents"`
}

type InstalledSkill struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Description   string               `json:"description,omitempty"`
	CanonicalPath string               `json:"canonical_path"`
	SourcePath    string               `json:"source_path"`
	Scope         Scope                `json:"scope"`
	Agents        []Agent              `json:"agents"`
	Bindings      []SkillBinding       `json:"bindings"`
	Manager       Manager              `json:"manager"`
	Provenance    string               `json:"provenance"`
	Source        string               `json:"source,omitempty"`
	SourceType    string               `json:"source_type,omitempty"`
	Plugin        string               `json:"plugin,omitempty"`
	Capability    ManagementCapability `json:"capability"`
}

type Inventory struct {
	GeneratedAt time.Time        `json:"generated_at"`
	CWD         string           `json:"cwd,omitempty"`
	Skills      []InstalledSkill `json:"skills"`
	Agents      []AgentSupport   `json:"agents"`
	Warnings    []string         `json:"warnings,omitempty"`
	incomplete  bool
}

type CatalogSkill struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Installs int64  `json:"installs"`
	Source   string `json:"source"`
}

type CatalogResult struct {
	Query  string         `json:"query"`
	Skills []CatalogSkill `json:"skills"`
}

type MutationOperation string

const (
	OperationInstall MutationOperation = "install"
	OperationRemove  MutationOperation = "remove"
)

type MutationRequest struct {
	Operation MutationOperation
	CWD       string
	SkillID   string
	Source    string
	SkillName string
	Scope     Scope
	Agents    []Agent
}

type MutationCommand struct {
	Operation MutationOperation `json:"operation"`
	Command   string            `json:"command"`
	CatalogID string            `json:"catalog_id,omitempty"`
	Source    string            `json:"source,omitempty"`
	SkillName string            `json:"skill_name"`
	Scope     Scope             `json:"scope"`
	Agents    []Agent           `json:"agents"`
}

func SupportedAgents() []AgentSupport {
	return []AgentSupport{
		{Agent: AgentCodex, Name: "Codex", Supported: true, CLIManaged: true},
		{Agent: AgentClaudeCode, Name: "Claude Code", Supported: true, CLIManaged: true},
		{Agent: AgentCursor, Name: "Cursor", Supported: true, CLIManaged: true},
		{
			Agent:      AgentGrok,
			Name:       "Grok",
			Supported:  false,
			CLIManaged: false,
			Reason:     "The official skills CLI does not expose a Grok target.",
		},
	}
}
