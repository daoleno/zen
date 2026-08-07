package modelprofiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeDeepSeekResponsesClassifications(t *testing.T) {
	env := envelopeDeepSeekV4Flash()

	t.Run("strip_previous_response_id", func(t *testing.T) {
		out, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","previous_response_id":"resp_old","input":"hi"
		}`), env)
		if err != nil {
			t.Fatal(err)
		}
		assertJSONKeysAbsent(t, out, "previous_response_id")
		assertJSONKeysPresent(t, out, "model", "input")
	})

	t.Run("strip_telemetry_cache_hints", func(t *testing.T) {
		out, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","input":"hi",
			"prompt_cache_key":"ck","prompt_cache_retention":"24h",
			"client_metadata":{"cli":"codex"},"safety_identifier":"s",
			"stream_options":{"include_usage":true},"metadata":{"m":1}
		}`), env)
		if err != nil {
			t.Fatal(err)
		}
		assertJSONKeysAbsent(t, out,
			"prompt_cache_key", "prompt_cache_retention", "client_metadata",
			"safety_identifier", "stream_options", "metadata")
		assertJSONKeysPresent(t, out, "model", "input")
	})

	t.Run("forward_parallel_tool_calls_true", func(t *testing.T) {
		out, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","parallel_tool_calls":true,"input":"hi"
		}`), env)
		if err != nil {
			t.Fatal(err)
		}
		assertJSONKeysPresent(t, out, "parallel_tool_calls")
	})

	t.Run("compat_strip_parallel_tool_calls_false", func(t *testing.T) {
		out, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","parallel_tool_calls":false,"input":"hi"
		}`), env)
		if err != nil {
			t.Fatal(err)
		}
		assertJSONKeysAbsent(t, out, "parallel_tool_calls")
	})

	t.Run("reject_max_tool_calls_effective_limit", func(t *testing.T) {
		_, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","max_tool_calls":4,"input":"hi"
		}`), env)
		if !errors.Is(err, ErrResponsesFeatureUnsupported) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("compat_strip_exact_codex_include", func(t *testing.T) {
		out, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","include":["reasoning.encrypted_content"],"input":"hi"
		}`), env)
		if err != nil {
			t.Fatal(err)
		}
		assertJSONKeysAbsent(t, out, "include")
	})

	t.Run("near_miss_include_extra_entry_rejects", func(t *testing.T) {
		_, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","include":["reasoning.encrypted_content","file_search_call.results"],"input":"hi"
		}`), env)
		if !errors.Is(err, ErrResponsesFeatureUnsupported) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("near_miss_include_other_value_rejects", func(t *testing.T) {
		_, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","include":["file_search_call.results"],"input":"hi"
		}`), env)
		if !errors.Is(err, ErrResponsesFeatureUnsupported) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("compat_strip_reasoning_summary_auto", func(t *testing.T) {
		out, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","reasoning":{"effort":"high","summary":"auto"},"input":"hi"
		}`), env)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]any
		if err := json.Unmarshal(out, &obj); err != nil {
			t.Fatal(err)
		}
		reasoning, _ := obj["reasoning"].(map[string]any)
		if reasoning["effort"] != "high" {
			t.Fatalf("effort must be preserved: %#v", reasoning)
		}
		if _, ok := reasoning["summary"]; ok {
			t.Fatalf("auto summary must be stripped: %#v", reasoning)
		}
	})

	t.Run("near_miss_reasoning_summary_detailed_rejects", func(t *testing.T) {
		_, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","reasoning":{"summary":"detailed"},"input":"hi"
		}`), env)
		if !errors.Is(err, ErrResponsesFeatureUnsupported) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("preserve_reasoning_effort", func(t *testing.T) {
		out, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","reasoning":{"effort":"high"},"input":"hi"
		}`), env)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]any
		if err := json.Unmarshal(out, &obj); err != nil {
			t.Fatal(err)
		}
		reasoning, _ := obj["reasoning"].(map[string]any)
		if reasoning["effort"] != "high" {
			t.Fatalf("effort must be preserved: %#v", reasoning)
		}
	})

	t.Run("reject_unknown_reasoning_nested_key", func(t *testing.T) {
		_, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","reasoning":{"effort":"high","generate_summary":true},"input":"hi"
		}`), env)
		if !errors.Is(err, ErrResponsesFeatureUnsupported) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("reject_case_incorrect_top_level_key", func(t *testing.T) {
		_, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","Include":["reasoning.encrypted_content"],"input":"hi"
		}`), env)
		if !errors.Is(err, ErrResponsesFeatureUnsupported) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("reject_text_verbosity", func(t *testing.T) {
		_, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","text":{"format":{"type":"text"},"verbosity":"high"},"input":"hi"
		}`), env)
		if !errors.Is(err, ErrResponsesFeatureUnsupported) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("reject_unknown_text_nested_key", func(t *testing.T) {
		_, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","text":{"format":{"type":"text"},"tone":"warm"},"input":"hi"
		}`), env)
		if !errors.Is(err, ErrResponsesFeatureUnsupported) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("preserve_text_format", func(t *testing.T) {
		out, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","text":{"format":{"type":"text"}},"input":"hi"
		}`), env)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), `"format"`) {
			t.Fatalf("text.format missing: %s", out)
		}
	})

	t.Run("reject_truncation", func(t *testing.T) {
		_, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","truncation":"auto","input":"hi"
		}`), env)
		if !errors.Is(err, ErrResponsesFeatureUnsupported) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("reject_context_management", func(t *testing.T) {
		_, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","context_management":[{"type":"x"}],"input":"hi"
		}`), env)
		if !errors.Is(err, ErrResponsesFeatureUnsupported) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("reject_service_tier", func(t *testing.T) {
		_, err := sanitizeDeepSeekResponsesRequest([]byte(`{
			"model":"gpt-5","service_tier":"priority","input":"hi"
		}`), env)
		if !errors.Is(err, ErrResponsesFeatureUnsupported) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("codex_envelope_compat_success", func(t *testing.T) {
		// Observed Codex 0.146.1 DeepSeek-bound shape (field structure only).
		body := []byte(`{
			"model":"gpt-5",
			"previous_response_id":"resp_old",
			"instructions":"be brief",
			"prompt_cache_key":"ck",
			"client_metadata":{"cli":"codex"},
			"include":["reasoning.encrypted_content"],
			"parallel_tool_calls":false,
			"reasoning":{"summary":"auto"},
			"text":{"format":{"type":"text"}},
			"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
				{"type":"function_call","name":"shell_command","call_id":"c1","arguments":"{}"},
				{"type":"function_call_output","call_id":"c1","output":"ok"},
				{"type":"reasoning","id":"r1","content":[{"type":"reasoning_text","text":"plan"}]}
			],
			"tools":[{"type":"function","name":"shell_command","parameters":{"type":"object"}}],
			"store":false,
			"stream":true,
			"tool_choice":"auto"
		}`)
		out, err := sanitizeDeepSeekResponsesRequest(body, env)
		if err != nil {
			t.Fatal(err)
		}
		assertJSONKeysAbsent(t, out,
			"previous_response_id", "prompt_cache_key", "client_metadata",
			"include", "parallel_tool_calls")
		assertJSONKeysPresent(t, out, "instructions", "input", "tools", "stream", "tool_choice")
		s := string(out)
		if !strings.Contains(s, "function_call_output") {
			t.Fatalf("history missing: %s", s)
		}
		if strings.Contains(s, `"summary"`) {
			t.Fatalf("auto summary must not remain: %s", s)
		}
	})

	rejectCases := []struct {
		name string
		body string
	}{
		{"unknown_field", `{"model":"gpt-5","not_a_field":1,"input":"hi"}`},
		{"conversation", `{"model":"gpt-5","conversation":"c1","input":"hi"}`},
		{"store_true", `{"model":"gpt-5","store":true,"input":"hi"}`},
		{"background", `{"model":"gpt-5","background":true,"input":"hi"}`},
		{"prompt", `{"model":"gpt-5","prompt":{"id":"p"},"input":"hi"}`},
		{"refusal", `{"model":"gpt-5","input":[{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"no"}]}]}`},
		{"encrypted_reasoning", `{"model":"gpt-5","input":[{"type":"reasoning","encrypted_content":"blob","content":[{"type":"reasoning_text","text":"x"}]}]}`},
		{"reasoning_item_summary", `{"model":"gpt-5","input":[{"type":"reasoning","summary":[{"type":"summary_text","text":"x"}],"content":[{"type":"reasoning_text","text":"x"}]}]}`},
		{"namespace_file_search", `{"model":"gpt-5","input":"hi","tools":[{"type":"namespace","name":"ns","tools":[{"type":"file_search"}]}]}`},
		{"file_search", `{"model":"gpt-5","input":"hi","tools":[{"type":"file_search"}]}`},
		{"mcp", `{"model":"gpt-5","input":"hi","tools":[{"type":"mcp"}]}`},
		{"unknown_tool", `{"model":"gpt-5","input":"hi","tools":[{"type":"weird_tool"}]}`},
		{"image", `{"model":"gpt-5","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://x"}]}]}`},
	}
	for _, tc := range rejectCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sanitizeDeepSeekResponsesRequest([]byte(tc.body), env)
			if !errors.Is(err, ErrResponsesFeatureUnsupported) {
				t.Fatalf("err=%v", err)
			}
		})
	}

	expanded, err := sanitizeDeepSeekResponsesRequest([]byte(`{
		"model":"gpt-5","input":"hi",
		"tools":[{"type":"namespace","name":"ns","tools":[{"type":"function","name":"shell_command","parameters":{"type":"object"}}]},{"type":"namespace","name":"empty"}]
	}`), env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(expanded), "shell_command") || strings.Contains(string(expanded), `"namespace"`) {
		t.Fatalf("expand=%s", expanded)
	}
}

