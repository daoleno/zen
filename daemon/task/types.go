package task

import "time"

type TaskStatus string

const (
	StatusBacklog    TaskStatus = "backlog"
	StatusTodo       TaskStatus = "todo"
	StatusInProgress TaskStatus = "in_progress"
	StatusDone       TaskStatus = "done"
	StatusCancelled  TaskStatus = "cancelled"
)

type Task struct {
	ID            string     `json:"id"`
	Number        int        `json:"number"`
	Title         string     `json:"title"`
	Description   string     `json:"description,omitempty"`
	Status        TaskStatus `json:"status"`
	Priority      int        `json:"priority"`
	Labels        []string   `json:"labels,omitempty"`
	ProjectID     string     `json:"project_id,omitempty"`
	SkillID       string     `json:"skill_id,omitempty"`
	Cwd           string     `json:"cwd,omitempty"`
	CurrentRunID  string     `json:"current_run_id,omitempty"`
	LastRunStatus string     `json:"last_run_status,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type TaskEvent struct {
	Type   string `json:"type"`
	TaskID string `json:"task_id"`
	Task   *Task  `json:"task,omitempty"`
}

type Skill struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Icon     string `json:"icon,omitempty"`
	AgentCmd string `json:"agent_cmd"`
	Prompt   string `json:"prompt"`
	Cwd      string `json:"cwd,omitempty"`
}

type Guidance struct {
	Preamble    string   `json:"preamble"`
	Constraints []string `json:"constraints"`
}

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Icon      string    `json:"icon,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
