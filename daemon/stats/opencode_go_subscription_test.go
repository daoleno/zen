package stats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
// route, and the optional server-function route from one httptest server.
// The server-function route is detected by the X-Server-Id header, which the
// challenge and models requests never carry.
func openCodeGoTestServer(t *testing.T, modelsBody string, modelsStatus int, challenge func(w http.ResponseWriter, r *http.Request), serverFn func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Header.Get("X-Server-Id") != "":
			if serverFn != nil {
				serverFn(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"rollingUsage":{"usagePercent":12.5,"resetInSec":3600},"weeklyUsage":{"usagePercent":25,"resetInSec":7200},"monthlyUsage":{"usagePercent":50,"resetInSec":10800}}`))
		case r.Method == http.MethodPost:
			if challenge != nil {
				challenge(w, r)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"invalid_request_error","message":"Empty input messages"}}`))
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

func TestNormalizeOpenCodeGoWorkspaceID(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want string
	}{
		{raw: "wrk_01ABCDEF0123456789", want: "wrk_01ABCDEF0123456789"},
		{raw: "  wrk_01ABCDEF0123456789  ", want: "wrk_01ABCDEF0123456789"},
		{raw: "https://opencode.ai/workspace/wrk_01ABCDEF0123456789/billing", want: "wrk_01ABCDEF0123456789"},
		{raw: "https://opencode.ai/workspace/wrk_01ABCDEF0123456789", want: "wrk_01ABCDEF0123456789"},
		{raw: "visit https://opencode.ai/workspace/wrk_01ABCDEF0123456789/go", want: "wrk_01ABCDEF0123456789"},
		{raw: "", want: ""},
		{raw: "not-a-workspace", want: ""},
		{raw: "wrk_", want: ""},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			if got := normalizeOpenCodeGoWorkspaceID(tt.raw); got != tt.want {
				t.Fatalf("normalize(%q) = %q, want %q", tt.raw, got, tt.want)
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

		usage, err := fetchOpenCodeGoSubscriptionViaAPI(context.Background(), server.Client(), server.URL, server.URL, auth, now)
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

		if _, err := fetchOpenCodeGoSubscriptionViaAPI(context.Background(), server.Client(), server.URL, server.URL, auth, now); err != nil {
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

		if _, err := fetchOpenCodeGoSubscriptionViaAPI(context.Background(), server.Client(), server.URL, server.URL, auth, now); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("all 2xx responses fail closed", func(t *testing.T) {
		server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"minimax-m3"},{"id":"deepseek-v4-flash"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion"}`))
		}, nil)
		defer server.Close()

		if _, err := fetchOpenCodeGoSubscriptionViaAPI(context.Background(), server.Client(), server.URL, server.URL, auth, now); err == nil {
			t.Fatal("expected 2xx-only challenge to fail closed")
		}
	})

	t.Run("401 auth error fails closed", func(t *testing.T) {
		server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`))
		}, nil)
		defer server.Close()

		if _, err := fetchOpenCodeGoSubscriptionViaAPI(context.Background(), server.Client(), server.URL, server.URL, auth, now); err == nil {
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
				if _, err := fetchOpenCodeGoSubscriptionViaAPI(context.Background(), server.Client(), server.URL, server.URL, auth, now); err == nil {
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
				if _, err := fetchOpenCodeGoSubscriptionViaAPI(context.Background(), server.Client(), server.URL, server.URL, auth, now); err == nil {
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

	t.Run("environment workspace url normalized", func(t *testing.T) {
		t.Setenv("OPENCODE_GO_WORKSPACE_ID", "https://opencode.ai/workspace/wrk_ENV123/billing")
		t.Setenv("OPENCODE_GO_AUTH_COOKIE", "cookie-env")
		cred := readOpenCodeGoDashboardCredential(t.TempDir())
		if cred == nil || cred.workspaceID != "wrk_ENV123" {
			t.Fatalf("credential = %#v", cred)
		}
	})

	t.Run("invalid environment workspace ignored", func(t *testing.T) {
		t.Setenv("OPENCODE_GO_WORKSPACE_ID", "not-a-workspace")
		t.Setenv("OPENCODE_GO_AUTH_COOKIE", "cookie-env")
		home := t.TempDir()
		writeOpenCodeGoDashboardConfig(t, filepath.Join(home, ".config", "opencode-bar", "opencode-go.json"), `{"workspaceId":"wrk_FILE","authCookie":"cookie-file"}`)
		cred := readOpenCodeGoDashboardCredential(home)
		if cred == nil || cred.workspaceID != "wrk_FILE" {
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

const openCodeGoServerPlainFixture = `{"rollingUsage":{"usagePercent":12.5,"resetInSec":3600},"weeklyUsage":{"usagePercent":"25","resetInSec":"7200"},"monthlyUsage":{"usagePercent":50,"resetInSec":10800}}`

const openCodeGoServerWrappedFixture = `{"data":{"rollingUsage":{"usagePercent":0.25,"resetInSec":18000},"weeklyUsage":{"usagePercent":31,"resetInSec":162822},"monthlyUsage":{"usagePercent":21,"resetInSec":1404782}}}`

const openCodeGoServerSolidFixture = `$R[24]($R[18],$R[30]={mine:!0,useBalance:!0,rollingUsage:$R[31]={status:"ok",resetInSec:18000,usagePercent:0},weeklyUsage:$R[32]={status:"ok",resetInSec:162822,usagePercent:31},monthlyUsage:$R[33]={status:"ok",resetInSec:1404782,usagePercent:21}});`

const openCodeGoServerNoMonthlyFixture = `{"rollingUsage":{"usagePercent":64,"resetInSec":900},"weeklyUsage":{"usagePercent":10,"resetInSec":604800}}`

const openCodeGoServerRollingOnlyFixture = `{"rollingUsage":{"usagePercent":40,"resetInSec":3600}}`

func TestParseOpenCodeGoServerUsageFixtures(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	t.Run("plain json with all windows", func(t *testing.T) {
		windows := parseOpenCodeGoServerUsage(openCodeGoServerPlainFixture, now)
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

	t.Run("wrapper object with fraction percent normalized", func(t *testing.T) {
		windows := parseOpenCodeGoServerUsage(openCodeGoServerWrappedFixture, now)
		if len(windows) != 3 {
			t.Fatalf("windows = %#v", windows)
		}
		if windows[0].Name != "rolling" || windows[0].UsedPercent != 25 {
			t.Fatalf("windows = %#v", windows)
		}
	})

	t.Run("serialized solid expression", func(t *testing.T) {
		windows := parseOpenCodeGoServerUsage(openCodeGoServerSolidFixture, now)
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

	t.Run("monthly omitted when absent", func(t *testing.T) {
		windows := parseOpenCodeGoServerUsage(openCodeGoServerNoMonthlyFixture, now)
		if len(windows) != 2 {
			t.Fatalf("windows = %#v", windows)
		}
		if windows[0].Name != "rolling" || windows[1].Name != "weekly" {
			t.Fatalf("windows = %#v", windows)
		}
	})

	t.Run("weekly omitted when absent", func(t *testing.T) {
		windows := parseOpenCodeGoServerUsage(openCodeGoServerRollingOnlyFixture, now)
		if len(windows) != 1 || windows[0].Name != "rolling" {
			t.Fatalf("windows = %#v", windows)
		}
	})
}

func TestParseOpenCodeGoServerUsageFailClosed(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name string
		text string
	}{
		{name: "signed out login page", text: `<!doctype html><html><body>Please log in to continue</body></html>`},
		{name: "sign in page", text: `<html>Sign in with your account</html>`},
		{name: "null payload", text: `null`},
		{name: "json null payload", text: `{"data":null}`},
		{name: "empty", text: ``},
		{name: "garbage", text: `not json at all`},
		{name: "html page", text: `<!doctype html><html><head><title>OpenCode</title></head><body>dashboard</body></html>`},
		{name: "missing rolling", text: `{"weeklyUsage":{"usagePercent":25,"resetInSec":7200}}`},
		{name: "rolling without percent", text: `{"rollingUsage":{"resetInSec":900}}`},
		{name: "rolling without reset", text: `{"rollingUsage":{"usagePercent":40}}`},
		{name: "negative percent", text: `{"rollingUsage":{"usagePercent":-5,"resetInSec":900}}`},
		{name: "non-object json", text: `[1,2,3]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if windows := parseOpenCodeGoServerUsage(tt.text, now); len(windows) != 0 {
				t.Fatalf("windows = %#v, want none", windows)
			}
		})
	}
}

