package work

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// SessionRunner is the tmux side of starting Calendar-owned Work.
type SessionRunner interface {
	Spawn(role, cwd, command string) (string, error)
	SendWhenReady(sessionID, command, text string) error
	Abort(sessionID string) error
}

var (
	ErrAlreadyStarted        = errors.New("work item already started")
	ErrExecutorNotConfigured = errors.New("executor not configured")
	ErrSpawnFailed           = errors.New("spawn failed")
)

// Launcher starts Calendar-owned Work in a fresh delegated executor session.
type Launcher struct {
	run   SessionRunner
	execs *ExecutorConfig
	now   func() time.Time
}

func NewLauncher(run SessionRunner, execs *ExecutorConfig) *Launcher {
	return &Launcher{
		run:   run,
		execs: execs,
		now:   time.Now,
	}
}

// StartDedicated always spawns a fresh configured delegated executor session.
// The caller is responsible for persisting the returned started metadata.
func (l *Launcher) StartDedicated(item *Item, cwd string) (*Item, error) {
	if item == nil {
		return nil, fmt.Errorf("work item required")
	}
	if item.Frontmatter.Started != nil {
		return nil, ErrAlreadyStarted
	}
	if l.execs == nil {
		return nil, fmt.Errorf("executor config required")
	}

	role := strings.TrimSpace(l.execs.DelegatedExecutor)
	executor, ok := l.execs.ByName[role]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrExecutorNotConfigured, role)
	}

	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return nil, fmt.Errorf("work cwd required")
	}
	sessionID, err := l.run.Spawn(role, cwd, executor.Command)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSpawnFailed, err)
	}

	prompt := buildInitialPrompt(item.Path)
	if err := l.run.SendWhenReady(sessionID, executor.Command, prompt); err != nil {
		launchErr := fmt.Errorf("%w: send prompt: %v", ErrSpawnFailed, err)
		if abortErr := l.run.Abort(sessionID); abortErr != nil {
			return nil, fmt.Errorf("%w; abort fresh session: %v", launchErr, abortErr)
		}
		return nil, launchErr
	}

	next := cloneItem(item)
	now := l.now()
	next.Frontmatter.Started = &now
	next.Frontmatter.AgentSession = sessionID
	return next, nil
}

func buildInitialPrompt(path string) string {
	return strings.TrimSpace(fmt.Sprintf(`
	Your work item is described in this file: %s
	Read it, do the work, and edit the file as you progress.
When finished, set `+"`done: <ISO8601 timestamp>`"+` in the frontmatter.
`, path)) + "\n"
}
