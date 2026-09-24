package modelprofiles

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
