package brain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

var (
	ErrAdapterNotConfigured = errors.New("brain adapter is not configured")
	ErrAdapterLockedByEnv   = errors.New("brain adapter is locked by ZEN_BRAIN_HOST_ADAPTER")
)

const (
	claudePermissionBypassFlag = "--permission-mode dontAsk"
	codexFullAuthorizationFlag = "--dangerously-bypass-approvals-and-sandbox"
)

type Watcher interface {
	Agents() []*classifier.Agent
	GetAgent(id string) *classifier.Agent
	HasSession(target string) bool
	CreateSession(preferredTarget string, opts watcher.CreateSessionOptions) (string, error)
	SendInput(sessionID, text string) error
	SendInputWhenReady(sessionID, command, text string) error
	KillSession(sessionID string) error
}

type Service struct {
	store   *Store
	watcher Watcher
	execs   *work.ExecutorConfig
	now     func() time.Time
}

type HeartbeatEvent struct {
	Reason    string
	AgentID   string
	Name      string
	Status    string
	Summary   string
	Cwd       string
	Phase     string
	Attention string
	OldState  string
	NewState  string
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
	chatThreadID, err := s.store.ChatThreadID()
	if err != nil {
		return Snapshot{}, err
	}
	hostAdapter := s.hostAdapter()
	host, err := s.ensureHostAgent(hostAdapter)
	if err != nil {
		return Snapshot{}, err
	}
	if host.ID != "" {
		snapshot.HostAgent = &host
	}
	hostAdapter.Preferred = true
	snapshot.HostAdapter = &hostAdapter
	snapshot.Adapters = s.agentAdapters(hostAdapter.ID)
	snapshot.ChatThreadID = chatThreadID
	if chatThreadID != "" && host.ID != "" {
		_ = s.store.TouchChatSession(chatThreadID, host.ID)
	}
	snapshot.Agents = s.agentRefs(host.ID)
	return snapshot, nil
}

func (s *Service) WorkspaceTree() (WorkspaceTree, error) {
	if s == nil || s.store == nil {
		return WorkspaceTree{}, fmt.Errorf("brain store is not configured")
	}
	return s.store.WorkspaceTree()
}

func (s *Service) ReadWorkspaceFile(path string) (WorkspaceFile, error) {
	if s == nil || s.store == nil {
		return WorkspaceFile{}, fmt.Errorf("brain store is not configured")
	}
	return s.store.ReadWorkspaceFile(path)
}

func (s *Service) SetHostAdapter(adapterID string) (Snapshot, error) {
	if s == nil || s.store == nil {
		return Snapshot{}, fmt.Errorf("brain store is not configured")
	}
	adapterID = strings.TrimSpace(adapterID)
	if adapterID == "" {
		return Snapshot{}, fmt.Errorf("brain adapter id is required")
	}
	if locked := strings.TrimSpace(os.Getenv("ZEN_BRAIN_HOST_ADAPTER")); locked != "" && locked != adapterID {
		return Snapshot{}, ErrAdapterLockedByEnv
	}
	if s.execs == nil {
		return Snapshot{}, ErrAdapterNotConfigured
	}
	adapter, ok := s.execs.AgentAdapter(adapterID)
	if !ok {
		return Snapshot{}, fmt.Errorf("%w: %s", ErrAdapterNotConfigured, adapterID)
	}
	if err := s.store.SetHostAdapterID(adapter.ID); err != nil {
		return Snapshot{}, err
	}
	return s.Snapshot()
}

func (s *Service) Heartbeat(event HeartbeatEvent) (bool, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return false, nil
	}
	reason := strings.TrimSpace(event.Reason)
	if reason == "" {
		return false, nil
	}
	hostSession, err := s.store.HostSession()
	if err != nil {
		return false, err
	}
	hostID := strings.TrimSpace(hostSession.ID)
	if hostID == "" || !s.watcher.HasSession(hostID) {
		return false, nil
	}
	message := formatHeartbeatWake(event)
	if message == "" {
		return false, nil
	}
	if err := s.watcher.SendInput(hostID, message+"\n"); err != nil {
		return false, err
	}
	return true, nil
}

