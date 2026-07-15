package work

import (
	"path/filepath"
	"sort"
	"strings"
)

const (
	AgentRuntimeTmux    = "tmux"
	AgentProviderCodex  = "codex"
	AgentProviderCursor = "cursor"
	AgentProviderGrok   = "grok"
	AgentProviderClaude = "claude"
	AgentProviderCustom = "custom"

	// CodexFullAuthorizationFlag is the Codex CLI flag that skips all
	// confirmation prompts and disables sandbox restrictions, providing the
	// most permissive non-interactive authorization mode available.
	// Brain-delegated Codex sessions use this so internal progress commands
	// never block on approval prompts (e.g. when the Zen control socket lives
	// outside the Codex sandbox).
	CodexFullAuthorizationFlag = "--dangerously-bypass-approvals-and-sandbox"
)

// HardenCodexDelegatedCommand returns a Codex launch command configured for
// non-interactive delegated execution. When command does not already declare
// the full-authorization flag, it is appended so Brain-delegated Codex
// sessions do not stop on approval prompts for internal shell/progress
// commands. Commands that already include the flag are returned unchanged so
// explicit user-provided authorization configuration is preserved.
func HardenCodexDelegatedCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		command = AgentProviderCodex
	}
	if strings.Contains(command, CodexFullAuthorizationFlag) {
		return command
	}
	return command + " " + CodexFullAuthorizationFlag
}

// AgentCapabilities describes capabilities Brain can delegate to an executor.
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

// AgentExecutor is the portable Brain-facing view of a configured executor.
type AgentExecutor struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Provider     string            `json:"provider"`
	Command      string            `json:"command,omitempty"`
	Runtime      string            `json:"runtime"`
	Capabilities AgentCapabilities `json:"capabilities"`
	Host         bool              `json:"host,omitempty"`
	Delegated    bool              `json:"delegated,omitempty"`
}

// AgentExecutors returns all configured executors as portable executor records.
func (c *ExecutorConfig) AgentExecutors() []AgentExecutor {
	if c == nil || len(c.ByName) == 0 {
		return nil
	}
	out := make([]AgentExecutor, 0, len(c.ByName))
	for name, executor := range c.ByName {
		out = append(out, NewAgentExecutor(name, executor))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// AgentExecutor returns one configured executor as a portable executor record.
func (c *ExecutorConfig) AgentExecutor(name string) (AgentExecutor, bool) {
	if c == nil {
		return AgentExecutor{}, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return AgentExecutor{}, false
	}
	executor, ok := c.ByName[name]
	if !ok {
		return AgentExecutor{}, false
	}
	return NewAgentExecutor(name, executor), true
}

// DelegatedAgentExecutor returns the configured executor for delegated execution.
func (c *ExecutorConfig) DelegatedAgentExecutor() (AgentExecutor, bool) {
	if c == nil {
		return AgentExecutor{}, false
	}
	if executor, ok := c.AgentExecutor(c.DelegatedExecutor); ok {
		executor.Delegated = true
		return executor, true
	}
	if executor, ok := c.AgentExecutor("codex"); ok {
		executor.Delegated = true
		return executor, true
	}
	executors := c.AgentExecutors()
	if len(executors) == 0 {
		return AgentExecutor{}, false
	}
	executors[0].Delegated = true
	return executors[0], true
}

// NewAgentExecutor converts one executor entry into Brain's portable executor
// vocabulary.
func NewAgentExecutor(name string, executor Executor) AgentExecutor {
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
	return AgentExecutor{
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
		case base == AgentProviderCursor:
			return AgentProviderCursor
		case strings.Contains(base, "codex"):
			return AgentProviderCodex
		case base == "cursor-agent" || strings.Contains(base, "cursor-agent"):
			return AgentProviderCursor
		case strings.Contains(base, "grok"):
			return AgentProviderGrok
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
	case AgentProviderGrok:
		caps.StructuredEvents = true
	case AgentProviderClaude:
		// Claude Code exposes structured chat via local JSONL transcripts under
		// ~/.claude/projects. It does not offer Codex-style native thread APIs.
		caps.StructuredEvents = true
	case AgentProviderCursor:
		// Cursor Agent appends provider-structured message, tool, and turn rows
		// under agent-transcripts. It does not expose Codex-style native APIs.
		caps.StructuredEvents = true
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
