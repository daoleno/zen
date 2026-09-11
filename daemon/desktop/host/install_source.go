package host

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// InstalledBinary is the root-owned same-ELF path for broker and agent roles.
const InstalledBinary = "/usr/libexec/zen/zen"

func ResolveInstallSource(binary, broker, agent string) (string, error) {
	binary = strings.TrimSpace(binary)
	broker = strings.TrimSpace(broker)
	agent = strings.TrimSpace(agent)
	candidates := make([]string, 0, 3)
	for _, path := range []string{binary, broker, agent} {
		if path != "" {
			candidates = append(candidates, path)
		}
	}
	if len(candidates) == 0 {
		return "", errors.New("install_binary_required")
	}
	first := candidates[0]
	sum, err := fileDigest(first)
	if err != nil {
		return "", err
	}
	for _, path := range candidates[1:] {
		other, err := fileDigest(path)
		if err != nil {
			return "", err
		}
		if other != sum {
			return "", errors.New("install_role_binaries_differ")
		}
	}
	return first, nil
}

func fileDigest(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("install_binary_unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("install_binary_unavailable")
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, io.LimitReader(file, (64<<20)+1)); err != nil {
		return "", errors.New("invalid_install_binary")
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// ForbiddenBrokerPath reports user-writable DEV/home/tmp locations that a
// root-installed broker must never execute as the agent identity.
func ForbiddenBrokerPath(path string) bool {
	clean := filepath.Clean(path)
	switch clean {
	case "/tmp", "/var/tmp", "/dev/shm", "/home":
		return true
	}
	for _, prefix := range []string{"/home/", "/tmp/", "/var/tmp/", "/dev/shm/", "/run/user/"} {
		if strings.HasPrefix(clean, prefix) {
			return true
		}
	}
	return strings.Contains(clean, "/zen-dev") || strings.Contains(clean, "/.zen/")
}
