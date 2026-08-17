package skills

import "time"

// Agent is a Zen-supported target Executor family. The six adapters in
// adapters.go map each Agent to a real skills-directory contract; custom
// executor names that infer to one of these providers reuse that adapter.
type Agent string

const (
	AgentCodex      Agent = "codex"
	AgentClaudeCode Agent = "claude-code"
	AgentCursor     Agent = "cursor"
	AgentGrok       Agent = "grok"
	AgentOpenCode   Agent = "opencode"
	AgentPi         Agent = "pi"
)

// Scope names the reach of an Agent binding.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
	ScopeMixed   Scope = "mixed"
	ScopePlugin  Scope = "plugin"
	ScopeBuiltin Scope = "builtin"
	ScopeUnknown Scope = "unknown"
)

// Manager names the ownership authority for an installed skill row.
type Manager string

const (
	// ManagerZen marks a Zen-owned package: content lives in the canonical
	// store and Zen fully manages its bindings and lifecycle.
	ManagerZen Manager = "zen"
	// ManagerExternal marks a skill discovered in an Agent directory that Zen
	// does not own. Zen may track it in inventory for adopt/forget bookkeeping
	// but never edits its files.
	ManagerExternal Manager = "external"
	ManagerPlugin   Manager = "plugin"
	ManagerBuiltin  Manager = "builtin"
	ManagerUnknown  Manager = "unknown"
)

// BindingMode is how one (agent, scope) binding reaches the package. Direct is
// presentation-only for externally owned folders already living in an Agent
// directory. Zen-owned inventory entries persist only symlink or copy modes.
type BindingMode string

const (
	BindingSymlink BindingMode = "symlink"
	BindingCopy    BindingMode = "copy"
	BindingDirect  BindingMode = "direct"
)

// MutationOperation is the daemon-authoritative lifecycle operation set.
// The App gates every affordance on this list; anything absent never renders.
type MutationOperation string

const (
	OperationImport    MutationOperation = "import"
	OperationMigrate   MutationOperation = "migrate"
	OperationBind      MutationOperation = "bind"
	OperationUnbind    MutationOperation = "unbind"
	OperationEnable    MutationOperation = "enable"
	OperationDisable   MutationOperation = "disable"
	OperationUninstall MutationOperation = "uninstall"
	OperationForget    MutationOperation = "forget"
	OperationAdopt     MutationOperation = "adopt"
	OperationUpdate    MutationOperation = "update"
)

// AgentSupport describes one canonical Agent adapter: its display name, the
// scopes it truly supports, how bindings are materialized, and any reason for
// a limitation.
type AgentSupport struct {
	Agent            Agent  `json:"agent"`
	Name             string `json:"name"`
	Supported        bool   `json:"supported"`
	GlobalScope      bool   `json:"global_scope"`
	ProjectScope     bool   `json:"project_scope"`
	BindingMode      string `json:"binding_mode"`
	BindingModeNote  string `json:"binding_mode_note,omitempty"`
	DefaultGlobalDir string `json:"default_global_dir"`
	Reason           string `json:"reason,omitempty"`
}

// ExecutorSupport maps a configured (possibly custom) executor identity to the
// provider adapter it infers to. The alias reuses the provider's lifecycle.
type ExecutorSupport struct {
	Name    string `json:"name"`
	Kind    string `json:"kind,omitempty"`
	Agent   Agent  `json:"agent"`
	Command string `json:"command,omitempty"`
}

// RiskSignal is one bounded static signal from a package scan. It is a warning
// aid, never a safety verdict.
type RiskSignal struct {
	Type     string `json:"type"`
	Detail   string `json:"detail,omitempty"`
	Severity string `json:"severity"` // "info" | "warn" | "alert"
	File     string `json:"file,omitempty"`
}

type SkillBinding struct {
	Agent      Agent  `json:"agent"`
	Scope      Scope  `json:"scope"`
	Mode       string `json:"mode"`
	TargetPath string `json:"target_path"`
	SourcePath string `json:"source_path"`
	Enabled    bool   `json:"enabled"`
	BoundAt    string `json:"bound_at"`
	DriftHash  string `json:"drift_hash,omitempty"`
	Note       string `json:"note,omitempty"`
	// Operations is the exact executable action set for this binding's current
	// state. The App must not infer enable/disable availability from booleans.
	Operations []MutationOperation `json:"operations,omitempty"`
}

// ManagementCapability is the fail-closed action authority for one row.
type ManagementCapability struct {
	CanManage  bool                `json:"can_manage"`
	Operations []MutationOperation `json:"operations,omitempty"`
	Reason     string              `json:"reason,omitempty"`
}

