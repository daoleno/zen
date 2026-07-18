package work

import (
	"os"
	"path/filepath"
)

// DefaultRoot returns ~/.zen/work.
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zen", "work"), nil
}

// DefaultWorktreeRoot returns the durable, Zen-managed location agents should
// use when concurrent write isolation genuinely requires a git worktree.
// It intentionally lives under the user's home directory rather than /tmp,
// which is commonly memory-backed and may be cleared across restarts.
func DefaultWorktreeRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zen", "worktrees"), nil
}

// DefaultExecutorsPath returns ~/.zen/executors.toml.
func DefaultExecutorsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zen", "executors.toml"), nil
}

// EnsureDir creates dir with mode 0o700 if it does not already exist.
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o700)
}
