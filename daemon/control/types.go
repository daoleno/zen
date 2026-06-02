package control

import (
	"os"
	"path/filepath"
	"time"
)

const SocketName = "zen.sock"

type Request struct {
	Type       string `json:"type"`
	Name       string `json:"name,omitempty"`
	Executor   string `json:"executor,omitempty"`
	AdapterID  string `json:"adapter_id,omitempty"`
	Command    string `json:"command,omitempty"`
	Cwd        string `json:"cwd,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	PromptFile string `json:"prompt_file,omitempty"`
	AgentID    string `json:"agent_id,omitempty"`
	Text       string `json:"text,omitempty"`
	Submit     bool   `json:"submit,omitempty"`
	Hidden     bool   `json:"hidden,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
	SearchTerm string `json:"search_term,omitempty"`
	Archived   bool   `json:"archived,omitempty"`
}

type Response struct {
	OK              bool      `json:"ok"`
	Error           *Error    `json:"error,omitempty"`
	Agent           *Agent    `json:"agent,omitempty"`
	Agents          []Agent   `json:"agents,omitempty"`
	Adapter         *Adapter  `json:"adapter,omitempty"`
	Adapters        []Adapter `json:"adapters,omitempty"`
	Threads         []Thread  `json:"threads,omitempty"`
	Text            string    `json:"text,omitempty"`
	Workspace       string    `json:"workspace,omitempty"`
	NextCursor      string    `json:"next_cursor,omitempty"`
	BackwardsCursor string    `json:"backwards_cursor,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Agent struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Summary   string    `json:"summary,omitempty"`
	Cwd       string    `json:"cwd,omitempty"`
	Command   string    `json:"command,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	Hidden    bool      `json:"hidden,omitempty"`
}

type AdapterCapabilities struct {
	NativeThreads    bool `json:"native_threads"`
	NativeSearch     bool `json:"native_search"`
	NativePinning    bool `json:"native_pinning"`
	NativeArchive    bool `json:"native_archive"`
	NativeWorktrees  bool `json:"native_worktrees"`
	NativeFork       bool `json:"native_fork"`
	NativeResume     bool `json:"native_resume"`
	NativeGoals      bool `json:"native_goals"`
	NativeAutomation bool `json:"native_automation"`
	InteractiveTTY   bool `json:"interactive_tty"`
	StructuredEvents bool `json:"structured_events"`
}

type Adapter struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Provider     string              `json:"provider"`
	Command      string              `json:"command,omitempty"`
	Runtime      string              `json:"runtime"`
	Capabilities AdapterCapabilities `json:"capabilities"`
	Preferred    bool                `json:"preferred,omitempty"`
}

type Thread struct {
	ID            string     `json:"id"`
	NativeID      string     `json:"native_id,omitempty"`
	Provider      string     `json:"provider,omitempty"`
	SessionID     string     `json:"session_id,omitempty"`
	ForkedFromID  string     `json:"forked_from_id,omitempty"`
	Title         string     `json:"title,omitempty"`
	Preview       string     `json:"preview,omitempty"`
	Snippet       string     `json:"snippet,omitempty"`
	Status        string     `json:"status,omitempty"`
	Cwd           string     `json:"cwd,omitempty"`
	Path          string     `json:"path,omitempty"`
	Source        string     `json:"source,omitempty"`
	ModelProvider string     `json:"model_provider,omitempty"`
	Ephemeral     bool       `json:"ephemeral,omitempty"`
	Archived      bool       `json:"archived,omitempty"`
	Pinned        bool       `json:"pinned,omitempty"`
	ReviewState   string     `json:"review_state,omitempty"`
	CreatedAt     *time.Time `json:"created_at,omitempty"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
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
