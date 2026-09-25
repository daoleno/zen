package watcher

import "testing"

// Auto mode is Zen's default Claude launch mode, so the readiness probe must
// accept its footer the same way it accepts bypass and manual mode footers.
func TestClaudeAutoModeFooterIsInputReady(t *testing.T) {
	pane := "Claude Code v2.1.282\n\n❯\u00a0\n⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\n"
	if !isClaudeInputReady(pane) {
		t.Fatal("Claude auto mode footer should be input-ready")
	}
	wrapped := "Claude Code v2.1.282\n❯\n⏵⏵ auto mode on · ? for shortcuts"
	if !isClaudeInputReady(wrapped) {
		t.Fatal("wrapped Claude auto mode footer should be input-ready")
	}
}
