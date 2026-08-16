package modelprofiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func routedCodex(upstreamURL, client, upstream string) Profile {
	p := codexResponsesProfile("p", client, upstream)
	p.BaseURL = strings.TrimSuffix(upstreamURL, "/") + "/v1"
	p.AuthMode = AuthModeNone
	p.CredentialEnv = ""
	return p
}

func routedClaude(upstreamURL, client, upstream string) Profile {
	p := claudeMessagesProfile("c", client, upstream)
	p.BaseURL = strings.TrimSuffix(upstreamURL, "/")
	p.AuthMode = AuthModeNone
	p.CredentialEnv = ""
	return p
}

func TestRouterCodexResponsesModelRewrite(t *testing.T) {
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var obj map[string]any
		_ = json.Unmarshal(body, &obj)
		if obj["model"] != "gpt-5.6-sol" {
			t.Errorf("model=%v", obj["model"])
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("auth should be stripped for AuthModeNone, got %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Request-Id", "req_1")
		w.Header().Set("X-Upstream", "should-not-leak")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: hello\n\n")
	})
	defer upstream.Close()

	table := NewRouteTable()
	profile := routedCodex(upstream.URL, "gpt-5.6-sol", "gpt-5.6-sol")
	state, err := table.BindLaunch("s1", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	req, _ := http.NewRequest(http.MethodPost, base+"/responses", bytes.NewReader([]byte(`{"model":"gpt-5.6-sol","keep":"yes"}`)))
	req.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if resp.Header.Get("X-Request-Id") != "req_1" {
		t.Fatalf("safe request-id missing: %v", resp.Header)
	}
	if resp.Header.Get("X-Upstream") != "" {
		t.Fatalf("arbitrary X-* leaked: %v", resp.Header)
	}
	got, _ := table.Get("s1")
	if got.Binding.HistoryState != HistoryStateMayContainOpaque {
		t.Fatalf("history state=%q", got.Binding.HistoryState)
	}
	if table.InFlightCount(state.Binding.RouteID) != 0 {
		t.Fatal("flight lease not released")
	}
}

func TestRouterClaudeMessagesCountTokens(t *testing.T) {
	var sawPath, sawQuery string
	var sawKey, sawVersion string
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		sawQuery = r.URL.RawQuery
		sawKey = r.Header.Get("X-Api-Key")
		sawVersion = r.Header.Get("Anthropic-Version")
		body, _ := io.ReadAll(r.Body)
		var obj map[string]any
		_ = json.Unmarshal(body, &obj)
		if obj["model"] != "claude-upstream" {
			t.Errorf("model=%v", obj["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"input_tokens":3}`)
	})
	defer upstream.Close()

	table := NewRouteTable()
	table.SetLookup(func(string) (string, bool) { return "sk-ant-test", true })
	profile := routedClaude(upstream.URL, "claude-sonnet-4-6", "claude-upstream")
	profile.AuthMode = AuthModeXAPIKeyEnv
	profile.CredentialEnv = "ANTHROPIC_API_KEY"
	state, err := table.BindLaunch("claude", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table, WithRouterLookup(func(string) (string, bool) { return "sk-ant-test", true }))
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	root, _ := LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)

	// /v1/messages?beta=true
	req, _ := http.NewRequest(http.MethodPost, root+"/v1/messages?beta=true", bytes.NewReader([]byte(`{"model":"claude-upstream","max_tokens":1}`)))
	req.Header.Set("X-Api-Key", LoopbackAuthPlaceholder)
	req.Header.Set("Anthropic-Version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || sawPath != "/v1/messages" || sawQuery != "beta=true" {
		t.Fatalf("messages path=%q query=%q status=%d", sawPath, sawQuery, resp.StatusCode)
	}
	if sawKey != "sk-ant-test" || sawVersion != "2023-06-01" {
		t.Fatalf("headers key=%q version=%q", sawKey, sawVersion)
	}

	// dedicated count_tokens
	req, _ = http.NewRequest(http.MethodPost, root+"/v1/messages/count_tokens?beta=true", bytes.NewReader([]byte(`{"model":"claude-upstream"}`)))
	req.Header.Set("X-Api-Key", "client-must-strip")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("count_tokens status=%d body=%s", resp.StatusCode, body)
	}
	if sawPath != "/v1/messages/count_tokens" || sawQuery != "beta=true" {
		t.Fatalf("count_tokens path=%q query=%q", sawPath, sawQuery)
	}
	if sawKey != "sk-ant-test" {
		t.Fatalf("count_tokens key=%q", sawKey)
	}
}

