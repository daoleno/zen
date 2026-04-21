package issue

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// SessionInfo describes a candidate tmux session that can receive issue work.
type SessionInfo struct {
	ID      string
	Project string
	Cwd     string
	Role    string
}

// SessionRegistry reports reusable sessions for a given executor role and cwd.
type SessionRegistry interface {
	IdleSessions(role, cwd string) []SessionInfo
}

// SessionRunner is the tmux side of dispatch: it can spawn and send text.
type SessionRunner interface {
	Spawn(role, cwd, command string) (string, error)
	Send(sessionID, text string) error
}

var (
	ErrAlreadyDispatched     = errors.New("issue already dispatched")
	ErrExecutorNotConfigured = errors.New("executor not configured")
	ErrSpawnFailed           = errors.New("spawn failed")
)

// Dispatcher picks a session and hands it the initial issue prompt.
type Dispatcher struct {
	reg   SessionRegistry
	run   SessionRunner
	execs *ExecutorConfig
	now   func() time.Time
}

func NewDispatcher(reg SessionRegistry, run SessionRunner, execs *ExecutorConfig) *Dispatcher {
	return &Dispatcher{
		reg:   reg,
		run:   run,
		execs: execs,
		now:   time.Now,
	}
}

// Dispatch sends iss to an agent and returns an updated issue with dispatched
// metadata populated. The caller is responsible for persisting it.
func (d *Dispatcher) Dispatch(iss *Issue, proj Project) (*Issue, error) {
	if iss == nil {
		return nil, fmt.Errorf("issue required")
	}
	if iss.Frontmatter.Dispatched != nil {
		return nil, ErrAlreadyDispatched
	}
	return d.dispatchInternal(iss, proj)
}

// Redispatch clears the dispatched metadata and sends the issue again.
func (d *Dispatcher) Redispatch(iss *Issue, proj Project) (*Issue, error) {
	if iss == nil {
		return nil, fmt.Errorf("issue required")
	}
	next := cloneIssue(iss)
	next.Frontmatter.Dispatched = nil
	next.Frontmatter.AgentSession = ""
	return d.dispatchInternal(next, proj)
}

func (d *Dispatcher) dispatchInternal(iss *Issue, proj Project) (*Issue, error) {
	if d.execs == nil {
		return nil, fmt.Errorf("executor config required")
	}

	role, targetSession := d.primaryMention(iss)
	if role == "" {
		role = strings.TrimSpace(proj.Executor)
		if role == "" {
			role = strings.TrimSpace(d.execs.Default)
		}
	}

	executor, ok := d.execs.ByName[role]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrExecutorNotConfigured, role)
	}

	sessionID := strings.TrimSpace(targetSession)
	if sessionID == "" {
		cwd := strings.TrimSpace(proj.Cwd)
		if cwd == "" {
			return nil, fmt.Errorf("project %q has no cwd set", proj.Name)
		}
		candidates := d.reg.IdleSessions(role, cwd)
		if len(candidates) > 0 {
			sessionID = candidates[0].ID
		} else {
			newID, err := d.run.Spawn(role, cwd, executor.Command)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrSpawnFailed, err)
			}
			sessionID = newID
		}
	}

	if err := d.run.Send(sessionID, buildInitialPrompt(iss.Path)); err != nil {
		return nil, fmt.Errorf("%w: send prompt: %v", ErrSpawnFailed, err)
	}

	next := cloneIssue(iss)
	now := d.now()
	next.Frontmatter.Dispatched = &now
	next.Frontmatter.AgentSession = sessionID
	return next, nil
}

func (d *Dispatcher) primaryMention(iss *Issue) (role, session string) {
	if iss == nil || len(iss.Mentions) == 0 {
		return "", ""
	}
	return iss.Mentions[0].Role, iss.Mentions[0].Session
}

func buildInitialPrompt(path string) string {
	return strings.TrimSpace(fmt.Sprintf(`
Your task is described in this file: %s
Read it, do the work, and edit the file as you progress.
When finished, set `+"`done: <ISO8601 timestamp>`"+` in the frontmatter.
`, path)) + "\n"
}
