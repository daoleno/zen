package work

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestOpenCodeBindRejectsAmbiguousSameCWD(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_a", Directory: "/repo", CreatedMS: started.Add(1 * time.Second).UnixMilli(), UpdatedMS: started.Add(2 * time.Second).UnixMilli()},
		{ID: "ses_b", Directory: "/repo", CreatedMS: started.Add(1 * time.Second).UnixMilli(), UpdatedMS: started.Add(3 * time.Second).UnixMilli()},
	}, nil, nil)
	t.Setenv("ZEN_OPENCODE_DB", dbPath)
	reader := NewProviderConversationReader()
	_, ok, err := reader.findOpenCodeSession(classifier.Agent{
		Cwd:       "/repo",
		Command:   "opencode",
		StartedAt: started,
	}, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("ambiguous same-CWD candidates must refuse bind")
	}
}

func TestOpenCodeExactAdmissionAndLifecycle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	payload := "opencode-zen-ack"
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_exact", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(5 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_exact", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
		{ID: "msg_asst", SessionID: "ses_exact", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"role":"assistant","finish":"stop","time":{"created":1,"completed":` + fmt.Sprintf("%d", started.Add(5*time.Second).UnixMilli()) + `}}`},
	}, []openCodePartSeed{
		{ID: "p1", MessageID: "msg_user", SessionID: "ses_exact", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"` + payload + `"}`},
		{ID: "p2", MessageID: "msg_asst", SessionID: "ses_exact", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
		{ID: "p3", MessageID: "msg_asst", SessionID: "ses_exact", CreatedMS: started.Add(3 * time.Second).UnixMilli(), Data: `{"type":"text","text":"ack"}`},
		{ID: "p4", MessageID: "msg_asst", SessionID: "ses_exact", CreatedMS: started.Add(4 * time.Second).UnixMilli(), Data: `{"type":"step-finish","reason":"stop"}`},
	})
	t.Setenv("ZEN_OPENCODE_DB", dbPath)
	reader := NewProviderConversationReader()
	got, err := reader.Load(classifier.Agent{
		Cwd:       "/repo",
		Command:   "opencode",
		StartedAt: started,
	}, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.SessionID != "ses_exact" {
		t.Fatalf("conversation = %+v", got)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	if len(got.Events) == 0 || got.Events[0].AdmissionSHA256 != want {
		t.Fatalf("admission events = %#v", got.Events)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("activity = %+v", got.Activity)
	}
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_exact", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(5 * time.Second).UnixMilli()},
		{ID: "ses_other", Directory: "/repo", CreatedMS: started.Add(30 * time.Second).UnixMilli(), UpdatedMS: started.Add(40 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_exact", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
		{ID: "msg_asst", SessionID: "ses_exact", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"role":"assistant","finish":"stop","time":{"created":1,"completed":` + fmt.Sprintf("%d", started.Add(5*time.Second).UnixMilli()) + `}}`},
	}, []openCodePartSeed{
		{ID: "p1", MessageID: "msg_user", SessionID: "ses_exact", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"` + payload + `"}`},
		{ID: "p2", MessageID: "msg_asst", SessionID: "ses_exact", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
		{ID: "p3", MessageID: "msg_asst", SessionID: "ses_exact", CreatedMS: started.Add(3 * time.Second).UnixMilli(), Data: `{"type":"text","text":"ack"}`},
		{ID: "p4", MessageID: "msg_asst", SessionID: "ses_exact", CreatedMS: started.Add(4 * time.Second).UnixMilli(), Data: `{"type":"step-finish","reason":"stop"}`},
	})
	second, err := reader.Load(classifier.Agent{
		Cwd:       "/repo",
		Command:   "opencode",
		StartedAt: started,
	}, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.SessionID != "ses_exact" {
		t.Fatalf("cross-bound to %q", second.SessionID)
	}
}

func TestHardenOpenCodeDelegatedCommand(t *testing.T) {
	got, err := HardenOpenCodeDelegatedCommand("opencode")
	if err != nil || got != "opencode --auto" {
		t.Fatalf("got %q err=%v", got, err)
	}
	got, err = HardenOpenCodeDelegatedCommand("opencode --auto")
	if err != nil || got != "opencode --auto" {
		t.Fatalf("idempotent got %q err=%v", got, err)
	}
	got, err = HardenOpenCodeDelegatedCommand("env PATH=/opt -- opencode")
	if err != nil || got != "env PATH=/opt -- opencode --auto" {
		t.Fatalf("env-wrapped got %q err=%v", got, err)
	}
	got, err = HardenOpenCodeDelegatedCommand("env PATH=/opt -- opencode --auto")
	if err != nil || got != "env PATH=/opt -- opencode --auto" {
		t.Fatalf("env-wrapped exact got %q err=%v", got, err)
	}
	if _, err := HardenOpenCodeDelegatedCommand("opencode --auto=false"); err == nil {
		t.Fatal("expected --auto=false rejection")
	}
	if _, err := HardenOpenCodeDelegatedCommand("opencode --auto=true"); err == nil {
		t.Fatal("expected --auto=true rejection")
	}
	if _, err := HardenOpenCodeDelegatedCommand("opencode --auto --auto=false"); err == nil {
		t.Fatal("expected contradictory auto flags rejection")
	}
	if _, err := HardenOpenCodeDelegatedCommand("env PATH=/x -- opencode --auto=false"); err == nil {
		t.Fatal("expected env-wrapped --auto=false rejection")
	}
	// Duplicate exact --auto remains enabled and unchanged.
	got, err = HardenOpenCodeDelegatedCommand("opencode --auto --pure")
	if err != nil || got != "opencode --auto --pure" {
		t.Fatalf("duplicate-safe exact got %q err=%v", got, err)
	}
}

func TestOpenCodeOwnedSessionIDIgnoresContinue(t *testing.T) {
	if got := OpenCodeOwnedSessionID("opencode --continue"); got != "" {
		t.Fatalf("continue must not own: %q", got)
	}
	if got := OpenCodeOwnedSessionID("opencode -s ses_123"); got != "ses_123" {
		t.Fatalf("got %q", got)
	}
}

func TestOpenCodeMessageFinishSettlesWithoutStepFinish(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	payload := "opencode-finish-settle"
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_finish", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(5 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_finish", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
		{ID: "msg_asst", SessionID: "ses_finish", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"role":"assistant","finish":"stop","time":{"created":1,"completed":` + fmt.Sprintf("%d", started.Add(4*time.Second).UnixMilli()) + `}}`},
	}, []openCodePartSeed{
		{ID: "p1", MessageID: "msg_user", SessionID: "ses_finish", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"` + payload + `"}`},
		{ID: "p2", MessageID: "msg_asst", SessionID: "ses_finish", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
		{ID: "p3", MessageID: "msg_asst", SessionID: "ses_finish", CreatedMS: started.Add(3 * time.Second).UnixMilli(), Data: `{"type":"text","text":"ack"}`},
	})
	got, err := parseOpenCodeConversation(dbPath, "ses_finish")
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("message finish must settle: %+v", got.Activity)
	}
}

func TestOpenCodeToolLifecycleAndFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_tool", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(6 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_tool", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
		{ID: "msg_asst", SessionID: "ses_tool", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"role":"assistant","finish":"error","time":{"created":1,"completed":` + fmt.Sprintf("%d", started.Add(5*time.Second).UnixMilli()) + `}}`},
	}, []openCodePartSeed{
		{ID: "p1", MessageID: "msg_user", SessionID: "ses_tool", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"run tool"}`},
		{ID: "p2", MessageID: "msg_asst", SessionID: "ses_tool", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
		{ID: "p3", MessageID: "msg_asst", SessionID: "ses_tool", CreatedMS: started.Add(3 * time.Second).UnixMilli(), Data: `{"type":"tool","tool":"bash","callID":"c1","state":{"status":"completed","input":"{\"cmd\":\"true\"}","output":"ok"}}`},
		{ID: "p4", MessageID: "msg_asst", SessionID: "ses_tool", CreatedMS: started.Add(4 * time.Second).UnixMilli(), Data: `{"type":"step-finish","reason":"error"}`},
	})
	got, err := parseOpenCodeConversation(dbPath, "ses_tool")
	if err != nil {
		t.Fatal(err)
	}
	var tool *CodexConversationEvent
	for i := range got.Events {
		if got.Events[i].Kind == "tool_call" {
			tool = &got.Events[i]
			break
		}
	}
	if tool == nil || tool.ToolName != "bash" || tool.Status != "completed" {
		t.Fatalf("tool event = %#v", tool)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityFailed {
		t.Fatalf("failed finish = %+v", got.Activity)
	}
}

