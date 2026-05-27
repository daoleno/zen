package brain

import "time"

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

type Snapshot struct {
	Memory      string     `json:"memory"`
	Profile     string     `json:"profile"`
	Personality string     `json:"personality"`
	Agents      []AgentRef `json:"agents"`
	HostAgent   *AgentRef  `json:"host_agent,omitempty"`
	Workspace   string     `json:"workspace,omitempty"`
	GeneratedAt time.Time  `json:"generated_at"`
}
