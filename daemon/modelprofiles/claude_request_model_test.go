package modelprofiles

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClaudeConnectionWithoutModelLaunchesAndRoutesRequestModel(t *testing.T) {
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel = body.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","role":"assistant","content":[]}`)
	}))
	defer upstream.Close()

	owner, _ := newDurableOwner(t, t.TempDir())
	defer owner.Close()
	projection, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		Name: "Claude account", Client: ClientClaude, PresetID: ProviderPresetCustom,
		BaseURL: upstream.URL, Advanced: true,
	}, "fixture-key", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connection := projection.Connections[0]
	if connection.ManualModelID != "" {
		t.Fatalf("connection unexpectedly owns a model: %#v", connection)
	}
	if len(projection.Models[connection.ID]) == 0 {
		t.Fatal("local fallback catalog should remain available without live discovery")
	}
	plan, err := owner.PrepareLaunch(ExecutorClaude, connection.ID, "claude")
	if err != nil {
		t.Fatalf("connection-only Claude launch: %v", err)
	}
	if strings.Contains(plan.Command, "--model") {
		t.Fatalf("local Claude model must be inherited: %q", plan.Command)
	}
	if !plan.State.Binding.RequestModelRouting || plan.State.Binding.UpstreamModel != "" {
		t.Fatalf("binding must be request-level without a fake model: %#v", plan.State.Binding)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "claude-no-model"); err != nil {
		t.Fatal(err)
	}

	request, err := http.NewRequest(http.MethodPost, plan.Env[EnvAnthropicBaseURL]+"/v1/messages", bytes.NewBufferString(`{"model":"vendor/local-agent-model","max_tokens":1,"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || gotModel != "vendor/local-agent-model" {
		t.Fatalf("request model was not forwarded: status=%d model=%q", response.StatusCode, gotModel)
	}
}

func TestClaudeRouteRejectsModelOutsideProviderCatalog(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()
	table := NewRouteTable()
	profile := routedClaude(upstream.URL, "claude-sonnet-4-6", "claude-upstream")
	state, err := table.BindLaunch("claude-catalog", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table, WithRouterModelCatalog(func(string) ([]ProviderModelEntry, error) {
		return []ProviderModelEntry{{ID: "claude-supported", Available: true}}, nil
	}))
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	root, _ := LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	req, _ := http.NewRequest(http.MethodPost, root+"/v1/messages", bytes.NewBufferString(`{"model":"claude-missing"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || upstreamCalls != 0 {
		t.Fatalf("unsupported model status=%d upstream_calls=%d", resp.StatusCode, upstreamCalls)
	}
}

func TestClaudeRoutePassesModelWhenCatalogIsUnavailable(t *testing.T) {
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel = body.Model
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"provider rejected model"}`)
	}))
	defer upstream.Close()
	table := NewRouteTable()
	profile := routedClaude(upstream.URL, "claude-sonnet-4-6", "claude-upstream")
	state, err := table.BindLaunch("claude-empty-catalog", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table, WithRouterModelCatalog(func(string) ([]ProviderModelEntry, error) {
		return nil, nil
	}))
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	root, _ := LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	request, _ := http.NewRequest(http.MethodPost, root+"/v1/messages", bytes.NewBufferString(`{"model":"vendor/unknown","messages":[]}`))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || gotModel != "vendor/unknown" {
		t.Fatalf("empty catalog must preserve model and upstream error: status=%d model=%q", response.StatusCode, gotModel)
	}
}

func TestFallbackCatalogRowsDoNotRejectCustomAgentModel(t *testing.T) {
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel = body.Model
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()
	table := NewRouteTable()
	profile := routedClaude(upstream.URL, "claude-local", "claude-fallback")
	state, err := table.BindLaunch("claude-fallback", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table, WithRouterReliableModelCatalog(func(string) (RouteModelCatalog, error) {
		return RouteModelCatalog{
			Entries:  []ProviderModelEntry{{ID: "claude-bundled", Available: true, Source: ModelSourceBundled}},
			Reliable: false,
		}, nil
	}))
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	root, _ := LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	request, _ := http.NewRequest(http.MethodPost, root+"/v1/messages", bytes.NewBufferString(`{"model":"vendor/local-agent-model","messages":[]}`))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || gotModel != "vendor/local-agent-model" {
		t.Fatalf("fallback rows must be display-only: status=%d model=%q", response.StatusCode, gotModel)
	}
}

func TestCodexConnectionWithoutModelUsesRequestRouting(t *testing.T) {
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel = body.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	owner, _ := newDurableOwner(t, t.TempDir())
	t.Cleanup(func() { _ = owner.Close() })
	projection, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		Name: "Codex account", Client: ClientCodex, PresetID: ProviderPresetCustom,
		BaseURL: upstream.URL + "/v1", Advanced: true,
	}, "fixture-key", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connection := projection.Connections[0]
	if len(projection.Models[connection.ID]) == 0 {
		t.Fatal("local fallback catalog should remain available without live discovery")
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, connection.ID, "codex")
	if err != nil {
		t.Fatalf("connection-only Codex launch: %v", err)
	}
	if strings.Contains(plan.Command, "--model") || !plan.State.Binding.RequestModelRouting || plan.State.Binding.UpstreamModel != "" {
		t.Fatalf("Codex connection must inherit local model and bind request-level: command=%q binding=%#v", plan.Command, plan.State.Binding)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "codex-no-model"); err != nil {
		t.Fatal(err)
	}
	base, err := LoopbackCodexBaseURL(owner.ListenAddr(), owner.RouteIDForSession("codex-no-model"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, base+"/responses", bytes.NewBufferString(`{"model":"vendor/local-agent-model","input":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || gotModel != "vendor/local-agent-model" {
		t.Fatalf("Codex request model was not forwarded: status=%d model=%q", response.StatusCode, gotModel)
	}
}