func TestOpenCodeNonmatchingUserDoesNotAdmitPayload(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_other", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_other", CreatedMS: started.UnixMilli(), Data: `{"role":"user"}`},
	}, []openCodePartSeed{
		{ID: "p1", MessageID: "msg_user", SessionID: "ses_other", CreatedMS: started.UnixMilli(), Data: `{"type":"text","text":"other-user-text"}`},
	})
	got, err := parseOpenCodeConversation(dbPath, "ses_other")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte("opencode-zen-ack")))
	for _, event := range got.Events {
		if event.Kind == "user_message" && event.AdmissionSHA256 == want {
			t.Fatalf("nonmatching user admitted exact digest: %#v", event)
		}
	}
}

func TestOpenCodeBindsRootNotChildSession(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_parent", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(5 * time.Second).UnixMilli()},
		{ID: "ses_child", Directory: "/repo", ParentID: "ses_parent", CreatedMS: started.Add(20 * time.Second).UnixMilli(), UpdatedMS: started.Add(30 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_parent", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
	}, []openCodePartSeed{
		{ID: "p1", MessageID: "msg_user", SessionID: "ses_parent", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"parent-user"}`},
	})
	t.Setenv("ZEN_OPENCODE_DB", dbPath)

	// startedAt aligned with the child: the root parent must still win.
	reader := NewProviderConversationReader()
	candidate, ok, err := reader.findOpenCodeSession(classifier.Agent{
		Cwd:       "/repo",
		Command:   "opencode",
		StartedAt: started.Add(20 * time.Second),
	}, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || candidate.ID != "ses_parent" {
		t.Fatalf("child session must never bind: ok=%v candidate=%+v", ok, candidate)
	}

	// startedAt zero: freshest root fallback still binds the parent, not the
	// child, and not session_not_found.
	reader = NewProviderConversationReader()
	candidate, ok, err = reader.findOpenCodeSession(classifier.Agent{
		Cwd:     "/repo",
		Command: "opencode",
	}, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || candidate.ID != "ses_parent" {
		t.Fatalf("zero startedAt must fall back to root: ok=%v candidate=%+v", ok, candidate)
	}
}

func TestOpenCodeBindFreshestRootWhenStartWindowMisses(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_old", Directory: "/repo", CreatedMS: started.Add(-30 * time.Minute).UnixMilli(), UpdatedMS: started.Add(-29 * time.Minute).UnixMilli()},
		{ID: "ses_new", Directory: "/repo", CreatedMS: started.Add(20 * time.Minute).UnixMilli(), UpdatedMS: started.Add(21 * time.Minute).UnixMilli()},
	}, nil, nil)
	t.Setenv("ZEN_OPENCODE_DB", dbPath)
	reader := NewProviderConversationReader()
	candidate, ok, err := reader.findOpenCodeSession(classifier.Agent{
		Cwd:       "/repo",
		Command:   "opencode",
		StartedAt: started,
	}, started.Add(22*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || candidate.ID != "ses_new" {
		t.Fatalf("window miss must bind freshest root: ok=%v candidate=%+v", ok, candidate)
	}
	// A newer root must not replace the pinned binding mid-subscription.
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_old", Directory: "/repo", CreatedMS: started.Add(-30 * time.Minute).UnixMilli(), UpdatedMS: started.Add(-29 * time.Minute).UnixMilli()},
		{ID: "ses_new", Directory: "/repo", CreatedMS: started.Add(20 * time.Minute).UnixMilli(), UpdatedMS: started.Add(21 * time.Minute).UnixMilli()},
		{ID: "ses_latest", Directory: "/repo", CreatedMS: started.Add(40 * time.Minute).UnixMilli(), UpdatedMS: started.Add(41 * time.Minute).UnixMilli()},
	}, nil, nil)
	candidate, ok, err = reader.findOpenCodeSession(classifier.Agent{
		Cwd:       "/repo",
		Command:   "opencode",
		StartedAt: started,
	}, started.Add(45*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || candidate.ID != "ses_new" {
		t.Fatalf("pinned binding must not cross-bind: ok=%v candidate=%+v", ok, candidate)
	}
}

func TestOpenCodeSubtaskAndToolStateProjection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_sub", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(10 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_sub", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
		{ID: "msg_asst", SessionID: "ses_sub", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"role":"assistant","finish":"stop","time":{"created":1,"completed":` + fmt.Sprintf("%d", started.Add(9*time.Second).UnixMilli()) + `}}`},
	}, []openCodePartSeed{
		{ID: "p1", MessageID: "msg_user", SessionID: "ses_sub", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"explore"}`},
		{ID: "p2", MessageID: "msg_asst", SessionID: "ses_sub", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
		{ID: "p3", MessageID: "msg_asst", SessionID: "ses_sub", CreatedMS: started.Add(3 * time.Second).UnixMilli(), Data: `{"type":"subtask","prompt":"explore data flow","description":"Explore terminal data flow","agent":"explore","command":"explore"}`},
		{ID: "p4", MessageID: "msg_asst", SessionID: "ses_sub", CreatedMS: started.Add(4 * time.Second).UnixMilli(), Data: `{"type":"tool","tool":"bash","callID":"c_pending","state":{"status":"pending","input":{"command":"true"},"raw":""}}`},
		{ID: "p5", MessageID: "msg_asst", SessionID: "ses_sub", CreatedMS: started.Add(5 * time.Second).UnixMilli(), Data: `{"type":"tool","tool":"bash","callID":"c_failed","state":{"status":"error","input":{"command":"boom"},"error":"boom"}}`},
		{ID: "p6", MessageID: "msg_asst", SessionID: "ses_sub", CreatedMS: started.Add(6 * time.Second).UnixMilli(), Data: `{"type":"step-finish","reason":"stop"}`},
	})
	got, err := parseOpenCodeConversation(dbPath, "ses_sub")
	if err != nil {
		t.Fatal(err)
	}
	var subtask *CodexConversationEvent
	var pending *CodexConversationEvent
	var failed *CodexConversationEvent
	for i := range got.Events {
		switch got.Events[i].Kind {
		case "tool_call":
			switch got.Events[i].ToolName {
			case "subtask":
				subtask = &got.Events[i]
			case "bash":
				if got.Events[i].CallID == "c_pending" {
					pending = &got.Events[i]
				} else if got.Events[i].CallID == "c_failed" {
					failed = &got.Events[i]
				}
			}
		}
	}
	if subtask == nil || subtask.Status != "completed" || subtask.Partial {
		t.Fatalf("subtask event = %#v", subtask)
	}
	if pending == nil || pending.Status != "pending" || pending.Partial {
		t.Fatalf("pending tool event = %#v", pending)
	}
	if failed == nil || failed.Status != "failed" || failed.Partial {
		t.Fatalf("error tool event = %#v", failed)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("activity = %+v", got.Activity)
	}
}

func TestOpenCodeSubtaskRunningWhileStepsOpen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_live", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(5 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_live", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
		{ID: "msg_asst", SessionID: "ses_live", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"role":"assistant"}`},
	}, []openCodePartSeed{
		{ID: "p1", MessageID: "msg_user", SessionID: "ses_live", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"live"}`},
		{ID: "p2", MessageID: "msg_asst", SessionID: "ses_live", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
		{ID: "p3", MessageID: "msg_asst", SessionID: "ses_live", CreatedMS: started.Add(3 * time.Second).UnixMilli(), Data: `{"type":"subtask","prompt":"still running","agent":"explore"}`},
	})
	got, err := parseOpenCodeConversation(dbPath, "ses_live")
	if err != nil {
		t.Fatal(err)
	}
	var subtask *CodexConversationEvent
	for i := range got.Events {
		if got.Events[i].Kind == "tool_call" && got.Events[i].ToolName == "subtask" {
			subtask = &got.Events[i]
			break
		}
	}
	if subtask == nil || subtask.Status != "running" || !subtask.Partial {
		t.Fatalf("in-flight subtask event = %#v", subtask)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityRunning {
		t.Fatalf("activity = %+v", got.Activity)
	}
}

// openCodeUpstreamSessions is a helper around real OpenCode v1.18.x shapes:
// part.data carries the Part union with type discriminator and tool state
// discriminated by status (pending/running/completed/error), message.data
// carries role + optional finish/time.completed, and timestamps are
// milliseconds. Both polls below rewrite the same part IDs to simulate the
// watcher observing the same live session across polls and restarts.
func TestOpenCodeRealUpstreamToolShapesAcrossPolls(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	started := time.Date(2026, 8, 7, 0, 0, 10, 0, time.UTC)
	session := []openCodeSessionSeed{
		{ID: "ses_up", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(9 * time.Second).UnixMilli()},
	}
	messages := []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_up", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user","time":{"created":1}}`},
		{ID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"role":"assistant","time":{"created":1}}`},
	}
	// Poll 1: in-flight turn with interleaved reasoning/text and two concurrent
	// tools (one running, one pending) using the exact upstream state shapes.
	// A live OpenCode turn has no finish/completed on the assistant message row
	// while parts are still streaming.
	inFlightParts := []openCodePartSeed{
		{ID: "prt_user", MessageID: "msg_user", SessionID: "ses_up", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"run both tools"}`},
		{ID: "prt_start", MessageID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"snapshot":"8ac09ffb","type":"step-start"}`},
		{ID: "prt_reason", MessageID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(3 * time.Second).UnixMilli(), Data: `{"type":"reasoning","text":"thinking hard","time":{"start":1,"end":2}}`},
		{ID: "prt_text1", MessageID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(4 * time.Second).UnixMilli(), Data: `{"type":"text","text":"starting"}`},
		{ID: "prt_tool_a", MessageID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(5 * time.Second).UnixMilli(), Data: `{"type":"tool","tool":"bash","callID":"call_00_a","state":{"metadata":{"output":""},"status":"running","input":{"command":"true"},"time":{"start":1}}}`},
		{ID: "prt_tool_b", MessageID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(6 * time.Second).UnixMilli(), Data: `{"type":"tool","tool":"read","callID":"call_00_b","state":{"status":"pending","input":{"filePath":"/repo/a.txt"},"raw":""}}`},
		{ID: "prt_text2", MessageID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(7 * time.Second).UnixMilli(), Data: `{"type":"text","text":"between tools"}`},
	}
	// Poll 2: same part IDs settled: tool a completed with output/title,
	// tool b failed carrying state.error (no state.output), an unknown part
	// type plus a snapshot part that must not drop surrounding messages, and a
	// step-finish closing the turn. The assistant message now carries the
	// authoritative finish and time.completed.
	settledMessages := []openCodeMessageSeed{
		messages[0],
		{ID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"role":"assistant","finish":"stop","time":{"created":1,"completed":` + intString(started.Add(8*time.Second).UnixMilli()) + `}}`},
	}
	settledParts := []openCodePartSeed{
		inFlightParts[0], inFlightParts[1], inFlightParts[2], inFlightParts[3],
		{ID: "prt_tool_a", MessageID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(5 * time.Second).UnixMilli(), Data: `{"type":"tool","tool":"bash","callID":"call_00_a","state":{"status":"completed","input":{"command":"true"},"output":"ok\n","title":"Run command: true","metadata":{"output":"ok\n","exit":0,"truncated":false},"time":{"start":1,"end":2}}}`},
		{ID: "prt_tool_b", MessageID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(6 * time.Second).UnixMilli(), Data: `{"type":"tool","tool":"read","callID":"call_00_b","state":{"status":"error","input":{"filePath":"/repo/a.txt"},"error":"Error: ENOENT: no such file or directory","metadata":{"output":""},"time":{"start":1,"end":2}}}`},
		{ID: "prt_text2", MessageID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(7 * time.Second).UnixMilli(), Data: `{"type":"text","text":"between tools"}`},
		{ID: "prt_unknown", MessageID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(8 * time.Second).UnixMilli(), Data: `{"type":"compaction","auto":false}`},
		{ID: "prt_snapshot", MessageID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(9 * time.Second).UnixMilli(), Data: `{"snapshot":"8ac09ffb","type":"snapshot"}`},
		{ID: "prt_finish", MessageID: "msg_asst", SessionID: "ses_up", CreatedMS: started.Add(10 * time.Second).UnixMilli(), Data: `{"reason":"stop","snapshot":"8ac09ffb","type":"step-finish","tokens":{"total":10,"input":5,"output":5,"reasoning":0,"cache":{"read":0,"write":0}},"cost":0.0001}`},
	}

	createOpenCodeFixtureDB(t, dbPath, session, messages, inFlightParts)
	inFlight, err := parseOpenCodeConversation(dbPath, "ses_up")
	if err != nil {
		t.Fatal(err)
	}
	// Restart/upsert simulation: a fresh read of the same session (new DB file
	// bytes, same part IDs) must not duplicate events and must preserve stable
	// identity, then the settled snapshot replaces the in-flight projection.
	createOpenCodeFixtureDB(t, dbPath, session, messages, inFlightParts)
	inFlightSecond, err := parseOpenCodeConversation(dbPath, "ses_up")
	if err != nil {
		t.Fatal(err)
	}
	if len(inFlight.Events) != len(inFlightSecond.Events) {
		t.Fatalf("restart duplicate drift: %d vs %d events", len(inFlight.Events), len(inFlightSecond.Events))
	}
	for i := range inFlight.Events {
		if inFlight.Events[i].ID != inFlightSecond.Events[i].ID {
			t.Fatalf("restart identity drift at %d: %s vs %s", i, inFlight.Events[i].ID, inFlightSecond.Events[i].ID)
		}
	}

	eventByCall := map[string]CodexConversationEvent{}
	kindCounts := map[string]int{}
	for _, event := range inFlight.Events {
		kindCounts[event.Kind]++
		if event.Kind == "tool_call" {
			eventByCall[event.CallID] = event
		}
	}
	if kindCounts["tool_call"] != 2 {
		t.Fatalf("in-flight tool_call events = %d, want 2: %#v", kindCounts["tool_call"], inFlight.Events)
	}
	if kindCounts["reasoning"] != 1 || kindCounts["assistant_message"] != 2 || kindCounts["user_message"] != 1 {
		t.Fatalf("in-flight kinds = %#v", kindCounts)
	}
	runningTool := eventByCall["call_00_a"]
	if runningTool.Status != "running" || !runningTool.Partial || runningTool.Input != `{"command":"true"}` {
		t.Fatalf("running tool = %#v", runningTool)
	}
	pendingTool := eventByCall["call_00_b"]
	if pendingTool.Status != "pending" || !pendingTool.Partial || pendingTool.ToolName != "read" {
		t.Fatalf("pending tool = %#v", pendingTool)
	}
	if inFlight.Activity == nil || inFlight.Activity.Status != ProviderActivityRunning {
		t.Fatalf("in-flight activity = %+v", inFlight.Activity)
	}

	createOpenCodeFixtureDB(t, dbPath, session, settledMessages, settledParts)
	settled, err := parseOpenCodeConversation(dbPath, "ses_up")
	if err != nil {
		t.Fatal(err)
	}
	settledByCall := map[string]CodexConversationEvent{}
	settledTexts := 0
	for _, event := range settled.Events {
		if event.Kind == "tool_call" {
			settledByCall[event.CallID] = event
		}
		if event.Kind == "assistant_message" {
			settledTexts++
		}
	}
	completed := settledByCall["call_00_a"]
	if completed.Status != "completed" || completed.Partial {
		t.Fatalf("completed tool = %#v", completed)
	}
	if completed.Output != "ok" {
		t.Fatalf("completed output = %q, want %q", completed.Output, "ok")
	}
	failed := settledByCall["call_00_b"]
	if failed.Status != "failed" || failed.Partial {
		t.Fatalf("error tool = %#v", failed)
	}
	if !strings.Contains(failed.Output, "ENOENT") {
		t.Fatalf("error tool must surface state.error as output, got %q", failed.Output)
	}
	// Unknown/snapshot parts must fail closed: skipped without dropping the
	// interleaved assistant text or any tool event.
	if settledTexts != 2 {
		t.Fatalf("settled assistant texts = %d, want 2: %#v", settledTexts, settled.Events)
	}
	if len(settled.Events) != len(inFlight.Events) {
		t.Fatalf("settled event count = %d, want %d (no dup/regression): %#v", len(settled.Events), len(inFlight.Events), settled.Events)
	}
	if settled.Activity == nil || settled.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("settled activity = %+v", settled.Activity)
	}
}

