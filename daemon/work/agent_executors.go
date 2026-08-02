package work

import (
	"errors"
	"fmt"
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

	// ClaudeFullAuthorizationFlag is the Claude CLI flag that bypasses
	// permission checks, providing the most permissive non-interactive
	// authorization mode available. Brain-delegated Claude sessions use this so
	// internal progress commands never block on approval prompts.
	ClaudeFullAuthorizationFlag = "--permission-mode bypassPermissions"
)

// ErrScheduledActionUnattended is returned before spawning when a Calendar
// scheduled_action cannot be given one proven, conflict-free unattended
// authorization mode.
var ErrScheduledActionUnattended = errors.New("scheduled_action executor cannot launch unattended")

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

// HardenClaudeCommand returns a Claude launch command configured for
// non-interactive autonomous execution. When command does not already declare
// an explicit authorization mode, ClaudeFullAuthorizationFlag is appended so
// Brain-delegated and Brain-host Claude sessions bypass permission checks for
// internal shell/progress commands. Commands that already include
// --permission-mode (any value) or --dangerously-skip-permissions are returned
// unchanged so explicit user-provided authorization configuration is preserved.
func HardenClaudeCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		command = AgentProviderClaude
	}
	if strings.Contains(command, "--permission-mode") ||
		strings.Contains(command, "--dangerously-skip-permissions") {
		return command
	}
	return command + " " + ClaudeFullAuthorizationFlag
}