func TestFetchOpenCodeGoServerUsage(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	cred := &openCodeGoDashboardCredential{workspaceID: "wrk_TEST123", authCookie: "cookie-value", source: "test"}

	t.Run("get success with required headers and url encoding", func(t *testing.T) {
		var gotInstance string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s, want GET", r.Method)
			}
			if r.Header.Get("Cookie") != "auth=cookie-value" {
				t.Errorf("cookie header = %q", r.Header.Get("Cookie"))
			}
			if r.Header.Get("X-Server-Id") != opencodeGoSubscriptionServerID {
				t.Errorf("x-server-id = %q", r.Header.Get("X-Server-Id"))
			}
			if gotInstance != "" && r.Header.Get("X-Server-Instance") == gotInstance {
				t.Error("x-server-instance must be unique per request")
			}
			gotInstance = r.Header.Get("X-Server-Instance")
			if !strings.HasPrefix(gotInstance, "server-fn:") {
				t.Errorf("x-server-instance = %q", gotInstance)
			}
			if r.Header.Get("Origin") != "https://opencode.ai" {
				t.Errorf("origin = %q", r.Header.Get("Origin"))
			}
			if r.Header.Get("Referer") != "https://opencode.ai/workspace/wrk_TEST123/billing" {
				t.Errorf("referer = %q", r.Header.Get("Referer"))
			}
			if !strings.Contains(r.Header.Get("Accept"), "text/javascript") {
				t.Errorf("accept = %q", r.Header.Get("Accept"))
			}
			query := r.URL.Query()
			if query.Get("id") != opencodeGoSubscriptionServerID {
				t.Errorf("query id = %q", query.Get("id"))
			}
			if query.Get("args") != `["wrk_TEST123"]` {
				t.Errorf("query args raw = %q", query.Get("args"))
			}
			raw := r.URL.RawQuery
			if !strings.Contains(raw, "args=%5B%22wrk_TEST123%22%5D") {
				t.Errorf("args must be url-encoded json, got %q", raw)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openCodeGoServerPlainFixture))
		}))
		defer server.Close()

		windows, status := fetchOpenCodeGoServerUsage(context.Background(), server.Client(), server.URL, cred, now)
		if status != openCodeGoServerUsageOK || len(windows) != 3 {
			t.Fatalf("windows = %#v", windows)
		}
	})

	t.Run("post fallback when get has no usage", func(t *testing.T) {
		var gotInstance string
		var postBody string
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			if requests == 1 {
				if r.Method != http.MethodGet {
					t.Fatalf("first request method = %s, want GET", r.Method)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"result":{}}`))
				return
			}
			if r.Method != http.MethodPost {
				t.Fatalf("second request method = %s, want POST", r.Method)
			}
			if r.Header.Get("Cookie") != "auth=cookie-value" {
				t.Errorf("cookie header = %q", r.Header.Get("Cookie"))
			}
			if r.Header.Get("X-Server-Id") != opencodeGoSubscriptionServerID {
				t.Errorf("x-server-id = %q", r.Header.Get("X-Server-Id"))
			}
			if r.Header.Get("X-Server-Instance") == gotInstance {
				t.Error("post must carry a fresh x-server-instance")
			}
			gotInstance = r.Header.Get("X-Server-Instance")
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
			}
			postBody = readRequestBody(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openCodeGoServerNoMonthlyFixture))
		}))
		defer server.Close()

		windows, status := fetchOpenCodeGoServerUsage(context.Background(), server.Client(), server.URL, cred, now)
		if status != openCodeGoServerUsageOK || len(windows) != 2 {
			t.Fatalf("windows = %#v", windows)
		}
		if postBody != `["wrk_TEST123"]` {
			t.Fatalf("post body = %q", postBody)
		}
		if gotInstance == "" {
			t.Fatal("post must carry x-server-instance")
		}
	})

	t.Run("fail closed on negative responses", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			status int
			body   string
		}{
			{name: "unauthorized", status: http.StatusUnauthorized, body: `<html>login</html>`},
			{name: "forbidden", status: http.StatusForbidden, body: `forbidden`},
			{name: "rate limited", status: http.StatusTooManyRequests, body: `<html>too many</html>`},
			{name: "server error", status: http.StatusBadGateway, body: `nope`},
			{name: "signed out text 200", status: http.StatusOK, body: `<html>Sign in with your account</html>`},
			{name: "null payload", status: http.StatusOK, body: `null`},
			{name: "html page", status: http.StatusOK, body: `<!doctype html><html><title>OpenCode</title></html>`},
		} {
			t.Run(tt.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				}))
				defer server.Close()
				if windows, status := fetchOpenCodeGoServerUsage(context.Background(), server.Client(), server.URL, cred, now); status != openCodeGoServerFailed || len(windows) != 0 {
					t.Fatalf("windows = %#v, want none", windows)
				}
			})
		}
	})

	t.Run("nil credential yields no request", func(t *testing.T) {
		if windows, status := fetchOpenCodeGoServerUsage(context.Background(), failingOpenCodeGoClient{}, "https://example.invalid/_server", nil, now); status != openCodeGoServerFailed || len(windows) != 0 {
			t.Fatalf("windows = %#v", windows)
		}
	})
}

func TestOpenCodeGoServerRequestURLEncoding(t *testing.T) {
	raw, err := openCodeGoServerRequestURL("https://opencode.ai/_server", opencodeGoSubscriptionServerID, []string{"wrk_TEST123"}, http.MethodGet)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("id") != opencodeGoSubscriptionServerID {
		t.Fatalf("id = %q", parsed.Query().Get("id"))
	}
	if parsed.Query().Get("args") != `["wrk_TEST123"]` {
		t.Fatalf("args = %q", parsed.Query().Get("args"))
	}

	postURL, err := openCodeGoServerRequestURL("https://opencode.ai/_server", opencodeGoSubscriptionServerID, []string{"wrk_TEST123"}, http.MethodPost)
	if err != nil {
		t.Fatal(err)
	}
	if postURL != "https://opencode.ai/_server" {
		t.Fatalf("post url = %q", postURL)
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
			collector := &Collector{opencodeGoClient: client, opencodeGoEndpoint: "https://example.invalid/models", opencodeGoChatEndpoint: "https://example.invalid/chat", opencodeGoServerEndpoint: "https://example.invalid/_server", opencodeGoTimeout: time.Second, now: time.Now}
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

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoServerEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
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
			c := &Collector{opencodeGoClient: client, opencodeGoEndpoint: "https://example.invalid/models", opencodeGoChatEndpoint: "https://example.invalid/chat", opencodeGoServerEndpoint: "https://example.invalid/_server", opencodeGoTimeout: time.Second, now: time.Now}
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

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoServerEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
	got := c.collectOpenCodeGoSubscription(home)
	if got == nil || got.AuthKind != "official" || got.State != "available" || got.Plan != "go" {
		t.Fatalf("subscription = %#v", got)
	}
	if got.UsageAvailable || len(got.Windows) != 0 {
		t.Fatalf("usage must be unavailable without dashboard credentials: %#v", got)
	}
}

func TestCollectOpenCodeGoSubscriptionWithServerUsage(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("OPENCODE_GO_WORKSPACE_ID", "wrk_COLLECT")
	t.Setenv("OPENCODE_GO_AUTH_COOKIE", "cookie-collect")
	home := t.TempDir()
	writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"api","key":"go-secret"}}`)
	server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openCodeGoServerSolidFixture))
	})
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoServerEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
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

