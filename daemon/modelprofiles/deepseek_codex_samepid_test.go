package modelprofiles_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/modelprofiles"
)

// Local captured-contract proof: running Codex Session activates DeepSeek
// Responses (same-protocol). After switch, DeepSeek emits function_call, Codex
// executes it, DeepSeek receives correlated function_call_output, then returns
// the final answer — same pane/client PID, native conversation, Session, Route.
//
//	ZEN_DEEPSEEK_SAMEPID=1 go test ./modelprofiles -run TestDeepSeekCodexSamePIDResponses -count=1 -timeout 300s -v
//
// Live official API gate (never claim live acceptance without this):
//
//	ZEN_DEEPSEEK_LIVE=1 DEEPSEEK_API_KEY=… go test ./modelprofiles -run TestDeepSeekCodexLiveOfficialAPI -count=1 -timeout 300s -v

func TestDeepSeekCodexSamePIDResponses(t *testing.T) {
	if os.Getenv("ZEN_DEEPSEEK_SAMEPID") == "" {
		t.Skip("set ZEN_DEEPSEEK_SAMEPID=1 for DeepSeek Codex same-PID Responses proof")
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("codex not on PATH: %v", err)
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatalf("tmux not on PATH: %v", err)
	}
	t.Logf("codex version=%s", runVersion(t, codexPath, "CODEX_HOME"))

	var rejectMu sync.Mutex
	var rejectPaths []string
	var rejectStructures []map[string]any
	modelprofiles.SetDeepSeekSanitizeRejectHook(func(path string, structure map[string]any) {
		rejectMu.Lock()
		rejectPaths = append(rejectPaths, path)
		rejectStructures = append(rejectStructures, structure)
		rejectMu.Unlock()
	})
	t.Cleanup(func() { modelprofiles.SetDeepSeekSanitizeRejectHook(nil) })

	artRoot := filepath.Join(tmpdir(t), "zen-deepseek-samepid")
	_ = os.RemoveAll(artRoot)
	_ = os.MkdirAll(artRoot, 0o700)
	codexHome := filepath.Join(artRoot, "codex-home")
	cwd := filepath.Join(artRoot, "cwd")
	_ = os.MkdirAll(codexHome, 0o700)
	_ = os.MkdirAll(cwd, 0o700)
	if err := exec.Command("git", "init", "-q", cwd).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	_ = os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"OPENAI_API_KEY":"zen-loopback-placeholder-not-a-secret"}`), 0o600)

	var mu sync.Mutex
	var hitOpenAI, hitDeepSeek [][]byte
	upOpenAI := httptest.NewServer(codexStatefulFake(t, &mu, &hitOpenAI, "A", true))
	defer upOpenAI.Close()
	upDeepSeek := httptest.NewServer(deepSeekResponsesToolFake(t, &mu, &hitDeepSeek))
	defer upDeepSeek.Close()

	table := modelprofiles.NewRouteTable()
	auth := modelprofiles.ContractAuth{Verifier: modelprofiles.BuiltinEnvelopeVerifier{}}
	openaiProfile := modelprofiles.Profile{
		ID: "openai-a", Name: "OpenAI A", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "fake-openai", ProviderLabel: "FakeOpenAI",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "model-a",
		ClientModelProvenance: modelprofiles.ContractProvenanceConfiguredCompatibility,
		BaseURL:               strings.TrimSuffix(upOpenAI.URL, "/") + "/v1",
		AuthMode:              modelprofiles.AuthModeNone,
	}
	deepseekProfile := modelprofiles.Profile{
		ID: "deepseek-codex", Name: "DeepSeek", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "deepseek", ProviderLabel: "DeepSeek",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "deepseek-v4-flash",
		ClientModelProvenance: modelprofiles.ContractProvenanceConfiguredCompatibility,
		BaseURL:               strings.TrimSuffix(upDeepSeek.URL, "/"),
		AuthMode:              modelprofiles.AuthModeNone,
	}

	state, err := table.BindLaunch("samepid-deepseek", openaiProfile, 1, auth)
	if err != nil {
		t.Fatal(err)
	}
	router := modelprofiles.NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, err := modelprofiles.LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("model = \"gpt-5\"\nmodel_provider = \"openai\"\nopenai_base_url = %q\napproval_policy = \"never\"\nsandbox_mode = \"danger-full-access\"\n", base)
	_ = os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(cfg), 0o600)

	sess := fmt.Sprintf("zen-deepseek-samepid-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", sess).Run() })
	cmd := exec.Command("tmux", "new-session", "-d", "-s", sess, "-c", cwd,
		"-e", "CODEX_HOME="+codexHome,
		"-e", "HOME="+artRoot,
		"-e", "OPENAI_API_KEY="+modelprofiles.LoopbackAuthPlaceholder,
		"-e", "TERM=xterm-256color",
		"--", codexPath, "--dangerously-bypass-approvals-and-sandbox",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v (%s)", err, out)
	}
	waitCodexReady(t, sess)

	before := captureIdentity(t, sess, codexHome, "codex")
	t.Logf("before: pane=%d client=%d native=%s route=%s", before.PanePID, before.ClientPID, before.NativeSessionID, state.Binding.RouteID)
	if before.PanePID == 0 || before.ClientPID == 0 {
		t.Fatalf("missing identity %#v", before)
	}

	sendTmuxLine(t, sess, `Create file proof.txt containing exactly HELLO using a shell command, then reply with exactly: alpha`)
	waitHits(t, &mu, &hitOpenAI, 45*time.Second)
	waitPaneContains(t, sess, "token-A", 45*time.Second)

	afterTurn1 := captureIdentity(t, sess, codexHome, "codex")
	if afterTurn1.NativeSessionID == "" {
		t.Fatal("native session id missing after turn1")
	}
	routeID := state.Binding.RouteID
	zenSession := "samepid-deepseek"

	next, err := table.Activate(zenSession, deepseekProfile, 2, state.Generation, auth)
	if err != nil {
		t.Fatalf("activate deepseek: %v", err)
	}
	if next.Binding.RouteID != routeID {
		t.Fatalf("route drifted %s -> %s", routeID, next.Binding.RouteID)
	}
	if next.Binding.UpstreamModel != "deepseek-v4-flash" {
		t.Fatalf("model=%q", next.Binding.UpstreamModel)
	}
	if next.Binding.HistoryPortability != modelprofiles.HistoryPortabilityStripOpaque {
		t.Fatalf("portability=%q", next.Binding.HistoryPortability)
	}

	sendTmuxLine(t, sess, `Create file proof.txt containing exactly HELLO using a shell command, then reply with exactly: deepseek-final`)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(hitDeepSeek)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	waitPaneContains(t, sess, "deepseek-final", 90*time.Second)

	after := captureIdentity(t, sess, codexHome, "codex")
	if after.PanePID != before.PanePID || after.ClientPID != before.ClientPID {
		t.Fatalf("PID drift before=%#v after=%#v", before, after)
	}
	if after.NativeSessionID != afterTurn1.NativeSessionID {
		t.Fatalf("native session drifted %q -> %q", afterTurn1.NativeSessionID, after.NativeSessionID)
	}
	final, ok := table.Get(zenSession)
	if !ok || final.Binding.RouteID != routeID || final.Binding.SessionID != zenSession {
		t.Fatalf("route/session lost: %#v", final)
	}
	if final.Binding.UpstreamModel != "deepseek-v4-flash" {
		t.Fatalf("model drifted %q", final.Binding.UpstreamModel)
	}

	mu.Lock()
	nDS := len(hitDeepSeek)
	nOpen := len(hitOpenAI)
	mu.Unlock()
	if nDS < 2 {
		rejectMu.Lock()
		paths := append([]string{}, rejectPaths...)
		structs := append([]map[string]any{}, rejectStructures...)
		rejectMu.Unlock()
		structJSON, _ := json.Marshal(structs)
		t.Fatalf("need >=2 DeepSeek hits for tool correlation, got %d openai=%d reject_paths=%v reject_structure=%s pane=\n%s",
			nDS, nOpen, paths, trimTail(string(structJSON), 4000), capturePane(t, sess))
	}
	const callID = "c_ds_tool"
	sawOutput := false
	for i, b := range hitDeepSeek {
		var obj map[string]any
		_ = json.Unmarshal(b, &obj)
		if _, ok := obj["input"]; !ok {
			t.Fatalf("hit[%d] missing Responses input: %s", i, trimTail(string(b), 400))
		}
		if _, ok := obj["messages"]; ok {
			t.Fatalf("hit[%d] Chat Completions body: %s", i, trimTail(string(b), 400))
		}
		if _, ok := obj["previous_response_id"]; ok {
			t.Fatalf("hit[%d] leaked previous_response_id", i)
		}
		if obj["model"] != "deepseek-v4-flash" {
			t.Fatalf("hit[%d] model=%v", i, obj["model"])
		}
		body := string(b)
		if strings.Contains(body, `"function_call_output"`) && strings.Contains(body, callID) {
			sawOutput = true
		}
	}
	if !sawOutput {
		t.Fatalf("tool correlation incomplete: missing function_call_output for %q hits=%d", callID, len(hitDeepSeek))
	}
	last := hitDeepSeek[len(hitDeepSeek)-1]
	assertNoOpaqueResponsesHistory(t, last)
	t.Logf("proof ok: openai_hits=%d deepseek_hits=%d call_id=%s route=%s session=%s pane=%d native=%s",
		len(hitOpenAI), len(hitDeepSeek), callID, routeID, zenSession, after.PanePID, after.NativeSessionID)
}

// deepSeekResponsesToolFake: turn1 emits function_call; after function_call_output
// returns final assistant text deepseek-final.
func deepSeekResponsesToolFake(t *testing.T, mu *sync.Mutex, sink *[][]byte) http.Handler {
	t.Helper()
	var n int
	const callID = "c_ds_tool"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = w.Write([]byte(`{"error":{"type":"route_websocket_rejected"}}`))
			return
		}
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		*sink = append(*sink, append([]byte(nil), body...))
		n++
		turn := n
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		write := func(obj any) {
			raw, _ := json.Marshal(obj)
			var typed struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(raw, &typed)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typed.Type, raw)
			if flusher != nil {
				flusher.Flush()
			}
		}
		id := fmt.Sprintf("resp_ds_%d", turn)
		// Correlate only on this fake's call_id — portable history may already
		// contain earlier OpenAI function_call_output items.
		hasOurOutput := strings.Contains(string(body), `"function_call_output"`) &&
			strings.Contains(string(body), callID)
		if !hasOurOutput {
			item := map[string]any{
				"type": "function_call", "name": "shell_command", "call_id": callID,
				"arguments": `{"command":"printf HELLO > proof.txt"}`,
			}
			write(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
			write(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
			write(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
			write(map[string]any{"type": "response.completed", "response": map[string]any{
				"id": id, "object": "response", "status": "completed", "model": "deepseek-v4-flash",
				"output": []any{item},
			}})
			return
		}
		msgID := fmt.Sprintf("msg_ds_%d", turn)
		text := "deepseek-final"
		final := map[string]any{
			"type": "message", "id": msgID, "status": "completed", "role": "assistant",
			"content": []any{map[string]string{"type": "output_text", "text": text}},
		}
		write(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		write(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"type": "message", "id": msgID, "status": "in_progress", "role": "assistant", "content": []any{}}})
		write(map[string]any{"type": "response.content_part.added", "item_id": msgID, "output_index": 0, "content_index": 0, "part": map[string]string{"type": "output_text", "text": ""}})
		write(map[string]any{"type": "response.output_text.delta", "item_id": msgID, "output_index": 0, "content_index": 0, "delta": text})
		write(map[string]any{"type": "response.output_text.done", "item_id": msgID, "output_index": 0, "content_index": 0, "text": text})
		write(map[string]any{"type": "response.content_part.done", "item_id": msgID, "output_index": 0, "content_index": 0, "part": map[string]string{"type": "output_text", "text": text}})
		write(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": final})
		write(map[string]any{"type": "response.completed", "response": map[string]any{
			"id": id, "object": "response", "status": "completed", "model": "deepseek-v4-flash", "output": []any{final},
		}})
	})
}
