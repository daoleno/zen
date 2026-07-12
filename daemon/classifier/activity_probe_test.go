package classifier

import "testing"

type stubAdapter struct {
	name   string
	match  bool
	signal ActivitySignal
	inferN int
	matchN int
}

func (s *stubAdapter) Name() string { return s.name }

func (s *stubAdapter) Match(in ActivityInput) bool {
	s.matchN++
	return s.match
}

func (s *stubAdapter) Infer(in ActivityInput) ActivitySignal {
	s.inferN++
	out := s.signal
	if out.Provider == "" {
		out.Provider = s.name
	}
	return out
}

func TestMultiActivityProbe_FirstMatchWins(t *testing.T) {
	codex := &stubAdapter{
		name:   "codex",
		match:  false,
		signal: ActivitySignal{State: StateRunning, Source: "codex_stub"},
	}
	cursor := &stubAdapter{
		name:   "cursor",
		match:  true,
		signal: ActivitySignal{State: StateRunning, Source: "cursor_stub"},
	}
	probe := NewActivityProbe(codex, cursor)
	got := probe.Infer(ActivityInput{
		Agent:       Agent{Command: "cursor-agent"},
		PaneContent: "Cursor Agent",
	})
	if got.Source != "cursor_stub" || got.Provider != "cursor" {
		t.Fatalf("got %#v", got)
	}
	if codex.inferN != 0 || cursor.inferN != 1 {
		t.Fatalf("infer counts codex=%d cursor=%d", codex.inferN, cursor.inferN)
	}
}

func TestMultiActivityProbe_NoMatchEmpty(t *testing.T) {
	probe := NewActivityProbe(&stubAdapter{name: "codex", match: false})
	got := probe.Infer(ActivityInput{Agent: Agent{Command: "zsh"}, PaneContent: "$ ls"})
	if got.State != "" || got.Source != "" {
		t.Fatalf("got %#v, want empty", got)
	}
}

func TestDefaultActivityProbe_IncludesCursor(t *testing.T) {
	probe := DefaultActivityProbe()
	got := probe.Infer(ActivityInput{
		Agent:       Agent{Command: "cursor-agent"},
		PaneContent: "Cursor Agent\nctrl+c to stop\n",
	})
	if got.State != StateRunning || got.Provider != "cursor" {
		t.Fatalf("got %#v", got)
	}
}

func TestTranscriptTurnActive(t *testing.T) {
	if !TranscriptTurnActive(10, 5) {
		t.Fatal("user after turn_ended should be active")
	}
	if TranscriptTurnActive(5, 10) {
		t.Fatal("turn_ended after user should be inactive")
	}
	if !TranscriptTurnActive(3, -1) {
		t.Fatal("user with no turn_ended should be active")
	}
	if TranscriptTurnActive(-1, 2) {
		t.Fatal("no user should be inactive")
	}
}
