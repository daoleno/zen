package brain

import (
	"time"

	"github.com/daoleno/zen/daemon/work"
)

type AgentRef struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Summary   string     `json:"summary,omitempty"`
	Cwd       string     `json:"cwd,omitempty"`
	Command   string     `json:"command,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	ProcessID int        `json:"process_id,omitempty"`
	Updated   time.Time  `json:"updated_at"`
	Hidden    bool       `json:"hidden,omitempty"`
	Delegated bool       `json:"delegated,omitempty"`
}

type Snapshot struct {
	Memory            string               `json:"memory"`
	Profile           string               `json:"profile"`
	Current           string               `json:"current,omitempty"`
	Personality       string               `json:"personality"`
	Agents            []AgentRef           `json:"agents"`
	HostAgent         *AgentRef            `json:"host_agent,omitempty"`
	HostExecutor      *work.AgentExecutor  `json:"host_executor,omitempty"`
	DelegatedExecutor *work.AgentExecutor  `json:"delegated_executor,omitempty"`
	Executors         []work.AgentExecutor `json:"executors"`
	ChatThreadID      string               `json:"chat_thread_id,omitempty"`
	Workspace         string               `json:"workspace,omitempty"`
	GeneratedAt       time.Time            `json:"generated_at"`
}

type BrainContext struct {
	ThreadID          string               `json:"thread_id,omitempty"`
	Workspace         string               `json:"workspace,omitempty"`
	Current           string               `json:"current,omitempty"`
	Memory            string               `json:"memory,omitempty"`
	Profile           string               `json:"profile,omitempty"`
	Personality       string               `json:"personality,omitempty"`
	Playbooks         []PlaybookEntry      `json:"playbooks,omitempty"`
	HostAgent         *AgentRef            `json:"host_agent,omitempty"`
	HostExecutor      *work.AgentExecutor  `json:"host_executor,omitempty"`
	DelegatedExecutor *work.AgentExecutor  `json:"delegated_executor,omitempty"`
	Executors         []work.AgentExecutor `json:"executors"`
	Agents            []AgentRef           `json:"agents"`
	GeneratedAt       time.Time            `json:"generated_at"`
}

type HousekeepingReport struct {
	Workspace            string     `json:"workspace,omitempty"`
	CurrentPath          string     `json:"current_path"`
	PolicyPaths          []string   `json:"policy_paths"`
	PlaybookPaths        []string   `json:"playbook_paths"`
	WorklogPath          string     `json:"worklog_path"`
	OpenDelegatedAgents  []AgentRef `json:"open_delegated_agents"`
	ChangedPaths         []string   `json:"changed_paths"`
	RecommendedNextSteps []string   `json:"recommended_next_steps,omitempty"`
	GeneratedAt          time.Time  `json:"generated_at"`
}

type WorkspaceTree struct {
	Workspace   string           `json:"workspace,omitempty"`
	Path        string           `json:"path,omitempty"`
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
