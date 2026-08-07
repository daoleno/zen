package stats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// opencodeGoModelsEndpoint is the official OpenCode Go models endpoint. It
	// is public (it returns the same payload for valid and invalid keys) and
	// is used only to discover the currently served Go models, never as
	// subscription evidence.
	opencodeGoModelsEndpoint = "https://opencode.ai/zen/go/v1/models"
	// opencodeGoChatEndpoint receives the non-generating invalid-request auth
	// challenge that positively confirms the Go subscription.
	opencodeGoChatEndpoint     = "https://opencode.ai/zen/go/v1/chat/completions"
	opencodeGoDashboardBaseURL = "https://opencode.ai/workspace"
	maxOpenCodeGoAuthBytes     = 2 << 20
	maxOpenCodeGoBodyBytes     = 8 << 20
	// opencodeGoChallengeMaxAttempts bounds how many discovered models are
	// probed before the challenge fails closed.
	opencodeGoChallengeMaxAttempts = 4
)

// openCodeGoWindowSpecs mirror the official Go dashboard usage windows and the
// limits published in the OpenCode Go documentation: $12 per 5 hours, $30 per
// week, $60 per month. The limits are plan facts from the docs; current used
// percentages come only from the authenticated dashboard page.
type openCodeGoWindowSpec struct {
	field    string
	name     string
	limitUSD float64
}

var openCodeGoWindowSpecs = []openCodeGoWindowSpec{
	{field: "rollingUsage", name: "rolling", limitUSD: 12},
	{field: "weeklyUsage", name: "weekly", limitUSD: 30},
	{field: "monthlyUsage", name: "monthly", limitUSD: 60},
}

var openCodeGoWindowObjectRes = func() []*regexp.Regexp {
	res := make([]*regexp.Regexp, 0, len(openCodeGoWindowSpecs))
	for _, spec := range openCodeGoWindowSpecs {
		res = append(res, regexp.MustCompile(`["']?`+regexp.QuoteMeta(spec.field)+`["']?\s*:\s*(?:\$R\[\d+\]\s*=\s*)?\{([^{}]*)\}`))
	}
	return res
}()

type openCodeGoHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type openCodeGoAuthMaterial struct {
	kind  string
	token string
}

// openCodeGoAuthFile mirrors the "opencode-go" entry the official CLI writes
// to $XDG_DATA_HOME/opencode/auth.json when connecting with an API key.
type openCodeGoAuthFile struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

// openCodeGoDashboardCredential addresses the OpenCode dashboard usage page.
// The values are sensitive and are never serialized, logged, or cached; only
// source is used in logs.
type openCodeGoDashboardCredential struct {
	workspaceID string
	authCookie  string
	source      string
}

// openCodeGoAuthPath resolves the auth file location the official CLI uses
// (xdg-basedir semantics): $XDG_DATA_HOME/opencode on Linux, the Library
// Application Support dir on macOS, and LOCALAPPDATA on Windows.
func openCodeGoAuthPath(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "opencode", "auth.json")
	case "windows":
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			return filepath.Join(local, "opencode", "auth.json")
		}
		return filepath.Join(home, "AppData", "Local", "opencode", "auth.json")
	default:
		if data := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); data != "" {
			return filepath.Join(data, "opencode", "auth.json")
		}
		return filepath.Join(home, ".local", "share", "opencode", "auth.json")
	}
}

func readOpenCodeGoAuth(home string) (openCodeGoAuthMaterial, error) {
	path := openCodeGoAuthPath(home)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return openCodeGoAuthMaterial{kind: "absent"}, nil
	}
	if err != nil {
		return openCodeGoAuthMaterial{kind: "unknown"}, fmt.Errorf("open opencode auth file: %w", err)
	}
	defer f.Close()

	var entries map[string]openCodeGoAuthFile
	decoder := json.NewDecoder(io.LimitReader(f, maxOpenCodeGoAuthBytes))
	if err := decoder.Decode(&entries); err != nil {
		return openCodeGoAuthMaterial{kind: "unknown"}, fmt.Errorf("decode opencode auth file: %w", err)
	}
	entry, ok := entries["opencode-go"]
	if !ok {
		return openCodeGoAuthMaterial{kind: "absent"}, nil
	}
	if strings.TrimSpace(entry.Type) != "api" {
		return openCodeGoAuthMaterial{kind: "unknown"}, nil
	}
	token := strings.TrimSpace(entry.Key)
	if token == "" {
		return openCodeGoAuthMaterial{kind: "unknown"}, nil
	}
	return openCodeGoAuthMaterial{kind: "official", token: token}, nil
}