func TestRouterRejectsWebSocketUpgradeForClaudeRoute(t *testing.T) {
	// A route whose protocol never uses Responses-over-WebSocket keeps the
	// honest 501 marker and the upstream is never contacted. The Responses
	// route WebSocket path is proxied transparently (wsproxy_test.go).
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be contacted for unsupported websocket")
	})
	defer upstream.Close()
	table := NewRouteTable()
	profile := routedClaude(upstream.URL, "claude-sonnet-4-6", "claude-sonnet-4-6")
	state, err := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	req, _ := http.NewRequest(http.MethodPost, base+"/v1/messages", bytes.NewReader([]byte(`{"model":"x"}`)))
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotImplemented || !bytes.Contains(body, []byte("route_websocket_rejected")) {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestResponseStripsDynamicConnectionHop(t *testing.T) {
	// Real httptest: Connection: close must not leak arbitrary X-Secret-Hop even if
	// Transport consumes the Connection header before the app observes tokens.
	for _, connVal := range []string{"close, X-Secret-Hop", "X-Secret-Hop, close", "close"} {
		connVal := connVal
		t.Run(connVal, func(t *testing.T) {
			upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Connection", connVal)
				w.Header().Set("X-Secret-Hop", "should-not-leak")
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-Id", "safe-id")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, `{"ok":true}`)
			})
			defer upstream.Close()
			table := NewRouteTable()
			profile := routedCodex(upstream.URL, "gpt-5.6-sol", "gpt-5.6-sol")
			state, _ := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
			router := NewRouter(table)
			srv := httptest.NewServer(router.Handler())
			defer srv.Close()
			base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)
			resp, err := http.Post(base+"/responses", "application/json", bytes.NewReader([]byte(`{"model":"x"}`)))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.Header.Get("X-Secret-Hop") != "" {
				t.Fatalf("dynamic hop leaked: %v", resp.Header)
			}
			if resp.Header.Get("X-Request-Id") != "safe-id" || resp.Header.Get("Retry-After") != "1" {
				t.Fatalf("safe headers missing: %v", resp.Header)
			}
		})
	}

	// Sticky RoundTripper preserves Connection tokens end-to-end for dual-filter proof.
	t.Run("sticky_round_tripper", func(t *testing.T) {
		hdr := make(http.Header)
		hdr.Set("Connection", "X-Secret-Hop, close")
		hdr.Set("X-Secret-Hop", "leak")
		hdr.Set("Content-Type", "application/json")
		hdr.Set("Openai-Organization", "org")
		hdr.Set("X-Custom-Unknown", "nope")
		rt := stickyHeaderRoundTripper{status: 200, header: hdr, body: `{"ok":true}`}
		out := make(http.Header)
		copySafeResponseHeaders(out, mustRoundTripHeaders(t, rt))
		if out.Get("X-Secret-Hop") != "" || out.Get("X-Custom-Unknown") != "" {
			t.Fatalf("leaked: %v", out)
		}
		if out.Get("Content-Type") != "application/json" || out.Get("Openai-Organization") != "org" {
			t.Fatalf("safe missing: %v", out)
		}
	})
}

type stickyHeaderRoundTripper struct {
	status int
	header http.Header
	body   string
}

func (s stickyHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	h := make(http.Header)
	for k, vv := range s.header {
		for _, v := range vv {
			h.Add(k, v)
		}
	}
	return &http.Response{
		StatusCode: s.status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Request:    req,
	}, nil
}

func mustRoundTripHeaders(t *testing.T, rt http.RoundTripper) http.Header {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/v1/responses", bytes.NewReader([]byte(`{}`)))
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.Header
}

