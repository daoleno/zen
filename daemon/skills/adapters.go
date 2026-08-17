package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type EnvResolver func(key string) string

func osEnvResolver() EnvResolver { return os.Getenv }

// Adapter is the native root contract for one supported Agent.
type Adapter struct {
	Agent   Agent
	Name    string
	Global  func(home string, env EnvResolver) string
	Project func(cwd string) string
}

var Adapters = map[Agent]Adapter{
	AgentCodex: {
		Agent: AgentCodex, Name: "Codex",
		Global: func(home string, env EnvResolver) string {
			if value := strings.TrimSpace(env("CODEX_HOME")); value != "" {
				return filepath.Join(value, "skills")
			}
			return filepath.Join(home, ".codex", "skills")
		},
		Project: func(cwd string) string { return filepath.Join(cwd, ".agents", "skills") },
	},
	AgentClaudeCode: {
		Agent: AgentClaudeCode, Name: "Claude Code",
		Global: func(home string, env EnvResolver) string {
			if value := strings.TrimSpace(env("CLAUDE_CONFIG_DIR")); value != "" {
				return filepath.Join(value, "skills")
			}
			return filepath.Join(home, ".claude", "skills")
		},
		Project: func(cwd string) string { return filepath.Join(cwd, ".claude", "skills") },
	},
	AgentCursor: {
		Agent: AgentCursor, Name: "Cursor",
		Global:  func(home string, _ EnvResolver) string { return filepath.Join(home, ".cursor", "skills") },
		Project: func(cwd string) string { return filepath.Join(cwd, ".cursor", "skills") },
	},
	AgentGrok: {
		Agent: AgentGrok, Name: "Grok",
		Global:  func(home string, _ EnvResolver) string { return filepath.Join(home, ".grok", "skills") },
		Project: func(cwd string) string { return filepath.Join(cwd, ".grok", "skills") },
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
		Project: func(cwd string) string { return filepath.Join(cwd, ".opencode", "skills") },
	},
	AgentPi: {
		Agent: AgentPi, Name: "Pi",
		Global:  func(home string, _ EnvResolver) string { return filepath.Join(home, ".pi", "agent", "skills") },
		Project: func(cwd string) string { return filepath.Join(cwd, ".pi", "skills") },
	},
}

var supportedAgents = []Agent{AgentCodex, AgentClaudeCode, AgentCursor, AgentGrok, AgentOpenCode, AgentPi}

func agentName(agent Agent) string {
	if adapter, ok := Adapters[agent]; ok {
		return adapter.Name
	}
	return string(agent)
}

func adapterFor(agent Agent) (Adapter, error) {
	adapter, ok := Adapters[agent]
	if !ok {
		return Adapter{}, fmt.Errorf("unsupported Skill target %q", agent)
	}
	return adapter, nil
}

func globalSkillsDir(adapter Adapter, home string, env EnvResolver) string {
	return filepath.Clean(adapter.Global(home, env))
}

func projectSkillsDir(adapter Adapter, cwd string) string {
	if strings.TrimSpace(cwd) == "" {
		return ""
	}
	return filepath.Clean(adapter.Project(filepath.Clean(cwd)))
}

func AgentSupportEntries(options InventoryOptions) []AgentSupport {
	entries := make([]AgentSupport, 0, len(supportedAgents))
	for _, agent := range supportedAgents {
		adapter := Adapters[agent]
		entries = append(entries, AgentSupport{
			Agent: agent, Name: adapter.Name, Supported: true,
			GlobalScope: true, ProjectScope: true,
			DefaultGlobalDir: globalRootForAgent(agent, options),
		})
	}
	return entries
}

func globalRootForAgent(agent Agent, options InventoryOptions) string {
	switch agent {
	case AgentCodex:
		return filepath.Join(options.CodexHome, "skills")
	case AgentClaudeCode:
		return filepath.Join(options.ClaudeHome, "skills")
	default:
		return globalSkillsDir(Adapters[agent], options.Home, envResolverFor(options))
	}
}

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
	for _, candidate := range strings.Fields(strings.ToLower(strings.TrimSpace(value))) {
		base := strings.Trim(candidate, "\"'\x60")
		if index := strings.LastIndexAny(base, "/\\"); index >= 0 {
			base = base[index+1:]
		}
		switch {
		case strings.Contains(base, "cursor-agent"):
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

func resolveExecutors(aliases []ExecutorAlias) []ExecutorSupport {
	out := make([]ExecutorSupport, 0, len(aliases))
	seen := map[string]bool{}
	for _, alias := range aliases {
		agent := resolveExecutorAgent(alias.Kind, alias.Command, alias.Name)
		name := strings.TrimSpace(alias.Name)
		if agent == "" || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, ExecutorSupport{Name: name, Kind: alias.Kind, Agent: agent, Command: alias.Command})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
