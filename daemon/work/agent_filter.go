package work

import (
	"path/filepath"
	"strings"

	"github.com/daoleno/zen/daemon/classifier"
)

func IsAgentSession(agent *classifier.Agent) bool {
	if agent == nil {
		return false
	}
	return IsAgentCommand(agent.Command)
}

func IsAgentCommand(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}

	name := strings.ToLower(filepath.Base(fields[0]))
	name = strings.TrimSuffix(name, ".exe")
	return name == "claude" || name == "claude-code" || name == "codex" || name == "cc"
}

func IsAgentWorkItem(item *Item) bool {
	if item == nil {
		return false
	}
	if strings.TrimSpace(item.Frontmatter.Kind) != brainLogKind {
		return false
	}
	return IsNativeAgentSource(item.Frontmatter.AgentSource)
}

func IsNativeAgentSource(source string) bool {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "codex", "claude":
		return true
	default:
		return false
	}
}

func FilterAgentWorkItems(items []*Item) []*Item {
	if len(items) == 0 {
		return nil
	}
	out := make([]*Item, 0, len(items))
	for _, item := range items {
		if IsAgentWorkItem(item) {
			out = append(out, item)
		}
	}
	return out
}