func assertJSONKeysAbsent(t *testing.T, raw []byte, keys ...string) {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if _, ok := obj[k]; ok {
			t.Fatalf("%s must be absent: %s", k, raw)
		}
	}
}

func assertJSONKeysPresent(t *testing.T, raw []byte, keys ...string) {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if _, ok := obj[k]; !ok {
			t.Fatalf("%s must be present: %s", k, raw)
		}
	}
}

func TestCompileDeepSeekAccountConnectionMultiClient(t *testing.T) {
	conn, err := CompileProviderConnection(ProviderConnectionInput{
		PresetID: ProviderPresetDeepSeek,
	})
	if err != nil {
		t.Fatal(err)
	}
	if conn.Name != "DeepSeek" {
		t.Fatalf("name derived from preset label: %q", conn.Name)
	}
	if !isAccountConnection(conn) || conn.ExecutorID != "" || conn.Protocol != "" || conn.Model != "" {
		t.Fatalf("account=%#v", conn)
	}
	codex, err := CompileConnectionTarget(conn, ClientCodex, "deepseek-v4-flash")
	if err != nil {
		t.Fatal(err)
	}
	if codex.Protocol != ProtocolOpenAIResponses || codex.BaseURL != "https://api.deepseek.com" {
		t.Fatalf("codex=%#v", codex)
	}
	claude, err := CompileConnectionTarget(conn, ClientClaude, "deepseek-v4-flash")
	if err != nil {
		t.Fatal(err)
	}
	if claude.Protocol != ProtocolAnthropicMessages || claude.BaseURL != "https://api.deepseek.com/anthropic" {
		t.Fatalf("claude=%#v", claude)
	}
	pub := providerConnectionFromProfile(conn, true)
	raw, _ := json.Marshal(pub)
	for _, banned := range []string{"executor_id", "auth_mode", "credential_env", "protocol", "client_model", "generation", `"model_id"`, `"provider_id"`, "https://api.deepseek.com"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("public leaked %q: %s", banned, raw)
		}
	}
	if len(pub.Clients) != 2 {
		t.Fatalf("clients=%v", pub.Clients)
	}
}