func formatHeartbeatWake(event HeartbeatEvent) string {
	reason := strings.TrimSpace(event.Reason)
	if reason == "" {
		return ""
	}
	lines := []string{
		"Heartbeat wake:",
		"reason: " + reason,
	}
	appendHeartbeatField := func(label, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			lines = append(lines, label+": "+value)
		}
	}
	appendHeartbeatField("agent_id", event.AgentID)
	appendHeartbeatField("agent_name", event.Name)
	appendHeartbeatField("status", event.Status)
	appendHeartbeatField("phase", event.Phase)
	appendHeartbeatField("attention", event.Attention)
	appendHeartbeatField("old_state", event.OldState)
	appendHeartbeatField("new_state", event.NewState)
	appendHeartbeatField("workspace", event.Cwd)
	appendHeartbeatField("summary", event.Summary)
	lines = append(lines,
		"",
		"Inspect the changed session if useful. Continue low-risk next steps autonomously; reuse the same delegated session while it still belongs to the larger task, and close it only after the task is complete and its result is recorded or reported; if blocked, consolidate options and a recommendation for the user.",
	)
	return strings.Join(lines, "\n")
}

func (s *Service) ensureHostAgent(adapter work.AgentAdapter) (AgentRef, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return AgentRef{}, nil
	}
	hostSession, err := s.store.HostSession()
	if err != nil {
		return AgentRef{}, err
	}
	command := s.hostCommand(adapter)
	if id := strings.TrimSpace(hostSession.ID); id != "" && s.watcher.HasSession(id) {
		if agent := s.watcher.GetAgent(id); agent != nil {
			if s.hostAgentMatches(agent, adapter) {
				if strings.TrimSpace(hostSession.AdapterID) != adapter.ID {
					if err := s.store.SetHostSession(id, adapter.ID); err != nil {
						return AgentRef{}, err
					}
				}
				return agentRefFromClassifier(agent), nil
			}
			_ = s.watcher.KillSession(id)
		} else {
			if strings.TrimSpace(hostSession.AdapterID) != adapter.ID {
				if err := s.store.SetHostSession(id, adapter.ID); err != nil {
					return AgentRef{}, err
				}
			}
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
	if err := s.store.SetHostSession(agentID, adapter.ID); err != nil {
		return AgentRef{}, err
	}
	if prompt := s.hostBootstrapPrompt(adapter); prompt != "" {
		_ = s.watcher.SendInputWhenReady(agentID, command, prompt+"\n")
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

func (s *Service) hostAdapter() work.AgentAdapter {
	preferred := strings.TrimSpace(os.Getenv("ZEN_BRAIN_HOST_ADAPTER"))
	if preferred == "" && s != nil && s.store != nil {
		if hostSession, err := s.store.HostSession(); err == nil {
			preferred = strings.TrimSpace(hostSession.AdapterID)
		}
	}
	if s != nil && s.execs != nil {
		if preferred != "" {
			if adapter, ok := s.execs.AgentAdapter(preferred); ok {
				return adapter
			}
		}
		if adapter, ok := s.execs.DefaultAgentAdapter(); ok {
			return adapter
		}
	}
	return work.NewAgentAdapter("claude", work.Executor{Name: "claude", Command: "claude", Kind: "claude", Runtime: work.AgentRuntimeTmux})
}

func (s *Service) agentAdapters(hostAdapterID string) []work.AgentAdapter {
	if s == nil || s.execs == nil {
		if hostAdapterID == "" {
			hostAdapterID = "claude"
		}
		adapter := work.NewAgentAdapter(hostAdapterID, work.Executor{Name: hostAdapterID, Command: hostAdapterID})
		adapter.Preferred = true
		return []work.AgentAdapter{adapter}
	}
	adapters := s.execs.AgentAdapters()
	if len(adapters) == 0 {
		if hostAdapterID == "" {
			hostAdapterID = "claude"
		}
		adapter := work.NewAgentAdapter(hostAdapterID, work.Executor{Name: hostAdapterID, Command: hostAdapterID})
		adapter.Preferred = true
		return []work.AgentAdapter{adapter}
	}
	for i := range adapters {
		adapters[i].Preferred = adapters[i].ID == hostAdapterID
	}
	sort.Slice(adapters, func(i, j int) bool {
		if adapters[i].Preferred != adapters[j].Preferred {
			return adapters[i].Preferred
		}
		return adapters[i].ID < adapters[j].ID
	})
	return adapters
}

func (s *Service) hostAgentMatches(agent *classifier.Agent, adapter work.AgentAdapter) bool {
	if agent == nil || !agent.Hidden {
		return false
	}
	expectedProvider := strings.TrimSpace(adapter.Provider)
	if expectedProvider != "" && expectedProvider != work.AgentProviderCustom {
		if work.InferAgentProvider(agent.Command) != expectedProvider {
			return false
		}
		return true
	}
	return commandBase(agent.Command) == commandBase(adapter.Command)
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func (s *Service) hostCommand(adapter work.AgentAdapter) string {
	command := strings.TrimSpace(adapter.Command)
	if command == "" {
		command = strings.TrimSpace(adapter.ID)
	}
	if command == "" {
		command = "codex"
	}
	provider := strings.TrimSpace(adapter.Provider)
	if provider == "" || provider == work.AgentProviderCustom {
		provider = work.InferAgentProvider(command, adapter.ID)
	}
	workspace := s.brainWorkspace()
	switch provider {
	case "codex":
		args := []string{command}
		if !codexCommandHasFullAuthorization(command) {
			args = append(args, codexFullAuthorizationFlag)
		}
		if !strings.Contains(command, "--no-alt-screen") {
			args = append(args, "--no-alt-screen")
		}
		if workspace != "" && !strings.Contains(command, " -C ") && !strings.Contains(command, " --cd ") {
			args = append(args, "-C", shellQuote(workspace))
		}
		return withZenCLIOnPath(strings.Join(args, " "))
	case "claude":
		if !claudeCommandHasPermissionBypass(command) {
			command = strings.TrimSpace(command + " " + claudePermissionBypassFlag)
		}
		if workspace != "" && !strings.Contains(command, " --add-dir ") {
			return withZenCLIOnPath(strings.TrimSpace(command + " --add-dir " + shellQuote(workspace)))
		}
		return withZenCLIOnPath(command)
	default:
		return withZenCLIOnPath(command)
	}
}

func withZenCLIOnPath(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return command
	}
	dir := zenExecutableDir()
	if dir == "" || pathContainsDir(os.Getenv("PATH"), dir) {
		return command
	}
	return "env PATH=" + shellQuote(dir) + ":$PATH " + command
}

func zenExecutableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return ""
	}
	base := strings.ToLower(commandBase(exe))
	if base != "zen" && base != "zen.exe" {
		return ""
	}
	return strings.TrimSpace(filepath.Dir(exe))
}

func pathContainsDir(pathValue, dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	for _, entry := range filepath.SplitList(pathValue) {
		if strings.TrimSpace(entry) == dir {
			return true
		}
	}
	return false
}

func codexCommandHasFullAuthorization(command string) bool {
	return strings.Contains(command, codexFullAuthorizationFlag)
}

func claudeCommandHasPermissionBypass(command string) bool {
	return strings.Contains(command, claudePermissionBypassFlag)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func commandBase(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	base := strings.ToLower(fields[0])
	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}
	return base
}

func (s *Service) hostBootstrapPrompt(adapter work.AgentAdapter) string {
	snapshot, err := s.store.Snapshot()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf(`
You are Brain inside zen, the user's private second brain and agent orchestrator.

Work as a warm, direct, capable chat assistant. Reply in the user's language unless they ask otherwise.

Brain workspace: %s
Active adapter: %s (%s via %s)
Adapter capabilities: %s

Durable state rules:
- Keep long-term memory in memory.md.
- Keep personality, preferences, and profile notes in profile.md.
- Use files in this workspace for plans, inbox notes, reminders, and follow-up state.
- Do not use arbitrary project repositories as Brain's default workspace.

Agent orchestration rules:
- You are running in a real tmux agent session.
- This Brain host is launched with the most permissive available non-interactive authorization mode for its adapter.
- The zen app sends user messages directly into this session.
- Treat the adapter as replaceable; do not make Brain's plans depend on Codex-only or Claude-only behavior unless the user asks for that adapter specifically.
- Brain is the user's scheduler: reduce decision load. For concrete work that needs repository/tool execution, independent progress, parallelism, or follow-up, proactively create or reuse a visible delegated agent session; stay in Brain for chat, memory, synthesis, reminders, and decisions that fit the current context.
- For a single larger task, prefer reusing the same delegated agent session across stages. Send follow-up instructions to that session until the task is genuinely complete. Open a separate delegated session only when the work is meaningfully independent, benefits from parallelism, needs a different repository/context, or the current session is blocked or unusable.
- Use the zen binary to spawn, send to, and inspect delegated agents. When delegating, write a short note with workspace, objective, context, acceptance criteria, safety constraints, and expected report.
- Zen CLI quick reference:
  - %s agent list --json lists visible sessions; only sessions with delegated=true are Brain-owned.
  - %s agent spawn -name "<name>" -executor <executor> -cwd <workspace> -prompt "<task>" creates a visible delegated agent.
  - %s agent capture -id <agent_id> --json inspects a delegated agent.
  - %s agent send -id <agent_id> -text "<message>" --submit=true continues a delegated agent.
  - %s agent close -id <agent_id> closes a delegated agent after the larger task is complete and its result is recorded or reported.
- Delegated agent lifecycle: keep ownership from spawn through inspection, follow-up, result consolidation, and close. Do not close a delegated session merely because a small stage finished; close it when the larger task is complete or you have intentionally moved the remaining work elsewhere.
- Never close, kill, rename, repurpose, or otherwise manage sessions whose agent list entry does not have delegated=true. Those belong to the user or another tool.
- Keep orchestration principles in Markdown, prompts, and agent instructions. Product code should provide tools, context, persistence, visibility, and safety boundaries rather than rigid workflow gates.
- Treat Heartbeat wake messages as compact actionable deltas; inspect only what is needed, then act, summarize, or sleep.
- Continue low-risk next steps autonomously. Ask only when critical context is missing, an action is high-risk or irreversible, credentials/permissions are needed, or the decision depends on the user's values; when blocked, consolidate options and a recommendation.

Current personality:
%s

Current profile notes:
%s

Current memory:
%s
`, snapshot.Workspace, adapter.ID, adapter.Provider, adapter.Runtime, adapterCapabilitiesSummary(adapter.Capabilities), zenCLICommand(), zenCLICommand(), zenCLICommand(), zenCLICommand(), zenCLICommand(), strings.TrimSpace(snapshot.Personality), strings.TrimSpace(snapshot.Profile), strings.TrimSpace(snapshot.Memory)))
}

func zenCLICommand() string {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return "zen"
	}
	if strings.ContainsAny(exe, " \t'") {
		return shellQuote(exe)
	}
	return exe
}

func adapterCapabilitiesSummary(caps work.AgentCapabilities) string {
	parts := []string{}
	if caps.InteractiveTTY {
		parts = append(parts, "interactive_tty")
	}
	if caps.StructuredEvents {
		parts = append(parts, "structured_events")
	}
	if len(parts) == 0 {
		return "none declared"
	}
	return strings.Join(parts, ", ")
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
		ID:        agent.ID,
		Name:      agent.Name,
		Status:    string(agent.State),
		Summary:   agent.Summary,
		Cwd:       agent.Cwd,
		Command:   agent.Command,
		Updated:   agent.UpdatedAt,
		Hidden:    agent.Hidden,
		Delegated: agent.Delegated,
	}
}

func (s *Service) brainWorkspace() string {
	if s != nil && s.store != nil {
		return s.store.WorkspacePath()
	}
	return ""
}
