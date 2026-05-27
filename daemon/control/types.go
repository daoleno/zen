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
	Command    string `json:"command,omitempty"`
	Cwd        string `json:"cwd,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	PromptFile string `json:"prompt_file,omitempty"`
	AgentID    string `json:"agent_id,omitempty"`
	Text       string `json:"text,omitempty"`
	Submit     bool   `json:"submit,omitempty"`
	Hidden     bool   `json:"hidden,omitempty"`
}

type Response struct {
	OK        bool        `json:"ok"`
	Error     *Error      `json:"error,omitempty"`
	Agent     *Agent      `json:"agent,omitempty"`
	Agents    []Agent     `json:"agents,omitempty"`
	Text      string      `json:"text,omitempty"`
	Workspace string      `json:"workspace,omitempty"`
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
