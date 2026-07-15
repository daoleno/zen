package server

import (
	"path/filepath"
	"strings"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/work"
)

type agentSessionWireCapabilities struct {
	StructuredEvents bool `json:"structured_events"`
}

type agentSessionWire struct {
	*classifier.Agent
	Capabilities agentSessionWireCapabilities `json:"capabilities"`
}

func (s *Server) agentSessionWire(agent *classifier.Agent) *agentSessionWire {
	if agent == nil {
		return nil
	}
	return &agentSessionWire{
		Agent: agent,
		Capabilities: agentSessionWireCapabilities{
			StructuredEvents: s.agentSupportsStructuredEvents(agent),
		},
	}
}

func (s *Server) agentSessionsWire(agents []*classifier.Agent) []*agentSessionWire {
	if len(agents) == 0 {
		return nil
	}
	out := make([]*agentSessionWire, 0, len(agents))
	for _, agent := range agents {
		if wire := s.agentSessionWire(agent); wire != nil {
			out = append(out, wire)
		}
	}
	return out
}

func (s *Server) agentSupportsStructuredEvents(agent *classifier.Agent) bool {
	return s.structuredProviderForAgent(agent) != ""
}

func (s *Server) structuredProviderForAgent(agent *classifier.Agent) string {
	if agent == nil {
		return ""
	}
	// A real process command outranks the display title. Titles are user-facing
	// and may mention a provider without the shell actually running one.
	provider := work.InferAgentProvider(agent.Command)
	if strings.TrimSpace(agent.Command) == "" {
		provider = work.InferAgentProvider(agent.Name)
	}
	if provider != "" {
		portable := work.NewAgentExecutor(provider, work.Executor{
			Name:    provider,
			Kind:    provider,
			Command: agent.Command,
		})
		if portable.Capabilities.StructuredEvents {
			return portable.Provider
		}
	}
	if s == nil || s.execs == nil {
		return ""
	}
	for name, configured := range s.execs.ByName {
		if !configuredExecutorMatchesAgent(name, configured, agent) {
			continue
		}
		portable := work.NewAgentExecutor(name, configured)
		if portable.Capabilities.StructuredEvents {
			return portable.Provider
		}
	}
	return ""
}

func configuredExecutorMatchesAgent(name string, configured work.Executor, agent *classifier.Agent) bool {
	if agent == nil {
		return false
	}
	agentCommand := strings.TrimSpace(agent.Command)
	configuredCommand := strings.TrimSpace(configured.Command)
	if agentCommand != "" && configuredCommand != "" && agentCommand == configuredCommand {
		return true
	}
	agentExecutable := agentCommandExecutable(agentCommand)
	configuredExecutable := agentCommandExecutable(configuredCommand)
	if agentExecutable != "" && agentExecutable == configuredExecutable && !ambiguousAgentExecutable(agentExecutable) {
		return true
	}
	if agentCommand != "" {
		configuredName := strings.TrimSpace(name)
		if configuredName == "" {
			configuredName = strings.TrimSpace(configured.Name)
		}
		return agentExecutable != "" && agentExecutable == filepath.Base(configuredName)
	}
	configuredName := strings.TrimSpace(name)
	if configuredName == "" {
		configuredName = strings.TrimSpace(configured.Name)
	}
	agentName := strings.TrimSpace(agent.Name)
	if agentName != "" && (agentName == configuredName || agentName == strings.TrimSpace(configured.Name)) {
		return true
	}
	return false
}

func agentCommandExecutable(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(strings.Trim(fields[0], `"'`))
}

func ambiguousAgentExecutable(executable string) bool {
	switch strings.ToLower(strings.TrimSpace(executable)) {
	case "bash", "bun", "deno", "fish", "node", "python", "python3", "sh", "zsh":
		return true
	default:
		return false
	}
}
