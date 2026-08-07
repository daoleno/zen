package stats

import (
	"context"
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

// openCodeGoTestServer serves the models discovery route, the chat challenge
// route, and the optional dashboard route from one httptest server.
func openCodeGoTestServer(t *testing.T, modelsBody string, modelsStatus int, challenge func(w http.ResponseWriter, r *http.Request), dashboard func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			if challenge != nil {
				challenge(w, r)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"invalid_request_error","message":"Empty input messages"}}`))
		case strings.Contains(r.URL.Path, "/go"):
			if dashboard != nil {
				dashboard(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!doctype html><html><title>Authorize</title></html>`))
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(modelsStatus)
			_, _ = w.Write([]byte(modelsBody))
		}
	}))
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

func TestDiscoverOpenCodeGoModels(t *testing.T) {
	auth := openCodeGoAuthMaterial{kind: "official", token: "go-secret"}

	t.Run("model ids from data", func(t *testing.T) {
		server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"minimax-m3"},{"id":"deepseek-v4-flash"}]}`, http.StatusOK, nil, nil)
		defer server.Close()
		models, err := discoverOpenCodeGoModels(context.Background(), server.Client(), server.URL, auth)
		if err != nil || len(models) != 2 || models[0] != "minimax-m3" {
			t.Fatalf("models = %v, err = %v", models, err)
		}
	})

	t.Run("models alias", func(t *testing.T) {
		server := openCodeGoTestServer(t, `{"models":["glm-5.2"]}`, http.StatusOK, nil, nil)
		defer server.Close()
		models, err := discoverOpenCodeGoModels(context.Background(), server.Client(), server.URL, auth)
		if err != nil || len(models) != 1 || models[0] != "glm-5.2" {
			t.Fatalf("models = %v, err = %v", models, err)
		}
	})

	t.Run("empty model list fails closed", func(t *testing.T) {
		server := openCodeGoTestServer(t, `{"object":"list","data":[]}`, http.StatusOK, nil, nil)
		defer server.Close()
		if _, err := discoverOpenCodeGoModels(context.Background(), server.Client(), server.URL, auth); err == nil {
			t.Fatal("expected empty model list to fail closed")
		}
	})

	t.Run("html page fails closed", func(t *testing.T) {
		server := openCodeGoTestServer(t, `<!doctype html><html>login</html>`, http.StatusOK, nil, nil)
		defer server.Close()
		if _, err := discoverOpenCodeGoModels(context.Background(), server.Client(), server.URL, auth); err == nil {
			t.Fatal("expected html to fail closed")
		}
	})

	t.Run("server failure fails closed", func(t *testing.T) {
		server := openCodeGoTestServer(t, `nope`, http.StatusBadGateway, nil, nil)
		defer server.Close()
		if _, err := discoverOpenCodeGoModels(context.Background(), server.Client(), server.URL, auth); err == nil {
			t.Fatal("expected failure to fail closed")
		}
	})
}

func challenge400(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte(body))
}

func TestOpenCodeGoChallengeConfirmsOnlyExactInvalidRequestError(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	auth := openCodeGoAuthMaterial{kind: "official", token: "go-secret"}

	t.Run("exact 400 confirms", func(t *testing.T) {
		var gotBody string
		var gotMethod string
		server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			if r.Header.Get("Authorization") != "Bearer go-secret" {
				t.Error("missing bearer credential")
			}
			gotBody = readRequestBody(t, r)
			challenge400(w, `{"error":{"type":"invalid_request_error","code":"invalid_request_error","message":"Invalid max_tokens value"}}`)
		}, nil)
		defer server.Close()

		usage, err := fetchOpenCodeGoSubscription(context.Background(), server.Client(), server.URL, server.URL, "https://example.invalid/workspace", auth, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		if gotMethod != http.MethodPost {
			t.Fatalf("challenge method = %s, want POST", gotMethod)
		}
		if !strings.Contains(gotBody, `"max_tokens":-1`) || !strings.Contains(gotBody, `"messages":[]`) {
			t.Fatalf("challenge payload must be non-generating: %s", gotBody)
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

	t.Run("skips inconclusive models until exact confirmation", func(t *testing.T) {
		attempts := []string{}
		server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"minimax-m3"},{"id":"deepseek-v4-flash"},{"id":"glm-5.2"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
			body := readRequestBody(t, r)
			var payload struct {
				Model string `json:"model"`
			}
			_ = json.Unmarshal([]byte(body), &payload)
			attempts = append(attempts, payload.Model)
			switch payload.Model {
			case "minimax-m3":
				challenge400(w, `{"error":{"type":"server_error","message":"upstream failed"}}`)
			case "glm-5.2":
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"error":{"param":"max_tokens","type":"invalid_request_error","message":"bad"}}`))
			default:
				challenge400(w, `{"error":{"type":"invalid_request_error","code":"invalid_request_error","message":"Empty input messages"}}`)
			}
		}, nil)
		defer server.Close()

		if _, err := fetchOpenCodeGoSubscription(context.Background(), server.Client(), server.URL, server.URL, "https://example.invalid/workspace", auth, nil, now); err != nil {
			t.Fatal(err)
		}
		if len(attempts) != 2 || attempts[0] != "minimax-m3" || attempts[1] != "deepseek-v4-flash" {
			t.Fatalf("challenge attempts = %v", attempts)
		}
	})

	t.Run("2xx challenge responses are never accepted", func(t *testing.T) {
		server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"minimax-m3"},{"id":"deepseek-v4-flash"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
			body := readRequestBody(t, r)
			var payload struct {
				Model string `json:"model"`
			}
			_ = json.Unmarshal([]byte(body), &payload)
			if payload.Model == "minimax-m3" {
				// A 2xx with a plausible completion body must not be parsed
				// or accepted as confirmation.
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"generated"}}]}`))
				return
			}
			challenge400(w, `{"error":{"type":"invalid_request_error","code":"invalid_request_error","message":"Empty input messages"}}`)
		}, nil)
		defer server.Close()

		if _, err := fetchOpenCodeGoSubscription(context.Background(), server.Client(), server.URL, server.URL, "https://example.invalid/workspace", auth, nil, now); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("all 2xx responses fail closed", func(t *testing.T) {
		server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"minimax-m3"},{"id":"deepseek-v4-flash"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion"}`))
		}, nil)
		defer server.Close()

		if _, err := fetchOpenCodeGoSubscription(context.Background(), server.Client(), server.URL, server.URL, "https://example.invalid/workspace", auth, nil, now); err == nil {
			t.Fatal("expected 2xx-only challenge to fail closed")
		}
	})

	t.Run("401 auth error fails closed", func(t *testing.T) {
		server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`))
		}, nil)
		defer server.Close()

		if _, err := fetchOpenCodeGoSubscription(context.Background(), server.Client(), server.URL, server.URL, "https://example.invalid/workspace", auth, nil, now); err == nil {
			t.Fatal("expected auth failure to fail closed")
		}
	})

	t.Run("forbidden, throttled, server errors fail closed", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			status int
			body   string
		}{
			{name: "forbidden", status: http.StatusForbidden, body: `{"error":"forbidden"}`},
			{name: "rate limited", status: http.StatusTooManyRequests, body: `{"error":{"type":"rate_limit_error"}}`},
			{name: "gateway failure", status: http.StatusBadGateway, body: `gateway unavailable`},
		} {
			t.Run(tt.name, func(t *testing.T) {
				server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				}, nil)
				defer server.Close()
				if _, err := fetchOpenCodeGoSubscription(context.Background(), server.Client(), server.URL, server.URL, "https://example.invalid/workspace", auth, nil, now); err == nil {
					t.Fatal("expected response to fail closed")
				}
			})
		}
	})

	t.Run("html and unknown error shapes fail closed", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			status int
			body   string
		}{
			{name: "html 400", status: http.StatusBadRequest, body: `<!doctype html><html>error</html>`},
			{name: "unexpected error type", status: http.StatusBadRequest, body: `{"error":{"type":"server_error","message":"boom"}}`},
			{name: "missing error code", status: http.StatusBadRequest, body: `{"error":{"type":"invalid_request_error"}}`},
			{name: "malformed body", status: http.StatusBadRequest, body: `{`},
		} {
			t.Run(tt.name, func(t *testing.T) {
				server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				}, nil)
				defer server.Close()
				if _, err := fetchOpenCodeGoSubscription(context.Background(), server.Client(), server.URL, server.URL, "https://example.invalid/workspace", auth, nil, now); err == nil {
					t.Fatal("expected response to fail closed")
				}
			})
		}
	})
}

func readRequestBody(t *testing.T, r *http.Request) string {
	t.Helper()
	var builder strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		builder.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return builder.String()
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
			collector := &Collector{opencodeGoClient: client, opencodeGoEndpoint: "https://example.invalid/models", opencodeGoChatEndpoint: "https://example.invalid/chat", opencodeGoDashboardEndpoint: "https://example.invalid/workspace", opencodeGoTimeout: time.Second, now: time.Now}
			if got := collector.collectOpenCodeGoSubscription(home); got != nil {
				t.Fatalf("subscription = %#v, want nil", got)
			}
			if client.calls != 0 {
				t.Fatalf("opencode go endpoint calls = %d, want 0", client.calls)
			}
		})
	}
}

func TestCollectOpenCodeGoSubscriptionPublicModelsCannotConfirm(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("OPENCODE_GO_WORKSPACE_ID", "")
	t.Setenv("OPENCODE_GO_AUTH_COOKIE", "")
	home := t.TempDir()
	writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"api","key":"bogus-key"}}`)
	server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`))
	}, nil)
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoDashboardEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
	if got := c.collectOpenCodeGoSubscription(home); got != nil {
		t.Fatalf("public models + failed challenge must not confirm: %#v", got)
	}
}

func TestCollectOpenCodeGoSubscriptionRequiresLiveConfirmation(t *testing.T) {
	for _, tt := range []struct {
		name            string
		challengeStatus int
		challengeBody   string
		wantNil         bool
	}{
		{name: "network failure", wantNil: true},
		{name: "unauthorized", challengeStatus: http.StatusUnauthorized, challengeBody: `{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`, wantNil: true},
		{name: "unavailable", challengeStatus: http.StatusServiceUnavailable, challengeBody: `unavailable`, wantNil: true},
		{name: "ambiguous", challengeStatus: http.StatusOK, challengeBody: `{"id":"1","object":"chat.completion"}`, wantNil: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", "")
			t.Setenv("OPENCODE_GO_WORKSPACE_ID", "")
			t.Setenv("OPENCODE_GO_AUTH_COOKIE", "")
			home := t.TempDir()
			writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"api","key":"go-secret"}}`)
			var client openCodeGoHTTPClient
			if tt.challengeStatus == 0 {
				client = failingOpenCodeGoClient{}
			} else {
				server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.challengeStatus)
					_, _ = w.Write([]byte(tt.challengeBody))
				}, nil)
				defer server.Close()
				client = server.Client()
			}
			c := &Collector{opencodeGoClient: client, opencodeGoEndpoint: "https://example.invalid/models", opencodeGoChatEndpoint: "https://example.invalid/chat", opencodeGoDashboardEndpoint: "https://example.invalid/workspace", opencodeGoTimeout: time.Second, now: time.Now}
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
	server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, nil, nil)
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoDashboardEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
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
	server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, nil, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(openCodeGoDashboardSolidFixture))
	})
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoDashboardEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
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

