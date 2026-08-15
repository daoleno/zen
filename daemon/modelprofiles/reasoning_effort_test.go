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
	"sync"
	"testing"
)

// reasoningEffortUpstream records the rewritten body of every request it
// serves so tests can assert exactly what effort (if any) reached the upstream.
type reasoningEffortUpstream struct {
	mu     sync.Mutex
	calls  []effortUpstreamCall
	server *httptest.Server
	hold   chan struct{} // when non-nil, blocks each request until released
}

type effortUpstreamCall struct {
	model  string
	effort string // "" = no reasoning.effort in the forwarded body
}

func newReasoningEffortUpstream(t *testing.T, hold chan struct{}) *reasoningEffortUpstream {
	t.Helper()
	u := &reasoningEffortUpstream{hold: hold}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var obj struct {
			Model     string `json:"model"`
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
		}
		_ = json.Unmarshal(body, &obj)
		u.mu.Lock()
		u.calls = append(u.calls, effortUpstreamCall{model: obj.Model, effort: obj.Reasoning.Effort})
		u.mu.Unlock()
		if hold != nil {
			<-hold
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"r_effort","object":"response","output":[]}`)
	}))
	t.Cleanup(u.server.Close)
	return u
}

func (u *reasoningEffortUpstream) last() (effortUpstreamCall, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.calls) == 0 {
		return effortUpstreamCall{}, false
	}
	return u.calls[len(u.calls)-1], true
}

func (u *reasoningEffortUpstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.calls)
}

func effortOwner(t *testing.T, root string) *Owner {
	t.Helper()
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath:  filepath.Join(root, "model-profiles.toml"),
		RoutesPath:    filepath.Join(root, "route-bindings.json"),
		ListenerPath:  filepath.Join(root, "route-listener.json"),
		DiscoveryPath: filepath.Join(root, "provider-discovery.json"),
		Lookup:        func(string) (string, bool) { return "", false },
		Verifier:      BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	creds, err := NewFileCredentialStore(filepath.Join(root, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner.SetCredentialStore(creds)
	return owner
}

func postEffortRequest(t *testing.T, routerAddr, routeID, model string) {
	t.Helper()
	postEffortRequestWithEffort(t, routerAddr, routeID, model, "")
}

func postEffortRequestWithEffort(t *testing.T, routerAddr, routeID, model, effort string) {
	t.Helper()
	base, err := LoopbackCodexBaseURL(routerAddr, routeID)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"` + model + `"`
	if effort != "" {
		body += `,"reasoning":{"effort":"` + effort + `"}`
	}
	body += `,"input":[]}`
	req, _ := http.NewRequest(http.MethodPost, base+"/responses", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("route %s status=%d", routeID, resp.StatusCode)
	}
}

