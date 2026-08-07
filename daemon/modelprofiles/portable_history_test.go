package modelprofiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestNormalizeResponsesPortableHistoryStripsOpaqueKeepsTextTools(t *testing.T) {
	in := []byte(`{
	  "model":"cli",
	  "previous_response_id":"resp_old",
	  "prompt_cache_key":"cache-1",
	  "input":[
	    {"type":"message","role":"user","content":[{"type":"input_text","text":"alpha"}],"internal_chat_message_metadata_passthrough":{"turn_id":"t1"}},
	    {"type":"reasoning","id":"rsn_1","encrypted_content":"opaque-blob"},
	    {"type":"item_reference","id":"item_1"},
	    {"type":"message","role":"assistant","content":[{"type":"output_text","text":"token-1"},{"type":"reasoning","encrypted_content":"x"},{"type":"refusal","refusal":"nope"}]},
	    {"type":"function_call","name":"Bash","call_id":"c1","arguments":"{\"command\":\"echo hi\"}","provider_secret":"leak"},
	    {"type":"function_call_output","call_id":"c1","output":"hi","provider_secret":"leak"}
	  ]
	}`)
	out, deg, err := normalizePortableHistoryBody(RouteProtocolResponses, in)
	if err != nil || deg != HistoryDegradationStripOpaque {
		t.Fatalf("err=%v deg=%q", err, deg)
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["previous_response_id"]; ok {
		t.Fatalf("previous_response_id leaked: %#v", obj)
	}
	if obj["prompt_cache_key"] != "cache-1" {
		t.Fatalf("prompt_cache_key=%v", obj["prompt_cache_key"])
	}
	raw, _ := json.Marshal(obj["input"])
	s := string(raw)
	if strings.Contains(s, "previous_response_id") || strings.Contains(s, "encrypted_content") || strings.Contains(s, "item_reference") {
		t.Fatalf("opaque leaked: %s", s)
	}
	if strings.Contains(s, "provider_secret") || strings.Contains(s, "internal_chat_message_metadata") {
		t.Fatalf("unknown provider fields leaked: %s", s)
	}
	if !strings.Contains(s, `"alpha"`) || !strings.Contains(s, `"token-1"`) || !strings.Contains(s, `"function_call"`) {
		t.Fatalf("portable text/tools missing: %s", s)
	}
	if !strings.Contains(s, `"refusal"`) || !strings.Contains(s, `"nope"`) {
		t.Fatalf("visible refusal dropped: %s", s)
	}
}

func TestNormalizeResponsesPortableHistoryFailsClosedUnknownType(t *testing.T) {
	in := []byte(`{"input":[{"type":"vendor_secret_item","payload":"x"}]}`)
	_, _, err := normalizePortableHistoryBody(RouteProtocolResponses, in)
	if !errors.Is(err, ErrRequestBodyNotPortable) {
		t.Fatalf("err=%v", err)
	}
}

func TestNormalizeAnthropicPortableHistoryStripsThinkingKeepsTextTools(t *testing.T) {
	in := []byte(`{
	  "model":"claude",
	  "thinking":{"type":"enabled","budget_tokens":1000},
	  "messages":[
	    {"role":"user","content":[{"type":"text","text":"alpha","cache_control":{"type":"ephemeral"}}]},
	    {"role":"assistant","content":[
	      {"type":"thinking","thinking":"secret","signature":"sig"},
	      {"type":"redacted_thinking","data":"blob"},
	      {"type":"text","text":"token-1"},
	      {"type":"tool_use","id":"t1","name":"Bash","input":{"command":"echo hi"},"provider_secret":"leak"}
	    ]},
	    {"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"hi"},{"type":"text","text":"beta"}]}
	  ]
	}`)
	out, deg, err := normalizePortableHistoryBody(RouteProtocolAnthropicMessages, in)
	if err != nil || deg != HistoryDegradationStripOpaque {
		t.Fatalf("err=%v deg=%q", err, deg)
	}
	s := string(out)
	if strings.Contains(s, `"thinking":"secret"`) || strings.Contains(s, "redacted_thinking") || strings.Contains(s, `"signature"`) {
		t.Fatalf("opaque thinking leaked: %s", s)
	}
	if strings.Contains(s, "provider_secret") {
		t.Fatalf("unknown tool fields leaked: %s", s)
	}
	if !strings.Contains(s, `"token-1"`) || !strings.Contains(s, `"tool_use"`) || !strings.Contains(s, `"tool_result"`) {
		t.Fatalf("portable text/tools missing: %s", s)
	}
	if !strings.Contains(s, `"budget_tokens":1000`) {
		t.Fatalf("thinking config stripped: %s", s)
	}
}

func TestNormalizeAnthropicThinkingOnlyKeepsRoleAdjacency(t *testing.T) {
	in := []byte(`{
	  "messages":[
	    {"role":"user","content":[{"type":"text","text":"alpha"}]},
	    {"role":"assistant","content":[{"type":"thinking","thinking":"secret","signature":"sig"}]},
	    {"role":"user","content":[{"type":"text","text":"beta"}]}
	  ]
	}`)
	out, _, err := normalizePortableHistoryBody(RouteProtocolAnthropicMessages, in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, anthropicThinkingOmittedPlaceholder) {
		t.Fatalf("placeholder missing: %s", s)
	}
	if strings.Contains(s, `"signature"`) {
		t.Fatalf("signature leaked: %s", s)
	}
}

func TestPortableHistoryStickySurvivesDurableRestore(t *testing.T) {
	table := NewRouteTable()
	a := routedCodex("http://127.0.0.1:9/v1", "gpt-5", "model-a")
	state, err := table.BindLaunch("sticky", a, 1, verifiedAuth(a))
	if err != nil {
		t.Fatal(err)
	}
	if err := table.MarkHistoryMayContainOpaque(state.Binding.RouteID); err != nil {
		t.Fatal(err)
	}
	b := routedCodex("http://127.0.0.1:9/v1", "gpt-5", "model-b")
	got, err := table.Activate("sticky", b, 2, state.Generation, verifiedAuth(b))
	if err != nil {
		t.Fatal(err)
	}
	if got.Binding.HistoryPortability != HistoryPortabilityStripOpaque {
		t.Fatalf("portability=%q", got.Binding.HistoryPortability)
	}
	raw, err := EncodeDurableSnapshot(table.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"history_portability"`)) {
		t.Fatalf("portability not durable: %s", raw)
	}
	states, err := DecodeDurableSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	restored := NewRouteTable()
	if err := restored.Restore(states, registerAllow(registerAllow(nil, a), b)); err != nil {
		t.Fatal(err)
	}
	again, ok := restored.Get("sticky")
	if !ok || again.Binding.HistoryPortability != HistoryPortabilityStripOpaque {
		t.Fatalf("sticky portability lost after restore: %#v", again.Binding)
	}
	if again.Binding.RouteID != state.Binding.RouteID {
		t.Fatalf("route id changed on restore")
	}
}

func TestNormalizeAnthropicPortableHistoryFailsClosedUnknownType(t *testing.T) {
	in := []byte(`{"messages":[{"role":"assistant","content":[{"type":"vendor_memory","data":"x"}]}]}`)
	_, _, err := normalizePortableHistoryBody(RouteProtocolAnthropicMessages, in)
	if !errors.Is(err, ErrRequestBodyNotPortable) {
		t.Fatalf("err=%v", err)
	}
}

func TestPortableHistoryHotSwitchRouterAThenB(t *testing.T) {
	var mu sync.Mutex
	var hitA, hitB []string
	upA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		hitA = append(hitA, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_a","object":"response","status":"completed","output":[]}`))
	}))
	defer upA.Close()
	upB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		hitB = append(hitB, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_b","object":"response","status":"completed","output":[]}`))
	}))
	defer upB.Close()

	table := NewRouteTable()
	table.SetLookup(func(string) (string, bool) { return "ready", true })
	a := routedCodex(upA.URL, "gpt-5", "model-a")
	state, err := table.BindLaunch("sess", a, 1, verifiedAuth(a))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, err := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}

	body1 := `{"model":"cli","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"alpha"}]}]}`
	resp, err := http.Post(base+"/responses", "application/json", strings.NewReader(body1))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	b := routedCodex(upB.URL, "gpt-5", "model-b")
	got, err := table.Activate("sess", b, 2, 1, verifiedAuth(b))
	if err != nil {
		t.Fatal(err)
	}
	if got.Binding.HistoryPortability != HistoryPortabilityStripOpaque {
		t.Fatalf("portability=%q", got.Binding.HistoryPortability)
	}
	if got.Binding.RouteID != state.Binding.RouteID {
		t.Fatalf("route id changed: %q -> %q", state.Binding.RouteID, got.Binding.RouteID)
	}

	body2 := `{
	  "model":"cli",
	  "previous_response_id":"resp_a",
	  "input":[
	    {"type":"message","role":"user","content":[{"type":"input_text","text":"alpha"}]},
	    {"type":"reasoning","encrypted_content":"nope"},
	    {"type":"message","role":"assistant","content":[{"type":"output_text","text":"token-1"}]},
	    {"type":"message","role":"user","content":[{"type":"input_text","text":"beta"}]}
	  ]
	}`
	resp, err = http.Post(base+"/responses", "application/json", strings.NewReader(body2))
	if err != nil {
		t.Fatal(err)
	}
	rawB, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, rawB)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(hitA) != 1 || len(hitB) != 1 {
		t.Fatalf("hitA=%d hitB=%d", len(hitA), len(hitB))
	}
	if !strings.Contains(hitA[0], `"model-a"`) && !strings.Contains(hitA[0], "alpha") {
		t.Fatalf("gateway A body=%s", hitA[0])
	}
	if strings.Contains(hitB[0], "previous_response_id") || strings.Contains(hitB[0], "encrypted_content") {
		t.Fatalf("gateway B received opaque: %s", hitB[0])
	}
	if !strings.Contains(hitB[0], `"model-b"`) || !strings.Contains(hitB[0], "token-1") || !strings.Contains(hitB[0], "beta") {
		t.Fatalf("gateway B missing portable continuity: %s", hitB[0])
	}
}

func TestPortableHistoryActivatePreservesInFlightOnOldBinding(t *testing.T) {
	hold := make(chan struct{})
	started := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-hold
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"r","status":"completed","output":[]}`))
	}))
	defer up.Close()
	table := NewRouteTable()
	a := routedCodex(up.URL, "gpt-5", "model-a")
	state, err := table.BindLaunch("s", a, 1, verifiedAuth(a))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := http.Post(base+"/responses", "application/json", strings.NewReader(`{"model":"cli","input":[]}`))
		if err == nil {
			resp.Body.Close()
		}
	}()
	<-started
	// Mark opaque then try cross-domain while in-flight — must stay busy.
	_ = table.MarkHistoryMayContainOpaque(state.Binding.RouteID)
	b := routedCodex(up.URL, "gpt-5", "model-b")
	_, err = table.Activate("s", b, 2, 1, verifiedAuth(b))
	if !errors.Is(err, ErrBindingBusy) {
		t.Fatalf("err=%v", err)
	}
	close(hold)
	<-done
}
