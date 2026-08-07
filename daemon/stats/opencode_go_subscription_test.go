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

func TestVerifyOpenCodeGoKeyReadOnlyConfirmation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "model list data", status: http.StatusOK, body: `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`},
		{name: "empty model list", status: http.StatusOK, body: `{"object":"list","data":[]}`},
		{name: "models alias", status: http.StatusOK, body: `{"models":["deepseek-v4-flash"]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var method string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method = r.Method
				if r.Header.Get("Authorization") != "Bearer go-secret" {
					t.Error("missing bearer credential")
				}
				if r.Header.Get("Accept") != "application/json" {
					t.Error("missing accept header")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
			usage, err := fetchOpenCodeGoSubscription(context.Background(), server.Client(), server.URL, "https://example.invalid/workspace", openCodeGoAuthMaterial{kind: "official", token: "go-secret"}, nil, now)
			if err != nil {
				t.Fatal(err)
			}
			if method != http.MethodGet {
				t.Fatalf("verification method = %s, want GET", method)
			}
			if usage.AuthKind != "official" || usage.State != "available" || usage.Plan != "go" || usage.FetchedAt != "2026-08-07T12:00:00Z" {
				t.Fatalf("usage = %#v", usage)
			}
			if usage.UsageAvailable || len(usage.Windows) != 0 {
				t.Fatalf("usage windows without dashboard credentials: %#v", usage.Windows)
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

func TestVerifyOpenCodeGoKeyRejectsNegativeAndAmbiguousResponses(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "invalid key", status: http.StatusUnauthorized, body: `{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`},
		{name: "forbidden", status: http.StatusForbidden, body: `{"error":"forbidden"}`},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"error":{"type":"rate_limit_error"}}`},
		{name: "gateway failure", status: http.StatusBadGateway, body: `gateway unavailable`},
		{name: "html login page", status: http.StatusOK, body: `<!doctype html><html>sign in</html>`},
		{name: "missing model list", status: http.StatusOK, body: `{"object":"list"}`},
		{name: "malformed body", status: http.StatusOK, body: `{`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			if _, err := fetchOpenCodeGoSubscription(context.Background(), server.Client(), server.URL, "https://example.invalid/workspace", openCodeGoAuthMaterial{kind: "official", token: "go-secret"}, nil, time.Now()); err == nil {
				t.Fatal("expected response to be rejected")
			}
		})
	}
}

