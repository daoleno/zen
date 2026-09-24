package modelprofiles

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestProviderGatewayResolvesResponsesAndAnthropicWithoutTranslation(t *testing.T) {
	var gotPath string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\"}\n\n")
	}))
	defer upstream.Close()

	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore(), WithGatewayRequestResolver(func(protocol, model string) (GatewayUpstream, error) {
		if (protocol == GatewayProtocolResponses && model != "openai/gpt-5") || (protocol == GatewayProtocolAnthropic && model != "claude-3-7-sonnet") {
			t.Fatalf("resolver got protocol=%q model=%q", protocol, model)
		}
		return GatewayUpstream{ProfileID: "openai", BaseURL: upstream.URL, Protocol: ProtocolOpenAIResponses}, nil
	}))
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	payload := []byte(`{"model":"openai/gpt-5","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"tools":[{"type":"function","name":"lookup"}]}`)
	req := httptest.NewRequest(http.MethodPost, "http://"+g.ActualAddr()+"/v1/responses", bytes.NewReader(payload))
	req.RemoteAddr = "127.0.0.1:1234"
	res := httptest.NewRecorder()
	g.ServeHTTP(res, req)
	if res.Code != http.StatusOK || gotPath != "/v1/responses" || !bytes.Equal(gotBody, payload) {
		t.Fatalf("response=%d path=%q body=%q", res.Code, gotPath, gotBody)
	}
	claudePayload := []byte(`{"model":"claude-3-7-sonnet","max_tokens":32,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	claudeReq := httptest.NewRequest(http.MethodPost, "http://"+g.ActualAddr()+"/v1/messages", bytes.NewReader(claudePayload))
	claudeReq.RemoteAddr = "127.0.0.1:1234"
	claudeRes := httptest.NewRecorder()
	g.ServeHTTP(claudeRes, claudeReq)
	if claudeRes.Code != http.StatusOK || gotPath != "/v1/messages" || !bytes.Equal(gotBody, claudePayload) {
		t.Fatalf("anthropic response=%d path=%q body=%q", claudeRes.Code, gotPath, gotBody)
	}
}

func TestProviderGatewayRetriesOnlyTransientUpstreamFailures(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore(), WithGatewayRequestResolver(func(string, string) (GatewayUpstream, error) {
		return GatewayUpstream{ProfileID: "p", BaseURL: upstream.URL, Protocol: ProtocolOpenAIResponses}, nil
	}))
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	req := httptest.NewRequest(http.MethodPost, "http://"+g.ActualAddr()+"/v1/responses", bytes.NewBufferString(`{"model":"gpt-5","input":"x"}`))
	req.RemoteAddr = "127.0.0.1:1234"
	res := httptest.NewRecorder()
	g.ServeHTTP(res, req)
	if res.Code != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("status=%d calls=%d", res.Code, calls.Load())
	}
}

func TestProviderGatewayRejectsNonLoopbackRequests(t *testing.T) {
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore())
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	req := httptest.NewRequest(http.MethodGet, "http://"+g.ActualAddr()+"/v1/models", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	res := httptest.NewRecorder()
	g.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("status=%d", res.Code)
	}
}
