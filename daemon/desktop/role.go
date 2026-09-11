package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	RoleHelper   = "desktop-helper"
	RoleHost     = "desktop-host"
	RoleAgent    = "desktop-agent"
	RoleIdentity = "desktop-identity"
)

// HelperOverrideEnv is a test-only absolute helper path. Production default
// launches RoleHelper from the running zen executable.
const HelperOverrideEnv = "ZEN_DESKTOP_HELPER"

var lookupExecutable = os.Executable

var (
	ErrHelperUnavailable = errors.New("desktop helper is unavailable")
)

func IsInternalRole(name string) bool {
	switch name {
	case RoleHelper, RoleHost, RoleAgent, RoleIdentity:
		return true
	default:
		return false
	}
}

// CurrentExecutable returns the running ELF, symlink-resolved, without PATH search.
func CurrentExecutable() (string, error) {
	exe, err := lookupExecutable()
	if err != nil {
		return "", err
	}
	if exe == "" {
		return "", ErrHelperUnavailable
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return filepath.Clean(exe), nil
	}
	return resolved, nil
}

func FileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, io.LimitReader(file, 256<<20)); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

type Identity struct {
	Executable       string   `json:"executable"`
	SHA256           string   `json:"sha256"`
	Native           bool     `json:"native"`
	NativeBuildInput string   `json:"native_build_input,omitempty"`
	Roles            []string `json:"roles"`
}

func NewIdentity(native bool) (Identity, error) {
	exe, err := CurrentExecutable()
	if err != nil {
		return Identity{}, err
	}
	sum, err := FileSHA256(exe)
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		Executable: exe,
		SHA256:     sum,
		Native:     native,
		Roles:      []string{RoleHelper, RoleHost, RoleAgent},
	}, nil
}

func (id Identity) Marshal() ([]byte, error) {
	return json.Marshal(id)
}

// HelperCommand launches the attended helper. Default is the same zen ELF with
// RoleHelper. An absolute ZEN_DESKTOP_HELPER remains a test override only.
func HelperCommand(args []string) (*exec.Cmd, error) {
	if override := strings.TrimSpace(os.Getenv(HelperOverrideEnv)); override != "" {
		if !filepath.IsAbs(override) {
			return nil, ErrHelperUnavailable
		}
		info, err := os.Stat(override)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			return nil, ErrHelperUnavailable
		}
		return exec.Command(override, args...), nil
	}
	exe, err := CurrentExecutable()
	if err != nil {
		return nil, ErrHelperUnavailable
	}
	info, err := os.Stat(exe)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return nil, ErrHelperUnavailable
	}
	roleArgs := append([]string{RoleHelper}, args...)
	return exec.Command(exe, roleArgs...), nil
}
