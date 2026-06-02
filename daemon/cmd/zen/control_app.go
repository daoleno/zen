package main

import (
	"context"
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
	SendInput(sessionID, text string) error
	KillSession(sessionID string) error
	CapturePaneContent(sessionID string) (string, error)
}

type controlApp struct {
	watcher    controlWatcher
	execs      *work.ExecutorConfig
	brainStore *brain.Store
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
	case "brain_adapters":
		return a.handleBrainAdapters()
	case "brain_set_adapter":
		return a.handleBrainSetAdapter(req)
	case "brain_workspace":
		if a == nil || a.brainStore == nil {
			return control.ErrorResponse("brain_unavailable", "Brain workspace is not configured.")
		}
		return control.Response{OK: true, Workspace: a.brainStore.WorkspacePath()}
	case "brain_threads":
		return a.handleBrainThreads(req)
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
		Cwd:      cwd,
		Command:  command,
		Name:     name,
		Detached: true,
		Hidden:   req.Hidden,
	})
	if err != nil {
		return control.ErrorResponse("spawn_failed", err.Error())
	}

	if prompt != "" {
		if err := a.watcher.SendInput(agentID, ensureTrailingNewline(prompt)); err != nil {
			return control.ErrorResponse("send_prompt_failed", err.Error())
		}
	}

	agent := a.watcher.GetAgent(agentID)
	if agent == nil {
		return control.Response{
			OK: true,
			Agent: &control.Agent{
				ID:      agentID,
				Name:    name,
				Status:  string(classifier.StateRunning),
				Cwd:     cwd,
				Command: command,
				Hidden:  req.Hidden,
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
	text := req.Text
	if strings.TrimSpace(text) == "" {
		return control.ErrorResponse("missing_text", "Text is required.")
	}
	if req.Submit {
		text = ensureTrailingNewline(text)
	}
	if err := a.watcher.SendInput(agentID, text); err != nil {
		return control.ErrorResponse("send_failed", err.Error())
	}
	agent := a.watcher.GetAgent(agentID)
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

func (a *controlApp) handleBrainThreads(req control.Request) control.Response {
	if a == nil || a.execs == nil {
		return control.ErrorResponse("executors_unavailable", "Executor config is not available.")
	}
	adapter, ok := a.currentBrainAdapter()
	if requested := strings.TrimSpace(req.AdapterID); requested != "" {
		adapter, ok = a.execs.AgentAdapter(requested)
	}
	if !ok {
		return control.ErrorResponse("adapter_unavailable", "No Brain adapter is configured.")
	}
	provider, ok := work.NewNativeThreadProvider(adapter)
	if !ok {
		return control.ErrorResponse("native_threads_unavailable", fmt.Sprintf("Brain adapter %q does not expose native threads.", adapter.ID))
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	var (
		page work.NativeThreadPage
		err  error
	)
	if searchTerm := strings.TrimSpace(req.SearchTerm); searchTerm != "" && adapter.Capabilities.NativeSearch {
		page, err = provider.SearchThreads(context.Background(), work.NativeThreadSearchOptions{
			Cursor:     req.Cursor,
			Limit:      limit,
			Cwd:        req.Cwd,
			SearchTerm: searchTerm,
		})
	} else {
		var archived *bool
		if req.Archived {
			archived = &req.Archived
		}
		page, err = provider.ListThreads(context.Background(), work.NativeThreadListOptions{
			Cursor:     req.Cursor,
			Limit:      limit,
			Cwd:        req.Cwd,
			SearchTerm: req.SearchTerm,
			Archived:   archived,
		})
	}
	if err != nil {
		return control.ErrorResponse("brain_threads_failed", err.Error())
	}
	page.Threads = a.annotateBrainThreads(page.Threads)
	adapter.Preferred = true
	converted := controlAdapter(adapter)
	return control.Response{
		OK:              true,
		Adapter:         &converted,
		Threads:         controlThreads(page.Threads),
		NextCursor:      page.NextCursor,
		BackwardsCursor: page.BackwardsCursor,
	}
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

func (a *controlApp) annotateBrainThreads(threads []work.NativeThread) []work.NativeThread {
	if a == nil || a.brainStore == nil || len(threads) == 0 {
		return threads
	}
	ids := make([]string, 0, len(threads))
	for _, thread := range threads {
		if id := strings.TrimSpace(thread.ID); id != "" {
			ids = append(ids, id)
		}
	}
	metadata, err := a.brainStore.ThreadMetadataMap(ids)
	if err != nil {
		return threads
	}
	next := append([]work.NativeThread(nil), threads...)
	for index := range next {
		meta := metadata[strings.TrimSpace(next[index].ID)]
		if meta.Pinned {
			next[index].Pinned = true
		}
		if meta.ReviewState != "" {
			next[index].ReviewState = meta.ReviewState
		}
	}
	sort.SliceStable(next, func(i, j int) bool {
		if next[i].Pinned != next[j].Pinned {
			return next[i].Pinned
		}
		leftNeedsReview := brainThreadNeedsReview(next[i])
		rightNeedsReview := brainThreadNeedsReview(next[j])
		if leftNeedsReview != rightNeedsReview {
			return leftNeedsReview
		}
		return false
	})
	return next
}

func brainThreadNeedsReview(thread work.NativeThread) bool {
	switch strings.TrimSpace(thread.ReviewState) {
	case "needs_review", "reviewing":
		return true
	default:
		return false
	}
}

func (a *controlApp) resolveSpawnCommand(req control.Request) (string, error) {
	if command := strings.TrimSpace(req.Command); command != "" {
		return command, nil
	}
	executorName := strings.TrimSpace(req.Executor)
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
		ID:        agent.ID,
		Name:      agent.Name,
		Status:    string(agent.State),
		Summary:   agent.Summary,
		Cwd:       agent.Cwd,
		Command:   agent.Command,
		UpdatedAt: agent.UpdatedAt,
		Hidden:    agent.Hidden,
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
			NativeThreads:    adapter.Capabilities.NativeThreads,
			NativeSearch:     adapter.Capabilities.NativeSearch,
			NativePinning:    adapter.Capabilities.NativePinning,
			NativeArchive:    adapter.Capabilities.NativeArchive,
			NativeWorktrees:  adapter.Capabilities.NativeWorktrees,
			NativeFork:       adapter.Capabilities.NativeFork,
			NativeResume:     adapter.Capabilities.NativeResume,
			NativeGoals:      adapter.Capabilities.NativeGoals,
			NativeAutomation: adapter.Capabilities.NativeAutomation,
			InteractiveTTY:   adapter.Capabilities.InteractiveTTY,
			StructuredEvents: adapter.Capabilities.StructuredEvents,
		},
		Preferred: adapter.Preferred,
	}
}

func controlThreads(threads []work.NativeThread) []control.Thread {
	out := make([]control.Thread, 0, len(threads))
	for _, thread := range threads {
		out = append(out, control.Thread{
			ID:            thread.ID,
			NativeID:      thread.NativeID,
			Provider:      thread.Provider,
			SessionID:     thread.SessionID,
			ForkedFromID:  thread.ForkedFromID,
			Title:         thread.Title,
			Preview:       thread.Preview,
			Snippet:       thread.Snippet,
			Status:        thread.Status,
			Cwd:           thread.Cwd,
			Path:          thread.Path,
			Source:        thread.Source,
			ModelProvider: thread.ModelProvider,
			Ephemeral:     thread.Ephemeral,
			Archived:      thread.Archived,
			Pinned:        thread.Pinned,
			ReviewState:   thread.ReviewState,
			CreatedAt:     thread.CreatedAt,
			UpdatedAt:     thread.UpdatedAt,
		})
	}
	return out
}

func spawnPrompt(req control.Request) (string, error) {
	prompt := strings.TrimSpace(req.Prompt)
	promptFile := strings.TrimSpace(req.PromptFile)
	if promptFile == "" {
		return prompt, nil
	}
	raw, err := os.ReadFile(promptFile)
	if err != nil {
		return "", fmt.Errorf("read prompt file: %w", err)
	}
	filePrompt := strings.TrimSpace(string(raw))
	switch {
	case prompt == "":
		return filePrompt, nil
	case filePrompt == "":
		return prompt, nil
	default:
		return strings.TrimSpace(prompt + "\n\n" + filePrompt), nil
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
