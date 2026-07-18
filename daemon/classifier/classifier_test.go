package classifier

import (
	"testing"
)

func TestClassify(t *testing.T) {
	grokChoiceFixture := []string{
		"◆ Task completed in 2m45s: Wait for dedicated em",
		"You hit your weekly limit.",
		"1 (O) Upgrade tier          Upgrade to a higher tier for more usage",
		"2 (O) Buy more credits      Purchase credits to keep using Grok Build",
		"↑/↓ navigate · y copy Enter:submit",
		"Esc:unselect | Tab:scrollback |",
	}
	tests := []struct {
		name       string
		paneAlive  bool
		command    string
		lines      []string
		wantState  AgentState
		wantSubstr string // substring expected in summary
	}{
		// === BLOCKED states ===
		{
			name:      "claude code Y/n prompt",
			paneAlive: true,
			lines:     []string{"Creating file src/main.go", "Do you want to create this file? (Y/n)"},
			wantState: StateBlocked,
		},
		{
			name:      "claude code y/N prompt",
			paneAlive: true,
			lines:     []string{"Deleting file old.go", "Are you sure? (y/N)"},
			wantState: StateBlocked,
		},
		{
			name:      "generic question mark ending",
			paneAlive: true,
			lines:     []string{"Should I continue with the migration?"},
			wantState: StateBlocked,
		},
		{
			name:      "approve/reject prompt",
			paneAlive: true,
			lines:     []string{"Please approve or reject this change"},
			wantState: StateBlocked,
		},
		{
			name:      "please approve prompt",
			paneAlive: true,
			lines:     []string{"Please approve this shell command"},
			wantState: StateBlocked,
		},
		{
			name:      "grok always-approve chrome is not blocked",
			paneAlive: true,
			lines: []string{
				"│ ❯                                                                        │",
				"╰─────────────────────────────────────── Grok 4.5 (high) · always-approve ─╯",
				"Shift+Tab:mode  │  Ctrl+c:cancel  │  Ctrl+g:send to bg  │  Ctrl+x:shortcuts",
			},
			wantState: StateUnknown,
		},
		{
			name:      "grok always-approve footer alone is not blocked",
			paneAlive: true,
			lines:     []string{"╰───── Grok 4.5 (high) · always-approve ─╯"},
			wantState: StateUnknown,
		},
		{
			name:      "would you like prompt",
			paneAlive: true,
			lines:     []string{"Would you like me to proceed with the refactor?"},
			wantState: StateBlocked,
		},
		{
			name:      "claude code run command prompt",
			paneAlive: true,
			lines:     []string{"Do you want to run `npm test`?"},
			wantState: StateBlocked,
		},
		{
			name:      "claude code edit prompt",
			paneAlive: true,
			lines:     []string{"Do you want to edit src/app.ts?"},
			wantState: StateBlocked,
		},
		{
			name:      "allow permission prompt",
			paneAlive: true,
			lines:     []string{"Allow Claude to read /etc/passwd?"},
			wantState: StateBlocked,
		},
		{
			name:      "blocked prompt",
			paneAlive: true,
			lines:     []string{"Shall I apply these changes?"},
			wantState: StateBlocked,
		},
		{
			name:      "codex approval picker with tmux status line",
			paneAlive: true,
			lines: []string{
				"$ systemctl list-unit-files --state=enabled --no-pager --no-legend",
				"› 1. Yes, proceed (y)",
				"  2. Yes, and don't ask again for commands that start with `systemctl` (p)",
				"  3. No, and tell Codex what to do differently (esc)",
				"Press enter to confirm or esc to cancel",
				"[zen-81984.] Action Required 23:31 19-6月-26",
			},
			wantState: StateBlocked,
		},
		{
			name:       "grok provider-native choice menu is blocked",
			paneAlive:  true,
			command:    "grok --no-alt-screen --permission-mode bypassPermissions",
			lines:      grokChoiceFixture,
			wantState:  StateBlocked,
			wantSubstr: "navigate",
		},
		{
			name:      "grok unicode radio choice menu is blocked",
			paneAlive: true,
			command:   "grok",
			lines: []string{
				"You hit your free usage limit.",
				"1 ○ Upgrade to SuperGrok",
				"2 ○ Upgrade to SuperGrok Heavy",
				"↑/↓ navigate · enter confirm · esc cancel",
			},
			wantState: StateBlocked,
		},
		{
			name:      "grok choice menu with selection caret is blocked",
			paneAlive: true,
			command:   "/usr/bin/grok --resume abc",
			lines: []string{
				"You hit your weekly limit.",
				"› 1 (O) Upgrade tier          Upgrade to a higher tier for more usage",
				"  2 (O) Buy more credits      Purchase credits to keep using Grok Build",
				"↑/↓ navigate · y copy Enter:submit",
			},
			wantState:  StateBlocked,
			wantSubstr: "navigate",
		},
		{
			name:      "grok-shaped fixture under codex is not phantom-blocked",
			paneAlive: true,
			command:   "codex --dangerously-bypass-approvals-and-sandbox",
			lines:     grokChoiceFixture,
			wantState: StateUnknown,
		},
		{
			name:      "grok-shaped fixture under claude is not phantom-blocked",
			paneAlive: true,
			command:   "claude --dangerously-skip-permissions",
			lines:     grokChoiceFixture,
			wantState: StateUnknown,
		},
		{
			name:      "grok-shaped fixture under cursor-agent is not phantom-blocked",
			paneAlive: true,
			command:   "cursor-agent --force --sandbox disabled",
			lines:     grokChoiceFixture,
			wantState: StateUnknown,
		},
		{
			name:      "grok-shaped fixture under shell is not phantom-blocked",
			paneAlive: true,
			command:   "zsh",
			lines:     grokChoiceFixture,
			wantState: StateUnknown,
		},
		{
			name:      "grok-shaped fixture with unknown command is not phantom-blocked",
			paneAlive: true,
			command:   "",
			lines:     grokChoiceFixture,
			wantState: StateUnknown,
		},
		{
			name:      "numbered lines without choice footer stay unknown",
			paneAlive: true,
			command:   "grok",
			lines: []string{
				"1 (O) noted in notes",
				"2 (O) also noted",
				"Shift+Tab:mode  │  Ctrl+c:cancel",
			},
			wantState: StateUnknown,
		},
		{
			name:      "navigate footer without numbered choices stays unknown",
			paneAlive: true,
			command:   "grok",
			lines: []string{
				"Resume session",
				"↑/↓ navigate · enter confirm · esc cancel",
			},
			wantState: StateUnknown,
		},
		{
			name:      "resolved grok pane without choice menu is unknown",
			paneAlive: true,
			command:   "grok",
			lines: []string{
				"│ ❯                                                                        │",
				"╰─────────────────────────────────────── Grok 4.5 (high) · always-approve ─╯",
				"Shift+Tab:mode  │  Ctrl+c:cancel  │  Ctrl+g:send to bg  │  Ctrl+x:shortcuts",
			},
			wantState: StateUnknown,
		},

		// === FAILED states ===
		{
			name:      "error in output",
			paneAlive: true,
			lines:     []string{"Compiling...", "error: cannot find module 'foo'"},
			wantState: StateFailed,
		},
		{
			name:      "panic in output",
			paneAlive: true,
			lines:     []string{"panic: runtime error: index out of range"},
			wantState: StateFailed,
		},
		{
			name:      "python traceback",
			paneAlive: true,
			lines:     []string{"Traceback (most recent call last):", "  File 'main.py', line 1"},
			wantState: StateFailed,
		},
		{
			name:      "permission denied",
			paneAlive: true,
			lines:     []string{"permission denied: /root/.ssh/id_rsa"},
			wantState: StateFailed,
		},
		{
			name:      "uppercase failed diagnostic",
			paneAlive: true,
			lines:     []string{"Running tests...", "FAILED: 3 tests failed"},
			wantState: StateFailed,
		},
		{
			name:      "dead pane with error",
			paneAlive: false,
			lines:     []string{"Running tests...", "FAILED: 3 tests failed"},
			wantState: StateFailed,
		},

		// === DONE states ===
		{
			name:      "dead pane normal exit",
			paneAlive: false,
			lines:     []string{"All tasks completed successfully.", "Goodbye!"},
			wantState: StateDone,
		},
		{
			name:      "dead pane no output",
			paneAlive: false,
			lines:     []string{},
			wantState: StateDone,
		},

		// === IDLE / UNKNOWN (alive pane without durable activity signal) ===
		{
			name:      "active output without progress is idle unknown",
			paneAlive: true,
			lines:     []string{"Reading file src/main.go...", "Analyzing dependencies..."},
			wantState: StateUnknown,
		},
		{
			name:      "recent output churn without progress is idle unknown",
			paneAlive: true,
			lines:     []string{"Writing test file..."},
			wantState: StateUnknown,
		},
		{
			name:      "heartbeat log with failed in agent name is not failure",
			paneAlive: true,
			lines: []string{
				"2026/06/08 00:20:19 brain heartbeat wake sent for brain-agent-zen-classifier-false-failed-1780",
			},
			wantState: StateUnknown,
		},
		{
			name:      "lifecycle valid values mention failed without failure",
			paneAlive: true,
			lines: []string{
				`"$ZEN_AGENT_PROGRESS_CMD" agent progress --status running --phase working --attention none --summary "Still checking" --lease 300`,
				`- Valid status values: running, done, failed, blocked.`,
				`- Valid phase values: starting, reading, planning, working, verifying, reporting.`,
				`- Valid attention values: none, done, blocked, failed, user_input, stale.`,
			},
			wantState: StateUnknown,
		},

		// === UNKNOWN states ===
		{
			name:      "no recognizable pattern",
			paneAlive: true,
			lines:     []string{"some random output that doesn't match anything"},
			wantState: StateUnknown,
		},
		{
			name:      "ordinary shell prompt stays unknown not running",
			paneAlive: true,
			lines:     []string{"user@host:~$ ls", "README.md", "user@host:~$"},
			wantState: StateUnknown,
		},
		{
			name:      "daemon transcript lookup warning is nonfatal",
			paneAlive: true,
			lines: []string{
				"2026/06/07 15:33:54 work transcript lookup failed for codex (/home/daoleno/workspace/onlora):",
				"query codex threads: exit status 5: Error: in prepare, database is locked (5)",
			},
			wantState: StateUnknown,
		},
		{
			name:      "daemon transcript lookup warning block ends at next timestamped log",
			paneAlive: true,
			lines: []string{
				"2026/06/07 15:33:54 work transcript lookup failed for codex (/home/daoleno/workspace/onlora):",
				"query codex threads: exit status 5: Error: in prepare, database is locked (5)",
				"2026/06/07 15:33:55 [stats] refresh complete",
				"error: daemon crashed",
			},
			wantState: StateFailed,
		},
		{
			name:      "alive but empty output",
			paneAlive: true,
			lines:     []string{},
			wantState: StateUnknown,
		},

		// === Edge cases ===
		{
			name:      "blank lines before blocked prompt",
			paneAlive: true,
			lines:     []string{"", "", "", "Do you want to proceed? (Y/n)"},
			wantState: StateBlocked,
		},
		{
			name:      "mixed: error then question (last line wins for blocked)",
			paneAlive: true,
			lines:     []string{"error: something went wrong", "Would you like me to fix it?"},
			wantState: StateBlocked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotState, gotSummary := Classify(tt.paneAlive, tt.lines, tt.command)
			if gotState != tt.wantState {
				t.Errorf("Classify() state = %q, want %q (summary: %q)", gotState, tt.wantState, gotSummary)
			}
			if tt.wantSubstr != "" && gotSummary == "" {
				t.Errorf("Classify() summary is empty, want substring %q", tt.wantSubstr)
			}
		})
	}
}

func TestLastNonEmpty(t *testing.T) {
	lines := []string{"hello", "", "world", "", "foo", ""}
	got := lastNonEmpty(lines, 2)
	if len(got) != 2 || got[0] != "world" || got[1] != "foo" {
		t.Errorf("lastNonEmpty() = %v, want [world, foo]", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 100); got != "short" {
		t.Errorf("truncate short = %q", got)
	}
	long := "this is a very long string that should be truncated at some point because it exceeds the maximum length"
	got := truncate(long, 50)
	if len(got) > 50 {
		t.Errorf("truncate long len = %d, want <= 50", len(got))
	}
	if got[len(got)-3:] != "..." {
		t.Errorf("truncate long should end with '...', got %q", got)
	}
}