func TestCollectOpenCodeGoSubscriptionServerConfirmsWithoutChallenge(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("OPENCODE_GO_WORKSPACE_ID", "wrk_COLLECT")
	t.Setenv("OPENCODE_GO_AUTH_COOKIE", "cookie-collect")
	home := t.TempDir()
	writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"api","key":"go-secret"}}`)
	server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		challenge400(w, `{"error":{"type":"server_error","message":"upstream failed"}}`)
	}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openCodeGoServerPlainFixture))
	})
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoServerEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
	got := c.collectOpenCodeGoSubscription(home)
	if got == nil || !got.UsageAvailable || len(got.Windows) != 3 {
		t.Fatalf("server-confirmed subscription = %#v", got)
	}
}

func TestCollectOpenCodeGoSubscriptionServerFailClosedKeepsCard(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("OPENCODE_GO_WORKSPACE_ID", "wrk_COLLECT")
	t.Setenv("OPENCODE_GO_AUTH_COOKIE", "cookie-collect")
	home := t.TempDir()
	writeOpenCodeGoAuthFixture(t, home, `{"opencode-go":{"type":"api","key":"go-secret"}}`)
	server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, nil, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!doctype html><html><title>Sign in</title></html>`))
	})
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoServerEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
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

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoServerEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
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

	if _, err := fetchOpenCodeGoSubscriptionViaAPI(context.Background(), server.Client(), server.URL, server.URL, auth, now); err == nil {
		t.Fatal("expected challenge to fail closed")
	}
	if attempts != opencodeGoChallengeMaxAttempts || len(modelIDs) != opencodeGoChallengeMaxAttempts {
		t.Fatalf("attempts = %d models = %v, want %d", attempts, modelIDs, opencodeGoChallengeMaxAttempts)
	}
}

