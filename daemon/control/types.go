package control

import (
	"os"
	"path/filepath"
	"time"

	"github.com/daoleno/zen/daemon/calendar"
)

const SocketName = "zen.sock"

type Request struct {
	Type         string         `json:"type"`
	Name         string         `json:"name,omitempty"`
	Executor     string         `json:"executor,omitempty"`
	ExecutorID   string         `json:"executor_id,omitempty"`
	Command      string         `json:"command,omitempty"`
	Cwd          string         `json:"cwd,omitempty"`
	Prompt       string         `json:"prompt,omitempty"`
	PromptFile   string         `json:"prompt_file,omitempty"`
	Profile      string         `json:"profile,omitempty"`
	AgentID      string         `json:"agent_id,omitempty"`
	Status       string         `json:"status,omitempty"`
	Phase        string         `json:"phase,omitempty"`
	Attention    string         `json:"attention,omitempty"`
	Summary      string         `json:"summary,omitempty"`
	TaskClass    string         `json:"task_class,omitempty"`
	EventKind    string         `json:"event_kind,omitempty"`
	DetailsJSON  string         `json:"details_json,omitempty"`
	LeaseSeconds int            `json:"lease_seconds,omitempty"`
	Text         string         `json:"text,omitempty"`
	Submit       bool           `json:"submit,omitempty"`
	Hidden       bool           `json:"hidden,omitempty"`
	Force        bool           `json:"force,omitempty"`
	ID           string         `json:"id,omitempty"`
	Revision     int64          `json:"revision,omitempty"`
	CalendarItem *calendar.Item `json:"calendar_item,omitempty"`
}

type Response struct {
	OK                bool            `json:"ok"`
	Error             *Error          `json:"error,omitempty"`
	Agent             *Agent          `json:"agent,omitempty"`
	Agents            []Agent         `json:"agents,omitempty"`
	Executor          *Executor       `json:"executor,omitempty"`
	DelegatedExecutor *Executor       `json:"delegated_executor,omitempty"`
	Executors         []Executor      `json:"executors,omitempty"`
	Context           any             `json:"context,omitempty"`
	Housekeeping      any             `json:"housekeeping,omitempty"`
	Playbooks         any             `json:"playbooks,omitempty"`
	Text              string          `json:"text,omitempty"`
	Workspace         string          `json:"workspace,omitempty"`
	CalendarItem      *calendar.Item  `json:"calendar_item,omitempty"`
	CalendarItems     []calendar.Item `json:"calendar_items,omitempty"`
	Confirmation      string          `json:"confirmation,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Agent struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Status              string     `json:"status"`
	Summary             string     `json:"summary,omitempty"`
	Phase               string     `json:"phase,omitempty"`
	Attention           string     `json:"attention,omitempty"`
	TaskClass           string     `json:"task_class,omitempty"`
	EventKind           string     `json:"event_kind,omitempty"`
	DetailsJSON         string     `json:"details_json,omitempty"`
	NeedsAttention      bool       `json:"needs_attention,omitempty"`
	LastProgressAt      *time.Time `json:"last_progress_at,omitempty"`
	ExpectedNextCheckAt *time.Time `json:"expected_next_check_at,omitempty"`
	LeaseSeconds        int        `json:"lease_seconds,omitempty"`
	Cwd                 string     `json:"cwd,omitempty"`
	Command             string     `json:"command,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at,omitempty"`
	Hidden              bool       `json:"hidden,omitempty"`
	Delegated           bool       `json:"delegated,omitempty"`
}

type ExecutorCapabilities struct {
	InteractiveTTY   bool `json:"interactive_tty"`
	StructuredEvents bool `json:"structured_events"`
}

type Executor struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Provider     string               `json:"provider"`
	Command      string               `json:"command,omitempty"`
	Runtime      string               `json:"runtime"`
	Capabilities ExecutorCapabilities `json:"capabilities"`
	Host         bool                 `json:"host,omitempty"`
	Delegated    bool                 `json:"delegated,omitempty"`
}

type Handler interface {
	HandleControlRequest(Request) Response
}

func DefaultSocketPath(stateDir string) (string, error) {
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		stateDir = filepath.Join(home, ".zen")
	}
	return filepath.Join(stateDir, "run", SocketName), nil
}

func ErrorResponse(code, message string) Response {
	return Response{
		OK: false,
		Error: &Error{
			Code:    code,
			Message: message,
		},
	}
}
