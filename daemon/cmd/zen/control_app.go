package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

type controlWatcher interface {
	Agents() []*classifier.Agent
	GetAgent(id string) *classifier.Agent
	HasSession(target string) bool
	CreateSession(preferredTarget string, opts watcher.CreateSessionOptions) (string, error)
	UpdateAgentProgress(id string, progress classifier.AgentProgress) (*classifier.Agent, error)
	SendInput(sessionID, text string) error
	SendInputWhenReady(sessionID, command, text string) error
	KillSession(sessionID string) error
	CapturePaneContent(sessionID string) (string, error)
}

type controlApp struct {
	watcher    controlWatcher
	execs      *work.ExecutorConfig
	brainStore *brain.Store
	stateDir   string
}

func (a *controlApp) HandleControlRequest(req control.Request) control.Response {
	switch strings.TrimSpace(req.Type) {
	case "agent_list":
		return a.handleAgentList()
	case "agent_spawn":
		return a.handleAgentSpawn(req)
	case "agent_send":
		return a.handleAgentSend(req)
	case "agent_capture":
		return a.handleAgentCapture(req)
	case "agent_status":
		return a.handleAgentStatus(req)
	case "agent_progress":
		return a.handleAgentProgress(req)
	case "agent_close", "agent_kill":
		return a.handleAgentClose(req)
	case "brain_adapters":
		return a.handleBrainAdapters()
	case "brain_context":
		return a.handleBrainContext()
	case "brain_gc":
		return a.handleBrainGC()
	case "brain_set_adapter":
		return a.handleBrainSetAdapter(req)
	case "brain_workspace":
		if a == nil || a.brainStore == nil {
			return control.ErrorResponse("brain_unavailable", "Brain workspace is not configured.")
		}
		return control.Response{OK: true, Workspace: a.brainStore.WorkspacePath()}
	default:
		return control.ErrorResponse("unknown_request", fmt.Sprintf("Unknown control request: %s", req.Type))
	}
}

func (a *controlApp) handleAgentList() control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}
	agents := visibleControlAgents(a.watcher.Agents())
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].UpdatedAt.After(agents[j].UpdatedAt)
	})
	return control.Response{OK: true, Agents: agents}
}

func (a *controlApp) handleAgentSpawn(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}

	command, err := a.resolveSpawnCommand(req)
	if err != nil {
		return control.ErrorResponse("invalid_executor", err.Error())
	}

	cwd := strings.TrimSpace(req.Cwd)
	if cwd == "" {
		return control.ErrorResponse("missing_cwd", "Agent spawn requires an explicit working directory.")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = defaultAgentName(req.Executor, command)
	}

	prompt, err := spawnPrompt(req)
	if err != nil {
		return control.ErrorResponse("prompt_failed", err.Error())
	}

	agentID, err := a.watcher.CreateSession("", watcher.CreateSessionOptions{
		Cwd:         cwd,
		Command:     command,
		Name:        name,
		Detached:    true,
		Hidden:      req.Hidden,
		ProgressEnv: true,
		Delegated:   !req.Hidden,
		Env:         progressEnvForStateDir(a.stateDir),
	})
	if err != nil {
		return control.ErrorResponse("spawn_failed", err.Error())
	}

	if prompt != "" {
		if err := a.watcher.SendInputWhenReady(agentID, command, ensureTrailingNewline(prompt)); err != nil {
			return control.ErrorResponse("send_prompt_failed", err.Error())
		}
	}

	agent := a.watcher.GetAgent(agentID)
	if agent == nil {
		return control.Response{
			OK: true,
			Agent: &control.Agent{
				ID:        agentID,
				Name:      name,
				Status:    string(classifier.StateRunning),
				Cwd:       cwd,
				Command:   command,
				Hidden:    req.Hidden,
				Delegated: !req.Hidden,
			},
		}
	}
	out := controlAgent(agent)
	return control.Response{OK: true, Agent: &out}
}

