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

// Controlled integration proof for the reported regression: a new Session
// launched with the UI-selected gpt-5.6-sol carries that exact model into the
// routed Codex request body — the CLI's client contract model (gpt-5) is
// replaced by the selected upstream model before anything reaches the gateway.
func TestRoutedLaunchCarriesGpt56SolToUpstreamUnchanged(t *testing.T) {
	var upstreamModel string
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var obj map[string]any
		_ = json.Unmarshal(body, &obj)
		upstreamModel, _ = obj["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"r_1","object":"response"}`)
	})
	defer upstream.Close()

	owner := startTestOwner(t, readyLookup("x"))
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "cf-api-fan", Name: "gateway.example", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: upstream.URL + "/v1",
		Advanced: true,
	}, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put("cf-api-fan", []string{"gpt-5.6-sol", "gpt-5.6-terra"}, nil)
	owner.mu.Unlock()
	proj, err = owner.SetProviderDefault(ClientCodex, "cf-api-fan", "gpt-5.6-sol", proj.Revision)
	if err != nil {
		t.Fatal(err)
	}

	// New Session launch carrying the client-selected model.
	plan, err := owner.PrepareLaunchModel(ExecutorCodex, "cf-api-fan", "gpt-5.6-sol", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if plan.State.Binding.UpstreamModel != "gpt-5.6-sol" {
		t.Fatalf("binding upstream_model=%q want gpt-5.6-sol", plan.State.Binding.UpstreamModel)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(owner.router.Handler())
	defer srv.Close()
	base, err := LoopbackCodexBaseURL(srv.Listener.Addr().String(), plan.State.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, base+"/responses",
		bytes.NewReader([]byte(`{"model":"gpt-5","input":"hello"}`)))
	// The Codex CLI authenticates to the loopback with the placeholder; the
	// router strips it and injects the real upstream credential (AuthModeNone
	// here, so no auth header is forwarded upstream).
	req.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if upstreamModel != "gpt-5.6-sol" {
		t.Fatalf("upstream received model=%q want gpt-5.6-sol (CLI contract model was %q)", upstreamModel, "gpt-5")
	}
	if strings.Contains(plan.Command, "gpt-5.6-sol") {
		t.Fatalf("launch command must keep the client contract model, not the upstream: %q", plan.Command)
	}
}
