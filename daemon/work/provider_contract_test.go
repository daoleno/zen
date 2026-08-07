package work

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// providerContractSemantics is the provider-neutral normalized projection used
// for parity assertions: user/assistant messages, intentional reasoning, tool
// call lifecycle, and completion state.
type providerContractSemantics struct {
	Kind     string
	Role     string
	ToolName string
	Status   string
	Body     string
}

func openCodeCanonicalConversation(t *testing.T, dbPath string, started time.Time) CodexConversation {
	t.Helper()
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_par", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(9 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_par", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
		{ID: "msg_asst", SessionID: "ses_par", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"role":"assistant","finish":"stop","time":{"created":1,"completed":` + intString(started.Add(8*time.Second).UnixMilli()) + `}}`},
	}, []openCodePartSeed{
		{ID: "p1", MessageID: "msg_user", SessionID: "ses_par", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"run the tool"}`},
		{ID: "p2", MessageID: "msg_asst", SessionID: "ses_par", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
		{ID: "p3", MessageID: "msg_asst", SessionID: "ses_par", CreatedMS: started.Add(3 * time.Second).UnixMilli(), Data: `{"type":"reasoning","text":"thinking hard"}`},
		{ID: "p4", MessageID: "msg_asst", SessionID: "ses_par", CreatedMS: started.Add(4 * time.Second).UnixMilli(), Data: `{"type":"text","text":"starting"}`},
		{ID: "p5", MessageID: "msg_asst", SessionID: "ses_par", CreatedMS: started.Add(5 * time.Second).UnixMilli(), Data: `{"type":"tool","tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"true"},"output":"ok"}}`},
		{ID: "p6", MessageID: "msg_asst", SessionID: "ses_par", CreatedMS: started.Add(6 * time.Second).UnixMilli(), Data: `{"type":"text","text":"done"}`},
		{ID: "p7", MessageID: "msg_asst", SessionID: "ses_par", CreatedMS: started.Add(7 * time.Second).UnixMilli(), Data: `{"type":"step-finish","reason":"stop"}`},
	})
	got, err := parseOpenCodeConversation(dbPath, "ses_par")
	if err != nil {
		t.Fatal(err)
	}
	got.Available = true
	return got
}

func piCanonicalConversation(t *testing.T, path string) CodexConversation {
	t.Helper()
	content := strings.Join([]string{
		`{"type":"session","version":3,"id":"sess-par","timestamp":"2026-08-06T00:00:00.000Z","cwd":"/repo"}`,
		`{"type":"message","id":"u1","parentId":null,"timestamp":"2026-08-06T00:00:01.000Z","message":{"role":"user","content":"run the tool"}}`,
		`{"type":"message","id":"a1","parentId":"u1","timestamp":"2026-08-06T00:00:02.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"thinking hard"},{"type":"text","text":"starting"},{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"true"}}],"stopReason":"toolUse"}}`,
		`{"type":"message","id":"r1","parentId":"a1","timestamp":"2026-08-06T00:00:03.000Z","message":{"role":"toolResult","toolCallId":"call_1","toolName":"bash","content":[{"type":"text","text":"ok"}],"isError":false}}`,
		`{"type":"message","id":"a2","parentId":"r1","timestamp":"2026-08-06T00:00:04.000Z","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"stopReason":"stop"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := parsePiConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	got.Available = true
	return got
}

func contractSemantics(conversation CodexConversation) []providerContractSemantics {
	out := make([]providerContractSemantics, 0, len(conversation.Events))
	for _, event := range conversation.Events {
		body := event.Body
		if event.Kind == "tool_call" {
			body = event.Output
		}
		out = append(out, providerContractSemantics{
			Kind:     event.Kind,
			Role:     event.Role,
			ToolName: event.ToolName,
			Status:   event.Status,
			Body:     body,
		})
	}
	return out
}

func assertContractParity(t *testing.T, name string, openCode, pi CodexConversation) {
	t.Helper()
	if !openCode.Available || !pi.Available {
		t.Fatalf("%s: adapters must be available: opencode=%+v pi=%+v", name, openCode, pi)
	}
	if len(openCode.Events) == 0 || len(pi.Events) == 0 {
		t.Fatalf("%s: adapters must not collapse to Working-only (empty events): opencode=%d pi=%d", name, len(openCode.Events), len(pi.Events))
	}
	openCodeSeq := contractSemantics(openCode)
	piSeq := contractSemantics(pi)
	if len(openCodeSeq) != len(piSeq) {
		t.Fatalf("%s: semantic sequence lengths differ:\nopencode=%#v\npi=%#v", name, openCodeSeq, piSeq)
	}
	for i := range openCodeSeq {
		if openCodeSeq[i] != piSeq[i] {
			t.Fatalf("%s: semantic event %d differs:\nopencode=%#v\npi=%#v\nfull opencode=%#v\nfull pi=%#v", name, i, openCodeSeq[i], piSeq[i], openCodeSeq, piSeq)
		}
	}
	if openCode.Activity == nil || pi.Activity == nil || openCode.Activity.Status != pi.Activity.Status {
		t.Fatalf("%s: completion state differs: opencode=%+v pi=%+v", name, openCode.Activity, pi.Activity)
	}
}

// TestProviderContractParity proves OpenCode and Pi adapters project the same
// normalized conversation semantics for one canonical scenario: user message,
// intentional reasoning, assistant text, tool call with result, final
// assistant text, and completed lifecycle. Provider differences are confined
// to ingestion; the common CodexConversationEvent model is identical.
func TestProviderContractParity(t *testing.T) {
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	openCode := openCodeCanonicalConversation(t, filepath.Join(t.TempDir(), "opencode.db"), started)
	pi := piCanonicalConversation(t, filepath.Join(t.TempDir(), "session.jsonl"))
	assertContractParity(t, "canonical", openCode, pi)

	want := []providerContractSemantics{
		{Kind: "user_message", Role: "user", Status: "", Body: "run the tool"},
		{Kind: "reasoning", Body: "thinking hard"},
		{Kind: "assistant_message", Role: "assistant", Body: "starting"},
		{Kind: "tool_call", ToolName: "bash", Status: "completed", Body: "ok"},
		{Kind: "assistant_message", Role: "assistant", Body: "done"},
	}
	if got := contractSemantics(openCode); !semanticsEqual(got, want) {
		t.Fatalf("opencode sequence = %#v, want %#v", got, want)
	}
}

func semanticsEqual(left, right []providerContractSemantics) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// TestProviderContractPiNotCollapsedToWorking covers the shared-directory
// binding path: a live Pi session launched without --session must produce a
// bound, non-empty conversation through the reader (the reported Working-only
// collapse).
func TestProviderContractPiNotCollapsedToWorking(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	sessionsDir := filepath.Join(agentDir, "sessions", encodePiSessionDirName("/repo"))
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	piCanonicalConversation(t, filepath.Join(sessionsDir, "2026-08-06T00-00-10-000Z_live.jsonl"))
	agent := classifier.Agent{Cwd: "/repo", Command: "pi"}
	conversation, err := NewProviderConversationReader().Load(agent, AgentProviderPi, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !conversation.Available || len(conversation.Events) == 0 {
		t.Fatalf("live Pi must bind shared dir and project events: %+v", conversation)
	}
}

func intString(value int64) string {
	return fmt.Sprintf("%d", value)
}