func (a *controlApp) handleAgentSend(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		return control.ErrorResponse("missing_agent_id", "Agent id is required.")
	}
	agent := a.watcher.GetAgent(agentID)
	if agent != nil && !agent.Delegated && !agent.Hidden && !req.Force {
		return control.ErrorResponse("agent_not_delegated", "Refusing to send input to a session that was not created as a Brain delegated agent. Use --force only when you intentionally want to control this external session.")
	}
	text := req.Text
	if strings.TrimSpace(text) == "" && !req.Submit {
		return control.ErrorResponse("missing_text", "Text is required.")
	}
	if req.Submit {
		text = ensureTrailingNewline(text)
	}
	if agent != nil && !a.watcher.HasSession(agentID) {
		return control.ErrorResponse("agent_session_unavailable", "Agent is listed but the tmux target is no longer available. Refresh the agent list and spawn a new session if needed.")
	}
	if err := a.watcher.SendInput(agentID, text); err != nil {
		return control.ErrorResponse("send_failed", err.Error())
	}
	agent = a.watcher.GetAgent(agentID)
	if agent == nil {
		return control.Response{OK: true}
	}
	out := controlAgent(agent)
	return control.Response{OK: true, Agent: &out}
}

func (a *controlApp) handleAgentCapture(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		return control.ErrorResponse("missing_agent_id", "Agent id is required.")
	}
	if agent := a.watcher.GetAgent(agentID); agent != nil && !a.watcher.HasSession(agentID) {
		return control.ErrorResponse("agent_session_unavailable", "Agent is listed but the tmux target is no longer available. Refresh the agent list and spawn a new session if needed.")
	}
	text, err := a.watcher.CapturePaneContent(agentID)
	if err != nil {
		return control.ErrorResponse("capture_failed", err.Error())
	}
	agent := a.watcher.GetAgent(agentID)
	if agent == nil {
		return control.Response{OK: true, Text: text}
	}
	out := controlAgent(agent)
	return control.Response{OK: true, Text: text, Agent: &out}
}

func (a *controlApp) handleAgentStatus(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		return control.ErrorResponse("missing_agent_id", "Agent id is required.")
	}
	agent := a.watcher.GetAgent(agentID)
	if agent == nil {
		return control.ErrorResponse("agent_not_found", "Agent session was not found.")
	}
	out := controlAgent(agent)
	return control.Response{OK: true, Agent: &out}
}

func (a *controlApp) handleAgentProgress(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		return control.ErrorResponse("missing_agent_id", "Agent id is required.")
	}
	if agent := a.watcher.GetAgent(agentID); agent == nil {
		return control.ErrorResponse("agent_not_found", "Agent session was not found.")
	}
	progress, err := classifier.ValidateProgress(classifier.AgentProgress{
		Status:       req.Status,
		Phase:        req.Phase,
		Attention:    req.Attention,
		Summary:      req.Summary,
		TaskClass:    req.TaskClass,
		EventKind:    req.EventKind,
		DetailsJSON:  req.DetailsJSON,
		LeaseSeconds: req.LeaseSeconds,
	})
	if err != nil {
		return control.ErrorResponse("invalid_progress", err.Error())
	}
	agent, err := a.watcher.UpdateAgentProgress(agentID, progress)
	if err != nil {
		return control.ErrorResponse("progress_failed", err.Error())
	}
	out := controlAgent(agent)
	return control.Response{OK: true, Agent: &out}
}

func (a *controlApp) handleAgentClose(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Agent watcher is not running.")
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		return control.ErrorResponse("missing_agent_id", "Agent id is required.")
	}
	agent := a.watcher.GetAgent(agentID)
	if agent != nil && !agent.Delegated && !agent.Hidden && !req.Force {
		return control.ErrorResponse("agent_not_delegated", "Refusing to close a session that was not created as a Brain delegated agent. Use --force only when you intentionally want to close this external session.")
	}
	if agent != nil && !req.Force && closeRequiresForce(agent) {
		return control.ErrorResponse("agent_running_requires_force", "Agent is still running or unresolved. Send it a cancellation request first, wait for done/failed/blocked, or close with force.")
	}
	if err := a.watcher.KillSession(agentID); err != nil {
		return control.ErrorResponse("close_failed", err.Error())
	}
	if agent == nil {
		return control.Response{OK: true}
	}
	out := controlAgent(agent)
	out.Status = string(classifier.StateRemoved)
	return control.Response{OK: true, Agent: &out}
}