func TestOpenCodeMalformedToolPartFailsClosed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	started := time.Date(2026, 8, 7, 0, 0, 10, 0, time.UTC)
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_mal", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(5 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_mal", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
		{ID: "msg_asst", SessionID: "ses_mal", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"role":"assistant","finish":"stop","time":{"created":1,"completed":` + intString(started.Add(4*time.Second).UnixMilli()) + `}}`},
	}, []openCodePartSeed{
		{ID: "p1", MessageID: "msg_user", SessionID: "ses_mal", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"before"}`},
		{ID: "p2", MessageID: "msg_asst", SessionID: "ses_mal", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `not json at all`},
		{ID: "p3", MessageID: "msg_asst", SessionID: "ses_mal", CreatedMS: started.Add(3 * time.Second).UnixMilli(), Data: `{"type":"tool","tool":"bash","callID":"c1","state":{"status":"completed","input":{"command":"true"},"output":"ok"}}`},
		{ID: "p4", MessageID: "msg_asst", SessionID: "ses_mal", CreatedMS: started.Add(4 * time.Second).UnixMilli(), Data: `{"type":"text","text":"after"}`},
	})
	got, err := parseOpenCodeConversation(dbPath, "ses_mal")
	if err != nil {
		t.Fatal(err)
	}
	var tool *CodexConversationEvent
	for i := range got.Events {
		if got.Events[i].Kind == "tool_call" {
			tool = &got.Events[i]
		}
	}
	if tool == nil || tool.Status != "completed" {
		t.Fatalf("malformed part must not drop the tool event: %#v", got.Events)
	}
	texts := 0
	for _, event := range got.Events {
		if event.Kind == "assistant_message" && event.Body == "after" {
			texts++
		}
	}
	if texts != 1 {
		t.Fatalf("surrounding text dropped by malformed part: %#v", got.Events)
	}
}

