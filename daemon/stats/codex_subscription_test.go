package stats

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCodexAuthFixture(t *testing.T, home, contents string) {
	t.Helper()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadCodexAuthClassification(t *testing.T) {
	tests := []struct {
		name      string
		contents  string
		envAPIKey string
		wantKind  string
		wantError bool
	}{
		{name: "absent", wantKind: "absent"},
		{name: "environment api key", envAPIKey: "sk-secret", wantKind: "api_key"},
		{name: "stored api key", contents: `{"auth_mode":"apikey","OPENAI_API_KEY":"sk-secret"}`, wantKind: "api_key"},
		{name: "legacy api key", contents: `{"OPENAI_API_KEY":"sk-secret"}`, wantKind: "api_key"},
		{name: "malformed", contents: `{`, wantKind: "unknown", wantError: true},
		{name: "unsupported mode", contents: `{"auth_mode":"headers"}`, wantKind: "unknown"},
		{name: "chatgpt without token", contents: `{"auth_mode":"chatgpt","tokens":{}}`, wantKind: "unknown"},
		{name: "official", contents: `{"auth_mode":"chatgpt","tokens":{"access_token":"top-secret","account_id":"acct-secret"}}`, wantKind: "official"},
		{name: "legacy official", contents: `{"tokens":{"access_token":"top-secret"}}`, wantKind: "official"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.contents != "" {
				writeCodexAuthFixture(t, home, tt.contents)
			}
			got, err := readCodexAuth(home, tt.envAPIKey)
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v, wantError %v", err, tt.wantError)
			}
			if got.kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q", got.kind, tt.wantKind)
			}
		})
	}
}

func TestFetchCodexSubscriptionNormalizesOfficialUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer top-secret" {
			t.Error("missing bearer credential")
		}
		if r.Header.Get("ChatGPT-Account-Id") != "acct-secret" {
			t.Error("missing account routing header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":25,"limit_window_seconds":18000,"reset_at":1783915200},"secondary_window":{"used_percent":80.5,"limit_window_seconds":604800,"reset_at":1784516400}}}`))
	}))
	defer server.Close()

	now := time.Date(2026, 7, 13, 3, 0, 0, 0, time.UTC)
	usage, err := fetchCodexSubscription(context.Background(), server.Client(), server.URL, codexAuthMaterial{kind: "official", token: "top-secret", accountID: "acct-secret"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Plan != "plus" || usage.FetchedAt != "2026-07-13T03:00:00Z" || len(usage.Windows) != 2 {
		t.Fatalf("usage = %#v", usage)
	}
	if usage.Windows[0].UsedPercent != 25 || usage.Windows[0].WindowMinutes != 300 || usage.Windows[0].ResetsAt == "" {
		t.Fatalf("primary = %#v", usage.Windows[0])
	}
	encoded, err := json.Marshal(usage)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "top-secret") || strings.Contains(string(encoded), "acct-secret") {
		t.Fatalf("serialized usage exposed credential material: %s", encoded)
	}
}

func TestFetchCodexSubscriptionRejectsMalformedAndFailedResponses(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "endpoint failure", status: http.StatusBadGateway, body: `gateway unavailable`},
		{name: "malformed json", status: http.StatusOK, body: `{`},
		{name: "missing rate limit", status: http.StatusOK, body: `{}`},
		{name: "invalid percentage", status: http.StatusOK, body: `{"rate_limit":{"primary_window":{"used_percent":101,"limit_window_seconds":300,"reset_at":1}}}`},
		{name: "missing windows", status: http.StatusOK, body: `{"rate_limit":{}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			if _, err := fetchCodexSubscription(context.Background(), server.Client(), server.URL, codexAuthMaterial{kind: "official", token: "secret"}, time.Now()); err == nil {
				t.Fatal("expected response to be rejected")
			}
		})
	}
}

type failingCodexUsageClient struct{}

func (failingCodexUsageClient) Do(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline")
}

type countingCodexUsageClient struct {
	calls int
}

