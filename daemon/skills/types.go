package skills

import "time"

// Agent is one supported local Agent family whose native Skills roots Zen can
// discover. Custom executor names may resolve to one of these families.
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
	ScopePlugin  Scope = "plugin"
	ScopeBuiltin Scope = "builtin"
	ScopeUnknown Scope = "unknown"
)

type MutationOperation string

const OperationDelete MutationOperation = "delete"

type AgentSupport struct {
	Agent            Agent  `json:"agent"`
	Name             string `json:"name"`
	Supported        bool   `json:"supported"`
	GlobalScope      bool   `json:"global_scope"`
	ProjectScope     bool   `json:"project_scope"`
	DefaultGlobalDir string `json:"default_global_dir"`
	Reason           string `json:"reason,omitempty"`
}

type ExecutorSupport struct {
	Name    string `json:"name"`
	Kind    string `json:"kind,omitempty"`
	Agent   Agent  `json:"agent"`
	Command string `json:"command,omitempty"`
}

type RiskSignal struct {
	Type     string `json:"type"`
	Detail   string `json:"detail,omitempty"`
	Severity string `json:"severity"`
	File     string `json:"file,omitempty"`
}

// DeleteCapability is the daemon's fail-closed authority for one exact copy.
// Read-only and provider-owned copies carry a human reason and no action.
type DeleteCapability struct {
	CanDelete bool   `json:"can_delete"`
	Reason    string `json:"reason,omitempty"`
}

// InstalledSkill is one physical directory entry discovered beneath one
// allowed Skills root. RootPath identifies the entry Zen may remove;
// CanonicalPath is its resolved content location and is diagnostic identity,
// never a deletion target supplied by the App.
type InstalledSkill struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Description   string           `json:"description,omitempty"`
	Enabled       bool             `json:"enabled"`
	RootPath      string           `json:"root_path"`
	CanonicalPath string           `json:"canonical_path"`
	AllowedRoot   string           `json:"allowed_root"`
	Location      string           `json:"location"`
	Scope         Scope            `json:"scope"`
	Agents        []Agent          `json:"agents"`
	ContentHash   string           `json:"content_hash,omitempty"`
	Plugin        string           `json:"plugin,omitempty"`
	Risk          []RiskSignal     `json:"risk,omitempty"`
	Warnings      []string         `json:"warnings,omitempty"`
	Capability    DeleteCapability `json:"capability"`
}

type Inventory struct {
	GeneratedAt        time.Time           `json:"generated_at"`
	CWD                string              `json:"cwd,omitempty"`
	Skills             []InstalledSkill    `json:"skills"`
	Agents             []AgentSupport      `json:"agents"`
	Executors          []ExecutorSupport   `json:"executors,omitempty"`
	Warnings           []string            `json:"warnings,omitempty"`
	MutationOperations []MutationOperation `json:"mutation_operations"`
	incomplete         bool
}

func SupportedMutationOperations() []MutationOperation {
	return []MutationOperation{OperationDelete}
}

// MutationRequest carries the complete inventory identity selected by the
// App. Every field is matched against fresh discovery; none is used as an
// unchecked filesystem path.
type MutationRequest struct {
	Operation     MutationOperation
	CWD           string
	CopyID        string
	SkillName     string
	RootPath      string
	CanonicalPath string
	AllowedRoot   string
}

// MutationCommand is the reviewed delete identity. Execution re-discovers and
// revalidates the same tuple immediately before touching the filesystem.
type MutationCommand struct {
	Operation     MutationOperation `json:"operation"`
	CopyID        string            `json:"copy_id"`
	SkillName     string            `json:"skill_name"`
	RootPath      string            `json:"root_path"`
	CanonicalPath string            `json:"canonical_path"`
	AllowedRoot   string            `json:"allowed_root"`
	Location      string            `json:"location"`
	Scope         Scope             `json:"scope"`
	Agents        []Agent           `json:"agents"`
	Summary       string            `json:"summary"`
	Destructive   bool              `json:"destructive"`
}
