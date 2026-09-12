package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// InitializeConfig writes the user-owned input for the reviewed host install.
// Re-running it with the same identity is a no-op; a changed existing file is
// never overwritten because it may have been reviewed by an administrator.
func InitializeConfig(path string, config HostConfig) (created bool, err error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false, errors.New("invalid_host_config_path")
	}
	if err := config.Validate(); err != nil {
		return false, err
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("unsafe_host_config_parent")
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (owner.Uid != 0 && owner.Uid != uint32(os.Getuid())) || info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return false, errors.New("unsafe_host_config_parent")
		}
		if parent == "/" {
			break
		}
	}
	body, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encode host config: %w", err)
	}
	body = append(body, '\n')

	if info, statErr := os.Lstat(path); statErr == nil {
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || owner.Uid != uint32(os.Getuid()) || info.Size() > 32768 {
			return false, errors.New("unsafe_host_config")
		}
		current, readErr := os.ReadFile(path)
		parsed, parseErr := ReadHostConfig(bytes.NewReader(current))
		if readErr != nil || parseErr != nil || parsed != config {
			return false, errors.New("host_config_exists_and_differs")
		}
		return false, nil
	} else if !os.IsNotExist(statErr) {
		return false, statErr
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, fmt.Errorf("create host config directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".desktop-host-*.json")
	if err != nil {
		return false, fmt.Errorf("stage host config: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return false, fmt.Errorf("protect host config: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return false, fmt.Errorf("write host config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, fmt.Errorf("sync host config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("close host config: %w", err)
	}
	// Linking the complete staged file publishes without overwriting a config
	// concurrently created by another setup or administrator review.
	if err := os.Link(tmpPath, path); err != nil {
		return false, fmt.Errorf("install host config: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return true, err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return true, err
	}
	return true, nil
}