func TestCollectOpenCodeGoDashboardConfirmsWithoutAuthEntry(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("OPENCODE_GO_WORKSPACE_ID", "wrk_COLLECT")
	t.Setenv("OPENCODE_GO_AUTH_COOKIE", "cookie-collect")

	for _, tt := range []struct {
		name     string
		authFile string // "" = absent, otherwise contents
	}{
		{name: "absent auth file"},
		{name: "malformed auth file", authFile: `{`},
		{name: "zen only auth", authFile: `{"opencode":{"type":"api","key":"zen-key"}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.authFile != "" {
				writeOpenCodeGoAuthFixture(t, home, tt.authFile)
			}
			var modelsCalls int
			server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, nil, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(openCodeGoServerPlainFixture))
			})
			defer server.Close()
			// models route is never reached when dashboard evidence succeeds
			server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Server-Id") != "" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(openCodeGoServerPlainFixture))
					return
				}
				modelsCalls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`boom`))
			})

			c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoServerEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
			got := c.collectOpenCodeGoSubscription(home)
			if got == nil || !got.UsageAvailable || len(got.Windows) != 3 {
				t.Fatalf("dashboard must confirm without auth entry: %#v", got)
			}
			if modelsCalls != 0 {
				t.Fatalf("models endpoint called %d times despite dashboard evidence", modelsCalls)
			}
		})
	}
}

func TestCollectOpenCodeGoDashboardFailsWithoutAuthNoCard(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("OPENCODE_GO_WORKSPACE_ID", "wrk_COLLECT")
	t.Setenv("OPENCODE_GO_AUTH_COOKIE", "cookie-collect")
	home := t.TempDir()
	server := openCodeGoTestServer(t, `{"object":"list","data":[{"id":"deepseek-v4-flash"}]}`, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"AuthError","message":"Invalid API key."}}`))
	}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`<html>login</html>`))
	})
	defer server.Close()

	c := &Collector{opencodeGoClient: server.Client(), opencodeGoEndpoint: server.URL, opencodeGoChatEndpoint: server.URL, opencodeGoServerEndpoint: server.URL, opencodeGoTimeout: time.Second, now: time.Now}
	if got := c.collectOpenCodeGoSubscription(home); got != nil {
		t.Fatalf("dashboard failure without auth must not confirm: %#v", got)
	}
}

