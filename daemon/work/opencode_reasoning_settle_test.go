package work

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// openCodeReasoningTurnSeeds builds the real OpenCode v1.18 terminal-turn
// shape observed in production: the last assistant message carries
// finish:"unknown" with the authoritative time.completed marker, its parts are
// step-start / reasoning / step-finish(reason:"unknown"), and every earlier
// step closed with tool-calls. Prior parser logic treated "unknown" like a
// live tool-calls yield, so the turn never settled and the reasoning event
// stayed partial — the Interface reasoning icon spun forever on completed
// history.
func openCodeReasoningTurnSeeds(started time.Time, terminalMessageData string) ([]openCodeMessageSeed, []openCodePartSeed) {
	messages := []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_rsn", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user","time":{"created":1}}`},
		{ID: "msg_yield", SessionID: "ses_rsn", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: fmt.Sprintf(`{"role":"assistant","finish":"tool-calls","time":{"created":1,"completed":%d}}`, started.Add(5*time.Second).UnixMilli())},
		{ID: "msg_terminal", SessionID: "ses_rsn", CreatedMS: started.Add(10 * time.Second).UnixMilli(), Data: terminalMessageData},
	}
	parts := []openCodePartSeed{
		{ID: "p_user", MessageID: "msg_user", SessionID: "ses_rsn", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"task"}`},
		{ID: "p_s1", MessageID: "msg_yield", SessionID: "ses_rsn", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
		{ID: "p_x1", MessageID: "msg_yield", SessionID: "ses_rsn", CreatedMS: started.Add(3 * time.Second).UnixMilli(), Data: `{"type":"reasoning","text":"first step thinking"}`},
		{ID: "p_t1", MessageID: "msg_yield", SessionID: "ses_rsn", CreatedMS: started.Add(4 * time.Second).UnixMilli(), Data: `{"type":"tool","tool":"bash","callID":"call-1","state":{"status":"completed","input":{"command":"true"},"output":"ok"}}`},
		{ID: "p_f1", MessageID: "msg_yield", SessionID: "ses_rsn", CreatedMS: started.Add(5 * time.Second).UnixMilli(), Data: `{"type":"step-finish","reason":"tool-calls"}`},
		{ID: "p_s2", MessageID: "msg_terminal", SessionID: "ses_rsn", CreatedMS: started.Add(10 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
		{ID: "p_x2", MessageID: "msg_terminal", SessionID: "ses_rsn", CreatedMS: started.Add(11 * time.Second).UnixMilli(), Data: `{"type":"reasoning","text":"terminal reasoning loaded"}`},
		{ID: "p_f2", MessageID: "msg_terminal", SessionID: "ses_rsn", CreatedMS: started.Add(14 * time.Second).UnixMilli(), Data: `{"type":"step-finish","reason":"unknown"}`},
	}
	return messages, parts
}

// TestOpenCodeUnknownFinishedTerminalMessageSettlesCompletedTurn is the red
// regression for the spinner defect: OpenCode's real terminal message carries
// finish:"unknown" + time.completed, and the turn must settle completed with
// the reasoning event losing Partial so the Interface settles the icon.
func TestOpenCodeUnknownFinishedTerminalMessageSettlesCompletedTurn(t *testing.T) {
	started := time.Date(2026, 8, 8, 0, 0, 10, 0, time.UTC)
	terminalData := fmt.Sprintf(
		`{"role":"assistant","finish":"unknown","time":{"created":1,"completed":%d}}`,
		started.Add(14*time.Second).UnixMilli(),
	)
	messages, parts := openCodeReasoningTurnSeeds(started, terminalData)
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_rsn", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(15 * time.Second).UnixMilli()},
	}, messages, parts)
	got, err := parseOpenCodeConversation(dbPath, "ses_rsn")
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("unknown+completed terminal message must settle completed: %+v", got.Activity)
	}
	wantSettled := normalizeCodexTimestamp(started.Add(14 * time.Second).UTC().Format(time.RFC3339Nano))
	if got.Activity.SettledAt != wantSettled {
		t.Fatalf("settled at = %q, want terminal time.completed %q", got.Activity.SettledAt, wantSettled)
	}
	var reasoning *CodexConversationEvent
	for i := range got.Events {
		if got.Events[i].Kind == "reasoning" && strings.Contains(got.Events[i].Body, "terminal reasoning") {
			reasoning = &got.Events[i]
		}
	}
	if reasoning == nil {
		t.Fatalf("terminal reasoning event missing: %#v", got.Events)
	}
	if reasoning.Partial {
		t.Fatalf("settled reasoning must clear Partial (spinner source): %#v", *reasoning)
	}
	if !strings.Contains(reasoning.Body, "terminal reasoning loaded") {
		t.Fatalf("loaded reasoning text must be preserved: %q", reasoning.Body)
	}
}

// TestOpenCodeUnknownFinishWithoutCompletedStaysLive keeps the fail-closed
// guarantee: a message row still mid-write (no time.completed) must never
// settle, and the reasoning part stays genuinely live.
func TestOpenCodeUnknownFinishWithoutCompletedStaysLive(t *testing.T) {
	started := time.Date(2026, 8, 8, 0, 0, 10, 0, time.UTC)
	messages, parts := openCodeReasoningTurnSeeds(started, `{"role":"assistant","finish":"unknown","time":{"created":1}}`)
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_rsn", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(15 * time.Second).UnixMilli()},
	}, messages, parts)
	got, err := parseOpenCodeConversation(dbPath, "ses_rsn")
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityRunning {
		t.Fatalf("unknown finish without completed must stay live: %+v", got.Activity)
	}
	for _, event := range got.Events {
		if event.Kind == "reasoning" && strings.Contains(event.Body, "terminal reasoning") && !event.Partial {
			t.Fatalf("live reasoning must stay partial: %#v", event)
		}
	}
}

