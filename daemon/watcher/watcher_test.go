package watcher

import (
	"reflect"
	"testing"
)

func TestBuildWindowCommandForShellStartsInteractiveLoginShell(t *testing.T) {
	got := buildWindowCommandForShell("/bin/zsh", "")
	want := "exec '/bin/zsh' -i -l"
	if got != want {
		t.Fatalf("buildWindowCommandForShell() = %q, want %q", got, want)
	}
}

func TestBuildWindowCommandForShellWrapsCommandInInteractiveLoginShell(t *testing.T) {
	got := buildWindowCommandForShell("/bin/zsh", "codex --dangerously-bypass-approvals-and-sandbox")
	want := "exec '/bin/zsh' -i -l -c 'codex --dangerously-bypass-approvals-and-sandbox'"
	if got != want {
		t.Fatalf("buildWindowCommandForShell(command) = %q, want %q", got, want)
	}
}

func TestTmuxWindowEnvironmentPreservesUsefulEnvAndSkipsTmuxManagedKeys(t *testing.T) {
	got := tmuxWindowEnvironment([]string{
		"OPENAI_API_KEY=test-key",
		"PATH=/usr/local/bin:/usr/bin",
		"TMUX=/tmp/tmux-1000/default,123,0",
		"TMUX_PANE=%1",
		"PWD=/tmp/demo",
		"TERM=screen-256color",
		"LANG=en_US.UTF-8",
	})
	want := []string{
		"LANG=en_US.UTF-8",
		"OPENAI_API_KEY=test-key",
		"PATH=/usr/local/bin:/usr/bin",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tmuxWindowEnvironment() = %v, want %v", got, want)
	}
}

func TestBaseSessionNameHandlesStableWindowIDs(t *testing.T) {
	got := baseSessionName("main:@3198")
	if got != "main" {
		t.Fatalf("baseSessionName() = %q, want %q", got, "main")
	}
}