func (a *controlApp) handleBrainAdapters() control.Response {
	adapter, adapters, resp := a.brainAdapterSnapshot()
	if !resp.OK || resp.Error != nil {
		return resp
	}
	return control.Response{
		OK:       true,
		Adapter:  adapter,
		Adapters: adapters,
	}
}

func (a *controlApp) handleBrainContext() control.Response {
	if a == nil || a.brainStore == nil {
		return control.ErrorResponse("brain_unavailable", "Brain workspace is not configured.")
	}
	service := brain.NewService(a.brainStore, a.watcher, a.execs)
	context, err := service.Context(12)
	if err != nil {
		return control.ErrorResponse("brain_context_failed", err.Error())
	}
	return control.Response{
		OK:      true,
		Context: context,
	}
}

func (a *controlApp) handleBrainGC() control.Response {
	if a == nil || a.brainStore == nil {
		return control.ErrorResponse("brain_unavailable", "Brain workspace is not configured.")
	}
	service := brain.NewService(a.brainStore, a.watcher, a.execs)
	report, err := service.Housekeeping()
	if err != nil {
		return control.ErrorResponse("brain_gc_failed", err.Error())
	}
	return control.Response{
		OK:           true,
		Housekeeping: report,
	}
}

func (a *controlApp) handleBrainSetAdapter(req control.Request) control.Response {
	if a == nil || a.brainStore == nil {
		return control.ErrorResponse("brain_unavailable", "Brain workspace is not configured.")
	}
	adapterID := strings.TrimSpace(req.AdapterID)
	if adapterID == "" {
		return control.ErrorResponse("missing_adapter", "Brain adapter id is required.")
	}
	if locked := strings.TrimSpace(os.Getenv("ZEN_BRAIN_HOST_ADAPTER")); locked != "" && locked != adapterID {
		return control.ErrorResponse("brain_adapter_locked_by_env", "ZEN_BRAIN_HOST_ADAPTER is set; unset it before changing Brain adapter through zen.")
	}
	if a.execs == nil {
		return control.ErrorResponse("executors_unavailable", "Executor config is not available.")
	}
	adapter, ok := a.execs.AgentAdapter(adapterID)
	if !ok {
		return control.ErrorResponse("invalid_adapter", fmt.Sprintf("Brain adapter %q is not configured.", adapterID))
	}
	if a.watcher != nil {
		service := brain.NewService(a.brainStore, a.watcher, a.execs)
		if _, err := service.SetHostAdapter(adapter.ID); err != nil {
			return control.ErrorResponse("set_adapter_failed", err.Error())
		}
	} else {
		if err := a.brainStore.SetHostAdapterID(adapter.ID); err != nil {
			return control.ErrorResponse("set_adapter_failed", err.Error())
		}
	}
	return a.handleBrainAdapters()
}

func (a *controlApp) brainAdapterSnapshot() (*control.Adapter, []control.Adapter, control.Response) {
	if a == nil || a.brainStore == nil {
		return nil, nil, control.ErrorResponse("brain_unavailable", "Brain workspace is not configured.")
	}
	if a.execs == nil {
		return nil, nil, control.ErrorResponse("executors_unavailable", "Executor config is not available.")
	}
	current, ok := a.currentBrainAdapter()
	if !ok {
		return nil, nil, control.ErrorResponse("adapter_unavailable", "No Brain adapters are configured.")
	}
	adapters := a.execs.AgentAdapters()
	out := make([]control.Adapter, 0, len(adapters))
	for _, adapter := range adapters {
		adapter.Preferred = adapter.ID == current.ID
		out = append(out, controlAdapter(adapter))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Preferred != out[j].Preferred {
			return out[i].Preferred
		}
		return out[i].ID < out[j].ID
	})
	current.Preferred = true
	converted := controlAdapter(current)
	return &converted, out, control.Response{OK: true}
}

