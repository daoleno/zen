package classifier

import "testing"

func TestCursorActivityAdapter_PaneAndProcessFacts(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		pane       string
		toolChild  bool
		wantState  AgentState
		wantSource string
	}{
		{
			name:       "running stop marker",
			command:    "cursor-agent --force",
			pane:       "Cursor Agent\nRunning\n→ Add a follow-up  ctrl+c to stop",
			wantState:  StateRunning,
			wantSource: "cursor_pane_stop_marker",
		},
		{
			name:       "idle composer",
			command:    "cursor-agent",
			pane:       "Cursor Agent\n→ Add a follow-up",
			wantState:  StateUnknown,
			wantSource: "cursor_idle",
		},
		{
			name:       "non MCP tool child",
			command:    "cursor-agent",
			pane:       "Cursor Agent\n→ Add a follow-up",
			toolChild:  true,
			wantState:  StateRunning,
			wantSource: "cursor_tool_child",
		},
		{
			name:       "workspace trust",
			command:    "cursor-agent",
			pane:       "Cursor Agent\nWorkspace Trust Required\nTrust this workspace?",
			wantState:  StateBlocked,
			wantSource: "cursor_pane_trust",
		},
		{
			name:       "permission",
			command:    "cursor-agent",
			pane:       "Cursor Agent\nWaiting for approval\nAllow this action?",
			wantState:  StateBlocked,
			wantSource: "cursor_pane_permission",
		},
		{
			name:      "ordinary shell",
			command:   "zsh",
			pane:      "$ go test ./...\nok\n$",
			wantState: "",
		},
	}

	adapter := NewCursorActivityAdapter()
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			input := ActivityInput{
				Agent:           Agent{Command: testCase.command},
				PaneContent:     testCase.pane,
				ToolChildActive: testCase.toolChild,
			}
			if !adapter.Match(input) {
				if testCase.wantState == "" {
					return
				}
				t.Fatal("adapter did not match provider pane")
			}
			got := adapter.Infer(input)
			if got.State != testCase.wantState || got.Source != testCase.wantSource {
				t.Fatalf("signal = %#v, want state=%q source=%q", got, testCase.wantState, testCase.wantSource)
			}
		})
	}
}

func TestMergeActivitySignal_OnlyUpgradesUnknown(t *testing.T) {
	got, summary := MergeActivitySignal(StateRunning, "lease", ActivitySignal{State: StateBlocked, Summary: "trust", Source: "cursor_pane_trust"})
	if got != StateRunning || summary != "lease" {
		t.Fatalf("got %q %q", got, summary)
	}
	got, summary = MergeActivitySignal(StateUnknown, "idle", ActivitySignal{State: StateRunning, Summary: "generating", Source: "cursor_pane_stop_marker"})
	if got != StateRunning || summary != "generating" {
		t.Fatalf("got %q %q", got, summary)
	}
}