// readOpenCodeGoDashboardCredential resolves dashboard credentials using the
// conventions shared with opgginc/opencode-bar and opencode-quota: environment
// variables first, then an explicit config file override, then the standard
// config locations. Browser cookie scanning is intentionally not implemented.
func readOpenCodeGoDashboardCredential(home string) *openCodeGoDashboardCredential {
	if workspaceID := strings.TrimSpace(os.Getenv("OPENCODE_GO_WORKSPACE_ID")); workspaceID != "" {
		if authCookie := strings.TrimSpace(os.Getenv("OPENCODE_GO_AUTH_COOKIE")); authCookie != "" {
			return &openCodeGoDashboardCredential{workspaceID: workspaceID, authCookie: authCookie, source: "environment"}
		}
	}
	if override := strings.TrimSpace(os.Getenv("OPENCODE_GO_CONFIG_FILE")); override != "" {
		if cred := openCodeGoDashboardCredentialFromFile(override); cred != nil {
			return cred
		}
	}
	for _, path := range openCodeGoDashboardConfigCandidates(home) {
		if cred := openCodeGoDashboardCredentialFromFile(path); cred != nil {
			return cred
		}
	}
	return nil
}

func openCodeGoDashboardConfigCandidates(home string) []string {
	var candidates []string
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		candidates = append(candidates,
			filepath.Join(xdg, "opencode-bar", "opencode-go.json"),
			filepath.Join(xdg, "opencode-quota", "opencode-go.json"),
		)
	}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			filepath.Join(home, "Library", "Application Support", "opencode-bar", "opencode-go.json"),
			filepath.Join(home, "Library", "Application Support", "opencode-quota", "opencode-go.json"),
		)
	}
	candidates = append(candidates,
		filepath.Join(home, ".config", "opencode-bar", "opencode-go.json"),
		filepath.Join(home, ".config", "opencode-quota", "opencode-go.json"),
	)
	return candidates
}

func openCodeGoDashboardCredentialFromFile(path string) *openCodeGoDashboardCredential {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var entry map[string]string
	if err := json.NewDecoder(io.LimitReader(f, maxOpenCodeGoAuthBytes)).Decode(&entry); err != nil {
		return nil
	}
	workspaceID := firstNonEmptyString(
		strings.TrimSpace(entry["workspaceId"]),
		strings.TrimSpace(entry["workspaceID"]),
		strings.TrimSpace(entry["workspace_id"]),
	)
	authCookie := firstNonEmptyString(
		strings.TrimSpace(entry["authCookie"]),
		strings.TrimSpace(entry["auth_cookie"]),
		strings.TrimSpace(entry["cookie"]),
	)
	if workspaceID == "" || authCookie == "" {
		return nil
	}
	return &openCodeGoDashboardCredential{workspaceID: workspaceID, authCookie: authCookie, source: path}
}

