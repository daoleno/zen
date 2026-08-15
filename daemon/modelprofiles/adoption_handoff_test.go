package modelprofiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAdoptSessionModelConvergesFromRequestIdentity proves the Terminal -> Zen
// convergence path: a request carrying a different daemon-known model/effort
// converges the route binding (persisted, projected) while the request is
// forwarded with its own identity — the router never silently overrides a
// visible model.
func TestAdoptSessionModelConvergesFromRequestIdentity(t *testing.T) {
	root := t.TempDir()
	owner := effortOwner(t, root)
	t.Cleanup(func() { _ = owner.Close() })

	proj, err := owner.UpsertProviderConnection(e2eCustomInput("", "CodexGW", "https://upstream.invalid/v1", "gpt-5.6-sol"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	conn := proj.Connections[0]
	seedModelCatalogs(t, owner, map[string][]string{conn.ID: {"gpt-5.6-sol", "gpt-5.5"}})
	if _, err := owner.SetProviderDefault(ClientCodex, conn.ID, "gpt-5.6-sol", proj.Revision); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, conn.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s-adopt"); err != nil {
		t.Fatal(err)
	}
	routeID := plan.State.Binding.RouteID

	// A request carrying a different KNOWN model converges the binding.
	if err := owner.AdoptSessionModel(routeID, "gpt-5.5", ReasoningEffortHigh, true); err != nil {
		t.Fatal(err)
	}
	state, ok := owner.Table().Get("s-adopt")
	if !ok || state.Binding.UpstreamModel != "gpt-5.5" || state.Binding.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("adoption did not converge binding: %#v", state.Binding)
	}
	if sel, _ := owner.ThreadRuntime("s-adopt"); sel.ModelID != "gpt-5.5" || sel.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("selection did not converge: %#v", sel)
	}
	if state.Binding.RouteID != routeID {
		t.Fatalf("adoption must keep the route: %q", state.Binding.RouteID)
	}

	// A request without effort clears the stale override (the CLI carries none).
	if err := owner.AdoptSessionModel(routeID, "gpt-5.5", "", false); err != nil {
		t.Fatal(err)
	}
	if sel, _ := owner.ThreadRuntime("s-adopt"); sel.ReasoningEffort != "" {
		t.Fatalf("missing effort must clear the override: %#v", sel)
	}

	// Unsupported effort for the target model fails closed, binding untouched.
	if err := owner.AdoptSessionModel(routeID, "gpt-5.5", ReasoningEffortMax, true); !errors.Is(err, ErrReasoningEffortUnsupported) {
		t.Fatalf("max on gpt-5.5 err=%v", err)
	}
	// Unknown model fails closed.
	if err := owner.AdoptSessionModel(routeID, "vendor/private-model", "", false); !errors.Is(err, ErrModelUnsupported) {
		t.Fatalf("unknown model err=%v", err)
	}
	// A model not available on the connection's synced allowlist fails closed.
	if err := owner.AdoptSessionModel(routeID, "gpt-5.4", "", false); !errors.Is(err, ErrUpstreamModelRequired) {
		t.Fatalf("unavailable model err=%v", err)
	}
	state, _ = owner.Table().Get("s-adopt")
	if state.Binding.UpstreamModel != "gpt-5.5" {
		t.Fatalf("failed adoptions must keep the binding: %#v", state.Binding)
	}

	// Restart restoration: the adopted identity survives.
	_ = owner.Close()
	owner2 := effortOwner(t, root)
	t.Cleanup(func() { _ = owner2.Close() })
	state2, ok := owner2.Table().Get("s-adopt")
	if !ok || state2.Binding.UpstreamModel != "gpt-5.5" {
		t.Fatalf("adopted identity must survive restart: %#v", state2.Binding)
	}
}

