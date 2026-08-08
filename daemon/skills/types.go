package skills

import "time"

type Agent string

const (
	AgentCodex      Agent = "codex"
	AgentClaudeCode Agent = "claude-code"
	AgentCursor     Agent = "cursor"
	AgentGrok       Agent = "grok"
	AgentOpenCode   Agent = "opencode"
	AgentPi         Agent = "pi"
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
	CanRemove    bool               `json:"can_remove"`
	RemovalPlans []AgentRemovalPlan `json:"removal_plans,omitempty"`
	Reason       string             `json:"reason,omitempty"`
}

type AgentRemovalPlan struct {
	Agent          Agent   `json:"agent"`
	AffectedAgents []Agent `json:"affected_agents"`
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
	GeneratedAt        time.Time           `json:"generated_at"`
	CWD                string              `json:"cwd,omitempty"`
	Skills             []InstalledSkill    `json:"skills"`
	Agents             []AgentSupport      `json:"agents"`
	Warnings           []string            `json:"warnings,omitempty"`
	MutationOperations []MutationOperation `json:"mutation_operations,omitempty"`
	incomplete         bool
}

// SupportedMutationOperations is the authoritative mutation capability set
// this daemon serves for standalone Skills. The App gates every lifecycle
// affordance on this list; anything absent must never render an action.
func SupportedMutationOperations() []MutationOperation {
	return []MutationOperation{OperationInstall, OperationRemove, OperationUpdate}
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

type CatalogView string

const (
	CatalogViewAllTime  CatalogView = "all-time"
	CatalogViewTrending CatalogView = "trending"
	CatalogViewHot      CatalogView = "hot"
)

type RankedCatalogSkill struct {
	ID                string `json:"id"`
	SkillID           string `json:"skill_id"`
	Name              string `json:"name"`
	Source            string `json:"source"`
	Rank              int    `json:"rank"`
	TotalInstalls     *int64 `json:"total_installs,omitempty"`
	Installs24h       *int64 `json:"installs_24h,omitempty"`
	CurrentInstalls   *int64 `json:"current_installs,omitempty"`
	YesterdayInstalls *int64 `json:"yesterday_installs,omitempty"`
	Change            *int64 `json:"change,omitempty"`
	Installable       bool   `json:"installable"`
}

type CatalogLeaderboard struct {
	View        CatalogView          `json:"view"`
	TotalSkills int64                `json:"total_skills"`
	Skills      []RankedCatalogSkill `json:"skills"`
}

type CatalogLeaderboards struct {
	AllTime  CatalogLeaderboard `json:"all_time"`
	Trending CatalogLeaderboard `json:"trending"`
	Hot      CatalogLeaderboard `json:"hot"`
}

type MutationOperation string

const (
	OperationInstall MutationOperation = "install"
	OperationRemove  MutationOperation = "remove"
	OperationUpdate  MutationOperation = "update"
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
	SkillName string            `json:"skill_name,omitempty"`
	Scope     Scope             `json:"scope"`
	Agents    []Agent           `json:"agents"`
}

func SupportedAgents() []AgentSupport {
	return []AgentSupport{
		{Agent: AgentCodex, Name: "Codex", Supported: true, CLIManaged: true},
		{Agent: AgentClaudeCode, Name: "Claude Code", Supported: true, CLIManaged: true},
		{Agent: AgentCursor, Name: "Cursor", Supported: true, CLIManaged: true},
		{Agent: AgentOpenCode, Name: "OpenCode", Supported: true, CLIManaged: true},
		{Agent: AgentPi, Name: "Pi", Supported: true, CLIManaged: true},
		{
			Agent:      AgentGrok,
			Name:       "Grok",
			Supported:  false,
			CLIManaged: false,
			Reason:     "The official skills CLI does not expose a Grok target.",
		},
	}
}