func (c *countingCodexUsageClient) Do(*http.Request) (*http.Response, error) {
	c.calls++
	return nil, errors.New("unexpected usage request")
}

func TestCollectCodexSubscriptionOmitsNonOfficialAuthWithoutRequest(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	for _, tt := range []struct {
		name    string
		prepare func(*testing.T, string)
	}{
		{name: "absent"},
		{name: "api key", prepare: func(t *testing.T, home string) {
			writeCodexAuthFixture(t, home, `{"auth_mode":"apikey","OPENAI_API_KEY":"sk-secret"}`)
		}},
		{name: "malformed", prepare: func(t *testing.T, home string) {
			writeCodexAuthFixture(t, home, `{`)
		}},
		{name: "unsupported", prepare: func(t *testing.T, home string) {
			writeCodexAuthFixture(t, home, `{"auth_mode":"headers"}`)
		}},
		{name: "unreadable", prepare: func(t *testing.T, home string) {
			dir := filepath.Join(home, ".codex")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("auth.json", filepath.Join(dir, "auth.json")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.prepare != nil {
				tt.prepare(t, home)
			}
			client := &countingCodexUsageClient{}
			collector := &Collector{codexUsageClient: client, codexUsageEndpoint: "https://example.invalid/usage", codexUsageTimeout: time.Second, now: time.Now}
			if got := collector.collectCodexSubscription(home); got != nil {
				t.Fatalf("subscription = %#v, want nil", got)
			}
			if client.calls != 0 {
				t.Fatalf("usage endpoint calls = %d, want 0", client.calls)
			}
		})
	}
}

func TestCollectCodexSubscriptionKeepsOfficialFailureEligible(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	home := t.TempDir()
	writeCodexAuthFixture(t, home, `{"auth_mode":"chatgpt","tokens":{"access_token":"secret"}}`)
	c := &Collector{codexUsageClient: failingCodexUsageClient{}, codexUsageEndpoint: "https://example.invalid/usage", codexUsageTimeout: time.Second, now: time.Now}
	got := c.collectCodexSubscription(home)
	if got == nil || got.AuthKind != "official" || got.State != "unavailable" {
		t.Fatalf("official unavailable subscription = %#v", got)
	}
}

func TestCollectCodexSubscriptionKeepsSuccessfulCacheOnFailure(t *testing.T) {
	home := t.TempDir()
	writeCodexAuthFixture(t, home, `{"tokens":{"access_token":"secret"}}`)
	c := &Collector{
		codexUsageClient:   failingCodexUsageClient{},
		codexUsageEndpoint: "https://example.invalid/usage",
		codexUsageTimeout:  time.Second,
		now:                time.Now,
		lastCodexSubscription: &CodexSubscriptionUsage{
			AuthKind: "official", State: "available", Plan: "pro",
			Windows: []CodexUsageWindow{{Name: "primary", UsedPercent: 20}},
		},
	}
	c.lastCodexAuthFingerprint = fmt.Sprintf("%x", sha256.Sum256([]byte("secret\x00")))
	got := c.collectCodexSubscription(home)
	if !got.Stale || got.State != "available" || len(got.Windows) != 1 {
		t.Fatalf("stale usage = %#v", got)
	}
}

func TestCollectCodexSubscriptionDoesNotReuseCacheAcrossCredentials(t *testing.T) {
	home := t.TempDir()
	writeCodexAuthFixture(t, home, `{"tokens":{"access_token":"new-secret"}}`)
	c := &Collector{
		codexUsageClient: failingCodexUsageClient{}, codexUsageEndpoint: "https://example.invalid/usage",
		codexUsageTimeout: time.Second, now: time.Now,
		lastCodexSubscription:    &CodexSubscriptionUsage{AuthKind: "official", State: "available", Plan: "pro"},
		lastCodexAuthFingerprint: fmt.Sprintf("%x", sha256.Sum256([]byte("old-secret\x00"))),
	}
	got := c.collectCodexSubscription(home)
	if got.State != "unavailable" || got.Stale {
		t.Fatalf("cross-account cache was reused: %#v", got)
	}
}