func TestSetProviderDefaultValidatesClientModel(t *testing.T) {
	owner := startTestOwner(t, func(string) (string, bool) { return "ready", true })
	conn, err := CompileProviderConnection(ProviderConnectionInput{PresetID: ProviderPresetDeepSeek})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.UpsertProfile(conn, 0, true); err != nil {
		t.Fatal(err)
	}
	rev := owner.Catalog().Revision

	proj, err := owner.SetProviderDefault(ClientCodex, conn.ID, "deepseek-v4-flash", rev)
	if err != nil {
		t.Fatal(err)
	}
	if proj.Defaults[ClientCodex].ModelID != "deepseek-v4-flash" {
		t.Fatalf("defaults=%#v", proj.Defaults)
	}
	rev = owner.Catalog().Revision

	_, err = owner.SetProviderDefault(ClientCodex, conn.ID, "deepseek-v4-pro", rev)
	if err == nil {
		t.Fatal("unsupported Codex model must reject before persistence")
	}
	if owner.Catalog().Revision != rev {
		t.Fatalf("revision mutated on unsupported model: %d", owner.Catalog().Revision)
	}
	if owner.store.DefaultModelID(ClientCodex) != "deepseek-v4-flash" {
		t.Fatalf("default mutated: %q", owner.store.DefaultModelID(ClientCodex))
	}

	// Claude Anthropic target accepts deepseek-v4-pro per that target's contract
	// (trusted model; Codex Responses restriction does not apply).
	_, err = owner.SetProviderDefault(ClientClaude, conn.ID, "deepseek-v4-pro", rev)
	if err != nil {
		t.Fatalf("claude pro: %v", err)
	}
	if owner.store.DefaultModelID(ClientClaude) != "deepseek-v4-pro" {
		t.Fatalf("claude default=%q", owner.store.DefaultModelID(ClientClaude))
	}
}

