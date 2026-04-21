package issue

import (
	"os"
	"path/filepath"
)

// DefaultRoot returns ~/.zen/issues.
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zen", "issues"), nil
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