func writeOpenCodeGoDashboardConfig(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadOpenCodeGoDashboardCredentialResolution(t *testing.T) {
	t.Run("environment pair", func(t *testing.T) {
		t.Setenv("OPENCODE_GO_WORKSPACE_ID", "wrk_ENV123")
		t.Setenv("OPENCODE_GO_AUTH_COOKIE", "cookie-env")
		cred := readOpenCodeGoDashboardCredential(t.TempDir())
		if cred == nil || cred.workspaceID != "wrk_ENV123" || cred.authCookie != "cookie-env" || cred.source != "environment" {
			t.Fatalf("credential = %#v", cred)
		}
	})

	t.Run("partial environment ignored", func(t *testing.T) {
		t.Setenv("OPENCODE_GO_WORKSPACE_ID", "wrk_ENV123")
		t.Setenv("OPENCODE_GO_AUTH_COOKIE", "")
		home := t.TempDir()
		writeOpenCodeGoDashboardConfig(t, filepath.Join(home, ".config", "opencode-bar", "opencode-go.json"), `{"workspaceId":"wrk_FILE","authCookie":"cookie-file"}`)
		cred := readOpenCodeGoDashboardCredential(home)
		if cred == nil || cred.workspaceID != "wrk_FILE" {
			t.Fatalf("credential = %#v", cred)
		}
	})

	t.Run("config file override", func(t *testing.T) {
		t.Setenv("OPENCODE_GO_WORKSPACE_ID", "")
		t.Setenv("OPENCODE_GO_AUTH_COOKIE", "")
		override := filepath.Join(t.TempDir(), "custom.json")
		writeOpenCodeGoDashboardConfig(t, override, `{"workspace_id":"wrk_OVR","cookie":"cookie-ovr"}`)
		t.Setenv("OPENCODE_GO_CONFIG_FILE", override)
		cred := readOpenCodeGoDashboardCredential(t.TempDir())
		if cred == nil || cred.workspaceID != "wrk_OVR" || cred.authCookie != "cookie-ovr" || cred.source != override {
			t.Fatalf("credential = %#v", cred)
		}
	})

	t.Run("first standard config path wins", func(t *testing.T) {
		t.Setenv("OPENCODE_GO_WORKSPACE_ID", "")
		t.Setenv("OPENCODE_GO_AUTH_COOKIE", "")
		home := t.TempDir()
		writeOpenCodeGoDashboardConfig(t, filepath.Join(home, ".config", "opencode-bar", "opencode-go.json"), `{"workspaceId":"wrk_FIRST","authCookie":"cookie-first"}`)
		writeOpenCodeGoDashboardConfig(t, filepath.Join(home, ".config", "opencode-quota", "opencode-go.json"), `{"workspaceId":"wrk_SECOND","authCookie":"cookie-second"}`)
		cred := readOpenCodeGoDashboardCredential(home)
		if cred == nil || cred.workspaceID != "wrk_FIRST" {
			t.Fatalf("credential = %#v", cred)
		}
	})

	t.Run("incomplete config ignored", func(t *testing.T) {
		t.Setenv("OPENCODE_GO_WORKSPACE_ID", "")
		t.Setenv("OPENCODE_GO_AUTH_COOKIE", "")
		home := t.TempDir()
		writeOpenCodeGoDashboardConfig(t, filepath.Join(home, ".config", "opencode-bar", "opencode-go.json"), `{"workspaceId":"wrk_ONLY_ID"}`)
		if cred := readOpenCodeGoDashboardCredential(home); cred != nil {
			t.Fatalf("credential = %#v, want nil", cred)
		}
	})

	t.Run("absent", func(t *testing.T) {
		t.Setenv("OPENCODE_GO_WORKSPACE_ID", "")
		t.Setenv("OPENCODE_GO_AUTH_COOKIE", "")
		if cred := readOpenCodeGoDashboardCredential(t.TempDir()); cred != nil {
			t.Fatalf("credential = %#v, want nil", cred)
		}
	})
}

const openCodeGoDashboardNextFixture = `<html><body><script>self.__next_f.push([1,"{\"rollingUsage\":{\"usagePercent\":12.5,\"resetInSec\":3600},\"weeklyUsage\":{\"usagePercent\":\"25\",\"resetInSec\":\"7200\"},\"monthlyUsage\":{\"usagePercent\":50,\"resetInSec\":10800}}"])</script></body></html>`

const openCodeGoDashboardSolidFixture = `<html><body><script>$R[24]($R[18],$R[30]={mine:!0,useBalance:!0,rollingUsage:$R[31]={status:"ok",resetInSec:18000,usagePercent:0},weeklyUsage:$R[32]={status:"ok",resetInSec:162822,usagePercent:31},monthlyUsage:$R[33]={status:"ok",resetInSec:1404782,usagePercent:21}});</script></body></html>`

const openCodeGoDashboardRollingOnlyFixture = `<html><body>{"rollingUsage":{"usagePercent":64,"resetInSec":900}}</body></html>`

func TestParseOpenCodeGoDashboardWindowsFixtures(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	t.Run("next payload json", func(t *testing.T) {
		windows := parseOpenCodeGoDashboardWindows(openCodeGoDashboardNextFixture, now)
		if len(windows) != 3 {
			t.Fatalf("windows = %#v", windows)
		}
		byName := map[string]OpenCodeGoUsageWindow{}
		for _, w := range windows {
			byName[w.Name] = w
		}
		if byName["rolling"].UsedPercent != 12.5 || byName["rolling"].LimitUSD != 12 || byName["rolling"].ResetInSeconds != 3600 {
			t.Fatalf("rolling = %#v", byName["rolling"])
		}
		if byName["weekly"].UsedPercent != 25 || byName["weekly"].LimitUSD != 30 || byName["weekly"].ResetInSeconds != 7200 {
			t.Fatalf("weekly = %#v", byName["weekly"])
		}
		if byName["monthly"].UsedPercent != 50 || byName["monthly"].LimitUSD != 60 || byName["monthly"].ResetInSeconds != 10800 {
			t.Fatalf("monthly = %#v", byName["monthly"])
		}
		if byName["rolling"].ResetsAt != "2026-08-07T13:00:00Z" {
			t.Fatalf("resetsAt = %q", byName["rolling"].ResetsAt)
		}
	})

	t.Run("solid serialized windows", func(t *testing.T) {
		windows := parseOpenCodeGoDashboardWindows(openCodeGoDashboardSolidFixture, now)
		if len(windows) != 3 {
			t.Fatalf("windows = %#v", windows)
		}
		byName := map[string]OpenCodeGoUsageWindow{}
		for _, w := range windows {
			byName[w.Name] = w
		}
		if byName["rolling"].UsedPercent != 0 || byName["rolling"].ResetInSeconds != 18000 {
			t.Fatalf("rolling = %#v", byName["rolling"])
		}
		if byName["weekly"].UsedPercent != 31 || byName["weekly"].ResetInSeconds != 162822 {
			t.Fatalf("weekly = %#v", byName["weekly"])
		}
		if byName["monthly"].UsedPercent != 21 || byName["monthly"].ResetInSeconds != 1404782 {
			t.Fatalf("monthly = %#v", byName["monthly"])
		}
	})

	t.Run("partial windows retained", func(t *testing.T) {
		windows := parseOpenCodeGoDashboardWindows(openCodeGoDashboardRollingOnlyFixture, now)
		if len(windows) != 1 || windows[0].Name != "rolling" || windows[0].UsedPercent != 64 || windows[0].ResetInSeconds != 900 {
			t.Fatalf("windows = %#v", windows)
		}
	})
}

func TestParseOpenCodeGoDashboardWindowsFailClosed(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name string
		page string
	}{
		{name: "login redirect page", page: `<!doctype html><html><head><title>Authorize</title></head><body>Sign in to continue</body></html>`},
		{name: "rate limit page", page: `<!doctype html><html><body>Too many requests</body></html>`},
		{name: "markup drift", page: `<html><body>{"rollingUsage":{"percent":64,"resetAt":900}}</body></html>`},
		{name: "garbage", page: `not html at all`},
		{name: "empty", page: ``},
		{name: "windows without reset", page: `<html>{"rollingUsage":{"usagePercent":64}}</html>`},
		{name: "windows without usage", page: `<html>{"rollingUsage":{"resetInSec":900}}</html>`},
		{name: "negative usage", page: `<html>{"rollingUsage":{"usagePercent":-5,"resetInSec":900}}</html>`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if windows := parseOpenCodeGoDashboardWindows(tt.page, now); len(windows) != 0 {
				t.Fatalf("windows = %#v, want none", windows)
			}
		})
	}
}

