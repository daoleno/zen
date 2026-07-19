package setup

import (
	"fmt"
	"strings"

	"github.com/daoleno/zen/daemon/work"
)

// BrainHardensHostAtRuntime reports whether the current daemon always injects
// noninteractive/bypass flags when launching Brain host sessions for known
// providers. Setup must not advertise a fake Safe Brain mode while this is true.
func BrainHardensHostAtRuntime() bool {
	// Inspected from daemon/brain/service.go hostCommand() and resolveSpawnCommand():
	// bare/default Codex gets --dangerously-bypass-approvals-and-sandbox; bare/default
	// Claude gets --permission-mode bypassPermissions. Explicit req.Command is unchanged.
	return true
}

// PermissionExplanation returns truthful Safe vs Autonomous copy for humans.
func PermissionExplanation() []string {
	lines := []string{
		"Permission profiles:",
		"  Safe/manual  — write executor commands without sandbox/permission bypass flags.",
		"                 Agents may prompt for approval in the TTY; unattended mobile control is slower.",
		"  Autonomous   — write high-risk bypass flags for Cursor/Grok (and accept Brain hardening).",
		"                 Agents can run tools/shell with little or no interactive approval.",
	}
	if BrainHardensHostAtRuntime() {
		lines = append(lines,
			"",
			"Brain limitation (current runtime):",
			"  Brain host sessions for Codex/Claude are hardened to noninteractive/bypass mode in code.",
			"  Safe profile therefore leaves Brain host unconfigured and does not claim a safe Brain mode.",
			"  Choosing Autonomous with explicit confirmation configures Brain host and accepts that risk.",
		)
	}
	return lines
}

func normalizeProfile(value string) (Profile, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(ProfileSafe), "manual":
		return ProfileSafe, nil
	case string(ProfileAutonomous), "auto", "yolo":
		return ProfileAutonomous, nil
	default:
		return "", fmt.Errorf("%w: unknown profile %q (want safe or autonomous)", ErrInvalidArgs, value)
	}
}

func commandForProfile(provider, id, existingCommand string, profile Profile) (command, kind string) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = work.InferAgentProvider(existingCommand, id)
	}
	existingCommand = strings.TrimSpace(existingCommand)
	kind = ""

	switch provider {
	case work.AgentProviderCodex:
		// Keep base command plain; Brain hardens at runtime when Brain is used.
		return "codex", ""
	case work.AgentProviderClaude:
		return "claude", ""
	case work.AgentProviderCursor:
		kind = "cursor"
		if profile == ProfileAutonomous {
			return "cursor-agent --force --sandbox disabled", kind
		}
		return "cursor-agent", kind
	case work.AgentProviderGrok:
		if profile == ProfileAutonomous {
			return "grok --no-alt-screen --permission-mode bypassPermissions", ""
		}
		return "grok --no-alt-screen", ""
	default:
		// Preserve custom commands. Never inject bypass flags.
		if existingCommand != "" {
			return existingCommand, ""
		}
		if id != "" {
			return id, ""
		}
		return "custom", ""
	}
}

func stripBypassFlags(command string) string {
	command = strings.TrimSpace(command)
	replacements := []string{
		work.CodexFullAuthorizationFlag,
		"--force",
		"--sandbox disabled",
		"--permission-mode bypassPermissions",
		"--permission-mode dontAsk",
		"--yolo",
	}
	for _, flag := range replacements {
		command = strings.ReplaceAll(command, flag, "")
	}
	return strings.Join(strings.Fields(command), " ")
}