// TestCodexEffortContract pins the daemon-owned effort vocabulary and the
// per-model contracts. Unsupported clients (Claude) and unknown models must
// never fabricate an effort surface.
func TestCodexEffortContract(t *testing.T) {
	for _, value := range codexReasoningEffortVocabulary {
		if !isCodexReasoningEffortValue(value) {
			t.Fatalf("vocabulary value %q must admit itself", value)
		}
	}
	for _, value := range []string{"", "none", "ultra", "turbo"} {
		if isCodexReasoningEffortValue(value) {
			t.Fatalf("value %q must be rejected by the daemon vocabulary", value)
		}
	}

	cases := []struct {
		model      string
		defaultEff string
		supported  []string
	}{
		{"gpt-5", ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		{"gpt-5-codex", ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		{"gpt-5.1-codex", ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh}},
		{"gpt-5.6-sol", ReasoningEffortMedium, []string{ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh, ReasoningEffortMax}},
		{"o3", ReasoningEffortMedium, []string{ReasoningEffortMinimal, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
		{"o4-mini", ReasoningEffortMedium, []string{ReasoningEffortMinimal, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh}},
	}
	for _, tc := range cases {
		entry, ok := lookupCodexModelMetadata(tc.model)
		if !ok {
			t.Fatalf("model %s must have catalog metadata", tc.model)
		}
		if entry.Effort == nil {
			t.Fatalf("model %s must have an effort contract", tc.model)
		}
		if entry.Effort.defaultEffort != tc.defaultEff {
			t.Fatalf("%s default=%q want %q", tc.model, entry.Effort.defaultEffort, tc.defaultEff)
		}
		if len(entry.Effort.supported) != len(tc.supported) {
			t.Fatalf("%s supported=%v want %v", tc.model, entry.Effort.supported, tc.supported)
		}
		for i, value := range tc.supported {
			if entry.Effort.supported[i] != value {
				t.Fatalf("%s supported=%v want %v", tc.model, entry.Effort.supported, tc.supported)
			}
		}
		for _, value := range entry.Effort.supported {
			if !codexEffortSupported(tc.model, value) {
				t.Fatalf("%s must support its own effort %q", tc.model, value)
			}
		}
	}

	// Unknown models and non-Codex clients admit nothing.
	if _, ok := lookupCodexModelMetadata("model-a"); ok {
		t.Fatal("unknown model must not fabricate catalog metadata")
	}
	if _, ok := lookupCodexModelMetadata("claude-sonnet-4-6"); ok {
		t.Fatal("Claude client model must not have a Codex catalog entry")
	}
	if codexEffortSupported("model-a", ReasoningEffortHigh) {
		t.Fatal("unknown model must not admit any effort")
	}
	if codexEffortSupported("claude-sonnet-4-6", ReasoningEffortHigh) {
		t.Fatal("Claude client model must not admit any effort")
	}
	if codexEffortSupported("gpt-5", ReasoningEffortXHigh) {
		t.Fatal("gpt-5 must not admit xhigh")
	}
	if codexEffortSupported("deepseek-v4-flash", ReasoningEffortHigh) {
		t.Fatal("deepseek-v4-flash must not admit effort (never speculated)")
	}

	snapshots := CodexEffortContractSnapshots()
	for _, snap := range snapshots {
		entry, ok := lookupCodexModelMetadata(snap.ClientModel)
		if !ok || entry.Effort == nil || entry.Effort.defaultEffort != snap.Default || len(entry.Effort.supported) != len(snap.Supported) {
			t.Fatalf("snapshot drift for %s", snap.ClientModel)
		}
	}
}

// TestRewriteRequestEffort pins the router body rewrite: effort is set on the
// top-level reasoning object, other reasoning keys (summary) and every other
// body field are preserved, the object is created when absent, and unknown or
// malformed inputs fail closed.
func TestRewriteRequestEffort(t *testing.T) {
	body := []byte(`{"model":"cli","reasoning":{"summary":"auto"},"input":[{"role":"user","content":"hi"}]}`)
	out, err := rewriteRequestEffort(body, ReasoningEffortHigh)
	if err != nil {
		t.Fatal(err)
	}
	var obj struct {
		Model     string `json:"model"`
		Reasoning struct {
			Effort  string `json:"effort"`
			Summary string `json:"summary"`
		} `json:"reasoning"`
		Input []map[string]string `json:"input"`
	}
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	if obj.Model != "cli" || obj.Reasoning.Effort != "high" || obj.Reasoning.Summary != "auto" || len(obj.Input) != 1 {
		t.Fatalf("rewrite corrupted body: %s", out)
	}

	// Creates the reasoning object when the body has none.
	out, err = rewriteRequestEffort([]byte(`{"model":"cli","input":[]}`), ReasoningEffortLow)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Reasoning struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	if err := json.Unmarshal(out, &created); err != nil || created.Reasoning.Effort != "low" {
		t.Fatalf("effort object not created: %s err=%v", out, err)
	}

	// Empty override is a pass-through.
	original := []byte(`{"model":"cli"}`)
	out, err = rewriteRequestEffort(original, "")
	if err != nil || !bytes.Equal(out, original) {
		t.Fatalf("empty effort must pass through: %s err=%v", out, err)
	}

	// Fail closed: unknown effort value, malformed body, non-object body.
	if _, err := rewriteRequestEffort(body, "turbo"); !errors.Is(err, ErrRequestBodyMalformed) {
		t.Fatalf("unknown effort err=%v", err)
	}
	if _, err := rewriteRequestEffort([]byte(`{"model":`), ReasoningEffortLow); !errors.Is(err, ErrRequestBodyMalformed) {
		t.Fatalf("malformed body err=%v", err)
	}
	if _, err := rewriteRequestEffort([]byte(`[1,2]`), ReasoningEffortLow); !errors.Is(err, ErrRequestBodyMalformed) {
		t.Fatalf("non-object body err=%v", err)
	}
	if _, err := rewriteRequestEffort([]byte(`{"model":"cli"} trailing`), ReasoningEffortLow); !errors.Is(err, ErrRequestBodyMalformed) {
		t.Fatalf("trailing junk err=%v", err)
	}
	// A reasoning object that is not a JSON object fails closed.
	if _, err := rewriteRequestEffort([]byte(`{"reasoning":42}`), ReasoningEffortLow); !errors.Is(err, ErrRequestBodyMalformed) {
		t.Fatalf("non-object reasoning err=%v", err)
	}
	cleared, err := clearRequestEffort([]byte(`{"model":"cli","reasoning":{"effort":"high","summary":"auto"},"input":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	var clearedObj struct {
		Reasoning struct {
			Effort  string `json:"effort"`
			Summary string `json:"summary"`
		} `json:"reasoning"`
	}
	if err := json.Unmarshal(cleared, &clearedObj); err != nil || clearedObj.Reasoning.Effort != "" || clearedObj.Reasoning.Summary != "auto" {
		t.Fatalf("clear effort corrupted body: %s err=%v", cleared, err)
	}
}

// TestReasoningEffortActivationE2E proves the Session effort lifecycle on one
// live routed Codex Session: launch has no override; an acknowledged
// Model+Effort activation applies to the next admitted request only; the
// in-flight request keeps its immutable effort; unsupported/unknown efforts
// fail inline keeping the old route; an omitted effort preserves the current
// override; the override survives restart; concurrent Sessions and the
// catalog are untouched; Claude Sessions never admit effort.
func TestReasoningEffortActivationE2E(t *testing.T) {
	root := t.TempDir()
	codexUpstream := newReasoningEffortUpstream(t, nil)
	codexURL := codexUpstream.server.URL + "/v1"

	owner := effortOwner(t, root)
	t.Cleanup(func() { _ = owner.Close() })

	proj, err := owner.UpsertProviderConnection(e2eCustomInput("", "CodexGW", codexURL, "gpt-5-codex"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	conn := proj.Connections[0]
	seedModelCatalogs(t, owner, map[string][]string{conn.ID: {"gpt-5-codex"}})
	if _, err := owner.SetProviderDefault(ClientCodex, conn.ID, "gpt-5-codex", proj.Revision); err != nil {
		t.Fatal(err)
	}

	plan, err := owner.PrepareLaunch(ExecutorCodex, conn.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s-effort"); err != nil {
		t.Fatal(err)
	}
	routeID := plan.State.Binding.RouteID
	routerSrv := httptest.NewServer(owner.router.Handler())
	t.Cleanup(routerSrv.Close)
	routerAddr := routerSrv.Listener.Addr().String()

	// Launch: no override; the selection still projects the daemon-owned
	// contract for the gpt-5 client model (default medium, low/medium/high).
	sel, ok := owner.ThreadRuntime("s-effort")
	if !ok {
		t.Fatal("selection missing")
	}
	if sel.ReasoningEffort != "" || sel.ReasoningEffortDefault != ReasoningEffortMedium ||
		len(sel.ReasoningEfforts) != 3 || sel.ReasoningEfforts[1] != ReasoningEffortMedium {
		t.Fatalf("launch selection effort projection=%#v", sel)
	}
	postEffortRequest(t, routerAddr, routeID, "gpt-5-codex")
	if got, _ := codexUpstream.last(); got.effort != "" {
		t.Fatalf("no override must not rewrite effort: %#v", got)
	}

	// Acknowledged activation: same model, explicit effort high. The next
	// admitted request is normalized to the binding even when the unchanged
	// CLI still sends its old effort; the Session is not recreated.
	beforeRev := owner.Catalog().Revision
	state, snap, persist, err := owner.SetThreadRuntime("s-effort", ThreadRuntimeChoice{ConnectionID: conn.ID, ModelID: "gpt-5-codex", Effect: ReasoningEffortHigh})
	if err != nil || !persist.Applied {
		t.Fatalf("activate high err=%v persist=%#v", err, persist)
	}
	if state.Binding.RouteID != routeID || state.Binding.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("activate high binding=%#v", state.Binding)
	}
	if snap.Current == nil || snap.Current.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("activate high snap=%#v", snap.Current)
	}
	if owner.Catalog().Revision != beforeRev {
		t.Fatal("activation mutated the catalog")
	}
	postEffortRequestWithEffort(t, routerAddr, routeID, "gpt-5-codex", ReasoningEffortLow)
	if got, _ := codexUpstream.last(); got.effort != ReasoningEffortHigh || got.model != "gpt-5-codex" {
		t.Fatalf("stale CLI effort was not normalized to binding=%#v", got)
	}
	if sel, _ := owner.ThreadRuntime("s-effort"); sel.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("request mismatch mutated the binding: %#v", sel)
	}
	// A request without effort receives the acknowledged override too.
	postEffortRequest(t, routerAddr, routeID, "gpt-5-codex")
	if got, _ := codexUpstream.last(); got.effort != ReasoningEffortHigh {
		t.Fatalf("missing CLI effort was not normalized: %#v", got)
	}

	// Omitted effort on a compatible model switch preserves the override.
	if _, _, _, err := owner.SetThreadRuntime("s-effort", ThreadRuntimeChoice{ConnectionID: conn.ID, ModelID: "gpt-5-codex", Effect: ReasoningEffortHigh}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.SetThreadRuntime("s-effort", ThreadRuntimeChoice{ConnectionID: conn.ID, ModelID: "gpt-5-codex", Effect: ""}); err != nil {
		t.Fatal(err)
	}
	if sel, _ := owner.ThreadRuntime("s-effort"); sel.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("omitted effort must preserve the override: %#v", sel)
	}

	// Explicit unsupported effort (xhigh is not in the gpt-5-codex contract)
	// fails inline and keeps the old route + old override; an invalid effort
	// never reaches the upstream.
	_, _, _, err = owner.SetThreadRuntime("s-effort", ThreadRuntimeChoice{ConnectionID: conn.ID, ModelID: "gpt-5-codex", Effect: ReasoningEffortXHigh})
	if !errors.Is(err, ErrReasoningEffortUnsupported) {
		t.Fatalf("xhigh err=%v", err)
	}
	state, _ = owner.Table().Get("s-effort")
	if state.Binding.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("failed activation must keep the old override: %#v", state.Binding)
	}
	if sel, _ := owner.ThreadRuntime("s-effort"); sel.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("selection must keep the old override: %#v", sel)
	}

	// Unknown vocabulary values fail closed too.
	if _, _, _, err := owner.SetThreadRuntime("s-effort", ThreadRuntimeChoice{ConnectionID: conn.ID, ModelID: "gpt-5-codex", Effect: "turbo"}); !errors.Is(err, ErrReasoningEffortUnsupported) {
		t.Fatalf("unknown effort err=%v", err)
	}

	// A second Session on the same connection is untouched.
	plan2, err := owner.PrepareLaunch(ExecutorCodex, conn.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan2.ProvisionalID, "s-other"); err != nil {
		t.Fatal(err)
	}
	if sel, _ := owner.ThreadRuntime("s-other"); sel.ReasoningEffort != "" {
		t.Fatalf("concurrent session must have no override: %#v", sel)
	}
	postEffortRequest(t, routerAddr, plan2.State.Binding.RouteID, "gpt-5-codex")
	if got, _ := codexUpstream.last(); got.effort != "" {
		t.Fatalf("concurrent session request must not carry effort: %#v", got)
	}

	// Restart restoration: the override survives and remains authoritative.
	_ = owner.Close()
	owner2 := effortOwner(t, root)
	t.Cleanup(func() { _ = owner2.Close() })
	routerSrv2 := httptest.NewServer(owner2.router.Handler())
	t.Cleanup(routerSrv2.Close)
	state2, ok := owner2.Table().Get("s-effort")
	if !ok || state2.Binding.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("restored binding lost override: %#v ok=%v", state2.Binding, ok)
	}
	if sel, _ := owner2.ThreadRuntime("s-effort"); sel.ReasoningEffort != ReasoningEffortHigh {
		t.Fatalf("restored selection lost override: %#v", sel)
	}
	postEffortRequestWithEffort(t, routerSrv2.Listener.Addr().String(), routeID, "gpt-5-codex", ReasoningEffortHigh)
	if got, _ := codexUpstream.last(); got.effort != ReasoningEffortHigh {
		t.Fatalf("request after restart lost override=%#v", got)
	}
}

// TestReasoningEffortInFlightImmutable proves the in-flight request keeps its
// immutable effort snapshot: a request admitted under high finishes against
// high even when the Session is activated to low while it is in flight; the
// next request admits under low.
func TestReasoningEffortInFlightImmutable(t *testing.T) {
	root := t.TempDir()
	hold := make(chan struct{})
	started := make(chan struct{})
	codexUpstream := newReasoningEffortUpstream(t, hold)
	codexUpstream.mu.Lock()
	codexUpstream.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var obj struct {
			Model     string `json:"model"`
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
		}
		_ = json.Unmarshal(body, &obj)
		codexUpstream.mu.Lock()
		codexUpstream.calls = append(codexUpstream.calls, effortUpstreamCall{model: obj.Model, effort: obj.Reasoning.Effort})
		codexUpstream.mu.Unlock()
		select {
		case <-started:
		default:
			close(started)
		}
		<-hold
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"r_effort","object":"response","output":[]}`)
	})
	codexUpstream.mu.Unlock()

	owner := effortOwner(t, root)
	t.Cleanup(func() { _ = owner.Close() })
	proj, err := owner.UpsertProviderConnection(e2eCustomInput("", "CodexGW", codexUpstream.server.URL+"/v1", "gpt-5-codex"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	conn := proj.Connections[0]
	seedModelCatalogs(t, owner, map[string][]string{conn.ID: {"gpt-5-codex"}})
	if _, err := owner.SetProviderDefault(ClientCodex, conn.ID, "gpt-5-codex", proj.Revision); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, conn.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s-inflight"); err != nil {
		t.Fatal(err)
	}
	routerSrv := httptest.NewServer(owner.router.Handler())
	t.Cleanup(routerSrv.Close)
	routerAddr := routerSrv.Listener.Addr().String()
	if _, _, _, err := owner.SetThreadRuntime("s-inflight", ThreadRuntimeChoice{ConnectionID: conn.ID, ModelID: "gpt-5-codex", Effect: ReasoningEffortHigh}); err != nil {
		t.Fatal(err)
	}
	// Request A admits under high and is held upstream.
	doneA := make(chan struct{})
	go func() {
		postEffortRequest(t, routerAddr, plan.State.Binding.RouteID, "gpt-5-codex")
		close(doneA)
	}()
	<-started
	if got, _ := codexUpstream.last(); got.effort != ReasoningEffortHigh {
		t.Fatalf("in-flight request must run high: %#v", got)
	}

	// Activation to low while A is in flight.
	if _, _, _, err := owner.SetThreadRuntime("s-inflight", ThreadRuntimeChoice{ConnectionID: conn.ID, ModelID: "gpt-5-codex", Effect: ReasoningEffortLow}); err != nil {
		t.Fatal(err)
	}
	close(hold)
	<-doneA

	// Request B admits under low.
	postEffortRequest(t, routerAddr, plan.State.Binding.RouteID, "gpt-5-codex")
	if codexUpstream.count() != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", codexUpstream.count())
	}
	calls := codexUpstream.calls
	if calls[0].effort != ReasoningEffortHigh {
		t.Fatalf("in-flight request A effort=%q want high", calls[0].effort)
	}
	if calls[1].effort != ReasoningEffortLow {
		t.Fatalf("next request B effort=%q want low", calls[1].effort)
	}
}

// TestReasoningEffortClaudeRejected proves non-Codex Sessions never admit an
// effort: the activation fails inline and the Claude route is unchanged.
func TestReasoningEffortClaudeRejected(t *testing.T) {
	root := t.TempDir()
	owner := effortOwner(t, root)
	t.Cleanup(func() { _ = owner.Close() })

	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		Name: "Anthropic", Client: ClientClaude,
		PresetID: ProviderPresetAnthropic, BaseURL: "https://api.anthropic.com",
	}, "key-claude", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	conn := proj.Connections[0]
	seedModelCatalogs(t, owner, map[string][]string{conn.ID: {"claude-sonnet-4-6"}})
	if _, err := owner.SetProviderDefault(ClientClaude, conn.ID, "claude-sonnet-4-6", proj.Revision); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorClaude, conn.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s-claude"); err != nil {
		t.Fatal(err)
	}
	sel, ok := owner.ThreadRuntime("s-claude")
	if !ok {
		t.Fatal("selection missing")
	}
	if sel.ReasoningEfforts != nil || sel.ReasoningEffortDefault != "" || sel.ReasoningEffort != "" {
		t.Fatalf("Claude selection must project no effort surface: %#v", sel)
	}
	if _, _, _, err := owner.SetThreadRuntime("s-claude", ThreadRuntimeChoice{ConnectionID: conn.ID, ModelID: "claude-sonnet-4-6", Effect: ReasoningEffortHigh}); !errors.Is(err, ErrReasoningEffortUnsupported) {
		t.Fatalf("Claude effort activation err=%v", err)
	}
	if state, _ := owner.Table().Get("s-claude"); state.Binding.ReasoningEffort != "" {
		t.Fatalf("Claude route must stay effort-free: %#v", state.Binding)
	}
}

