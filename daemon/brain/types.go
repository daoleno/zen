package brain

import (
	"time"

	"github.com/daoleno/zen/daemon/work"
)

type AgentRef struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Summary   string    `json:"summary,omitempty"`
	Cwd       string    `json:"cwd,omitempty"`
	Command   string    `json:"command,omitempty"`
	Updated   time.Time `json:"updated_at"`
	Hidden    bool      `json:"hidden,omitempty"`
	Delegated bool      `json:"delegated,omitempty"`
}

type ChatMessage struct {
	ID        string    `json:"id"`
	ThreadID  string    `json:"thread_id,omitempty"`
	SessionID string    `json:"session_id"`
	AdapterID string    `json:"adapter_id,omitempty"`
	Role      string    `json:"role"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type Snapshot struct {
	Memory       string              `json:"memory"`
	Profile      string              `json:"profile"`
	Current      string              `json:"current,omitempty"`
	Personality  string              `json:"personality"`
	Agents       []AgentRef          `json:"agents"`
	HostAgent    *AgentRef           `json:"host_agent,omitempty"`
	HostAdapter  *work.AgentAdapter  `json:"host_adapter,omitempty"`
	Adapters     []work.AgentAdapter `json:"adapters"`
	ChatThreadID string              `json:"chat_thread_id,omitempty"`
	Workspace    string              `json:"workspace,omitempty"`
	GeneratedAt  time.Time           `json:"generated_at"`
}

type BrainContext struct {
	ThreadID       string              `json:"thread_id,omitempty"`
	Workspace      string              `json:"workspace,omitempty"`
	Current        string              `json:"current,omitempty"`
	Memory         string              `json:"memory,omitempty"`
	Profile        string              `json:"profile,omitempty"`
	Personality    string              `json:"personality,omitempty"`
	HostAgent      *AgentRef           `json:"host_agent,omitempty"`
	HostAdapter    *work.AgentAdapter  `json:"host_adapter,omitempty"`
	Adapters       []work.AgentAdapter `json:"adapters"`
	Agents         []AgentRef          `json:"agents"`
	RecentMessages []ChatMessage       `json:"recent_messages"`
	GeneratedAt    time.Time           `json:"generated_at"`
}

type HousekeepingReport struct {
	Workspace            string     `json:"workspace,omitempty"`
	CurrentPath          string     `json:"current_path"`
	PolicyPaths          []string   `json:"policy_paths"`
	WorklogPath          string     `json:"worklog_path"`
	OpenDelegatedAgents  []AgentRef `json:"open_delegated_agents"`
	RecentMessageCount   int        `json:"recent_message_count"`
	BackfilledWorkspace  bool       `json:"backfilled_workspace"`
	RecommendedNextSteps []string   `json:"recommended_next_steps,omitempty"`
	GeneratedAt          time.Time  `json:"generated_at"`
}

type WorkspaceTree struct {
	Workspace   string           `json:"workspace,omitempty"`
	Entries     []WorkspaceEntry `json:"entries"`
	GeneratedAt time.Time        `json:"generated_at"`
}

type WorkspaceEntry struct {
	Name       string           `json:"name"`
	Path       string           `json:"path"`
	Kind       string           `json:"kind"`
	Size       int64            `json:"size,omitempty"`
	ModifiedAt time.Time        `json:"modified_at,omitempty"`
	Children   []WorkspaceEntry `json:"children,omitempty"`
}

type WorkspaceFile struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Kind       string    `json:"kind"`
	Language   string    `json:"language"`
	Content    string    `json:"content"`
	Size       int64     `json:"size,omitempty"`
	ModifiedAt time.Time `json:"modified_at,omitempty"`
}