func TestCollectOpenCodeGoSubscriptionDashboardConfirmsWithoutChallenge(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("OPENCODE_GO_WORKSPACE_ID", "wrk_COLLECT")
	t.Setenv("OPENCODE_GO_AUTH_COOKIE", "cookie-collect")
	home := t.TempDir()
	writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"api","key":"go-secret"}}`)
	server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		challenge400(w, `{"error":{"type":"server_error","message":"upstream failed"}}`)
	}, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(openCodeGoDashboardNextFixture))
	})
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoDashboardEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
	got := c.collectOpenCodeGoSubscription(home)
	if got == nil || !got.UsageAvailable || len(got.Windows) != 3 {
		t.Fatalf("dashboard-confirmed subscription = %#v", got)
	}
}

func TestCollectOpenCodeGoSubscriptionDashboardFailClosedKeepsCard(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("OPENCODE_GO_WORKSPACE_ID", "wrk_COLLECT")
	t.Setenv("OPENCODE_GO_AUTH_COOKIE", "cookie-collect")
	home := t.TempDir()
	writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"api","key":"go-secret"}}`)
	server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, nil, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!doctype html><html><title>Authorize</title></html>`))
	})
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoDashboardEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
	got := c.collectOpenCodeGoSubscription(home)
	if got == nil || got.AuthKind != "official" {
		t.Fatalf("subscription = %#v", got)
	}
	if got.UsageAvailable || len(got.Windows) != 0 {
		t.Fatalf("usage must fail closed: %#v", got)
	}
}

func TestCollectOpenCodeGoSubscriptionEmptyModelsFailClosed(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("OPENCODE_GO_WORKSPACE_ID", "")
	t.Setenv("OPENCODE_GO_AUTH_COOKIE", "")
	home := t.TempDir()
	writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"api","key":"go-secret"}}`)
	server := openCodeGoTestServer(t, `{"object":"list","data":[]}`, http.StatusOK, nil, nil)
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoDashboardEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
	if got := c.collectOpenCodeGoSubscription(home); got != nil {
		t.Fatalf("empty models must fail closed: %#v", got)
	}
}

