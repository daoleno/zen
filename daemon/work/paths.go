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

// DefaultModelProfilesPath returns ~/.zen/model-profiles.toml.
func DefaultModelProfilesPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zen", "model-profiles.toml"), nil
}

// DefaultProviderDiscoveryPath returns ~/.zen/provider-discovery.json.
// Secret-free TTL/LKG model id cache; live ids never authorize capabilities.
func DefaultProviderDiscoveryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zen", "provider-discovery.json"), nil
}

// DefaultProviderCredentialsPath returns ~/.zen/provider-credentials.json.
func DefaultProviderCredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zen", "provider-credentials.json"), nil
}

// DefaultRouteBindingsPath returns ~/.zen/route-bindings.json.
// Stage 2B Session lifecycle owns Save/Load against this path; Stage 2A only
// defines the codec and RouteTable.Restore contract.
func DefaultRouteBindingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zen", "route-bindings.json"), nil
}

// DefaultRouteListenerPath returns ~/.zen/route-listener.json.
// Persists the loopback listen address so daemon restart rebinds the same port
// and surviving CLI Sessions keep working.
func DefaultRouteListenerPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zen", "route-listener.json"), nil
}

// EnsureDir creates dir with mode 0o700 if it does not already exist.
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o700)
}
