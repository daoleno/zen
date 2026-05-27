package brain

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

type Watcher interface {
	Agents() []*classifier.Agent
	GetAgent(id string) *classifier.Agent
	HasSession(target string) bool
	CreateSession(preferredTarget string, opts watcher.CreateSessionOptions) (string, error)
	SendInput(sessionID, text string) error
}

type Service struct {
	store   *Store
	watcher Watcher
	execs   *work.ExecutorConfig
	now     func() time.Time
}

func NewService(store *Store, watcher Watcher, execs *work.ExecutorConfig) *Service {
	return &Service{
		store:   store,
		watcher: watcher,
		execs:   execs,
		now:     time.Now,
	}
}

func (s *Service) Snapshot() (Snapshot, error) {
	snapshot, err := s.store.Snapshot()
	if err != nil {
		return Snapshot{}, err
	}
	host, err := s.ensureHostAgent()
	if err != nil {
		return Snapshot{}, err
	}
	if host.ID != "" {
		snapshot.HostAgent = &host
	}
	snapshot.Agents = s.agentRefs(host.ID)
	return snapshot, nil
}

func (s *Service) ensureHostAgent() (AgentRef, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return AgentRef{}, nil
	}
	hostSession, err := s.store.HostSession()
	if err != nil {
		return AgentRef{}, err
	}
	executor := s.hostExecutor()
	command := s.hostCommand(executor)
	if id := strings.TrimSpace(hostSession.ID); id != "" && s.watcher.HasSession(id) {
		if agent := s.watcher.GetAgent(id); agent != nil {
			if s.hostAgentMatches(agent, command) {
				return agentRefFromClassifier(agent), nil
			}
		} else {
			return AgentRef{
				ID:      id,
				Name:    "Brain",
				Status:  string(classifier.StateRunning),
				Summary: "Session starting",
				Cwd:     s.brainWorkspace(),
				Command: command,
				Updated: firstNonZeroTime(hostSession.UpdatedAt, s.now().UTC()),
				Hidden:  true,
			}, nil
		}
	}
	if id := strings.TrimSpace(hostSession.ID); id != "" {
		// Host record exists but tmux session is gone. Create a replacement.
		// Continue below.
	} else {
		// No recorded host yet.
	}

	agentID, err := s.watcher.CreateSession("", watcher.CreateSessionOptions{
		Cwd:      s.brainWorkspace(),
		Command:  command,
		Name:     "Brain",
		Detached: true,
		Hidden:   true,
	})
	if err != nil {
		return AgentRef{}, err
	}
	if err := s.store.SetHostSessionID(agentID); err != nil {
		return AgentRef{}, err
	}
	if prompt := s.hostBootstrapPrompt(); prompt != "" {
		_ = s.watcher.SendInput(agentID, prompt+"\n")
	}
	if agent := s.watcher.GetAgent(agentID); agent != nil {
		return agentRefFromClassifier(agent), nil
	}
	return AgentRef{
		ID:      agentID,
		Name:    "Brain",
		Status:  string(classifier.StateRunning),
		Summary: "Session starting",
		Cwd:     s.brainWorkspace(),
		Command: command,
		Updated: s.now().UTC(),
		Hidden:  true,
	}, nil
}

func (s *Service) hostExecutor() string {
	if s.execs != nil {
		if _, ok := s.execs.ByName["codex"]; !ok && strings.TrimSpace(s.execs.Default) != "" {
			return strings.TrimSpace(s.execs.Default)
		}
	}
	return "codex"
}

