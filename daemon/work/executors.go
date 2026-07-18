package work

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// ErrUnknownExecutor is returned when a delegated selection names an executor
// that is not present in the loaded catalog.
var ErrUnknownExecutor = errors.New("unknown executor")

// ErrDelegatedExecutorLocked is returned when a live selection change conflicts
// with a startup environment lock (ZEN_DELEGATED_EXECUTOR).
var ErrDelegatedExecutorLocked = errors.New("delegated executor locked by environment")

// ExecutorConfig holds the parsed executors.toml content plus built-in defaults.
//
// ByName is a static catalog snapshot. The effective Delegated Executor is the
// sole live setting owned here, including any startup ZEN_DELEGATED_EXECUTOR
// lock. One process-wide owner updates durable selection and exposes one
// effective value so Brain, ordinary default spawn, and Calendar agree without
// restart, polling, or per-call file reloads.
type ExecutorConfig struct {
	mu sync.RWMutex

	path              string // durable file; empty means memory-only
	delegatedExecutor string // durable selection (file / live Set)
	envLock           string // startup ZEN_DELEGATED_EXECUTOR, captured once
	ByName            map[string]Executor
}

// NewExecutorConfig builds an in-memory ExecutorConfig (no durable path).
func NewExecutorConfig(delegated string, byName map[string]Executor) *ExecutorConfig {
	if byName == nil {
		byName = map[string]Executor{}
	}
	return &ExecutorConfig{
		delegatedExecutor: strings.TrimSpace(delegated),
		envLock:           strings.TrimSpace(os.Getenv("ZEN_DELEGATED_EXECUTOR")),
		ByName:            byName,
	}
}

// GetDelegatedExecutor returns the effective delegated selection.
// A valid startup env lock wins over the durable selection.
func (c *ExecutorConfig) GetDelegatedExecutor() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.effectiveDelegatedLocked()
}

// SetDelegatedExecutor validates id against the loaded catalog, persists the
// durable selection atomically when a path is configured, then updates memory.
// Existing sessions are not migrated or restarted.
//
// Already-effective ids (durable or env lock) are a zero-write no-op.
// A conflicting set while the env lock is active returns
// ErrDelegatedExecutorLocked without changing memory or disk. Validation or
// persistence failure keeps the previous durable selection and file bytes.
func (c *ExecutorConfig) SetDelegatedExecutor(id string) error {
	if c == nil {
		return fmt.Errorf("executor config required")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: delegated executor id is required", ErrUnknownExecutor)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.ByName[id]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownExecutor, id)
	}
	if c.effectiveDelegatedLocked() == id {
		return nil
	}
	if lock, active := c.activeEnvLockLocked(); active {
		return fmt.Errorf("%w: %s (ZEN_DELEGATED_EXECUTOR)", ErrDelegatedExecutorLocked, lock)
	}
	if err := c.persistDelegatedExecutorLocked(id); err != nil {
		return err
	}
	c.delegatedExecutor = id
	return nil
}

// Roles returns executor names sorted alphabetically.
func (c *ExecutorConfig) Roles() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.ByName))
	for name := range c.ByName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (c *ExecutorConfig) effectiveDelegatedLocked() string {
	if lock, active := c.activeEnvLockLocked(); active {
		return lock
	}
	return strings.TrimSpace(c.delegatedExecutor)
}

func (c *ExecutorConfig) activeEnvLockLocked() (string, bool) {
	lock := strings.TrimSpace(c.envLock)
	if lock == "" {
		return "", false
	}
	if _, ok := c.ByName[lock]; !ok {
		return "", false
	}
	return lock, true
}

type executorFile struct {
	DelegatedExecutor string     `toml:"delegated_executor"`
	Executors         []Executor `toml:"executors"`
}

// LoadExecutors reads path, or returns built-in defaults when missing.
// The config retains path for live persistence and captures any startup
// ZEN_DELEGATED_EXECUTOR lock into the same owner.
func LoadExecutors(path string) (*ExecutorConfig, error) {
	cfg := &ExecutorConfig{
		path:              path,
		delegatedExecutor: "codex",
		envLock:           strings.TrimSpace(os.Getenv("ZEN_DELEGATED_EXECUTOR")),
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
		cfg.delegatedExecutor = trimmed
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

func (c *ExecutorConfig) persistDelegatedExecutorLocked(id string) error {
	path := strings.TrimSpace(c.path)
	if path == "" {
		return nil
	}

	var raw []byte
	existing, err := os.ReadFile(path)
	if err == nil {
		raw = existing
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomicWriteFile(path, rewriteDelegatedExecutorTOML(raw, id), 0o600)
}

// Matches a single-line top-level delegated_executor assignment in the
// currently supported config shape (quoted value, optional inline comment).
var topLevelDelegatedExecutorAssignRE = regexp.MustCompile(
	`(?m)^([ \t]*delegated_executor[ \t]*=[ \t]*)("[^"]*"|'[^']*')([ \t]*(?:#[^\r\n]*)?\r?\n?)`,
)

var tableHeaderLineRE = regexp.MustCompile(`^[ \t]*\[`)

// rewriteDelegatedExecutorTOML updates only the top-level selection value.
func rewriteDelegatedExecutorTOML(raw []byte, id string) []byte {
	quoted := "\"" + escapeTOMLString(id) + "\""
	if len(raw) == 0 {
		return []byte("delegated_executor = " + quoted + "\n")
	}
	prefix, rest := splitTopLevelPrefix(raw)
	loc := topLevelDelegatedExecutorAssignRE.FindSubmatchIndex(prefix)
	if loc != nil && len(loc) >= 8 && loc[4] >= 0 && loc[6] >= 0 {
		out := make([]byte, 0, len(raw)+len(quoted))
		out = append(out, prefix[:loc[4]]...)
		out = append(out, quoted...)
		out = append(out, prefix[loc[6]:loc[1]]...)
		out = append(out, prefix[loc[1]:]...)
		out = append(out, rest...)
		return out
	}
	line := "delegated_executor = " + quoted + "\n"
	out := make([]byte, 0, len(line)+2+len(raw))
	out = append(out, line...)
	if raw[0] != '\n' {
		out = append(out, '\n')
	}
	return append(out, raw...)
}

func splitTopLevelPrefix(raw []byte) (prefix, rest []byte) {
	if len(raw) == 0 {
		return nil, nil
	}
	offset := 0
	for offset < len(raw) {
		end := bytes.IndexByte(raw[offset:], '\n')
		var line []byte
		next := len(raw)
		if end < 0 {
			line = raw[offset:]
		} else {
			line = raw[offset : offset+end]
			next = offset + end + 1
		}
		if tableHeaderLineRE.Match(bytes.TrimRight(line, "\r")) {
			return raw[:offset], raw[offset:]
		}
		offset = next
		if end < 0 {
			break
		}
	}
	return raw, nil
}

func escapeTOMLString(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".executors-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