// ScheduledActionCommand derives Calendar's unattended command without
// mutating the configured command used by ordinary launches.
func ScheduledActionCommand(executorID string, executor Executor) (string, error) {
	executorID = strings.TrimSpace(executorID)
	agentExecutor := NewAgentExecutor(executorID, executor)
	command := strings.TrimSpace(agentExecutor.Command)
	options, inspectable := inspectLaunchCommandOptions(command)
	if agentExecutor.Provider != AgentProviderCustom {
		if !inspectable {
			return "", fmt.Errorf("%w: executor %q has an unsupported launch command", ErrScheduledActionUnattended, executorID)
		}
		if !scheduledProviderExecutable(agentExecutor.Provider, options.executable) {
			return "", fmt.Errorf(
				"%w: executor %q provider %q does not match executable %q",
				ErrScheduledActionUnattended,
				executorID,
				agentExecutor.Provider,
				options.executable,
			)
		}
	}
	switch agentExecutor.Provider {
	case AgentProviderCodex:
		bypassPresent, bypassEnabled := options.option("", CodexFullAuthorizationFlag)
		approvalPresent, approvalCompatible := options.option("never", "--ask-for-approval", "-a")
		sandboxPresent, sandboxCompatible := options.option("danger-full-access", "--sandbox", "-s")
		if bypassPresent && !bypassEnabled {
			return "", fmt.Errorf("%w: executor %q has an invalid Codex bypass option", ErrScheduledActionUnattended, executorID)
		}
		if !approvalCompatible || !sandboxCompatible {
			return "", fmt.Errorf("%w: executor %q has a non-unattended Codex approval or sandbox mode", ErrScheduledActionUnattended, executorID)
		}
		if bypassEnabled || approvalPresent && sandboxPresent {
			return command, nil
		}
		if approvalPresent {
			return appendScheduledOptions(executorID, command, options, "--sandbox", "danger-full-access")
		}
		if sandboxPresent {
			return appendScheduledOptions(executorID, command, options, "--ask-for-approval", "never")
		}
		return appendScheduledOptions(executorID, command, options, CodexFullAuthorizationFlag)
	case AgentProviderClaude:
		permissionPresent, permissionCompatible := options.option("bypassPermissions", "--permission-mode")
		skipPresent, skipEnabled := options.option("", "--dangerously-skip-permissions")
		if !permissionCompatible || skipPresent && !skipEnabled {
			return "", fmt.Errorf("%w: executor %q has a non-bypass Claude permission mode", ErrScheduledActionUnattended, executorID)
		}
		if permissionPresent || skipEnabled {
			return command, nil
		}
		return appendScheduledOptions(executorID, command, options, "--permission-mode", "bypassPermissions")
	case AgentProviderCursor:
		autoReviewPresent, _ := options.option("", "--auto-review")
		planPresent, _ := options.option("", "--plan")
		modePresent, _ := options.option("", "--mode")
		sandboxPresent, sandboxCompatible := options.option("disabled", "--sandbox")
		if autoReviewPresent || planPresent || modePresent || !sandboxCompatible {
			return "", fmt.Errorf("%w: executor %q has an interactive or read-only Cursor mode", ErrScheduledActionUnattended, executorID)
		}
		forcePresent, forceEnabled := options.option("", "--force")
		yoloPresent, yoloEnabled := options.option("", "--yolo")
		trustPresent, trustEnabled := options.option("", "--trust")
		approveMCPsPresent, approveMCPsEnabled := options.option("", "--approve-mcps")
		if forcePresent && !forceEnabled || yoloPresent && !yoloEnabled ||
			trustPresent && !trustEnabled || approveMCPsPresent && !approveMCPsEnabled {
			return "", fmt.Errorf("%w: executor %q has an invalid Cursor unattended option", ErrScheduledActionUnattended, executorID)
		}
		appendArgs := make([]string, 0, 5)
		if !forceEnabled && !yoloEnabled {
			appendArgs = append(appendArgs, "--force")
		}
		if !sandboxPresent {
			appendArgs = append(appendArgs, "--sandbox", "disabled")
		}
		if !trustEnabled {
			appendArgs = append(appendArgs, "--trust")
		}
		if !approveMCPsEnabled {
			appendArgs = append(appendArgs, "--approve-mcps")
		}
		return appendScheduledOptions(executorID, command, options, appendArgs...)
	case AgentProviderGrok:
		permissionPresent, permissionCompatible := options.option("bypassPermissions", "--permission-mode")
		sandboxPresent, sandboxCompatible := options.option("off", "--sandbox")
		if !permissionCompatible {
			return "", fmt.Errorf("%w: executor %q has a non-bypass Grok permission mode", ErrScheduledActionUnattended, executorID)
		}
		if !sandboxCompatible {
			return "", fmt.Errorf("%w: executor %q has a restricted Grok sandbox", ErrScheduledActionUnattended, executorID)
		}
		alwaysApprovePresent, alwaysApproveEnabled := options.option("", "--always-approve")
		yoloPresent, yoloEnabled := options.option("", "--yolo")
		if alwaysApprovePresent && !alwaysApproveEnabled || yoloPresent && !yoloEnabled {
			return "", fmt.Errorf("%w: executor %q has an invalid Grok unattended option", ErrScheduledActionUnattended, executorID)
		}
		appendArgs := make([]string, 0, 4)
		if !permissionPresent && !alwaysApproveEnabled && !yoloEnabled {
			appendArgs = append(appendArgs, "--permission-mode", "bypassPermissions")
		}
		if !sandboxPresent {
			appendArgs = append(appendArgs, "--sandbox", "off")
		}
		return appendScheduledOptions(executorID, command, options, appendArgs...)
	default:
		return "", fmt.Errorf("%w: executor %q uses unsupported provider %q", ErrScheduledActionUnattended, executorID, agentExecutor.Provider)
	}
}

func appendCommandOptions(command string, options ...string) string {
	command = strings.TrimSpace(command)
	for _, option := range options {
		if option != "" {
			command += " " + option
		}
	}
	return command
}

type launchCommandOptions struct {
	executable string
	argv       []string
	terminated bool
}

func inspectLaunchCommandOptions(command string) (launchCommandOptions, bool) {
	fields, ok := splitSupportedLaunchFields(command)
	if !ok || len(fields) == 0 {
		return launchCommandOptions{}, false
	}
	executable := 0
	if filepath.Base(fields[0]) == "env" {
		executable++
		for executable < len(fields) && isLaunchEnvAssignment(fields[executable]) {
			executable++
		}
		if executable < len(fields) && fields[executable] == "--" {
			executable++
		}
	}
	if executable >= len(fields) || strings.HasPrefix(fields[executable], "-") {
		return launchCommandOptions{}, false
	}
	options := launchCommandOptions{
		executable: filepath.Base(fields[executable]),
		argv:       fields[executable+1:],
	}
	for index, argument := range options.argv {
		if argument == "--" {
			options.argv = options.argv[:index]
			options.terminated = true
			break
		}
	}
	return options, true
}