func TestRouterActivateNextRequestSemantics(t *testing.T) {
	hold := make(chan struct{})
	firstStarted := make(chan struct{})
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		close(firstStarted)
		<-hold
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	defer upstream.Close()

	table := NewRouteTable()
	profile := routedCodex(upstream.URL, "gpt-5.6-sol", "gpt-5.6-sol")
	state, err := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
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
		resp, err := http.Post(base+"/responses", "application/json", bytes.NewReader([]byte(`{"model":"claude-upstream"}`)))
		if err != nil {
			t.Errorf("first: %v", err)
			return
		}
		resp.Body.Close()
	}()
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not start")
	}
	if table.InFlightCount(state.Binding.RouteID) != 1 {
		t.Fatalf("expected 1 in-flight, got %d", table.InFlightCount(state.Binding.RouteID))
	}
	got, _ := table.Get("s")
	if got.Binding.HistoryState != HistoryStateEmpty {
		t.Fatalf("in-flight must not mark history yet: %q", got.Binding.HistoryState)
	}
	// Cross-domain activation while a request is in-flight must swap the route
	// atomically: the in-flight request keeps its immutable old snapshot and
	// later requests admit under the new binding (never a busy/read-only
	// Session).
	changed := routedCodex(upstream.URL, "gpt-5.5", "gpt-5.5")
	got, err = table.Activate("s", changed, 2, 1, verifiedAuth(changed))
	if err != nil {
		t.Fatalf("cross-domain while in-flight err=%v", err)
	}
	if got.Binding.UpstreamModel != "gpt-5.5" || got.Binding.RouteID != state.Binding.RouteID {
		t.Fatalf("route must swap atomically: %#v", got.Binding)
	}
	close(hold)
	<-done
	got, _ = table.Get("s")
	if got.Binding.HistoryState != HistoryStateMayContainOpaque {
		t.Fatalf("2xx should mark opaque: %q", got.Binding.HistoryState)
	}
	next := routedCodex(upstream.URL, "gpt-5.6-sol", "gpt-5.6-sol")
	got, err = table.Activate("s", next, 3, 2, verifiedAuth(next))
	if err != nil {
		t.Fatalf("opaque same-protocol portable activate err=%v", err)
	}
	if got.Binding.HistoryPortability != HistoryPortabilityStripOpaque {
		t.Fatalf("history_portability=%q", got.Binding.HistoryPortability)
	}
	if got.History[len(got.History)-1].HistoryDegradation != HistoryDegradationStripOpaque {
		t.Fatalf("degradation=%#v", got.History[len(got.History)-1])
	}
}