// TestRouterRequestIdentityPassThroughAndAdoption proves the router forwards a
// request's own model identity unchanged (never a silent rewrite) while the
// daemon converges the binding when the identity is admitted.
func TestRouterRequestIdentityPassThroughAndAdoption(t *testing.T) {
	var gotModels []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var obj struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &obj)
		gotModels = append(gotModels, obj.Model)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"r","object":"response","output":[]}`)
	}))
	t.Cleanup(upstream.Close)

	root := t.TempDir()
	owner := effortOwner(t, root)
	t.Cleanup(func() { _ = owner.Close() })
	proj, err := owner.UpsertProviderConnection(e2eCustomInput("", "CodexGW", upstream.URL+"/v1", "gpt-5.6-sol"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	conn := proj.Connections[0]
	seedModelCatalogs(t, owner, map[string][]string{conn.ID: {"gpt-5.6-sol", "gpt-5.5"}})
	if _, err := owner.SetProviderDefault(ClientCodex, conn.ID, "gpt-5.6-sol", proj.Revision); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, conn.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s-router"); err != nil {
		t.Fatal(err)
	}
	routerSrv := httptest.NewServer(owner.router.Handler())
	t.Cleanup(routerSrv.Close)
	base, err := LoopbackCodexBaseURL(routerSrv.Listener.Addr().String(), plan.State.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}

	post := func(body string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, base+"/responses", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", resp.StatusCode, body)
		}
	}

	// Same identity: forwarded untouched.
	post(`{"model":"gpt-5.6-sol","input":[]}`)
	// Different KNOWN identity: forwarded with the CLI's own model; binding
	// converges (Terminal -> Zen).
	post(`{"model":"gpt-5.5","reasoning":{"effort":"high"},"input":[]}`)
	// Unknown identity: forwarded as-is; binding untouched (no silent rewrite).
	post(`{"model":"vendor/private-model","input":[]}`)

	if len(gotModels) != 3 || gotModels[0] != "gpt-5.6-sol" || gotModels[1] != "gpt-5.5" || gotModels[2] != "vendor/private-model" {
		t.Fatalf("upstream models=%v want exact request identities", gotModels)
	}
	sel, _ := owner.ThreadRuntime("s-router")
	if sel.ModelID != "gpt-5.5" || sel.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("binding must converge to the admitted request identity: %#v", sel)
	}

	// Pending handoff transition: the binding wins for admitted requests.
	owner.SetSessionHandoffPending(plan.State.Binding.RouteID, true)
	post(`{"model":"gpt-5.6-sol","input":[]}`)
	if len(gotModels) != 4 || gotModels[3] != "gpt-5.5" {
		t.Fatalf("pending handoff must rewrite to the binding identity: %v", gotModels)
	}
	owner.SetSessionHandoffPending(plan.State.Binding.RouteID, false)
}

// TestCompileCodexResume pins the managed-handoff command shape: resume the
// SAME thread with the new model/effort plus the route config; unknown models
// and unsupported efforts fail closed.
func TestCompileCodexResume(t *testing.T) {
	cmd, err := CompileCodexResume("thread-uuid", "gpt-5.6-sol", ReasoningEffortHigh, "http://127.0.0.1:9/r/rt_1", "/tmp/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"codex resume thread-uuid",
		`--config 'model="gpt-5.6-sol"'`,
		`--config 'model_reasoning_effort="high"'`,
		`--config 'model_provider="openai"'`,
		`--config 'openai_base_url="http://127.0.0.1:9/r/rt_1"'`,
		`--config 'model_catalog_json="/tmp/catalog.json"'`,
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("resume command missing %q: %q", want, cmd)
		}
	}

	// No effort -> no model_reasoning_effort override (CLI default applies).
	cmd, err = CompileCodexResume("thread-uuid", "gpt-5.6-sol", "", "http://127.0.0.1:9/r/rt_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cmd, "model_reasoning_effort") {
		t.Fatalf("empty effort must not emit an override: %q", cmd)
	}

	if _, err := CompileCodexResume("", "gpt-5.6-sol", "", "http://127.0.0.1:9/r/rt_1", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty thread err=%v", err)
	}
	if _, err := CompileCodexResume("t", "vendor/private-model", "", "http://127.0.0.1:9/r/rt_1", ""); !errors.Is(err, ErrModelUnsupported) {
		t.Fatalf("unknown model err=%v", err)
	}
	if _, err := CompileCodexResume("t", "gpt-5.5", ReasoningEffortMax, "http://127.0.0.1:9/r/rt_1", ""); !errors.Is(err, ErrReasoningEffortUnsupported) {
		t.Fatalf("unsupported effort err=%v", err)
	}
}
