package work

import (
	"strings"
	"testing"
)

func TestTmuxRunnerRequiresOwnedWatcherLifecycle(t *testing.T) {
	runner := TmuxRunner{}
	if _, err := runner.Spawn("codex", "/repo", "codex"); err == nil || !strings.Contains(err.Error(), "delegated watcher is required") {
		t.Fatalf("Spawn error = %v", err)
	}
	if err := runner.Abort("main:@42"); err == nil || !strings.Contains(err.Error(), "delegated watcher is required") {
		t.Fatalf("Abort error = %v", err)
	}
}