func TestActivateSessionProviderDoesNotMutateCatalog(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	a := codexResponsesProfile("a", "gpt-5", "up-a")
	if _, err := owner.UpsertProfile(a, 0, true); err != nil {
		t.Fatal(err)
	}
	b := codexResponsesProfile("b", "gpt-5", "up-b")
	b.BaseURL = a.BaseURL
	b.ProviderID = "deepseek"
	b.ProviderLabel = "DeepSeek"
	if _, err := owner.UpsertProfile(b, 1, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, a.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	beforeRev := owner.Catalog().Revision
	beforeModel := b.Model
	state, snap, persist, err := owner.ActivateSessionProvider("s1", b.ID, "session-only-model")
	if err != nil || !persist.Applied {
		t.Fatalf("activate err=%v persist=%#v", err, persist)
	}
	if snap.Current == nil || snap.Current.ModelID != "session-only-model" {
		t.Fatalf("snap=%#v", snap)
	}
	if owner.Catalog().Revision != beforeRev {
		t.Fatalf("catalog revision mutated %d -> %d", beforeRev, owner.Catalog().Revision)
	}
	got, _ := owner.GetProfile(b.ID)
	if got.Model != beforeModel {
		t.Fatalf("connection model mutated %q -> %q", beforeModel, got.Model)
	}
	_ = state
}

func TestDeepSeekPortableActivateSetsStripOpaque(t *testing.T) {
	table := NewRouteTable()
	auth := ContractAuth{Verifier: BuiltinEnvelopeVerifier{}}
	openai := Profile{
		ID: "o", Name: "O", ExecutorID: ExecutorCodex, ProviderID: "openai", ProviderLabel: "OpenAI",
		Protocol: ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "gpt-5",
		ClientModelProvenance: ContractProvenanceConfiguredCompatibility,
		BaseURL:               "https://api.openai.com/v1", AuthMode: AuthModeNone,
	}
	state, err := table.BindLaunch("s", openai, 1, auth)
	if err != nil {
		t.Fatal(err)
	}
	state.Binding.HistoryState = HistoryStateMayContainOpaque
	table.bySession["s"] = state

	ds := Profile{
		ID: "ds", Name: "DeepSeek", ExecutorID: ExecutorCodex, ProviderID: "deepseek", ProviderLabel: "DeepSeek",
		Protocol: ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "deepseek-v4-flash",
		ClientModelProvenance: ContractProvenanceConfiguredCompatibility,
		BaseURL:               "https://api.deepseek.com", AuthMode: AuthModeNone,
	}
	next, err := table.Activate("s", ds, 1, state.Generation, auth)
	if err != nil {
		t.Fatal(err)
	}
	if next.Binding.HistoryPortability != HistoryPortabilityStripOpaque {
		t.Fatalf("portability=%q", next.Binding.HistoryPortability)
	}
}

func TestDeepSeekRouterForwardsCodexShapedRequest(t *testing.T) {
	var got []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"model\":\"deepseek-v4-flash\",\"output\":[]}}\n\n")
	}))
	t.Cleanup(up.Close)
	table := NewRouteTable()
	auth := ContractAuth{Verifier: BuiltinEnvelopeVerifier{}}
	p := Profile{
		ID: "ds", Name: "DS", ExecutorID: ExecutorCodex, ProviderID: "deepseek", ProviderLabel: "DeepSeek",
		Protocol: ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "deepseek-v4-flash",
		ClientModelProvenance: ContractProvenanceConfiguredCompatibility,
		BaseURL:               strings.TrimSuffix(up.URL, "/"), AuthMode: AuthModeNone,
	}
	st, err := table.BindLaunch("s1", p, 1, auth)
	if err != nil {
		t.Fatal(err)
	}
	st.Binding.HistoryPortability = HistoryPortabilityStripOpaque
	table.bySession["s1"] = st
	router := NewRouter(table)
	body := []byte(`{"model":"gpt-5","previous_response_id":"resp_old","store":false,"include":["reasoning.encrypted_content"],"parallel_tool_calls":false,"reasoning":{"summary":"auto"},"prompt_cache_key":"ck","client_metadata":{"cli":"codex"},"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],"tools":[{"type":"function","name":"shell_command","parameters":{"type":"object"}},{"type":"namespace","name":"ns","tools":[]}],"stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/r/"+st.Binding.RouteID+"/v1/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:1"
	rr := httptest.NewRecorder()
	router.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(got) == 0 {
		t.Fatal("upstream empty")
	}
	if strings.Contains(string(got), "previous_response_id") || strings.Contains(string(got), "prompt_cache_key") {
		t.Fatalf("strip failed: %s", got)
	}
	if strings.Contains(string(got), `"include"`) || strings.Contains(string(got), `"parallel_tool_calls"`) || strings.Contains(string(got), `"summary"`) {
		t.Fatalf("compat fields must be stripped: %s", got)
	}
	if !strings.Contains(string(got), "deepseek-v4-flash") {
		t.Fatalf("model rewrite missing: %s", got)
	}

	// Near-miss include remains fail-closed at the proxy boundary.
	bad := []byte(`{"model":"gpt-5","include":["file_search_call.results"],"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],"stream":true}`)
	req2 := httptest.NewRequest(http.MethodPost, "/r/"+st.Binding.RouteID+"/v1/responses", bytes.NewReader(bad))
	req2.Header.Set("Content-Type", "application/json")
	req2.RemoteAddr = "127.0.0.1:1"
	rr2 := httptest.NewRecorder()
	router.Handler().ServeHTTP(rr2, req2)
	if rr2.Code == 200 {
		t.Fatalf("near-miss include must be rejected, body=%s", rr2.Body.String())
	}
}

func TestDiscoverySaveBarrierNewerWriterWins(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "provider-discovery.json")
	c := newModelDiscoveryCache()
	c.put("c1", []string{"model-A"}, nil)

	releaseA := make(chan struct{})
	aClaimed := make(chan struct{})
	c.setSaveHook(func(phase string) {
		if phase == "after_claim" {
			select {
			case <-aClaimed:
			default:
				close(aClaimed)
			}
			<-releaseA
		}
	})

	errA := make(chan error, 1)
	go func() { errA <- c.save(path) }()
	<-aClaimed

	c.put("c1", []string{"model-B"}, nil)
	errB := make(chan error, 1)
	go func() { errB <- c.save(path) }()

	// B blocks on saveMu until A finishes; release A then wait for both.
	close(releaseA)
	if err := <-errA; err != nil {
		t.Fatalf("writer A: %v", err)
	}
	if err := <-errB; err != nil {
		t.Fatalf("writer B: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc durableDiscoveryFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	e := doc.Entries["c1"]
	if len(e.IDs) != 1 || e.IDs[0] != "model-B" {
		t.Fatalf("final durable content want model-B, got %#v file=%s", e, raw)
	}
	reloaded := newModelDiscoveryCache()
	if err := reloaded.load(path); err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.get("c1")
	if !ok || len(got.LastGood) != 1 || got.LastGood[0] != "model-B" {
		t.Fatalf("reload=%#v", got)
	}
}