// fetchOpenCodeGoSubscription confirms the OpenCode Go subscription with the
// non-generating invalid-request auth challenge (and, when dashboard
// credentials are configured, the authenticated dashboard page). The models
// endpoint is only used to discover current Go models and is never treated as
// evidence. The projection is produced only when the subscription is
// positively confirmed; otherwise an error is returned so no card exists.
func fetchOpenCodeGoSubscription(ctx context.Context, client openCodeGoHTTPClient, modelsEndpoint, chatEndpoint, dashboardBaseURL string, auth openCodeGoAuthMaterial, dashboard *openCodeGoDashboardCredential, now time.Time) (*OpenCodeGoSubscriptionUsage, error) {
	if auth.kind != "official" || strings.TrimSpace(auth.token) == "" {
		return nil, errors.New("official OpenCode Go authentication required")
	}
	models, err := discoverOpenCodeGoModels(ctx, client, modelsEndpoint, auth)
	if err != nil {
		return nil, err
	}

	usage := &OpenCodeGoSubscriptionUsage{
		AuthKind:  "official",
		State:     "available",
		Plan:      "go",
		FetchedAt: now.UTC().Format(time.RFC3339),
	}
	windows := fetchOpenCodeGoDashboardWindows(ctx, client, dashboardBaseURL, dashboard, now)
	if len(windows) > 0 {
		usage.UsageAvailable = true
		usage.Windows = windows
	}

	confirmed := len(windows) > 0
	if !confirmed {
		confirmed = runOpenCodeGoAuthChallenge(ctx, client, chatEndpoint, auth, models)
	}
	if !confirmed {
		return nil, errors.New("opencode go subscription not confirmed")
	}
	return usage, nil
}

