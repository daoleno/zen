package brain

import (
	"time"

	"github.com/daoleno/zen/daemon/work"
)

type AgentRef struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Status  string    `json:"status"`
	Summary string    `json:"summary,omitempty"`
	Cwd     string    `json:"cwd,omitempty"`
	Command string    `json:"command,omitempty"`
	Updated time.Time `json:"updated_at"`
	Hidden  bool      `json:"hidden,omitempty"`
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

type AttentionSummary struct {
	Pinned              int    `json:"pinned"`
	NeedsReview         int    `json:"needs_review"`
	Reviewing           int    `json:"reviewing"`
	ReviewQueue         int    `json:"review_queue"`
	ActiveAgents        int    `json:"active_agents"`
	BlockedAgents       int    `json:"blocked_agents"`
	InFlightAgents      int    `json:"in_flight_agents"`
	MaxInFlightAgents   int    `json:"max_in_flight_agents"`
	ReviewQueueLimit    int    `json:"review_queue_limit"`
	AvailableAgentSlots int    `json:"available_agent_slots"`
	CanStartAgent       bool   `json:"can_start_agent"`
	BackpressureReason  string `json:"backpressure_reason,omitempty"`
	Pressure            string `json:"pressure"`
}

type AttentionQueueItem struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary,omitempty"`
	AgentID     string    `json:"agent_id,omitempty"`
	ThreadID    string    `json:"thread_id,omitempty"`
	WorkItemID  string    `json:"work_item_id,omitempty"`
	Status      string    `json:"status,omitempty"`
	ReviewState string    `json:"review_state,omitempty"`
	Pinned      bool      `json:"pinned,omitempty"`
	Project     string    `json:"project,omitempty"`
	Cwd         string    `json:"cwd,omitempty"`
	Command     string    `json:"command,omitempty"`
	Path        string    `json:"path,omitempty"`
	Updated     time.Time `json:"updated_at"`
}

type Snapshot struct {
	Memory         string               `json:"memory"`
	Profile        string               `json:"profile"`
	Personality    string               `json:"personality"`
	Agents         []AgentRef           `json:"agents"`
	HostAgent      *AgentRef            `json:"host_agent,omitempty"`
	HostAdapter    *work.AgentAdapter   `json:"host_adapter,omitempty"`
	Adapters       []work.AgentAdapter  `json:"adapters"`
	ChatThreadID   string               `json:"chat_thread_id,omitempty"`
	Attention      AttentionSummary     `json:"attention"`
	AttentionQueue []AttentionQueueItem `json:"attention_queue"`
	Workspace      string               `json:"workspace,omitempty"`
	GeneratedAt    time.Time            `json:"generated_at"`
}