func TestOpenCodeGoChallengeAttemptBounding(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	auth := openCodeGoAuthMaterial{kind: "official", token: "go-secret"}
	attempts := 0
	var modelIDs []string
	server := openCodeGoTestServer(t, fmt.Sprintf(`{"object":"list","data":[{"id":"m%d"},{"id":"m%d"},{"id":"m%d"},{"id":"m%d"},{"id":"m%d"},{"id":"m%d"},{"id":"m%d"},{"id":"m%d"}]}`, 1, 2, 3, 4, 5, 6, 7, 8), http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		body := readRequestBody(t, r)
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal([]byte(body), &payload)
		attempts++
		modelIDs = append(modelIDs, payload.Model)
		challenge400(w, `{"error":{"type":"server_error","message":"upstream failed"}}`)
	}, nil)
	defer server.Close()

	if _, err := fetchOpenCodeGoSubscription(context.Background(), server.Client(), server.URL, server.URL, "https://example.invalid/workspace", auth, nil, now); err == nil {
		t.Fatal("expected challenge to fail closed")
	}
	if attempts != opencodeGoChallengeMaxAttempts || len(modelIDs) != opencodeGoChallengeMaxAttempts {
		t.Fatalf("attempts = %d models = %v, want %d", attempts, modelIDs, opencodeGoChallengeMaxAttempts)
	}
}