type InstalledSkill struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Description   string               `json:"description,omitempty"`
	Manager       Manager              `json:"manager"`
	Owned         bool                 `json:"owned"`
	Tracked       bool                 `json:"tracked"`
	Enabled       bool                 `json:"enabled"`
	CanonicalPath string               `json:"canonical_path"`
	SourcePath    string               `json:"source_path"`
	Scope         Scope                `json:"scope"`
	Agents        []Agent              `json:"agents"`
	Bindings      []SkillBinding       `json:"bindings"`
	Provenance    string               `json:"provenance"`
	Source        string               `json:"source,omitempty"`
	SourceType    string               `json:"source_type,omitempty"`
	SourceURL     string               `json:"source_url,omitempty"`
	Ref           string               `json:"ref,omitempty"`
	ContentHash   string               `json:"content_hash,omitempty"`
	InstalledAt   string               `json:"installed_at,omitempty"`
	UpdatedAt     string               `json:"updated_at,omitempty"`
	Plugin        string               `json:"plugin,omitempty"`
	Risk          []RiskSignal         `json:"risk,omitempty"`
	Warnings      []string             `json:"warnings,omitempty"`
	Migration     string               `json:"migration,omitempty"`
	Capability    ManagementCapability `json:"capability"`
}

type MigrationStatus struct {
	Owned     int `json:"owned"`
	External  int `json:"external"`
	Duplicate int `json:"duplicate"`
	Conflict  int `json:"conflict"`
	Tracked   int `json:"tracked"`
}

type Inventory struct {
	GeneratedAt        time.Time           `json:"generated_at"`
	CWD                string              `json:"cwd,omitempty"`
	Skills             []InstalledSkill    `json:"skills"`
	Agents             []AgentSupport      `json:"agents"`
	Executors          []ExecutorSupport   `json:"executors,omitempty"`
	Warnings           []string            `json:"warnings,omitempty"`
	MutationOperations []MutationOperation `json:"mutation_operations,omitempty"`
	Migration          MigrationStatus     `json:"migration,omitempty"`
	incomplete         bool
}

// SupportedMutationOperations is the authoritative mutation capability set.
func SupportedMutationOperations() []MutationOperation {
	return []MutationOperation{
		OperationMigrate,
		OperationBind,
		OperationUnbind,
		OperationEnable,
		OperationDisable,
		OperationUninstall,
		OperationForget,
		OperationAdopt,
		OperationUpdate,
	}
}

// SourceType is how a package's content was acquired.
type SourceType string

const (
	SourceTypeCatalog SourceType = "catalog"
	SourceTypeLocal   SourceType = "local"
	SourceTypeArchive SourceType = "archive"
	SourceTypeGithub  SourceType = "github"
	// SourceTypeExternal marks an unowned inventory entry that points at an
	// external installation (never edited by Zen).
	SourceTypeExternal SourceType = "external"
)

// MutationRequest carries structured, validated lifecycle inputs. The same
// inputs build the reviewable plan (skills_command) and then execute it
// (skills_mutation); the daemon never trusts displayed text.
type MutationRequest struct {
	Operation MutationOperation
	CWD       string
	SkillID   string
	Source    string
	SkillName string
	Ref       string
	InfoPath  string
	Scope     Scope
	Agents    []Agent
}

// MutationChange is one exact before/after filesystem effect in the plan.
type MutationChange struct {
	Kind        string `json:"kind"` // "create_dir" | "copy_file" | "symlink" | "remove" | "keep" | "write"
	Path        string `json:"path"`
	Destination string `json:"destination,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

// MutationCommand is the reviewed plan the App confirms and the daemon
// executes. Summary is human-readable and enters the confirmation dialog;
// Changes are the exact effects that dialog must describe.
type MutationCommand struct {
	Operation    MutationOperation `json:"operation"`
	Scope        Scope             `json:"scope"`
	Agents       []Agent           `json:"agents"`
	SkillName    string            `json:"skill_name"`
	CopyID       string            `json:"copy_id,omitempty"`
	ImportID     string            `json:"-"`
	Source       string            `json:"source,omitempty"`
	Ref          string            `json:"ref,omitempty"`
	InfoPath     string            `json:"-"`
	Description  string            `json:"-"`
	ExpectedHash string            `json:"-"`
	Summary      string            `json:"summary"`
	Changes      []MutationChange  `json:"changes"`
	Destructive  bool              `json:"destructive"`
}
