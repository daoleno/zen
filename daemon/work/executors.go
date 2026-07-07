package work

import (
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// ExecutorConfig holds the parsed executors.toml content plus built-in defaults.
type ExecutorConfig struct {
	DelegatedExecutor string
	ByName            map[string]Executor
}

// Roles returns executor names sorted alphabetically.
func (c *ExecutorConfig) Roles() []string {
	out := make([]string, 0, len(c.ByName))
	for name := range c.ByName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

type executorFile struct {
	DelegatedExecutor string     `toml:"delegated_executor"`
	Executors         []Executor `toml:"executors"`
}

// LoadExecutors reads the file at path. If the file does not exist, a built-in
// default config is returned.
func LoadExecutors(path string) (*ExecutorConfig, error) {
	cfg := &ExecutorConfig{
		DelegatedExecutor: "codex",
		ByName: map[string]Executor{
			"agent":  {Name: "agent", Command: "cursor-agent --force --sandbox disabled", Kind: "cursor"},
			"claude": {Name: "claude", Command: "claude"},
			"codex":  {Name: "codex", Command: "codex"},
			"grok":   {Name: "grok", Command: "grok --no-alt-screen --permission-mode bypassPermissions"},
		},
	}

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	var file executorFile
	if err := toml.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if trimmed := strings.TrimSpace(file.DelegatedExecutor); trimmed != "" {
		cfg.DelegatedExecutor = trimmed
	}
	for _, executor := range file.Executors {
		name := strings.TrimSpace(executor.Name)
		if name == "" {
			continue
		}
		executor.Name = name
		executor.Command = strings.TrimSpace(executor.Command)
		if executor.Command == "" {
			executor.Command = name
		}
		cfg.ByName[name] = executor
	}
	return cfg, nil
}
