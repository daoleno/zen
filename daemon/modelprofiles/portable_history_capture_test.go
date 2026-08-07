package modelprofiles_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/modelprofiles"
)

// Evidence capture for portable post-history hot switching.
//
//	ZEN_PORTABLE_HISTORY_CAPTURE=1 go test ./modelprofiles -run 'TestCapture' -count=1 -timeout 180s -v
//
// Uses Agent-owned temp dirs and local fake gateways only — no real credentials,
// user config writes, or live zen Sessions.

type capturedReq struct {
	Method string
	Path   string
	Body   json.RawMessage
}

func TestCaptureCodexMultiTurnResponsesBodies(t *testing.T) {
	if os.Getenv("ZEN_PORTABLE_HISTORY_CAPTURE") == "" {
		t.Skip("set ZEN_PORTABLE_HISTORY_CAPTURE=1 to capture Codex multi-turn bodies")
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("codex not on PATH: %v", err)
	}
	ver := runVersion(t, codexPath, "CODEX_HOME")
	t.Logf("codex version=%s", ver)
	if !strings.Contains(ver, "0.146.1") {
		t.Logf("WARNING: expected codex-cli 0.146.1, got %q — evidence is version-specific", ver)
	}

	tmpRoot := t.TempDir()
	codexHome := filepath.Join(tmpRoot, "codex-home")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var reqs []capturedReq
	var turn int
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = w.Write([]byte(`{"error":{"type":"route_websocket_rejected","message":"use post"}}`))
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/responses") {
			mu.Lock()
			turn++
			n := turn
			reqs = append(reqs, capturedReq{Method: r.Method, Path: r.URL.Path, Body: append(json.RawMessage(nil), body...)})
			mu.Unlock()
			id := "resp_fake_turn_" + itoa(n)
			msgID := "msg_out_" + itoa(n)
			text := "token-" + itoa(n)
			// Codex 0.146.1 posts stream:true and waits for a full item lifecycle
			// ending in response.completed (OutputTextDelta without active item
			// means the assistant turn was not accepted into local history).
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			writeSSE := func(data string) {
				_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", jsonType(data), data)
				if flusher != nil {
					flusher.Flush()
				}
			}
			msgItem := fmt.Sprintf(`{"type":"message","id":%q,"status":"in_progress","role":"assistant","content":[]}`, msgID)
			finalMsg := fmt.Sprintf(`{"type":"message","id":%q,"status":"completed","role":"assistant","content":[{"type":"output_text","text":%q}]}`, msgID, text)
			respCompleted := fmt.Sprintf(`{"id":%q,"object":"response","created_at":1710000000,"status":"completed","model":"gpt-4o","output":[%s],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`, id, finalMsg)
			writeSSE(fmt.Sprintf(`{"type":"response.created","response":{"id":%q,"object":"response","created_at":1710000000,"status":"in_progress","model":"gpt-4o","output":[]}}`, id))
			writeSSE(fmt.Sprintf(`{"type":"response.output_item.added","output_index":0,"item":%s}`, msgItem))
			writeSSE(fmt.Sprintf(`{"type":"response.content_part.added","item_id":%q,"output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`, msgID))
			writeSSE(fmt.Sprintf(`{"type":"response.output_text.delta","item_id":%q,"output_index":0,"content_index":0,"delta":%q}`, msgID, text))
			writeSSE(fmt.Sprintf(`{"type":"response.output_text.done","item_id":%q,"output_index":0,"content_index":0,"text":%q}`, msgID, text))
			writeSSE(fmt.Sprintf(`{"type":"response.content_part.done","item_id":%q,"output_index":0,"content_index":0,"part":{"type":"output_text","text":%q}}`, msgID, text))
			writeSSE(fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":%s}`, finalMsg))
			writeSSE(fmt.Sprintf(`{"type":"response.completed","response":%s}`, respCompleted))
			return
		}
		http.NotFound(w, r)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	base := "http://" + ln.Addr().String() + "/v1"

	env := append(scrubEnv(os.Environ(), "CODEX_HOME", "OPENAI_API_KEY", "OPENAI_BASE_URL"),
		"CODEX_HOME="+codexHome,
		"HOME="+tmpRoot,
		"OPENAI_API_KEY="+modelprofiles.LoopbackAuthPlaceholder,
	)
	cfg := []string{
		"-c", `model_provider="openai"`,
		"-c", `openai_base_url="` + base + `"`,
		"-c", `model="gpt-4o"`,
		"--dangerously-bypass-approvals-and-sandbox",
		"--skip-git-repo-check",
	}
	cmd1 := exec.Command(codexPath, append([]string{"exec"}, append(cfg, "Reply with exactly: alpha")...)...)
	cmd1.Env = env
	cmd1.Dir = tmpRoot
	out1, err1 := cmd1.CombinedOutput()
	t.Logf("codex turn1 exit=%v tail=%q", err1, trimTail(string(out1), 600))

	cmd2 := exec.Command(codexPath, append([]string{"exec", "resume", "--last"}, append(cfg, "Reply with exactly: beta")...)...)
	cmd2.Env = env
	cmd2.Dir = tmpRoot
	out2, err2 := cmd2.CombinedOutput()
	t.Logf("codex turn2 exit=%v tail=%q", err2, trimTail(string(out2), 600))

	mu.Lock()
	defer mu.Unlock()
	if len(reqs) < 2 {
		t.Fatalf("expected >=2 POST /responses, got %d: %#v", len(reqs), summarizeBodies(reqs))
	}
	for i, req := range reqs {
		keys := topLevelKeys(req.Body)
		t.Logf("codex req[%d] path=%s keys=%v opaque=%v input=%s",
			i, req.Path, keys, codexOpaqueSignals(req.Body), extractFieldJSON(req.Body, "input", 1800))
	}
	artDir := os.Getenv("TMPDIR")
	if artDir == "" {
		artDir = t.TempDir()
	}
	art := filepath.Join(artDir, "zen-codex-portable-capture.json")
	raw, _ := json.MarshalIndent(reqs, "", "  ")
	if err := os.WriteFile(art, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote evidence %s (%d requests)", art, len(reqs))
}

func TestCaptureClaudeMultiTurnMessagesBodies(t *testing.T) {
	if os.Getenv("ZEN_PORTABLE_HISTORY_CAPTURE") == "" {
		t.Skip("set ZEN_PORTABLE_HISTORY_CAPTURE=1 to capture Claude multi-turn bodies")
	}
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		t.Fatalf("claude not on PATH: %v", err)
	}
	verOut, err := exec.Command(claudePath, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("claude --version: %v (%s)", err, verOut)
	}
	ver := strings.TrimSpace(string(verOut))
	t.Logf("claude version=%s", ver)
	if !strings.Contains(ver, "2.1.224") {
		t.Logf("WARNING: expected Claude Code 2.1.224, got %q — evidence is version-specific", ver)
	}

	tmpRoot := t.TempDir()
	claudeCfg := filepath.Join(tmpRoot, "claude")
	if err := os.MkdirAll(claudeCfg, 0o700); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var reqs []capturedReq
	var turn int
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/messages") || strings.Contains(r.URL.Path, "/v1/messages")) {
			mu.Lock()
			turn++
			n := turn
			reqs = append(reqs, capturedReq{Method: r.Method, Path: r.URL.Path, Body: append(json.RawMessage(nil), body...)})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
			  "id":"msg_fake_turn_` + itoa(n) + `",
			  "type":"message",
			  "role":"assistant",
			  "model":"claude-sonnet-4-20250514",
			  "content":[{"type":"text","text":"token-` + itoa(n) + `"}],
			  "stop_reason":"end_turn",
			  "usage":{"input_tokens":12,"output_tokens":3}
			}`))
			return
		}
		http.NotFound(w, r)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	base := "http://" + ln.Addr().String()

	env := append(scrubEnv(os.Environ(), "ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CONFIG_DIR"),
		"HOME="+tmpRoot,
		"CLAUDE_CONFIG_DIR="+claudeCfg,
		"ANTHROPIC_BASE_URL="+base,
		"ANTHROPIC_API_KEY=sk-ant-test-placeholder-not-a-secret",
		"CLAUDE_CODE_SIMPLE=1",
	)
	cmd1 := exec.Command(claudePath,
		"-p", "--bare", "--dangerously-skip-permissions",
		"--model", "claude-sonnet-4-20250514",
		"--output-format", "json",
		"Reply with exactly: alpha",
	)
	cmd1.Env = env
	cmd1.Dir = tmpRoot
	out1, err1 := cmd1.CombinedOutput()
	t.Logf("claude turn1 exit=%v tail=%q", err1, trimTail(string(out1), 600))

	// Wait briefly for session persistence.
	time.Sleep(200 * time.Millisecond)
	cmd2 := exec.Command(claudePath,
		"-p", "--bare", "--dangerously-skip-permissions",
		"--continue",
		"--model", "claude-sonnet-4-20250514",
		"--output-format", "json",
		"Reply with exactly: beta",
	)
	cmd2.Env = env
	cmd2.Dir = tmpRoot
	out2, err2 := cmd2.CombinedOutput()
	t.Logf("claude turn2 exit=%v tail=%q", err2, trimTail(string(out2), 600))

	mu.Lock()
	defer mu.Unlock()
	if len(reqs) < 2 {
		t.Fatalf("expected >=2 POST /messages, got %d: %#v", len(reqs), summarizeBodies(reqs))
	}
	for i, req := range reqs {
		t.Logf("claude req[%d] path=%s keys=%v opaque=%v thinking=%s messages=%s",
			i, req.Path, topLevelKeys(req.Body), claudeOpaqueSignals(req.Body),
			extractFieldJSON(req.Body, "thinking", 400),
			extractFieldJSON(req.Body, "messages", 2000))
	}
	artDir := os.Getenv("TMPDIR")
	if artDir == "" {
		artDir = t.TempDir()
	}
	art := filepath.Join(artDir, "zen-claude-portable-capture.json")
	raw, _ := json.MarshalIndent(reqs, "", "  ")
	if err := os.WriteFile(art, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote evidence %s (%d requests)", art, len(reqs))
}

func runVersion(t *testing.T, bin, homeKey string) string {
	t.Helper()
	tmp := t.TempDir()
	cmd := exec.Command(bin, "--version")
	cmd.Env = append(scrubEnv(os.Environ(), homeKey), homeKey+"="+tmp, "HOME="+tmp)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s --version: %v (%s)", bin, err, out)
	}
	return strings.TrimSpace(string(out))
}

func topLevelKeys(body []byte) []string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	return keys
}

func codexOpaqueSignals(body []byte) map[string]any {
	var obj map[string]any
	if json.Unmarshal(body, &obj) != nil {
		return nil
	}
	out := map[string]any{}
	for _, k := range []string{"previous_response_id", "prompt_cache_key", "conversation_id", "store"} {
		if v, ok := obj[k]; ok {
			out[k] = v
		}
	}
	if input, ok := obj["input"]; ok {
		out["input_kind"] = typeName(input)
		raw, _ := json.Marshal(input)
		s := string(raw)
		out["input_has_reasoning"] = strings.Contains(s, `"reasoning"`) || strings.Contains(s, `"type":"reasoning"`)
		out["input_has_item_reference"] = strings.Contains(s, `"item_reference"`) || strings.Contains(s, `"type":"item_reference"`)
		out["input_has_function_call"] = strings.Contains(s, `"function_call"`) || strings.Contains(s, `"function_call_output"`)
		out["input_has_output_text"] = strings.Contains(s, `"output_text"`) || strings.Contains(s, `"input_text"`)
	}
	return out
}

func claudeOpaqueSignals(body []byte) map[string]any {
	var obj map[string]any
	if json.Unmarshal(body, &obj) != nil {
		return nil
	}
	out := map[string]any{}
	raw, _ := json.Marshal(obj)
	s := string(raw)
	out["has_thinking"] = strings.Contains(s, `"thinking"`) || strings.Contains(s, `"redacted_thinking"`)
	out["has_tool_use"] = strings.Contains(s, `"tool_use"`) || strings.Contains(s, `"tool_result"`)
	out["has_cache_control"] = strings.Contains(s, `"cache_control"`)
	if msgs, ok := obj["messages"]; ok {
		out["messages_kind"] = typeName(msgs)
		b, _ := json.Marshal(msgs)
		out["messages_bytes"] = len(b)
	}
	return out
}

func typeName(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "other"
	}
}

func compactJSON(body []byte, max int) string {
	var v any
	if json.Unmarshal(body, &v) != nil {
		return trimTail(string(body), max)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return trimTail(string(body), max)
	}
	return trimTail(string(b), max)
}

func extractFieldJSON(body []byte, field string, max int) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return ""
	}
	raw, ok := obj[field]
	if !ok {
		return "<missing>"
	}
	return trimTail(string(raw), max)
}

func summarizeBodies(reqs []capturedReq) []string {
	out := make([]string, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, r.Path+":"+string(itoa(len(r.Body))))
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func jsonType(data string) string {
	var obj struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(data), &obj) != nil || obj.Type == "" {
		return "message"
	}
	return obj.Type
}
