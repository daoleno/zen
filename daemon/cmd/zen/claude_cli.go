package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/daoleno/zen/daemon/control"
)

// runClaudeCommand is the process boundary used by the installed claude shim.
// It resolves the real Claude binary, asks the daemon for a route-scoped launch
// plan, removes inherited Anthropic overrides, and execs the native CLI.
func runClaudeCommand(args []string, stderr io.Writer) error {
	native, err := findNativeClaude()
	if err != nil {
		return err
	}
	parts := []string{shellQuote(native)}
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	cfg, err := parseCLIConfig("zen claude", nil, stderr)
	if err != nil {
		return err
	}
	resp, err := callControl(cfg, control.Request{
		Type:     "claude_launch",
		Command:  strings.Join(parts, " "),
		WorkerID: "cli:claude:" + fmt.Sprint(os.Getpid()),
	})
	if err != nil {
		return err
	}
	if !resp.OK {
		if resp.Error != nil {
			return errors.New(resp.Error.Message)
		}
		return errors.New("Zen could not prepare Claude routing")
	}
	env := scrubClaudeEnv(os.Environ())
	for key, value := range resp.LaunchEnv {
		env = append(env, key+"="+value)
	}
	// Replace the Zen process so the route's PID remains the actual Claude
	// process across daemon restarts and no parent shell is left behind.
	return syscall.Exec("/bin/sh", []string{"sh", "-c", "exec " + resp.LaunchCommand}, env)
}

func findNativeClaude() (string, error) {
	wrapper := strings.TrimSpace(os.Getenv("ZEN_CLAUDE_WRAPPER"))
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, "claude")
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		if wrapper != "" {
			if w, werr := filepath.EvalSymlinks(wrapper); werr == nil && resolved == w {
				continue
			}
		}
		if info, err := os.Stat(resolved); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			return resolved, nil
		}
	}
	return "", errors.New("Claude Code is not installed on PATH (Zen wrapper was not recursive)")
}

func scrubClaudeEnv(input []string) []string {
	out := make([]string, 0, len(input)+4)
	for _, item := range input {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "ANTHROPIC_") || key == "CLAUDE_CODE_USE_BEDROCK" || key == "CLAUDE_CODE_USE_VERTEX" || key == "CLAUDE_CODE_USE_FOUNDRY" || key == "ZEN_CLAUDE_WRAPPER" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
