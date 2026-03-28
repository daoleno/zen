package classifier

import (
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name       string
		paneAlive  bool
		lines      []string
		staleCount int
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
			name:      "blocked after stale period",
			paneAlive: true,
			lines:     []string{"Shall I apply these changes?"},
			staleCount: 50,
			wantState:  StateBlocked,
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

		// === RUNNING states ===
		{
			name:       "active output",
			paneAlive:  true,
			lines:      []string{"Reading file src/main.go...", "Analyzing dependencies..."},
			staleCount: 0,
			wantState:  StateRunning,
		},
		{
			name:       "recently active",
			paneAlive:  true,
			lines:      []string{"Writing test file..."},
			staleCount: 10,
			wantState:  StateRunning,
		},

		// === UNKNOWN states ===
		{
			name:       "stale with no recognizable pattern",
			paneAlive:  true,
			lines:      []string{"some random output that doesn't match anything"},
			staleCount: 50,
			wantState:  StateUnknown,
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
			name:       "mixed: error then question (last line wins for blocked)",
			paneAlive:  true,
			lines:      []string{"error: something went wrong", "Would you like me to fix it?"},
			staleCount: 0,
			wantState:  StateBlocked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotState, gotSummary := Classify(tt.paneAlive, tt.lines, tt.staleCount)
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
