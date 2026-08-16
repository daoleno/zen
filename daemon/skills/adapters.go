package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvResolver resolves one environment override; tests and fixtures inject a
// table so adapter paths never depend on the real user's environment.
type EnvResolver func(key string) string

func osEnvResolver() EnvResolver {
	return func(key string) string { return osGetenv(key) }
}

// Adapter is the real filesystem contract for one Agent. Every lifecycle
// operation uses adapters exclusively: there is no agent-specific branching
// anywhere else in the package.
type Adapter struct {
	Agent   Agent
	Name    string
	Global  func(home string, env EnvResolver) string
	Project func(cwd string) string
	Mode    BindingMode
	Note    string
}

// Adapters is the authoritative six-Agent registry. Global scope is supported
// by every canonical Agent; project scope uses the per-Agent directory
// convention below. Modes were chosen from the actual installed CLI behavior:
//
//   - Codex, Claude Code, Pi, and OpenCode walk their skills directories at
//     session start and follow directory symlinks (Pi's own skill layout is a
//     symlink fanout), so Zen uses symlink bindings there.
//   - Cursor's desktop agent resolves skill folders from indexed workspace
//     state and is not guaranteed to re-resolve a symlinked folder without an
//     IDE restart; Zen therefore materializes copies and detects drift.
//   - Grok's TUI keeps its own enabled/disabled skill registry with file
//     watching; Zen materializes copies so enable/disable state stays fully
//     observable and drift is detected.
var Adapters = map[Agent]Adapter{
	AgentCodex: {
		Agent: AgentCodex, Name: "Codex",
		Global: func(home string, env EnvResolver) string {
			if value := strings.TrimSpace(env("CODEX_HOME")); value != "" {
				return filepath.Join(value, "skills")
			}
			return filepath.Join(home, ".codex", "skills")
		},
		Project: func(cwd string) string {
			// Codex resolves project skills through the shared .agents store.
			return filepath.Join(cwd, ".agents", "skills")
		},
		Mode: BindingSymlink,
	},
	AgentClaudeCode: {
		Agent: AgentClaudeCode, Name: "Claude Code",
		Global: func(home string, env EnvResolver) string {
			if value := strings.TrimSpace(env("CLAUDE_CONFIG_DIR")); value != "" {
				return filepath.Join(value, "skills")
			}
			return filepath.Join(home, ".claude", "skills")
		},
		Project: func(cwd string) string {
			return filepath.Join(cwd, ".claude", "skills")
		},
		Mode: BindingSymlink,
	},
	AgentCursor: {
		Agent: AgentCursor, Name: "Cursor",
		Global: func(home string, env EnvResolver) string {
			return filepath.Join(home, ".cursor", "skills")
		},
		Project: func(cwd string) string {
			return filepath.Join(cwd, ".cursor", "skills")
		},
		Mode: BindingCopy,
		Note: "Cursor's desktop agent indexes skill folders; Zen materializes copies and detects drift.",
	},
	AgentGrok: {
		Agent: AgentGrok, Name: "Grok",
		Global: func(home string, env EnvResolver) string {
			return filepath.Join(home, ".grok", "skills")
		},
		Project: func(cwd string) string {
			return filepath.Join(cwd, ".grok", "skills")
		},
		Mode: BindingCopy,
		Note: "Grok tracks skill state in its own registry with file watching; Zen materializes copies and detects drift.",
	},
	AgentOpenCode: {
		Agent: AgentOpenCode, Name: "OpenCode",
		Global: func(home string, env EnvResolver) string {
			configHome := strings.TrimSpace(env("XDG_CONFIG_HOME"))
			if configHome == "" {
				configHome = filepath.Join(home, ".config")
			}
			return filepath.Join(configHome, "opencode", "skills")
		},
		Project: func(cwd string) string {
			return filepath.Join(cwd, ".opencode", "skills")
		},
		Mode: BindingSymlink,
	},
	AgentPi: {
		Agent: AgentPi, Name: "Pi",
		Global: func(home string, env EnvResolver) string {
			return filepath.Join(home, ".pi", "agent", "skills")
		},
		Project: func(cwd string) string {
			return filepath.Join(cwd, ".pi", "skills")
		},
		Mode: BindingSymlink,
	},
}

func osGetenv(key string) string { return os.Getenv(key) }

// agentNamesForDisplay keeps one shared display-name source for the wire.
func agentName(agent Agent) string {
	return Adapters[agent].Name
}

// adapterFor returns the canonical adapter for one Agent.
func adapterFor(agent Agent) (Adapter, error) {
	adapter, ok := Adapters[agent]
	if !ok {
		return Adapter{}, fmt.Errorf("unsupported Skill target %q", agent)
	}
	return adapter, nil
}

// globalSkillsDir resolves the Agent's user/global skills directory under a
// fixture home and resolver.
func globalSkillsDir(adapter Adapter, home string, env EnvResolver) string {
	return filepath.Clean(adapter.Global(home, env))
}

// projectSkillsDir resolves the Agent's project skills directory under cwd.
func projectSkillsDir(adapter Adapter, cwd string) string {
	if strings.TrimSpace(cwd) == "" {
		return ""
	}
	return filepath.Clean(adapter.Project(filepath.Clean(cwd)))
}

// AgentSupportEntries builds the canonical adapter capability table.
func AgentSupportEntries() []AgentSupport {
	agents := []Agent{AgentCodex, AgentClaudeCode, AgentCursor, AgentGrok, AgentOpenCode, AgentPi}
	entries := make([]AgentSupport, 0, len(agents))
	for _, agent := range agents {
		adapter := Adapters[agent]
		entries = append(entries, AgentSupport{
			Agent:            agent,
			Name:             adapter.Name,
			Supported:        true,
			GlobalScope:      true,
			ProjectScope:     true,
			BindingMode:      string(adapter.Mode),
			BindingModeNote:  adapter.Note,
			DefaultGlobalDir: globalSkillsDir(adapter, "~", osEnvResolver()),
		})
	}
	return entries
}

// resolveExecutorAgent infers the provider adapter for a configured executor
// identity from its explicit kind, command, or name, mirroring the daemon's
// executor inference rules. Unknown identities resolve to "" and are never
// granted a lifecycle.
func resolveExecutorAgent(kind, command, name string) Agent {
	if agent := executorKindAgent(kind); agent != "" {
		return agent
	}
	for _, value := range []string{command, name} {
		if agent := inferAgentFromToken(value); agent != "" {
			return agent
		}
	}
	return ""
}

func executorKindAgent(kind string) Agent {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "codex":
		return AgentCodex
	case "claude", "claude-code":
		return AgentClaudeCode
	case "cursor":
		return AgentCursor
	case "grok":
		return AgentGrok
	case "opencode":
		return AgentOpenCode
	case "pi":
		return AgentPi
	default:
		return ""
	}
}

func inferAgentFromToken(value string) Agent {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	for _, candidate := range strings.Fields(value) {
		base := strings.Trim(candidate, "\"'\x60")
		if index := strings.LastIndexAny(base, "/\\"); index >= 0 {
			base = base[index+1:]
		}
		switch {
		case base == "cursor-agent" || strings.Contains(base, "cursor-agent"):
			return AgentCursor
		case strings.Contains(base, "codex"):
			return AgentCodex
		case strings.Contains(base, "grok"):
			return AgentGrok
		case strings.Contains(base, "claude") || base == "cc":
			return AgentClaudeCode
		case base == "opencode":
			return AgentOpenCode
		case base == "pi":
			return AgentPi
		}
	}
	return ""
}
