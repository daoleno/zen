package modelprofiles

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// newRetryRouter binds one Claude route to a fake upstream and returns the
// loopback root URL the Claude Code client would use.
func newRetryRouter(t *testing.T, handler http.Handler, opts ...RouterOption) string {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	table := NewRouteTable()
	profile := routedClaude(upstream.URL, "claude-sonnet-4-6", "claude-upstream")
	state, err := table.BindLaunch("claude-retry", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table, opts...)
	srv := httptest.NewServer(router.Handler())
	t.Cleanup(srv.Close)
	root, err := LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func postClaudeMessages(t *testing.T, root string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, root+"/v1/messages", bytes.NewBufferString(`{"model":"claude-sonnet-4-6","max_tokens":1,"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// A transient 5xx is retried and the successful retry is returned to the
// client, mirroring the machine-level Gateway.
func TestRouterRetriesTransientUpstream5xx(t *testing.T) {
	var calls atomic.Int32
	root := newRetryRouter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	resp := postClaudeMessages(t, root)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("status=%d calls=%d, want 200 after one retry", resp.StatusCode, calls.Load())
	}
}

// A 4xx is a real client/upstream answer and must never be retried or its body
// rewritten: the relay's own 400 error text must reach Claude Code unchanged.
func TestRouterDoesNotRetryUpstream4xxAndPreservesBody(t *testing.T) {
	var calls atomic.Int32
	const body = `{"error":{"message":"Upstream request error (request_ori_id: 3b3ae9fe-38cd-4dea-94e2-71ee8469a339)"}}`
	root := newRetryRouter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, body)
	}))
	resp := postClaudeMessages(t, root)
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || calls.Load() != 1 || string(got) != body {
		t.Fatalf("status=%d calls=%d body=%s, want one 400 with the upstream body", resp.StatusCode, calls.Load(), got)
	}
}

// After the bounded attempts the final 5xx status and error body are returned
// intact instead of an empty drained body.
func TestRouterPreservesFinalUpstream5xxBodyAfterBoundedRetry(t *testing.T) {
	var calls atomic.Int32
	const body = `{"error":{"message":"No available accounts: no available accounts","type":"api_error"}}`
	root := newRetryRouter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, body)
	}))
	resp := postClaudeMessages(t, root)
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable || calls.Load() != RouterRetryAttempts || string(got) != body {
		t.Fatalf("status=%d calls=%d body=%s, want final 503 body after %d attempts", resp.StatusCode, calls.Load(), got, RouterRetryAttempts)
	}
}

// A transport error is retried with the buffered request body replayed.
func TestRouterRetriesTransportError(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(`{"ok":true}`)),
		}, nil
	})}
	table := NewRouteTable()
	profile := routedClaude("http://127.0.0.1:1", "claude-sonnet-4-6", "claude-upstream")
	state, err := table.BindLaunch("claude-retry-transport", profile, 1, verifiedAuth(profile))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(table, WithRouterClient(client))
	srv := httptest.NewServer(router.Handler())
	defer srv.Close()
	root, err := LoopbackClaudeRootURL(srv.Listener.Addr().String(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	resp := postClaudeMessages(t, root)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("status=%d calls=%d, want 200 after one transport retry", resp.StatusCode, calls.Load())
	}
}

// Retry backoff stays bounded so a flapping upstream cannot stall a Session.
func TestRouterRetryBackoffIsBounded(t *testing.T) {
	var calls atomic.Int32
	start := time.Now()
	root := newRetryRouter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	resp := postClaudeMessages(t, root)
	_ = resp.Body.Close()
	if elapsed := time.Since(start); elapsed > time.Second || calls.Load() != RouterRetryAttempts {
		t.Fatalf("elapsed=%s calls=%d, want bounded retry", elapsed, calls.Load())
	}
}
