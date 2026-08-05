package work

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const piOwnedSessionDirName = "provider-sessions/pi"

// NewPiOwnedSessionPath allocates an absolute Zen-owned Pi JSONL path that is
// unique a priori. Ownership does not depend on Pi's shared per-CWD directory.
func NewPiOwnedSessionPath(zenHome string) (string, error) {
	root, err := piOwnedSessionRoot(zenHome)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(root, uuid.NewString()+".jsonl")
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("pi owned session path is not absolute: %s", path)
	}
	return path, nil
}

func piOwnedSessionRoot(zenHome string) (string, error) {
	zenHome = strings.TrimSpace(zenHome)
	if zenHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		zenHome = filepath.Join(home, ".zen")
	}
	return filepath.Join(zenHome, piOwnedSessionDirName), nil
}

// EnsurePiSessionLaunchCommand injects an absolute --session path when the
// command is a Pi launch that does not already own one. Commands that already
// declare an absolute --session or exclusive --session-dir are unchanged.
// Continue/resume/no-session shapes are left untouched so callers can reject them.
func EnsurePiSessionLaunchCommand(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		command = AgentProviderPi
	}
	if InferAgentProvider(command) != AgentProviderPi {
		return command, nil
	}
	options, ok := inspectLaunchCommandOptions(command)
	if !ok {
		return command, nil
	}
	if !scheduledProviderExecutable(AgentProviderPi, options.executable) {
		return command, nil
	}
	continuePresent, _ := options.option("", "--continue", "-c")
	resumePresent, _ := options.option("", "--resume", "-r")
	noSessionPresent, _ := options.option("", "--no-session")
	if continuePresent || resumePresent || noSessionPresent {
		return command, nil
	}
	sessionPresent, sessionPath := options.optionValue("--session")
	sessionDirPresent, sessionDir := options.optionValue("--session-dir")
	if sessionPresent {
		if sessionPath != "" && filepath.IsAbs(sessionPath) {
			return command, nil
		}
		return "", fmt.Errorf("pi --session requires an absolute path")
	}
	if sessionDirPresent {
		if sessionDir != "" && filepath.IsAbs(sessionDir) {
			return command, nil
		}
		return "", fmt.Errorf("pi --session-dir requires an absolute path")
	}
	if options.terminated {
		return "", fmt.Errorf("pi launch places arguments after an option terminator")
	}
	ownedPath, err := NewPiOwnedSessionPath("")
	if err != nil {
		return "", err
	}
	return appendCommandOptions(command, "--session", shellQuoteForLaunch(ownedPath)), nil
}

// PiOwnedSessionPath extracts the absolute --session path from a Pi launch
// command when present.
func PiOwnedSessionPath(command string) string {
	options, ok := inspectLaunchCommandOptions(command)
	if !ok {
		return ""
	}
	present, path := options.optionValue("--session")
	if !present {
		return ""
	}
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	return path
}

// PiOwnedSessionDir extracts the absolute --session-dir path from a Pi launch
// command when present.
func PiOwnedSessionDir(command string) string {
	options, ok := inspectLaunchCommandOptions(command)
	if !ok {
		return ""
	}
	present, path := options.optionValue("--session-dir")
	if !present {
		return ""
	}
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	return path
}

// OpenCodeOwnedSessionID extracts an explicit -s/--session ses_* ownership
// token from an OpenCode launch command. --continue is never treated as owned.
func OpenCodeOwnedSessionID(command string) string {
	options, ok := inspectLaunchCommandOptions(command)
	if !ok {
		return ""
	}
	if present, _ := options.option("", "--continue", "-c"); present {
		return ""
	}
	for _, name := range []string{"--session", "-s"} {
		present, value := options.optionValue(name)
		if !present {
			continue
		}
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "ses_") {
			return value
		}
	}
	return ""
}

func shellQuoteForLaunch(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if !strings.ContainsAny(value, " \t\"'\\$`") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
