package classifier

import (
	"testing"
	"time"
)

func TestInferCursorActivity_RunningStopMarker(t *testing.T) {
	pane := `
Cursor Agent

  Running  29.72k tokens

  → Add a follow-up                                             ctrl+c to stop

  Cursor Grok 4.5 High Fast · 12.9% · 37 files edited           Run Everything
`
	got := InferCursorActivity(CursorActivityInput{
		Command:     "cursor-agent --force",
		PaneContent: pane,
	})
	if got.State != StateRunning || got.Source != "cursor_pane_stop_marker" {
		t.Fatalf("got %#v, want running via stop marker", got)
	}
}

func TestInferCursorActivity_IdleComposerWithoutStop(t *testing.T) {
	pane := `
Cursor Agent

  → Add a follow-up

  Cursor Grok · 72.·19 files  Run Everything
  ~/workspace/zen · main
`
	got := InferCursorActivity(CursorActivityInput{
		Command:     "cursor-agent",
		PaneContent: pane,
	})
	if got.State != StateUnknown || got.Source != "cursor_idle" {
		t.Fatalf("got %#v, want unknown idle composer", got)
	}
}

func TestInferCursorActivity_ToolChildReinforcesRunning(t *testing.T) {
	pane := `
Cursor Agent

  → Add a follow-up
`
	got := InferCursorActivity(CursorActivityInput{
		Command:         "cursor-agent",
		PaneContent:     pane,
		ToolChildActive: true,
	})
	if got.State != StateRunning || got.Source != "cursor_tool_child" {
		t.Fatalf("got %#v, want running via tool child", got)
	}
}

func TestInferCursorActivity_TranscriptActiveWithoutPaneStop(t *testing.T) {
	active := true
	pane := `
Cursor Agent

  → Add a follow-up
`
	got := InferCursorActivity(CursorActivityInput{
		Command:          "cursor-agent",
		PaneContent:      pane,
		TranscriptActive: &active,
	})
	if got.State != StateRunning || got.Source != "cursor_transcript_active" {
		t.Fatalf("got %#v, want running via transcript", got)
	}
}

func TestInferCursorActivity_BlockedWorkspaceTrust(t *testing.T) {
	pane := `
Cursor Agent
Workspace Trust Required
Trust this workspace to continue?
`
	got := InferCursorActivity(CursorActivityInput{
		Command:     "cursor-agent",
		PaneContent: pane,
	})
	if got.State != StateBlocked || got.Source != "cursor_pane_trust" {
		t.Fatalf("got %#v, want blocked trust", got)
	}
}

func TestInferCursorActivity_BlockedPermission(t *testing.T) {
	pane := "Cursor Agent\nWaiting for approval\nAllow this action?"
	got := InferCursorActivity(CursorActivityInput{
		Command:     "cursor-agent",
		PaneContent: pane,
	})
	if got.State != StateBlocked {
		t.Fatalf("got %#v, want blocked permission", got)
	}
}

func TestInferCursorActivity_OrdinaryShellIgnored(t *testing.T) {
	pane := `
$ go test ./...
ok
$ echo scrolling
scrolling
$
`
	got := InferCursorActivity(CursorActivityInput{
		Command:     "zsh",
		PaneContent: pane,
	})
	if got.State != "" {
		t.Fatalf("got %#v, want empty signal for ordinary shell", got)
	}
}

func TestTranscriptTurnActiveAlias(t *testing.T) {
	if !CursorTranscriptTurnActive(10, 5) {
		t.Fatal("alias should match TranscriptTurnActive")
	}
}

