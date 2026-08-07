package stats

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeOpenCodeGoAuthFixture(t *testing.T, home, contents string) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("LOCALAPPDATA", "")
	path := openCodeGoAuthPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadOpenCodeGoAuthClassification(t *testing.T) {
	tests := []struct {
		name      string
		contents  string
		wantKind  string
		wantError bool
	}{
		{name: "absent", wantKind: "absent"},
		{name: "no go entry", contents: `{"opencode":{"type":"api","key":"zen-key"}}`, wantKind: "absent"},
		{name: "malformed", contents: `{`, wantKind: "unknown", wantError: true},
		{name: "oauth type", contents: `{"opencode-go":{"type":"oauth","key":"token"}}`, wantKind: "unknown"},
		{name: "missing type", contents: `{"opencode-go":{"key":"go-key"}}`, wantKind: "unknown"},
		{name: "empty key", contents: `{"opencode-go":{"type":"api","key":""}}`, wantKind: "unknown"},
		{name: "official api key", contents: `{"opencode-go":{"type":"api","key":"go-secret"}}`, wantKind: "official"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.contents != "" {
				writeOpenCodeGoAuthFixture(t, home, tt.contents)
			}
			got, err := readOpenCodeGoAuth(home)
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v, wantError %v", err, tt.wantError)
			}
			if got.kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q", got.kind, tt.wantKind)
			}
			if tt.wantKind == "official" && got.token != "go-secret" {
				t.Fatalf("token = %q, want go-secret", got.token)
			}
		})
	}
}

func TestFetchOpenCodeGoSubscriptionConfirmsAuthenticatedResponses(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "payload validation confirms auth", status: http.StatusBadRequest, body: `{"error":{"type":"invalid_request_error","code":"invalid_request_error","message":"Invalid max_tokens value"}}`},
		{name: "ok completion confirms auth", status: http.StatusOK, body: `{"id":"1","object":"chat.completion","choices":[],"usage":{"total_tokens":0}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer go-secret" {
					t.Error("missing bearer credential")
				}
				if r.Method != http.MethodPost || !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
					t.Errorf("unexpected request method/type: %s %s", r.Method, r.Header.Get("Content-Type"))
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
			usage, err := fetchOpenCodeGoSubscription(context.Background(), server.Client(), server.URL, openCodeGoAuthMaterial{kind: "official", token: "go-secret"}, now)
			if err != nil {
				t.Fatal(err)
			}
			if usage.AuthKind != "official" || usage.State != "available" || usage.Plan != "go" || usage.FetchedAt != "2026-08-07T12:00:00Z" {
				t.Fatalf("usage = %#v", usage)
			}
			encoded, err := json.Marshal(usage)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "go-secret") {
				t.Fatalf("serialized usage exposed credential material: %s", encoded)
			}
		})
	}
}

func TestFetchOpenCodeGoSubscriptionRejectsNegativeAndAmbiguousResponses(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "invalid key", status: http.StatusUnauthorized, body: `{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`},
		{name: "missing key", status: http.StatusUnauthorized, body: `{"type":"error","error":{"type":"AuthError","message":"Missing API key."}}`},
		{name: "model unsupported", status: http.StatusUnauthorized, body: `{"type":"error","error":{"type":"ModelError","message":"Model x is not supported"}}`},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"error":{"type":"rate_limit_error"}}`},
		{name: "gateway failure", status: http.StatusBadGateway, body: `gateway unavailable`},
		{name: "malformed body", status: http.StatusBadRequest, body: `{`},
		{name: "rejection with unknown error type", status: http.StatusBadRequest, body: `{"error":{"type":"server_error"}}`},
		{name: "html proxy response", status: http.StatusOK, body: `<!doctype html><html>login page</html>`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			if _, err := fetchOpenCodeGoSubscription(context.Background(), server.Client(), server.URL, openCodeGoAuthMaterial{kind: "official", token: "go-secret"}, time.Now()); err == nil {
				t.Fatal("expected response to be rejected")
			}
		})
	}
}

type failingOpenCodeGoClient struct{}

func (failingOpenCodeGoClient) Do(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline")
}

type countingOpenCodeGoClient struct {
	calls int
}

func (c *countingOpenCodeGoClient) Do(*http.Request) (*http.Response, error) {
	c.calls++
	return nil, errors.New("unexpected opencode go request")
}

func TestCollectOpenCodeGoSubscriptionOmitsNonOfficialAuthWithoutRequest(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	for _, tt := range []struct {
		name    string
		prepare func(*testing.T, string)
	}{
		{name: "absent"},
		{name: "zen only", prepare: func(t *testing.T, home string) {
			writeOpenCodeGoAuthFixture(t, home, `{"opencode":{"type":"api","key":"zen-key"}}`)
		}},
		{name: "malformed", prepare: func(t *testing.T, home string) {
			writeOpenCodeGoAuthFixture(t, home, `{`)
		}},
		{name: "oauth", prepare: func(t *testing.T, home string) {
			writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"oauth","key":"t"}}`)
		}},
		{name: "unreadable", prepare: func(t *testing.T, home string) {
			path := openCodeGoAuthPath(home)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("auth.json", path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.prepare != nil {
				tt.prepare(t, home)
			}
			client := &countingOpenCodeGoClient{}
			collector := &Collector{opencodeGoClient: client, opencodeGoEndpoint: "https://example.invalid/go", opencodeGoTimeout: time.Second, now: time.Now}
			if got := collector.collectOpenCodeGoSubscription(home); got != nil {
				t.Fatalf("subscription = %#v, want nil", got)
			}
			if client.calls != 0 {
				t.Fatalf("opencode go endpoint calls = %d, want 0", client.calls)
			}
		})
	}
}

func TestCollectOpenCodeGoSubscriptionRequiresLiveConfirmation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantNil bool
	}{
		{name: "network failure", wantNil: true},
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`, wantNil: true},
		{name: "unavailable", status: http.StatusServiceUnavailable, body: `unavailable`, wantNil: true},
		{name: "ambiguous", status: http.StatusOK, body: `not json`, wantNil: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", "")
			home := t.TempDir()
			writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"api","key":"go-secret"}}`)
			var client openCodeGoHTTPClient
			if tt.status == 0 {
				client = failingOpenCodeGoClient{}
			} else {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				}))
				defer server.Close()
				client = server.Client()
			}
			c := &Collector{opencodeGoClient: client, opencodeGoEndpoint: "https://example.invalid/go", opencodeGoTimeout: time.Second, now: time.Now}
			got := c.collectOpenCodeGoSubscription(home)
			if tt.wantNil && got != nil {
				t.Fatalf("subscription = %#v, want nil", got)
			}
			if !tt.wantNil && got == nil {
				t.Fatal("subscription = nil, want projection")
			}
		})
	}
}

func TestCollectOpenCodeGoSubscriptionPositiveConfirmation(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	home := t.TempDir()
	writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"api","key":"go-secret"}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"Invalid max_tokens value"}}`))
	}))
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
	got := c.collectOpenCodeGoSubscription(home)
	if got == nil || got.AuthKind != "official" || got.State != "available" || got.Plan != "go" {
		t.Fatalf("subscription = %#v", got)
	}
}