func TestFetchOpenCodeGoDashboardWindows(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	t.Run("authenticated page yields windows", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Cookie") != "auth=cookie-value" {
				t.Errorf("cookie header = %q", r.Header.Get("Cookie"))
			}
			if !strings.Contains(r.URL.Path, "wrk_TEST123") {
				t.Errorf("path = %q", r.URL.Path)
			}
			_, _ = w.Write([]byte(openCodeGoDashboardNextFixture))
		}))
		defer server.Close()

		cred := &openCodeGoDashboardCredential{workspaceID: "wrk_TEST123", authCookie: "cookie-value", source: "test"}
		windows := fetchOpenCodeGoDashboardWindows(context.Background(), server.Client(), server.URL, cred, now)
		if len(windows) != 3 {
			t.Fatalf("windows = %#v", windows)
		}
	})

	t.Run("full cookie header passthrough", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Cookie") != "session=x; auth=full" {
				t.Errorf("cookie header = %q", r.Header.Get("Cookie"))
			}
			_, _ = w.Write([]byte(openCodeGoDashboardNextFixture))
		}))
		defer server.Close()

		cred := &openCodeGoDashboardCredential{workspaceID: "wrk_TEST123", authCookie: "session=x; auth=full", source: "test"}
		windows := fetchOpenCodeGoDashboardWindows(context.Background(), server.Client(), server.URL, cred, now)
		if len(windows) != 3 {
			t.Fatalf("windows = %#v", windows)
		}
	})

	t.Run("fail closed on non-2xx and login page", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			status int
			body   string
		}{
			{name: "unauthorized", status: http.StatusUnauthorized, body: `<html>login</html>`},
			{name: "rate limited", status: http.StatusTooManyRequests, body: `<html>too many</html>`},
			{name: "gateway failure", status: http.StatusBadGateway, body: `nope`},
			{name: "login page 200", status: http.StatusOK, body: `<!doctype html><html><title>Authorize</title></html>`},
		} {
			t.Run(tt.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				}))
				defer server.Close()
				cred := &openCodeGoDashboardCredential{workspaceID: "wrk_TEST123", authCookie: "cookie-value", source: "test"}
				if windows := fetchOpenCodeGoDashboardWindows(context.Background(), server.Client(), server.URL, cred, now); len(windows) != 0 {
					t.Fatalf("windows = %#v, want none", windows)
				}
			})
		}
	})

	t.Run("nil credential yields no request", func(t *testing.T) {
		if windows := fetchOpenCodeGoDashboardWindows(context.Background(), failingOpenCodeGoClient{}, "https://example.invalid", nil, now); len(windows) != 0 {
			t.Fatalf("windows = %#v", windows)
		}
	})
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
			collector := &Collector{opencodeGoClient: client, opencodeGoEndpoint: "https://example.invalid/models", opencodeGoTimeout: time.Second, now: time.Now}
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
			c := &Collector{opencodeGoClient: client, opencodeGoEndpoint: "https://example.invalid/models", opencodeGoDashboardEndpoint: "https://example.invalid/workspace", opencodeGoTimeout: time.Second, now: time.Now}
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
	t.Setenv("OPENCODE_GO_WORKSPACE_ID", "")
	t.Setenv("OPENCODE_GO_AUTH_COOKIE", "")
	home := t.TempDir()
	writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"api","key":"go-secret"}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`))
	}))
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoDashboardEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
	got := c.collectOpenCodeGoSubscription(home)
	if got == nil || got.AuthKind != "official" || got.State != "available" || got.Plan != "go" {
		t.Fatalf("subscription = %#v", got)
	}
	if got.UsageAvailable || len(got.Windows) != 0 {
		t.Fatalf("usage must be unavailable without dashboard credentials: %#v", got)
	}
}

func TestCollectOpenCodeGoSubscriptionWithDashboardUsage(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("OPENCODE_GO_WORKSPACE_ID", "wrk_COLLECT")
	t.Setenv("OPENCODE_GO_AUTH_COOKIE", "cookie-collect")
	home := t.TempDir()
	writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"api","key":"go-secret"}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/go") {
			_, _ = w.Write([]byte(openCodeGoDashboardSolidFixture))
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`))
	}))
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoDashboardEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
	got := c.collectOpenCodeGoSubscription(home)
	if got == nil || !got.UsageAvailable || len(got.Windows) != 3 {
		t.Fatalf("subscription = %#v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(encoded)
	for _, secret := range []string{"go-secret", "wrk_COLLECT", "cookie-collect"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("serialized usage exposed secret material %q: %s", secret, serialized)
		}
	}
}

func TestCollectOpenCodeGoSubscriptionDashboardFailClosedKeepsCard(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("OPENCODE_GO_WORKSPACE_ID", "wrk_COLLECT")
	t.Setenv("OPENCODE_GO_AUTH_COOKIE", "cookie-collect")
	home := t.TempDir()
	writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"api","key":"go-secret"}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/go") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!doctype html><html><title>Authorize</title></html>`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`))
	}))
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoDashboardEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
	got := c.collectOpenCodeGoSubscription(home)
	if got == nil || got.AuthKind != "official" {
		t.Fatalf("subscription = %#v", got)
	}
	if got.UsageAvailable || len(got.Windows) != 0 {
		t.Fatalf("usage must fail closed: %#v", got)
	}
}
