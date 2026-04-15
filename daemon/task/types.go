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
	ID            string        `json:"id"`
	Number        int           `json:"number"`
	Title         string        `json:"title"`
	Description   string        `json:"description,omitempty"`
	Status        TaskStatus    `json:"status"`
	Priority      int           `json:"priority"`
	Labels        []string      `json:"labels,omitempty"`
	ProjectID     string        `json:"project_id,omitempty"`
	SkillID       string        `json:"skill_id,omitempty"`
	DueDate       string        `json:"due_date,omitempty"`
	Cwd           string        `json:"cwd,omitempty"`
	CurrentRunID  string        `json:"current_run_id,omitempty"`
	LastRunStatus string        `json:"last_run_status,omitempty"`
	Comments      []TaskComment `json:"comments,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type TaskComment struct {
	ID             string    `json:"id"`
	Body           string    `json:"body"`
	AuthorKind     string    `json:"author_kind"`
	AuthorLabel    string    `json:"author_label,omitempty"`
	ParentID       string    `json:"parent_id,omitempty"`
	DeliveryMode   string    `json:"delivery_mode,omitempty"`
	RunID          string    `json:"run_id,omitempty"`
	AgentSessionID string    `json:"agent_session_id,omitempty"`
	TargetLabel    string    `json:"target_label,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
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
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Icon        string    `json:"icon,omitempty"`
	RepoRoot    string    `json:"repo_root,omitempty"`
	WorktreeRoot string   `json:"worktree_root,omitempty"`
	BaseBranch  string    `json:"base_branch,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