func (a *controlApp) currentBrainAdapter() (work.AgentAdapter, bool) {
	if a == nil || a.execs == nil {
		return work.AgentAdapter{}, false
	}
	if preferred := strings.TrimSpace(os.Getenv("ZEN_BRAIN_HOST_ADAPTER")); preferred != "" {
		return a.execs.AgentAdapter(preferred)
	}
	if a.brainStore != nil {
		if hostSession, err := a.brainStore.HostSession(); err == nil {
			if adapterID := strings.TrimSpace(hostSession.AdapterID); adapterID != "" {
				return a.execs.AgentAdapter(adapterID)
			}
		}
	}
	return a.execs.DefaultAgentAdapter()
}

func (a *controlApp) resolveSpawnCommand(req control.Request) (string, error) {
	if command := strings.TrimSpace(req.Command); command != "" {
		return command, nil
	}
	executorName := strings.TrimSpace(req.Executor)
	if executorName == "" {
		if brainExecutor, ok := a.brainCallerExecutor(req.AgentID); ok {
			executorName = brainExecutor
		}
	}
	if executorName == "" && a != nil && a.execs != nil {
		executorName = strings.TrimSpace(a.execs.Default)
	}
	if executorName == "" {
		executorName = "codex"
	}
	if a != nil && a.execs != nil {
		executor, ok := a.execs.ByName[executorName]
		if !ok {
			return "", fmt.Errorf("executor %q is not configured", executorName)
		}
		if command := strings.TrimSpace(executor.Command); command != "" {
			return command, nil
		}
		return executorName, nil
	}
	return executorName, nil
}

func (a *controlApp) brainCallerExecutor(agentID string) (string, bool) {
	if a == nil || a.brainStore == nil || a.execs == nil {
		return "", false
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", false
	}
	host, err := a.brainStore.HostSession()
	if err != nil || strings.TrimSpace(host.ID) == "" || strings.TrimSpace(host.ID) != agentID {
		return "", false
	}
	if adapterID := strings.TrimSpace(host.AdapterID); adapterID != "" {
		if _, ok := a.execs.ByName[adapterID]; ok {
			return adapterID, true
		}
	}
	if adapter, ok := a.currentBrainAdapter(); ok {
		return adapter.ID, true
	}
	return "", false
}

func visibleControlAgents(agents []*classifier.Agent) []control.Agent {
	out := make([]control.Agent, 0, len(agents))
	for _, agent := range agents {
		if agent == nil || agent.Hidden {
			continue
		}
		out = append(out, controlAgent(agent))
	}
	return out
}

func controlAgent(agent *classifier.Agent) control.Agent {
	if agent == nil {
		return control.Agent{}
	}
	return control.Agent{
		ID:                  agent.ID,
		Name:                agent.Name,
		Status:              string(agent.State),
		Summary:             agent.Summary,
		Phase:               agent.Phase,
		Attention:           agent.Attention,
		TaskClass:           agent.TaskClass,
		EventKind:           agent.EventKind,
		DetailsJSON:         agent.DetailsJSON,
		NeedsAttention:      agent.NeedsAttention,
		LastProgressAt:      agent.LastProgressAt,
		ExpectedNextCheckAt: agent.ExpectedNextCheckAt,
		LeaseSeconds:        agent.LeaseSeconds,
		Cwd:                 agent.Cwd,
		Command:             agent.Command,
		UpdatedAt:           agent.UpdatedAt,
		Hidden:              agent.Hidden,
		Delegated:           agent.Delegated,
	}
}

func controlAdapter(adapter work.AgentAdapter) control.Adapter {
	return control.Adapter{
		ID:       adapter.ID,
		Name:     adapter.Name,
		Provider: adapter.Provider,
		Command:  adapter.Command,
		Runtime:  adapter.Runtime,
		Capabilities: control.AdapterCapabilities{
			InteractiveTTY:   adapter.Capabilities.InteractiveTTY,
			StructuredEvents: adapter.Capabilities.StructuredEvents,
		},
		Preferred: adapter.Preferred,
	}
}