func TestFetchOpenCodeGoServerUsagePostBoundary(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	cred := &openCodeGoDashboardCredential{workspaceID: "wrk_TEST123", authCookie: "cookie-value", source: "test"}

	t.Run("post fallback only after valid 200 without usage", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			getStatus   int
			getBody     string
			getType     string
			postBody    string
			wantCalls   int
			wantStatus  openCodeGoServerStatus
			wantWindows int
		}{
			{name: "get 200 no usage then post success", getStatus: 200, getBody: `{"result":{}}`, getType: "application/json", postBody: openCodeGoServerNoMonthlyFixture, wantCalls: 2, wantStatus: openCodeGoServerUsageOK, wantWindows: 2},
			{name: "get 200 no usage then post no usage", getStatus: 200, getBody: `{}`, getType: "application/json", postBody: `{}`, wantCalls: 2, wantStatus: openCodeGoServerNoUsage},
			{name: "get 401 no post", getStatus: 401, getBody: `<html>login</html>`, getType: "text/html", wantCalls: 1, wantStatus: openCodeGoServerFailed},
			{name: "get 403 no post", getStatus: 403, getBody: `forbidden`, getType: "text/plain", wantCalls: 1, wantStatus: openCodeGoServerFailed},
			{name: "get 429 no post", getStatus: 429, getBody: `<html>too many</html>`, getType: "text/html", wantCalls: 1, wantStatus: openCodeGoServerFailed},
			{name: "get 500 no post", getStatus: 500, getBody: `boom`, getType: "text/plain", wantCalls: 1, wantStatus: openCodeGoServerFailed},
			{name: "get signed out no post", getStatus: 200, getBody: `<html>Sign in with your account</html>`, getType: "text/html", wantCalls: 1, wantStatus: openCodeGoServerFailed},
			{name: "get null no post", getStatus: 200, getBody: `null`, getType: "application/json", wantCalls: 1, wantStatus: openCodeGoServerFailed},
			{name: "get wrapper null no post", getStatus: 200, getBody: `{"data":null}`, getType: "application/json", wantCalls: 1, wantStatus: openCodeGoServerFailed},
			{name: "get html with embedded usage no post", getStatus: 200, getBody: `<html><script>{"rollingUsage":{"usagePercent":12.5,"resetInSec":3600},"weeklyUsage":{"usagePercent":25,"resetInSec":7200}}</script></html>`, getType: "text/html", wantCalls: 1, wantStatus: openCodeGoServerFailed},
			{name: "get missing content type no post", getStatus: 200, getBody: openCodeGoServerPlainFixture, getType: "", wantCalls: 1, wantStatus: openCodeGoServerFailed},
			{name: "get octet stream no post", getStatus: 200, getBody: openCodeGoServerPlainFixture, getType: "application/octet-stream", wantCalls: 1, wantStatus: openCodeGoServerFailed},
		} {
			t.Run(tt.name, func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if tt.getType != "" {
						w.Header().Set("Content-Type", tt.getType)
					}
					if calls == 1 {
						w.WriteHeader(tt.getStatus)
						_, _ = w.Write([]byte(tt.getBody))
						return
					}
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(tt.postBody))
				}))
				defer server.Close()

				windows, status := fetchOpenCodeGoServerUsage(context.Background(), server.Client(), server.URL, cred, now)
				if status != tt.wantStatus {
					t.Fatalf("status = %v, want %v (windows %#v)", status, tt.wantStatus, windows)
				}
				if len(windows) != tt.wantWindows {
					t.Fatalf("windows = %#v, want %d", windows, tt.wantWindows)
				}
				if calls != tt.wantCalls {
					t.Fatalf("calls = %d, want %d", calls, tt.wantCalls)
				}
			})
		}
	})

	t.Run("text/javascript content type accepted", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(openCodeGoServerSolidFixture))
		}))
		defer server.Close()
		windows, status := fetchOpenCodeGoServerUsage(context.Background(), server.Client(), server.URL, cred, now)
		if status != openCodeGoServerUsageOK || len(windows) != 3 || calls != 1 {
			t.Fatalf("status = %v windows = %#v calls = %d", status, windows, calls)
		}
	})

	t.Run("application/json with charset accepted", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(openCodeGoServerPlainFixture))
		}))
		defer server.Close()
		windows, status := fetchOpenCodeGoServerUsage(context.Background(), server.Client(), server.URL, cred, now)
		if status != openCodeGoServerUsageOK || len(windows) != 3 {
			t.Fatalf("status = %v windows = %#v", status, windows)
		}
	})
}