func scheduledProviderExecutable(provider, executable string) bool {
	switch provider {
	case AgentProviderCodex:
		return executable == "codex"
	case AgentProviderClaude:
		return executable == "claude" || executable == "cc"
	case AgentProviderCursor:
		return executable == "cursor-agent"
	case AgentProviderGrok:
		return executable == "grok" || strings.HasPrefix(executable, "grok-")
	default:
		return false
	}
}

// splitSupportedLaunchFields only recognizes the direct executable and
// env-assignment launch shapes Zen emits. It handles shell quoting needed to
// recover argv but deliberately rejects shell comments, command composition,
// and substitution.
func splitSupportedLaunchFields(command string) ([]string, bool) {
	var fields []string
	var token strings.Builder
	var quote byte
	started := false
	flush := func() {
		if started {
			fields = append(fields, token.String())
			token.Reset()
			started = false
		}
	}
	for index := 0; index < len(command); index++ {
		current := command[index]
		if quote != 0 {
			if current == quote {
				quote = 0
				continue
			}
			if quote == '"' && (current == '`' ||
				current == '$' && index+1 < len(command) && command[index+1] == '(') {
				return nil, false
			}
			if quote == '"' && current == '\\' {
				index++
				if index >= len(command) {
					return nil, false
				}
				current = command[index]
			}
			token.WriteByte(current)
			started = true
			continue
		}
		switch current {
		case '\'', '"':
			quote = current
			started = true
		case '\\':
			index++
			if index >= len(command) {
				return nil, false
			}
			token.WriteByte(command[index])
			started = true
		case '#', ';', '|', '&', '<', '>', '`', '(', ')', '\n', '\r':
			return nil, false
		case ' ', '\t':
			flush()
		default:
			token.WriteByte(current)
			started = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	flush()
	return fields, true
}

func appendScheduledOptions(executorID, command string, options launchCommandOptions, additions ...string) (string, error) {
	if options.terminated {
		return "", fmt.Errorf("%w: executor %q places arguments after an option terminator", ErrScheduledActionUnattended, executorID)
	}
	return appendCommandOptions(command, additions...), nil
}

func isLaunchEnvAssignment(value string) bool {
	equals := strings.IndexByte(value, '=')
	if equals <= 0 {
		return false
	}
	for _, current := range value[:equals] {
		if current != '_' &&
			(current < 'a' || current > 'z') &&
			(current < 'A' || current > 'Z') &&
			(current < '0' || current > '9') {
			return false
		}
	}
	return true
}

// option reports whether any named option is present and whether every
// occurrence has the exact requested value. An empty want denotes a boolean
// flag, for which equals forms are invalid rather than truthy.
// Codex's -a and -s options additionally accept attached values.
func (options launchCommandOptions) option(want string, names ...string) (bool, bool) {
	present := false
	compatible := true
	for index, argument := range options.argv {
		for _, name := range names {
			if want == "" {
				if argument == name {
					present = true
				} else if strings.HasPrefix(argument, name+"=") {
					present = true
					compatible = false
				}
				continue
			}
			value := ""
			matched := false
			if argument == name {
				present = true
				if index+1 < len(options.argv) && options.argv[index+1] != "--" {
					value = options.argv[index+1]
				}
				matched = true
			} else if strings.HasPrefix(argument, name+"=") {
				present = true
				value = strings.TrimPrefix(argument, name+"=")
				matched = true
			} else if (name == "-a" || name == "-s") &&
				strings.HasPrefix(argument, name) && len(argument) > len(name) {
				present = true
				value = strings.TrimPrefix(argument, name)
				matched = true
			}
			if matched && value != want {
				compatible = false
			}
		}
	}
	if want == "" {
		return present, present && compatible
	}
	return present, compatible
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
// It always reads the live delegated selection owned by ExecutorConfig.
func (c *ExecutorConfig) DelegatedAgentExecutor() (AgentExecutor, bool) {
	if c == nil {
		return AgentExecutor{}, false
	}
	if executor, ok := c.AgentExecutor(c.GetDelegatedExecutor()); ok {
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