type openCodeSessionSeed struct {
	ID, Directory string
	ParentID      string
	CreatedMS     int64
	UpdatedMS     int64
}

type openCodeMessageSeed struct {
	ID, SessionID, Data string
	CreatedMS           int64
}

type openCodePartSeed struct {
	ID, MessageID, SessionID, Data string
	CreatedMS                      int64
}

func createOpenCodeFixtureDB(t *testing.T, path string, sessions []openCodeSessionSeed, messages []openCodeMessageSeed, parts []openCodePartSeed) {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 required")
	}
	_ = os.Remove(path)
	var b strings.Builder
	b.WriteString(`
CREATE TABLE project (id TEXT PRIMARY KEY);
CREATE TABLE session (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  parent_id TEXT,
  slug TEXT NOT NULL,
  directory TEXT NOT NULL,
  title TEXT NOT NULL,
  version TEXT NOT NULL,
  time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL
);
CREATE TABLE message (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL,
  data TEXT NOT NULL
);
CREATE TABLE part (
  id TEXT PRIMARY KEY,
  message_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL,
  data TEXT NOT NULL
);
INSERT INTO project(id) VALUES ('proj');
`)
	for _, session := range sessions {
		parentValue := "NULL"
		if strings.TrimSpace(session.ParentID) != "" {
			parentValue = sqliteStringLiteral(session.ParentID)
		}
		fmt.Fprintf(&b,
			"INSERT INTO session(id, project_id, parent_id, slug, directory, title, version, time_created, time_updated) VALUES (%s, 'proj', %s, 'slug', %s, 't', '1', %d, %d);\n",
			sqliteStringLiteral(session.ID),
			parentValue,
			sqliteStringLiteral(session.Directory),
			session.CreatedMS,
			session.UpdatedMS,
		)
	}
	for _, message := range messages {
		fmt.Fprintf(&b,
			"INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES (%s, %s, %d, %d, %s);\n",
			sqliteStringLiteral(message.ID),
			sqliteStringLiteral(message.SessionID),
			message.CreatedMS,
			message.CreatedMS,
			sqliteStringLiteral(message.Data),
		)
	}
	for _, part := range parts {
		fmt.Fprintf(&b,
			"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES (%s, %s, %s, %d, %d, %s);\n",
			sqliteStringLiteral(part.ID),
			sqliteStringLiteral(part.MessageID),
			sqliteStringLiteral(part.SessionID),
			part.CreatedMS,
			part.CreatedMS,
			sqliteStringLiteral(part.Data),
		)
	}
	cmd := exec.Command("sqlite3", path)
	cmd.Stdin = strings.NewReader(b.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3 fixture: %v: %s", err, out)
	}
}