func spawnPrompt(req control.Request) (string, error) {
	prompt := strings.TrimSpace(req.Prompt)
	promptFile := strings.TrimSpace(req.PromptFile)
	if promptFile != "" {
		raw, err := os.ReadFile(promptFile)
		if err != nil {
			return "", fmt.Errorf("read prompt file: %w", err)
		}
		filePrompt := strings.TrimSpace(string(raw))
		switch {
		case prompt == "":
			prompt = filePrompt
		case filePrompt != "":
			prompt = strings.TrimSpace(prompt + "\n\n" + filePrompt)
		}
	}
	protocol := lifecycleProtocol(req.Profile)
	if prompt == "" {
		return protocol, nil
	}
	return strings.TrimSpace(prompt + "\n\n" + protocol), nil
}

func progressEnvForStateDir(stateDir string) map[string]string {
	env := map[string]string{
		"ZEN_AGENT_PROGRESS_CMD": "zen agent progress",
	}
	if stateDir = strings.TrimSpace(stateDir); stateDir != "" {
		env["ZEN_STATE_DIR"] = stateDir
	}
	return env
}

func closeRequiresForce(agent *classifier.Agent) bool {
	if agent == nil || !agent.Delegated || agent.Hidden {
		return false
	}
	switch agent.State {
	case classifier.StateDone, classifier.StateFailed, classifier.StateBlocked:
		return false
	default:
		return true
	}
}

func lifecycleProtocol(profile string) string {
	profile = normalizeAgentProfile(profile)
	return strings.TrimSpace(fmt.Sprintf(`Zen lifecycle protocol:
- Profile: %s.
- Treat the prompt as a loop contract: preserve the objective, acceptance criteria, safety constraints, verification, and expected report.
- Start lasting design or implementation work by identifying the core invariants. Prefer making invalid states unrepresentable over adding fallback paths.
- Use task classes consistently: exploration for research/scanning, mechanical_change for bounded repeatable edits, lasting_design for product semantics, data models, architecture, and long-lived code.
- Report progress through the Zen control plane only when your phase changes, when you take a meaningful long-running step, when you need attention, and when you finish.
- ZEN_AGENT_ID is already set for this session. ZEN_AGENT_PROGRESS_CMD contains the base command.
- Command shape:
  $ZEN_AGENT_PROGRESS_CMD --status running --phase working --attention none --summary "Short current work" --lease 300
- Semantic event shape:
  $ZEN_AGENT_PROGRESS_CMD --status running --phase planning --attention none --task-class lasting_design --event-kind invariant --summary "Defined durable state invariants" --details-json '{"invariants":["canonical source is X"]}' --lease 300
- Valid status values: running, done, failed, blocked.
- Valid phase values: starting, reading, planning, working, verifying, reporting.
- Valid attention values: none, done, blocked, failed, user_input, stale.
- Valid task classes: exploration, mechanical_change, lasting_design.
- Valid event kinds: progress, invariant, artifact, risk, needs_judgment, verification, done.
- Use attention "none" while you are making normal progress.
- Use attention "user_input" only when user input is required.
- Use event-kind "needs_judgment" when the next step depends on product values, user risk tolerance, or a choice between root design and patching.
- Use attention "done" with status "done" only after the requested work and feasible verification are complete.
- For implementation work, several minutes of reading before file edits is normal.`, profile))
}

func normalizeAgentProfile(profile string) string {
	switch strings.TrimSpace(profile) {
	case "quick", "research", "implementation", "long_running":
		return strings.TrimSpace(profile)
	default:
		return "implementation"
	}
}

func defaultAgentName(executor, command string) string {
	if executor := strings.TrimSpace(executor); executor != "" {
		return executor
	}
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return "Agent"
	}
	return fields[0]
}

func ensureTrailingNewline(value string) string {
	if strings.HasSuffix(value, "\n") || strings.HasSuffix(value, "\r") {
		return value
	}
	return value + "\n"
}
