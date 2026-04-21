package issue

import (
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// ExecutorConfig holds the parsed executors.toml content plus built-in defaults.
type ExecutorConfig struct {
	Default string
	ByName  map[string]Executor
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
	DefaultExecutor string     `toml:"default_executor"`
	Executors       []Executor `toml:"executors"`
}

// LoadExecutors reads the file at path. If the file does not exist, a built-in
// default config (claude + codex, default claude) is returned.
func LoadExecutors(path string) (*ExecutorConfig, error) {
	cfg := &ExecutorConfig{
		Default: "claude",
		ByName: map[string]Executor{
			"claude": {Name: "claude", Command: "claude"},
			"codex":  {Name: "codex", Command: "codex"},
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
	if trimmed := strings.TrimSpace(file.DefaultExecutor); trimmed != "" {
		cfg.Default = trimmed
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