func (s *Service) hostAgentMatches(agent *classifier.Agent, command string) bool {
	if agent == nil || !agent.Hidden {
		return false
	}
	expectedProvider := providerName(command)
	if expectedProvider == "" {
		return false
	}
	return providerName(agent.Command) == expectedProvider
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func (s *Service) hostCommand(executor string) string {
	executor = strings.TrimSpace(executor)
	if executor == "" {
		executor = "codex"
	}
	command := s.executorCommand(executor)
	name := providerName(command)
	if name == "" {
		name = providerName(executor)
	}
	workspace := s.brainWorkspace()
	switch name {
	case "codex":
		args := []string{command}
		if !strings.Contains(command, "--no-alt-screen") {
			args = append(args, "--no-alt-screen")
		}
		if workspace != "" && !strings.Contains(command, " -C ") && !strings.Contains(command, " --cd ") {
			args = append(args, "-C", shellQuote(workspace))
		}
		return strings.Join(args, " ")
	case "claude":
		if workspace != "" && !strings.Contains(command, " --add-dir ") {
			return strings.TrimSpace(command + " --add-dir " + shellQuote(workspace))
		}
		return command
	default:
		return command
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func providerName(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	base := strings.ToLower(fields[0])
	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}
	switch {
	case strings.Contains(base, "codex"):
		return "codex"
	case strings.Contains(base, "claude"):
		return "claude"
	default:
		return ""
	}
}

func (s *Service) hostBootstrapPrompt() string {
	snapshot, err := s.store.Snapshot()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf(`
You are Brain inside zen, the user's private second brain and agent orchestrator.

Work as a warm, direct, capable chat assistant. Reply in the user's language unless they ask otherwise.

Brain workspace: %s

Durable state rules:
- Keep long-term memory in memory.md.
- Keep personality, preferences, and profile notes in profile.md.
- Use files in this workspace for plans, inbox notes, reminders, and follow-up state.
- Do not use arbitrary project repositories as Brain's default workspace.

Agent orchestration rules:
- You are running in a real tmux agent session.
- The zen app sends user messages directly into this session.
- Only create or ask for a visible delegated agent session when the user explicitly asks you to delegate real work.
- Use the zen binary to spawn, send to, and inspect delegated agents.
- Prefer zen agent spawn with a delegation note in the Brain workspace.
- For ordinary chat, remembering, planning, organizing, and reminders, stay in this Brain session and update local files when useful.

Current personality:
%s

Current profile notes:
%s

Current memory:
%s
`, snapshot.Workspace, strings.TrimSpace(snapshot.Personality), strings.TrimSpace(snapshot.Profile), strings.TrimSpace(snapshot.Memory)))
}

func (s *Service) agentRefs(hostID string) []AgentRef {
	if s == nil || s.watcher == nil {
		return []AgentRef{}
	}
	agents := s.watcher.Agents()
	out := make([]AgentRef, 0, len(agents))
	for _, agent := range agents {
		if agent == nil {
			continue
		}
		if agent.Hidden || (hostID != "" && agent.ID == hostID) {
			continue
		}
		out = append(out, agentRefFromClassifier(agent))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Updated.After(out[j].Updated)
	})
	return out
}

func agentRefFromClassifier(agent *classifier.Agent) AgentRef {
	if agent == nil {
		return AgentRef{}
	}
	return AgentRef{
		ID:      agent.ID,
		Name:    agent.Name,
		Status:  string(agent.State),
		Summary: agent.Summary,
		Cwd:     agent.Cwd,
		Command: agent.Command,
		Updated: agent.UpdatedAt,
		Hidden:  agent.Hidden,
	}
}

func (s *Service) defaultExecutor() string {
	if s.execs != nil && strings.TrimSpace(s.execs.Default) != "" {
		return strings.TrimSpace(s.execs.Default)
	}
	if s.execs != nil {
		if _, ok := s.execs.ByName["codex"]; ok {
			return "codex"
		}
	}
	return "codex"
}

func (s *Service) executorCommand(name string) string {
	if s.execs != nil {
		if executor, ok := s.execs.ByName[name]; ok && strings.TrimSpace(executor.Command) != "" {
			return strings.TrimSpace(executor.Command)
		}
	}
	return name
}

func (s *Service) brainWorkspace() string {
	if s != nil && s.store != nil {
		return s.store.WorkspacePath()
	}
	return ""
}