// discoverOpenCodeGoModels fetches the currently served Go model IDs from the
// public models endpoint. It is model discovery only: the endpoint returns the
// same payload for invalid keys, so its success never confirms a subscription.
// An empty list or an unparseable response fails closed.
func discoverOpenCodeGoModels(ctx context.Context, client openCodeGoHTTPClient, endpoint string, auth openCodeGoAuthMaterial) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+auth.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "zen-stats")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request opencode go models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("opencode go models endpoint returned status %d", resp.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []string `json:"models"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxOpenCodeGoBodyBytes))
	if err := decoder.Decode(&payload); err != nil {
		return nil, errors.New("opencode go models response is not parseable json")
	}
	var models []string
	for _, model := range payload.Data {
		if id := strings.TrimSpace(model.ID); id != "" {
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		for _, id := range payload.Models {
			if id = strings.TrimSpace(id); id != "" {
				models = append(models, id)
			}
		}
	}
	if len(models) == 0 {
		return nil, errors.New("opencode go models response has no models")
	}
	return models, nil
}

// runOpenCodeGoAuthChallenge probes the chat completions endpoint with a
// payload that cannot generate a completion: an empty messages list and a
// negative max_tokens. Only an exact 400 whose error type and code are both
// invalid_request_error confirms that the key is accepted by the Go service;
// such a response proves authentication without producing any token usage.
// Auth failures (401/403), throttling (429), server errors (5xx), unexpected
// 2xx responses, HTML, and unknown error shapes are never accepted; 2xx
// responses are skipped without parsing and inconclusive probe responses move
// on to the next discovered model. The check fails closed when no discovered
// model yields the exact confirmation.
func runOpenCodeGoAuthChallenge(ctx context.Context, client openCodeGoHTTPClient, endpoint string, auth openCodeGoAuthMaterial, models []string) bool {
	attempts := len(models)
	if attempts > opencodeGoChallengeMaxAttempts {
		attempts = opencodeGoChallengeMaxAttempts
	}
	for i := 0; i < attempts; i++ {
		payload := fmt.Sprintf(`{"model":%q,"messages":[],"max_tokens":-1,"stream":false}`, models[i])
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(payload))
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+auth.token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "zen-stats")
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxOpenCodeGoBodyBytes))
		resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusOK:
			// A 2xx response can never confirm: the payload must not
			// generate. Skip the model without parsing the body.
			continue
		case resp.StatusCode == http.StatusBadRequest:
			if readErr != nil {
				continue
			}
			var errorBody struct {
				Error struct {
					Type string `json:"type"`
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(body, &errorBody); err != nil {
				continue
			}
			if errorBody.Error.Type == "invalid_request_error" && errorBody.Error.Code == "invalid_request_error" {
				return true
			}
			continue
		case resp.StatusCode == http.StatusUnauthorized,
			resp.StatusCode == http.StatusForbidden,
			resp.StatusCode == http.StatusTooManyRequests,
			resp.StatusCode >= 500:
			return false
		default:
			continue
		}
	}
	return false
}

func fetchOpenCodeGoDashboardWindows(ctx context.Context, client openCodeGoHTTPClient, dashboardBaseURL string, dashboard *openCodeGoDashboardCredential, now time.Time) []OpenCodeGoUsageWindow {
	if dashboard == nil || strings.TrimSpace(dashboard.workspaceID) == "" || strings.TrimSpace(dashboard.authCookie) == "" {
		return nil
	}
	endpoint := fmt.Sprintf("%s/%s/go", strings.TrimRight(dashboardBaseURL, "/"), url.PathEscape(dashboard.workspaceID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Cookie", openCodeGoCookieHeader(dashboard.authCookie))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOpenCodeGoBodyBytes))
	if err != nil {
		return nil
	}
	return parseOpenCodeGoDashboardWindows(string(body), now)
}

func openCodeGoCookieHeader(raw string) string {
	if strings.Contains(raw, "auth=") {
		return raw
	}
	return "auth=" + raw
}

// parseOpenCodeGoDashboardWindows extracts the 5-hour, weekly, and monthly
// usage windows from the dashboard page markup. The page embeds the windows
// as JSON or SolidJS-serialized objects (for example inside __next_f payloads
// or $R[n]={...} expressions). Windows that cannot be parsed are omitted and
// no value is ever guessed; a page without any parseable window (login page,
// markup drift, rate-limit page) yields no windows.
func parseOpenCodeGoDashboardWindows(page string, now time.Time) []OpenCodeGoUsageWindow {
	text := openCodeGoNormalizeDashboardHTML(page)
	var windows []OpenCodeGoUsageWindow
	for i, spec := range openCodeGoWindowSpecs {
		match := openCodeGoWindowObjectRes[i].FindStringSubmatch(text)
		if match == nil {
			continue
		}
		used, usedOK := openCodeGoCaptureNumber(match[1], "usagePercent")
		reset, resetOK := openCodeGoCaptureNumber(match[1], "resetInSec")
		if !usedOK || !resetOK || math.IsNaN(used) || math.IsInf(used, 0) || used < 0 {
			continue
		}
		seconds := int64(math.Round(reset))
		if seconds < 0 {
			seconds = 0
		}
		windows = append(windows, OpenCodeGoUsageWindow{
			Name:           spec.name,
			UsedPercent:    used,
			LimitUSD:       spec.limitUSD,
			ResetInSeconds: seconds,
			ResetsAt:       now.Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339),
		})
	}
	return windows
}

func openCodeGoCaptureNumber(body, name string) (float64, bool) {
	re := regexp.MustCompile(`["']?` + regexp.QuoteMeta(name) + `["']?\s*:\s*"?(-?\d+(?:\.\d+)?)"?`)
	match := re.FindStringSubmatch(body)
	if match == nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func openCodeGoNormalizeDashboardHTML(page string) string {
	text := html.UnescapeString(page)
	text = strings.ReplaceAll(text, `\"`, `"`)
	text = strings.ReplaceAll(text, `\u0022`, `"`)
	return text
}

// collectOpenCodeGoSubscription returns the subscription projection only when
// the most recent verification positively confirmed the OpenCode Go
// subscription: the non-generating invalid-request auth challenge yielded the
// exact invalid_request_error 400, or the authenticated dashboard page parsed
// real usage windows. Any negative or ambiguous outcome (missing credentials,
// auth failure, network failure, ambiguous response, no challenge signal)
// yields nil so the app can never retain or produce a Go card.
func (c *Collector) collectOpenCodeGoSubscription(home string) *OpenCodeGoSubscriptionUsage {
	auth, err := readOpenCodeGoAuth(home)
	if err != nil || auth.kind != "official" {
		return nil
	}
	dashboard := readOpenCodeGoDashboardCredential(home)

	ctx, cancel := context.WithTimeout(context.Background(), c.opencodeGoTimeout)
	defer cancel()
	usage, err := fetchOpenCodeGoSubscription(ctx, c.opencodeGoClient, c.opencodeGoEndpoint, c.opencodeGoChatEndpoint, c.opencodeGoDashboardEndpoint, auth, dashboard, c.now())
	if err != nil {
		return nil
	}
	return usage
}
