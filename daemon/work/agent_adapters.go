package work

import (
	"path/filepath"
	"sort"
	"strings"
)

const (
	AgentRuntimeTmux    = "tmux"
	AgentProviderCodex  = "codex"
	AgentProviderClaude = "claude"
	AgentProviderCustom = "custom"
)

// AgentCapabilities describes capabilities Brain can delegate to an adapter.
// Native capabilities are tool-owned features. Runtime capabilities are the
// portable substrate Brain can rely on even when the tool has no native thread
// model.
type AgentCapabilities struct {
	NativeThreads    bool `json:"native_threads"`
	NativeSearch     bool `json:"native_search"`
	NativePinning    bool `json:"native_pinning"`
	NativeArchive    bool `json:"native_archive"`
	NativeWorktrees  bool `json:"native_worktrees"`
	NativeFork       bool `json:"native_fork"`
	NativeResume     bool `json:"native_resume"`
	NativeGoals      bool `json:"native_goals"`
	NativeAutomation bool `json:"native_automation"`
	InteractiveTTY   bool `json:"interactive_tty"`
	StructuredEvents bool `json:"structured_events"`
}

// AgentAdapter is the portable Brain-facing view of a configured executor.
type AgentAdapter struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Provider     string            `json:"provider"`
	Command      string            `json:"command,omitempty"`
	Runtime      string            `json:"runtime"`
	Capabilities AgentCapabilities `json:"capabilities"`
	Preferred    bool              `json:"preferred,omitempty"`
}

// AgentAdapters returns all configured executors as portable adapter records.
func (c *ExecutorConfig) AgentAdapters() []AgentAdapter {
	if c == nil || len(c.ByName) == 0 {
		return nil
	}
	out := make([]AgentAdapter, 0, len(c.ByName))
	for name, executor := range c.ByName {
		out = append(out, NewAgentAdapter(name, executor))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// AgentAdapter returns one configured executor as a portable adapter record.
func (c *ExecutorConfig) AgentAdapter(name string) (AgentAdapter, bool) {
	if c == nil {
		return AgentAdapter{}, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return AgentAdapter{}, false
	}
	executor, ok := c.ByName[name]
	if !ok {
		return AgentAdapter{}, false
	}
	return NewAgentAdapter(name, executor), true
}

// DefaultAgentAdapter returns the configured default executor as an adapter.
func (c *ExecutorConfig) DefaultAgentAdapter() (AgentAdapter, bool) {
	if c == nil {
		return AgentAdapter{}, false
	}
	if adapter, ok := c.AgentAdapter(c.Default); ok {
		adapter.Preferred = true
		return adapter, true
	}
	adapters := c.AgentAdapters()
	if len(adapters) == 0 {
		return AgentAdapter{}, false
	}
	adapters[0].Preferred = true
	return adapters[0], true
}

// NewAgentAdapter converts one executor entry into Brain's portable adapter
// vocabulary.
func NewAgentAdapter(name string, executor Executor) AgentAdapter {
	id := strings.TrimSpace(name)
	if id == "" {
		id = strings.TrimSpace(executor.Name)
	}
	command := strings.TrimSpace(executor.Command)
	if command == "" {
		command = id
	}
	provider := InferAgentProvider(executor.Kind, command, id)
	if provider == "" {
		provider = AgentProviderCustom
	}
	runtime := normalizeAgentRuntime(executor.Runtime)
	return AgentAdapter{
		ID:           id,
		Name:         firstNonEmptyString(strings.TrimSpace(executor.Name), id),
		Provider:     provider,
		Command:      command,
		Runtime:      runtime,
		Capabilities: agentCapabilities(provider, runtime),
	}
}

// InferAgentProvider detects known agent providers from config names,
// explicit kind metadata, commands, or process command lines.
func InferAgentProvider(values ...string) string {
	for _, value := range values {
		if provider := inferAgentProviderOne(value); provider != "" {
			return provider
		}
	}
	return ""
}

func inferAgentProviderOne(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	fields := strings.Fields(value)
	candidates := fields
	if len(candidates) == 0 {
		candidates = []string{value}
	}
	for _, candidate := range candidates {
		base := filepath.Base(strings.Trim(candidate, `"'`))
		switch {
		case strings.Contains(base, "codex"):
			return AgentProviderCodex
		case strings.Contains(base, "claude") || base == "cc":
			return AgentProviderClaude
		}
	}
	return ""
}

func normalizeAgentRuntime(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return AgentRuntimeTmux
	}
	return value
}

func agentCapabilities(provider, runtime string) AgentCapabilities {
	caps := AgentCapabilities{}
	if runtime == AgentRuntimeTmux {
		caps.InteractiveTTY = true
	}
	switch provider {
	case AgentProviderCodex:
		caps.NativeThreads = true
		caps.NativeSearch = true
		caps.NativeArchive = true
		caps.NativeWorktrees = true
		caps.NativeFork = true
		caps.NativeResume = true
		caps.NativeGoals = true
		caps.NativeAutomation = true
		caps.StructuredEvents = true
	case AgentProviderClaude:
		// Claude Code is currently treated as a portable TTY adapter here.
		// Brain can still manage it via tmux, transcript capture, and worktree
		// isolation without assuming Claude-native thread semantics.
	}
	return caps
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
