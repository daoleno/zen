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
	CreateSession(preferredTarget string, opts watcher.CreateSessionOptions) (string, error)
	SendInput(sessionID, text string) error
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