func TestResolveSessionStatus_TruthTable(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	progressAt := now.Add(-time.Minute)
	activeLease := now.Add(2 * time.Minute)
	expiredLease := now.Add(-time.Minute)
	trueVal := true

	tests := []struct {
		name       string
		agent      *Agent
		classified AgentState
		activity   ActivitySignal
		want       AgentState
	}{
		{
			name: "cursor running stop marker fills unknown",
			agent: &Agent{
				PaneAlive: true,
				State:     StateUnknown,
				Command:   "cursor-agent",
			},
			classified: StateUnknown,
			activity:   InferCursorActivity(CursorActivityInput{Command: "cursor-agent", PaneContent: "Cursor Agent\nctrl+c to stop"}),
			want:       StateRunning,
		},
		{
			name: "cursor idle stays unknown",
			agent: &Agent{
				PaneAlive: true,
				State:     StateUnknown,
				Command:   "cursor-agent",
			},
			classified: StateUnknown,
			activity:   InferCursorActivity(CursorActivityInput{Command: "cursor-agent", PaneContent: "Cursor Agent\n→ Add a follow-up"}),
			want:       StateUnknown,
		},
		{
			name: "cursor transcript active fills unknown",
			agent: &Agent{
				PaneAlive: true,
				State:     StateUnknown,
				Command:   "cursor-agent",
			},
			classified: StateUnknown,
			activity: InferCursorActivity(CursorActivityInput{
				Command:          "cursor-agent",
				PaneContent:      "Cursor Agent\n→ Add a follow-up",
				TranscriptActive: &trueVal,
			}),
			want: StateRunning,
		},
		{
			name: "cursor tool child fills unknown",
			agent: &Agent{
				PaneAlive: true,
				State:     StateUnknown,
				Command:   "cursor-agent",
			},
			classified: StateUnknown,
			activity: InferCursorActivity(CursorActivityInput{
				Command:         "cursor-agent",
				PaneContent:     "Cursor Agent",
				ToolChildActive: true,
			}),
			want: StateRunning,
		},
		{
			name: "cursor blocked permission fills unknown",
			agent: &Agent{
				PaneAlive: true,
				State:     StateUnknown,
				Command:   "cursor-agent",
			},
			classified: StateUnknown,
			activity:   InferCursorActivity(CursorActivityInput{Command: "cursor-agent", PaneContent: "Cursor Agent\nPermission required"}),
			want:       StateBlocked,
		},
		{
			name: "ordinary shell stays unknown",
			agent: &Agent{
				PaneAlive: true,
				State:     StateUnknown,
				Command:   "zsh",
			},
			classified: StateUnknown,
			activity:   InferCursorActivity(CursorActivityInput{Command: "zsh", PaneContent: "ok\n$ ls\nfile\n$"}),
			want:       StateUnknown,
		},
		{
			name: "active brain lease wins over idle cursor pane",
			agent: &Agent{
				PaneAlive:           true,
				State:               StateRunning,
				Summary:             "Brain lease work",
				LastProgressAt:      &progressAt,
				ExpectedNextCheckAt: &activeLease,
				Command:             "cursor-agent",
			},
			classified: StateUnknown,
			activity:   InferCursorActivity(CursorActivityInput{Command: "cursor-agent", PaneContent: "Cursor Agent\n→ Add a follow-up"}),
			want:       StateRunning,
		},
		{
			name: "expired lease falls through to cursor stop marker",
			agent: &Agent{
				PaneAlive:           true,
				State:               StateRunning,
				Summary:             "stale lease",
				LastProgressAt:      &progressAt,
				ExpectedNextCheckAt: &expiredLease,
				Command:             "cursor-agent",
			},
			classified: StateUnknown,
			activity:   InferCursorActivity(CursorActivityInput{Command: "cursor-agent", PaneContent: "Cursor Agent\nctrl+c to stop"}),
			want:       StateRunning,
		},
		{
			name: "sticky done not overridden by cursor running",
			agent: &Agent{
				PaneAlive:      true,
				State:          StateDone,
				Summary:        "Finished",
				LastProgressAt: &progressAt,
				Command:        "cursor-agent",
			},
			classified: StateUnknown,
			activity:   InferCursorActivity(CursorActivityInput{Command: "cursor-agent", PaneContent: "Cursor Agent\nctrl+c to stop"}),
			want:       StateDone,
		},
		{
			name: "sticky failed not overridden",
			agent: &Agent{
				PaneAlive:      true,
				State:          StateFailed,
				Summary:        "Boom",
				LastProgressAt: &progressAt,
				Command:        "cursor-agent",
			},
			classified: StateUnknown,
			activity:   InferCursorActivity(CursorActivityInput{Command: "cursor-agent", PaneContent: "Cursor Agent\nctrl+c to stop"}),
			want:       StateFailed,
		},
		{
			name: "classify blocked overrides cursor running activity",
			agent: &Agent{
				PaneAlive: true,
				State:     StateUnknown,
				Command:   "cursor-agent",
			},
			classified: StateBlocked,
			activity:   InferCursorActivity(CursorActivityInput{Command: "cursor-agent", PaneContent: "Cursor Agent\nctrl+c to stop"}),
			want:       StateBlocked,
		},
		{
			name: "classify failed overrides cursor running activity",
			agent: &Agent{
				PaneAlive: true,
				State:     StateUnknown,
				Command:   "cursor-agent",
			},
			classified: StateFailed,
			activity:   InferCursorActivity(CursorActivityInput{Command: "cursor-agent", PaneContent: "Cursor Agent\nctrl+c to stop"}),
			want:       StateFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := ResolveSessionStatus(tt.agent, tt.classified, "detail", now, tt.activity)
			if got != tt.want {
				t.Fatalf("state = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMergeActivitySignal_OnlyUpgradesUnknown(t *testing.T) {
	got, summary := MergeActivitySignal(StateRunning, "lease", ActivitySignal{State: StateBlocked, Summary: "trust", Source: "x"})
	if got != StateRunning || summary != "lease" {
		t.Fatalf("got %q %q", got, summary)
	}
	got, summary = MergeActivitySignal(StateUnknown, "idle", ActivitySignal{State: StateRunning, Summary: "gen", Source: "x"})
	if got != StateRunning || summary != "gen" {
		t.Fatalf("got %q %q", got, summary)
	}
}