func TestHistoryStaysEmptyOnLocalAndNetworkFailures(t *testing.T) {
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach upstream for malformed")
	})
	defer upstream.Close()
	table := NewRouteTable()
	profile := routedCodex(upstream.URL, "gpt-5.6-sol", "gpt-5.6-sol")
	state, err := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table, WithRouterMaxBody(8))
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)

	resp, err := http.Post(base+"/responses", "application/json", bytes.NewReader(bytes.Repeat([]byte("a"), 64)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	got, _ := table.Get("s")
	if got.Binding.HistoryState != HistoryStateEmpty || table.InFlightCount(state.Binding.RouteID) != 0 {
		t.Fatalf("malformed must keep empty history/lease: state=%q inflight=%d", got.Binding.HistoryState, table.InFlightCount(state.Binding.RouteID))
	}

	// Credential fail
	table2 := NewRouteTable()
	table2.SetLookup(func(string) (string, bool) { return "ready", true })
	p2 := routedCodex(upstream.URL, "gpt-5.6-sol", "gpt-5.6-sol")
	p2.AuthMode = AuthModeBearerEnv
	p2.CredentialEnv = "K"
	state2, err := table2.BindLaunch("s2", p2, 1, verifiedAuth(p2))
	if err != nil {
		t.Fatal(err)
	}
	router2 := NewRouter(table2, WithRouterLookup(func(string) (string, bool) { return "", false }))
	srv2 := httptest.NewServer(router2.Handler())
	defer srv2.Close()
	base2, _ := LoopbackCodexBaseURL(srv2.Listener.Addr().String(), state2.Binding.RouteID)
	resp, err = http.Post(base2+"/responses", "application/json", bytes.NewReader([]byte(`{"model":"x"}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	got2, _ := table2.Get("s2")
	if got2.Binding.HistoryState != HistoryStateEmpty {
		t.Fatalf("credential fail marked history: %q", got2.Binding.HistoryState)
	}

	// Network fail: dial closed listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedURL := "http://" + ln.Addr().String()
	_ = ln.Close()
	table3 := NewRouteTable()
	p3 := routedCodex(closedURL, "gpt-5.6-sol", "gpt-5.6-sol")
	state3, err := table3.BindLaunch("s3", p3, 1, verifiedAuth(p3))
	if err != nil {
		t.Fatal(err)
	}
	router3 := NewRouter(table3)
	srv3 := httptest.NewServer(router3.Handler())
	defer srv3.Close()
	base3, _ := LoopbackCodexBaseURL(srv3.Listener.Addr().String(), state3.Binding.RouteID)
	resp, err = http.Post(base3+"/responses", "application/json", bytes.NewReader([]byte(`{"model":"x"}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	got3, _ := table3.Get("s3")
	if got3.Binding.HistoryState != HistoryStateEmpty || table3.InFlightCount(state3.Binding.RouteID) != 0 {
		t.Fatalf("network fail must keep empty: state=%q inflight=%d", got3.Binding.HistoryState, table3.InFlightCount(state3.Binding.RouteID))
	}

	// After failures, empty history may still switch domain.
	next := routedCodex(closedURL, "gpt-5.5", "gpt-5.5")
	if _, err := table3.Activate("s3", next, 2, 1, verifiedAuth(next)); err != nil {
		t.Fatalf("empty history domain switch: %v", err)
	}
}

func TestRouterStreamingFirstByteAndCancel(t *testing.T) {
	first := make(chan struct{})
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("no flusher")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "chunk1")
		flusher.Flush()
		close(first)
		select {
		case <-r.Context().Done():
			return
		case <-time.After(3 * time.Second):
			_, _ = io.WriteString(w, "chunk2")
		}
	})
	defer upstream.Close()
	table := NewRouteTable()
	profile := routedCodex(upstream.URL, "gpt-5.6-sol", "gpt-5.6-sol")
	state, err := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/responses", bytes.NewReader([]byte(`{"model":"x"}`)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 6)
	n, err := io.ReadFull(resp.Body, buf)
	if err != nil || n != 6 || string(buf) != "chunk1" {
		t.Fatalf("first byte n=%d err=%v buf=%q", n, err, buf)
	}
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("upstream first chunk not observed")
	}
	cancel()
	_, _ = io.Copy(io.Discard, resp.Body)
}

func TestRouterNoAmbientProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	var hit atomic.Bool
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		hit.Store(true)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	defer upstream.Close()
	table := NewRouteTable()
	profile := routedCodex(upstream.URL, "gpt-5.6-sol", "gpt-5.6-sol")
	state, _ := table.BindLaunch("s", profile, 1, verifiedAuth(profile))
	router := NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	resp, err := http.Post(base+"/responses", "application/json", bytes.NewReader([]byte(`{"model":"x"}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !hit.Load() {
		t.Fatal("expected direct upstream hit despite proxy env")
	}
}

func TestDurableSnapshotRestoreReverify(t *testing.T) {
	table := NewRouteTable()
	table.SetLookup(func(string) (string, bool) { return "tok", true })
	p1 := codexResponsesProfile("p1", "gpt-5", "m1")
	_, err := table.BindLaunch("s1", p1, 1, verifiedAuth(p1))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := EncodeDurableSnapshot(table.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"tok"`)) || bytes.Contains(raw, []byte(LoopbackAuthPlaceholder)) {
		t.Fatal("secret/placeholder in snapshot")
	}
	if !bytes.Contains(raw, []byte(`"schema_version": 4`)) {
		t.Fatalf("schema bump missing: %s", raw)
	}
	states, err := DecodeDurableSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	verifier := registerAllow(nil, p1)
	restored := NewRouteTable()
	restored.SetLookup(func(string) (string, bool) { return "tok", true })
	if _, err := restored.Restore(states, nil); !errors.Is(err, ErrContractUnverified) {
		t.Fatalf("restore without verifier err=%v", err)
	}
	if _, err := restored.Restore(states, verifier); err != nil {
		t.Fatal(err)
	}
	got, ok := restored.Get("s1")
	if !ok || got.Binding.UpstreamModel != "m1" || !got.Binding.CredentialReady {
		t.Fatalf("restored=%#v", got.Binding)
	}

	// Forged disk model is kept serving with an advisory drift notice: contract
	// drift never drops a live Session, never bricks restore. The daemon starts
	// with every structurally valid route; the restored binding stays
	// authoritative and normalizes stale request payloads (or errors upstream).
	forged := states
	forged[0].Binding.UpstreamModel = "forged"
	clean := NewRouteTable()
	clean.SetLookup(func(string) (string, bool) { return "tok", true })
	notices, err := clean.Restore(forged, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 || notices[0].SessionID != "s1" || strings.TrimSpace(notices[0].Reason) == "" {
		t.Fatalf("forged route must be reported as drift: %#v", notices)
	}
	if got, ok := clean.Get("s1"); !ok || got.Binding.UpstreamModel != "forged" {
		t.Fatalf("forged route must stay live after restore: %#v", got.Binding)
	}

	// A mixed snapshot keeps every route; only the contract-invalid one is
	// reported as drift.
	p2 := codexResponsesProfile("p2", "gpt-5", "m2")
	mix := NewRouteTable()
	mix.SetLookup(func(string) (string, bool) { return "tok", true })
	if _, err := mix.BindLaunch("s2", p2, 1, verifiedAuth(p2)); err != nil {
		t.Fatal(err)
	}
	mixStates := append([]SessionRouteState{}, mix.Snapshot()...)
	mixStates = append(mixStates, forged[0])
	mixed := NewRouteTable()
	mixed.SetLookup(func(string) (string, bool) { return "tok", true })
	notices, err = mixed.Restore(mixStates, registerAllow(registerAllow(nil, p1), p2))
	if err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 || notices[0].SessionID != "s1" {
		t.Fatalf("mixed restore drift=%#v", notices)
	}
	if got, ok := mixed.Get("s1"); !ok || got.Binding.UpstreamModel != "forged" {
		t.Fatalf("forged route must stay live in mixed restore: %#v", got.Binding)
	}
	if got, ok := mixed.Get("s2"); !ok || got.Binding.UpstreamModel != "m2" {
		t.Fatalf("valid route must survive mixed restore: %#v", got.Binding)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "route-bindings.json")
	file, err := NewRouteStateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Save(table); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("perm=%v", info.Mode())
	}
	loaded := NewRouteTable()
	loaded.SetLookup(func(string) (string, bool) { return "tok", true })
	loaded.SetContractVerifier(verifier)
	notices, loadErr := file.Load(loaded)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(notices) != 0 {
		t.Fatalf("notices=%#v", notices)
	}
	if loaded.Len() != 1 {
		t.Fatalf("len=%d", loaded.Len())
	}
}

func TestRouterConcurrentCrossSessionIsolation(t *testing.T) {
	var hits sync.Map
	upstream := newFakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var obj map[string]any
		_ = json.Unmarshal(body, &obj)
		hits.Store(obj["model"], true)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	defer upstream.Close()
	table := NewRouteTable()
	pa := routedCodex(upstream.URL, "gpt-5.6-sol", "gpt-5.6-sol")
	pb := routedCodex(upstream.URL, "gpt-5.5", "gpt-5.5")
	s1, err := table.BindLaunch("a", pa, 1, verifiedAuth(pa))
	if err != nil {
		t.Fatal(err)
	}
	s2, err := table.BindLaunch("b", pb, 1, verifiedAuth(pb))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table)
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	base1, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), s1.Binding.RouteID)
	base2, _ := LoopbackCodexBaseURL(srv.Listener.Addr().String(), s2.Binding.RouteID)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			resp, err := http.Post(base1+"/responses", "application/json", bytes.NewReader([]byte(`{"model":"gpt-5.6-sol"}`)))
			if err == nil {
				resp.Body.Close()
			}
		}()
		go func() {
			defer wg.Done()
			resp, err := http.Post(base2+"/responses", "application/json", bytes.NewReader([]byte(`{"model":"gpt-5.5"}`)))
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()
	if _, ok := hits.Load("gpt-5.6-sol"); !ok {
		t.Fatal("missing gpt-5.6-sol")
	}
	if _, ok := hits.Load("gpt-5.5"); !ok {
		t.Fatal("missing gpt-5.5")
	}
}

func newFakeUpstream(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}
