package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	body, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encode host config: %w", err)
	}
	body = append(body, '\n')

	if current, readErr := os.ReadFile(path); readErr == nil {
		var existing HostConfig
		parsed, parseErr := ReadHostConfig(bytes.NewReader(current))
		if parseErr != nil ||
			json.Unmarshal(current, &existing) != nil || parsed != existing || existing != config {
			return false, errors.New("host_config_exists_and_differs")
		}
		return false, nil
	} else if !os.IsNotExist(readErr) {
		return false, readErr
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
	if err := os.Rename(tmpPath, path); err != nil {
		return false, fmt.Errorf("install host config: %w", err)
	}
	return true, nil
}
