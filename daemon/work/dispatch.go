package work

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// SessionInfo describes a candidate tmux session that can receive work.
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

// SessionRunner is the tmux side of starting work: it can spawn and send text.
type SessionRunner interface {
	Spawn(role, cwd, command string) (string, error)
	Send(sessionID, text string) error
}

var (
	ErrAlreadyStarted        = errors.New("work item already started")
	ErrExecutorNotConfigured = errors.New("executor not configured")
	ErrSpawnFailed           = errors.New("spawn failed")
)

// Launcher picks a session and hands it the initial work prompt.
type Launcher struct {
	reg   SessionRegistry
	run   SessionRunner
	execs *ExecutorConfig
	now   func() time.Time
}

func NewLauncher(reg SessionRegistry, run SessionRunner, execs *ExecutorConfig) *Launcher {
	return &Launcher{
		reg:   reg,
		run:   run,
		execs: execs,
		now:   time.Now,
	}
}

// Start sends item to an agent and returns an updated work item with started
// metadata. The caller is responsible for persisting it.
func (l *Launcher) Start(item *Item, proj Project) (*Item, error) {
	if item == nil {
		return nil, fmt.Errorf("work item required")
	}
	if item.Frontmatter.Started != nil {
		return nil, ErrAlreadyStarted
	}
	return l.startInternal(item, proj)
}

// Rerun clears the started metadata and sends the work item again.
func (l *Launcher) Rerun(item *Item, proj Project) (*Item, error) {
	if item == nil {
		return nil, fmt.Errorf("work item required")
	}
	next := cloneItem(item)
	next.Frontmatter.Started = nil
	next.Frontmatter.AgentSession = ""
	return l.startInternal(next, proj)
}

func (l *Launcher) startInternal(item *Item, proj Project) (*Item, error) {
	if l.execs == nil {
		return nil, fmt.Errorf("executor config required")
	}

	role, targetSession := l.primaryMention(item)
	if role == "" {
		role = strings.TrimSpace(proj.Executor)
		if role == "" {
			role = strings.TrimSpace(l.execs.Default)
		}
	}

	executor, ok := l.execs.ByName[role]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrExecutorNotConfigured, role)
	}

	sessionID := strings.TrimSpace(targetSession)
	if sessionID == "" {
		cwd := strings.TrimSpace(proj.Cwd)
		if cwd == "" {
			return nil, fmt.Errorf("project %q has no cwd set", proj.Name)
		}
		candidates := l.reg.IdleSessions(role, cwd)
		if len(candidates) > 0 {
			sessionID = candidates[0].ID
		} else {
			newID, err := l.run.Spawn(role, cwd, executor.Command)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrSpawnFailed, err)
			}
			sessionID = newID
		}
	}

	if err := l.run.Send(sessionID, buildInitialPrompt(item.Path)); err != nil {
		return nil, fmt.Errorf("%w: send prompt: %v", ErrSpawnFailed, err)
	}

	next := cloneItem(item)
	now := l.now()
	next.Frontmatter.Started = &now
	next.Frontmatter.AgentSession = sessionID
	return next, nil
}

func (l *Launcher) primaryMention(item *Item) (role, session string) {
	if item == nil || len(item.Mentions) == 0 {
		return "", ""
	}
	return item.Mentions[0].Role, item.Mentions[0].Session
}

func buildInitialPrompt(path string) string {
	return strings.TrimSpace(fmt.Sprintf(`
	Your work item is described in this file: %s
	Read it, do the work, and edit the file as you progress.
When finished, set `+"`done: <ISO8601 timestamp>`"+` in the frontmatter.
`, path)) + "\n"
}