// TestReasoningEffortPersistenceFailClosed proves a persisted route whose
// override is outside the daemon vocabulary fails restoration — an unknown
// effort can never be restored into a live route.
func TestReasoningEffortPersistenceFailClosed(t *testing.T) {
	root := t.TempDir()
	owner := effortOwner(t, root)
	proj, err := owner.UpsertProviderConnection(e2eCustomInput("", "CodexGW", "https://upstream.invalid/v1", "gpt-5-codex"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	conn := proj.Connections[0]
	seedModelCatalogs(t, owner, map[string][]string{conn.ID: {"gpt-5-codex"}})
	plan, err := owner.PrepareLaunch(ExecutorCodex, conn.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s-corrupt"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.SetThreadRuntime("s-corrupt", ThreadRuntimeChoice{ConnectionID: conn.ID, ModelID: "gpt-5-codex", Effect: ReasoningEffortHigh}); err != nil {
		t.Fatal(err)
	}
	_ = owner.Close()

	// Corrupt the durable route file's override to an unknown value.
	path := filepath.Join(root, "route-bindings.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := bytes.ReplaceAll(raw, []byte(`"reasoning_effort": "high"`), []byte(`"reasoning_effort":"turbo"`))
	if bytes.Equal(corrupt, raw) {
		t.Fatal("corruption did not apply; durable effort field missing")
	}
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	owner2, err := StartOwner(OwnerConfig{
		ProfilesPath:  filepath.Join(root, "model-profiles.toml"),
		RoutesPath:    path,
		ListenerPath:  filepath.Join(root, "route-listener.json"),
		DiscoveryPath: filepath.Join(root, "provider-discovery.json"),
		Lookup:        func(string) (string, bool) { return "", false },
		Verifier:      BuiltinEnvelopeVerifier{},
	})
	if owner2 != nil {
		_ = owner2.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "unknown persisted reasoning effort") {
		t.Fatalf("restore must fail closed on unknown effort, err=%v", err)
	}
}
