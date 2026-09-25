package modelprofiles

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeClaudeSettings extracts and decodes the single CLI --settings argument
// appended by compileClaude.
func decodeClaudeSettings(t *testing.T, command string) ClaudeLaunchSettings {
	t.Helper()
	const marker = "--settings "
	idx := strings.Index(command, marker)
	if idx < 0 {
		t.Fatalf("launch command has no --settings payload: %q", command)
	}
	raw := strings.Trim(strings.TrimSpace(command[idx+len(marker):]), "'")
	var settings ClaudeLaunchSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		t.Fatalf("decode settings %q: %v", raw, err)
	}
	return settings
}

func compileClaudeLaunch(t *testing.T, baseCommand string, authMode string) (command string, env map[string]string) {
	t.Helper()
	profile := claudeMessagesProfile("claude-default-mode", "claude-sonnet-4-6", "upstream-claude")
	profile.AuthMode = authMode
	profile.CredentialEnv = ""
	resolved, err := Compile(baseCommand, profile, CompileOptions{
		LoopbackRouteURL:        "http://127.0.0.1:4317/r/rt_auto",
		CatalogRevision:         1,
		VerifiedProfileContract: contractFor(profile),
	})
	if err != nil {
		t.Fatalf("compile Claude launch: %v", err)
	}
	return resolved.Command, resolved.Env
}

func TestClaudeLaunchDefaultsToAutoPermissionMode(t *testing.T) {
	command, env := compileClaudeLaunch(t, "claude", AuthModeNone)
	if env[EnvAnthropicBaseURL] == "" {
		t.Fatalf("loopback route env missing: %#v", env)
	}
	if ClaudeDefaultPermissionMode != "auto" {
		t.Fatalf("Zen Claude default mode changed without a protocol review: %q", ClaudeDefaultPermissionMode)
	}
	settings := decodeClaudeSettings(t, command)
	if settings.Permissions.DefaultMode != ClaudeDefaultPermissionMode {
		t.Fatalf("default permission mode = %q, want %q", settings.Permissions.DefaultMode, ClaudeDefaultPermissionMode)
	}
	if settings.Env[EnvAnthropicBaseURL] == "" {
		t.Fatalf("loopback env must be part of the settings payload: %#v", settings.Env)
	}
	if strings.Contains(command, "--permission-mode") || strings.Contains(command, "--dangerously-skip-permissions") {
		t.Fatalf("default launch must not force a permission flag: %q", command)
	}
}

func TestClaudeLaunchExplicitPermissionModeIsPreserved(t *testing.T) {
	command, _ := compileClaudeLaunch(t, "claude --permission-mode bypassPermissions", AuthModeNone)
	if !strings.Contains(command, "--permission-mode bypassPermissions") {
		t.Fatalf("explicit permission mode was dropped: %q", command)
	}
	settings := decodeClaudeSettings(t, command)
	if settings.Permissions.DefaultMode != "auto" {
		t.Fatalf("settings default = %q, want auto", settings.Permissions.DefaultMode)
	}
}

func TestClaudeLaunchNativePassthroughStillGetsAutoDefault(t *testing.T) {
	command, _ := compileClaudeLaunch(t, "claude", AuthModeNativePassthrough)
	settings := decodeClaudeSettings(t, command)
	if settings.Permissions.DefaultMode != "auto" {
		t.Fatalf("passthrough default = %q, want auto", settings.Permissions.DefaultMode)
	}
	if len(settings.Env) != 0 {
		t.Fatalf("native passthrough must not inject route env into settings: %#v", settings.Env)
	}
}