// TestOpenCodeUnknownFinishWithRunningToolStaysLive verifies the terminal
// boundary is refused while a tool call is still in flight.
func TestOpenCodeUnknownFinishWithRunningToolStaysLive(t *testing.T) {
	started := time.Date(2026, 8, 8, 0, 0, 10, 0, time.UTC)
	terminalData := fmt.Sprintf(
		`{"role":"assistant","finish":"unknown","time":{"created":1,"completed":%d}}`,
		started.Add(14*time.Second).UnixMilli(),
	)
	messages, parts := openCodeReasoningTurnSeeds(started, terminalData)
	parts = append(parts, openCodePartSeed{
		ID: "p_t2", MessageID: "msg_terminal", SessionID: "ses_rsn",
		CreatedMS: started.Add(12 * time.Second).UnixMilli(),
		Data:      `{"type":"tool","tool":"bash","callID":"call-2","state":{"status":"running","input":{"command":"sleep"}}}`,
	})
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_rsn", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(15 * time.Second).UnixMilli()},
	}, messages, parts)
	got, err := parseOpenCodeConversation(dbPath, "ses_rsn")
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityRunning {
		t.Fatalf("unknown+completed with a running tool must stay live: %+v", got.Activity)
	}
}

// TestOpenCodeTerminalDeltaConvergesReasoningPartial exercises the
// incremental cache + changed-id delta owner: poll 1 sees the live turn
// (reasoning partial), poll 2 lands only the terminal message row update
// (finish unknown + time.completed), and the second load must report the
// reasoning event id in ChangedEventIDs so the server's memoized delta
// carries the Partial flip to the App.
func TestOpenCodeTerminalDeltaConvergesReasoningPartial(t *testing.T) {
	started := time.Date(2026, 8, 8, 0, 0, 10, 0, time.UTC)
	session := []openCodeSessionSeed{
		{ID: "ses_rsn", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(15 * time.Second).UnixMilli()},
	}
	liveMessages, parts := openCodeReasoningTurnSeeds(started, `{"role":"assistant","finish":"unknown","time":{"created":1}}`)
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	createOpenCodeFixtureDB(t, dbPath, session, liveMessages, parts)
	t.Setenv("ZEN_OPENCODE_DB", dbPath)
	reader := NewProviderConversationReader()
	agent := classifier.Agent{Cwd: "/repo", Command: "opencode", StartedAt: started}
	first, err := reader.Load(agent, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var reasoningID string
	for _, event := range first.Events {
		if event.Kind == "reasoning" && strings.Contains(event.Body, "terminal reasoning") {
			reasoningID = event.ID
			if !event.Partial {
				t.Fatalf("live reasoning must be partial: %#v", event)
			}
		}
	}
	if reasoningID == "" {
		t.Fatal("reasoning event missing from live poll")
	}
	version := reader.ConversationVersion()
	if version == 0 {
		t.Fatal("no content version")
	}
	terminalMessages := []openCodeMessageSeed{
		liveMessages[0], liveMessages[1],
		{ID: "msg_terminal", SessionID: "ses_rsn", CreatedMS: started.Add(10 * time.Second).UnixMilli(), Data: fmt.Sprintf(`{"role":"assistant","finish":"unknown","time":{"created":1,"completed":%d}}`, started.Add(14*time.Second).UnixMilli())},
	}
	createOpenCodeFixtureDB(t, dbPath, session, terminalMessages, parts)
	second, err := reader.Load(agent, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if reader.ConversationVersion() == version {
		t.Fatal("terminal settle must bump the content version")
	}
	if second.Activity == nil || second.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("terminal delta must settle: %+v", second.Activity)
	}
	for _, event := range second.Events {
		if event.ID == reasoningID && event.Partial {
			t.Fatalf("delta-converged reasoning must clear Partial: %#v", event)
		}
	}
	changed := map[string]bool{}
	for _, id := range reader.ChangedEventIDs() {
		changed[id] = true
	}
	if !changed[reasoningID] {
		t.Fatalf("settled Partial flip not reported as changed for memoized delta: %q", reasoningID)
	}
}

// TestOpenCodeInterruptedTerminalMessageConverges covers cancellation: an
// aborted/failed finish settles the turn interrupted/failed and clears the
// reasoning Partial.
func TestOpenCodeInterruptedTerminalMessageConverges(t *testing.T) {
	started := time.Date(2026, 8, 8, 0, 0, 10, 0, time.UTC)
	terminalData := fmt.Sprintf(
		`{"role":"assistant","finish":"aborted","time":{"created":1,"completed":%d}}`,
		started.Add(14*time.Second).UnixMilli(),
	)
	messages, parts := openCodeReasoningTurnSeeds(started, terminalData)
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_rsn", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(15 * time.Second).UnixMilli()},
	}, messages, parts)
	got, err := parseOpenCodeConversation(dbPath, "ses_rsn")
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityInterrupted {
		t.Fatalf("aborted terminal finish must settle interrupted: %+v", got.Activity)
	}
	for _, event := range got.Events {
		if event.Kind == "reasoning" && event.Partial {
			t.Fatalf("interrupted reasoning must clear Partial: %#v", event)
		}
	}
}
