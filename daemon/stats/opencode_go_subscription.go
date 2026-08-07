package stats

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	opencodeGoChatEndpoint = "https://opencode.ai/zen/go/v1/chat/completions"
	// opencodeGoServerEndpoint hosts the server-function calls that the
	// OpenCode web console uses (see steipete/CodexBar OpenCodeUsageFetcher).
	opencodeGoServerEndpoint = "https://opencode.ai/_server"
	// opencodeGoSubscriptionServerID is the subscription.get server-function
	// id. It takes the workspace ID and returns rolling/weekly/monthly usage.
	opencodeGoSubscriptionServerID = "7abeebee372f304e050aaaf92be863f4a86490e382f8c79db68fd94040d691b4"
	maxOpenCodeGoAuthBytes         = 2 << 20
	maxOpenCodeGoBodyBytes         = 8 << 20
	// opencodeGoChallengeMaxAttempts bounds how many discovered models are
	// probed before the challenge fails closed.
	opencodeGoChallengeMaxAttempts = 4
)

// openCodeGoWindowSpecs mirror the official Go usage windows and the limits
// published in the OpenCode Go documentation: $12 per 5 hours, $30 per week,
// $60 per month. The limits are plan facts from the docs; current used
// percentages come only from the authenticated subscription server-function
// response.
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

// openCodeGoWindowAliases are the key spellings the subscription response may
// use for each window, mirroring CodexBar's OpenCodeUsageFetcher.
var openCodeGoWindowAliases = map[string][]string{
	"rollingUsage": {"rollingUsage", "rolling", "rolling_usage", "rollingWindow", "rolling_window"},
	"weeklyUsage":  {"weeklyUsage", "weekly", "weekly_usage", "weeklyWindow", "weekly_window"},
	"monthlyUsage": {"monthlyUsage", "monthly", "monthly_usage", "monthlyWindow", "monthly_window"},
}

var openCodeGoPercentKeys = []string{"usagePercent", "usedPercent", "percentUsed", "percent", "usage_percent", "used_percent", "utilization"}

var openCodeGoResetInKeys = []string{"resetInSec", "resetInSeconds", "resetsInSec", "resetsInSeconds", "resetSec", "resetIn", "reset_sec", "reset_in_sec"}

var openCodeGoWorkspaceIDRe = regexp.MustCompile(`wrk_[A-Za-z0-9]+`)

// openCodeGoWindowObjectRes extracts a window object body from serialized
// response text (plain JSON or SolidJS-style $R[n]={...} expressions).
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

// openCodeGoDashboardCredential addresses the OpenCode subscription
// server-function. The values are sensitive and are never serialized, logged,
// or cached; only source is used in logs.
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

// normalizeOpenCodeGoWorkspaceID accepts a raw wrk_ id, a full workspace URL,
// or a string containing one, mirroring CodexBar's workspace override rules.
func normalizeOpenCodeGoWorkspaceID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "wrk_") && len(trimmed) > 4 {
		return trimmed
	}
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		for i, part := range parts {
			if part == "workspace" && i+1 < len(parts) && strings.HasPrefix(parts[i+1], "wrk_") && len(parts[i+1]) > 4 {
				return parts[i+1]
			}
		}
	}
	if match := openCodeGoWorkspaceIDRe.FindString(trimmed); match != "" {
		return match
	}
	return ""
}

// readOpenCodeGoDashboardCredential resolves dashboard credentials using the
// conventions shared with opgginc/opencode-bar and opencode-quota: environment
// variables first, then an explicit config file override, then the standard
// config locations. Workspace IDs are normalized (raw id, URL, or embedded
// id); a candidate without a valid workspace id and cookie is skipped.
// Browser cookie scanning is intentionally not implemented.
func readOpenCodeGoDashboardCredential(home string) *openCodeGoDashboardCredential {
	if workspaceID := normalizeOpenCodeGoWorkspaceID(os.Getenv("OPENCODE_GO_WORKSPACE_ID")); workspaceID != "" {
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
	workspaceID := normalizeOpenCodeGoWorkspaceID(firstNonEmptyString(
		strings.TrimSpace(entry["workspaceId"]),
		strings.TrimSpace(entry["workspaceID"]),
		strings.TrimSpace(entry["workspace_id"]),
	))
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
// credentials are configured, the authenticated subscription server-function
// response). The models endpoint is only used to discover current Go models
// and is never treated as evidence. The projection is produced only when the
// subscription is positively confirmed; otherwise an error is returned so no
// card exists.
func fetchOpenCodeGoSubscription(ctx context.Context, client openCodeGoHTTPClient, modelsEndpoint, chatEndpoint, serverEndpoint string, auth openCodeGoAuthMaterial, dashboard *openCodeGoDashboardCredential, now time.Time) (*OpenCodeGoSubscriptionUsage, error) {
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
	windows := fetchOpenCodeGoServerUsage(ctx, client, serverEndpoint, dashboard, now)
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

// fetchOpenCodeGoServerUsage reads the rolling/weekly/monthly usage windows
// through the OpenCode subscription server-function. The GET carries the
// workspace ID as a JSON-encoded args query parameter; if the GET response
// yields no usage windows, a POST with the same headers and a JSON body of
// the args is attempted. 401/403, signed-out text, explicit null payloads,
// non-200 responses, and malformed bodies all fail closed with no windows.
func fetchOpenCodeGoServerUsage(ctx context.Context, client openCodeGoHTTPClient, serverEndpoint string, dashboard *openCodeGoDashboardCredential, now time.Time) []OpenCodeGoUsageWindow {
	if dashboard == nil || strings.TrimSpace(dashboard.workspaceID) == "" || strings.TrimSpace(dashboard.authCookie) == "" {
		return nil
	}
	args := []string{dashboard.workspaceID}

	text, ok := fetchOpenCodeGoServerText(ctx, client, serverEndpoint, opencodeGoSubscriptionServerID, args, http.MethodGet, dashboard)
	if ok {
		if windows := parseOpenCodeGoServerUsage(text, now); len(windows) > 0 {
			return windows
		}
	}
	text, ok = fetchOpenCodeGoServerText(ctx, client, serverEndpoint, opencodeGoSubscriptionServerID, args, http.MethodPost, dashboard)
	if !ok {
		return nil
	}
	return parseOpenCodeGoServerUsage(text, now)
}

// fetchOpenCodeGoServerText performs one server-function request. It returns
// the response body only when the response is a 200 that does not look signed
// out and is not an explicit null payload; every other outcome fails closed.
func fetchOpenCodeGoServerText(ctx context.Context, client openCodeGoHTTPClient, serverEndpoint, serverID string, args []string, method string, dashboard *openCodeGoDashboardCredential) (string, bool) {
	endpoint, err := openCodeGoServerRequestURL(serverEndpoint, serverID, args, method)
	if err != nil {
		return "", false
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return "", false
	}
	if method == http.MethodPost {
		body, err := json.Marshal(args)
		if err != nil {
			return "", false
		}
		req.Body = io.NopCloser(strings.NewReader(string(body)))
		req.ContentLength = int64(len(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Cookie", openCodeGoCookieHeader(dashboard.authCookie))
	req.Header.Set("X-Server-Id", serverID)
	req.Header.Set("X-Server-Instance", "server-fn:"+openCodeGoRequestID())
	req.Header.Set("Origin", "https://opencode.ai")
	req.Header.Set("Referer", "https://opencode.ai/workspace/"+url.PathEscape(dashboard.workspaceID)+"/billing")
	req.Header.Set("Accept", "text/javascript, application/json;q=0.9, */*;q=0.8")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxOpenCodeGoBodyBytes))
	resp.Body.Close()

	if readErr != nil || resp.StatusCode != http.StatusOK {
		return "", false
	}
	text := string(body)
	if openCodeGoLooksSignedOut(text) || openCodeGoIsNullPayload(text) {
		return "", false
	}
	return text, true
}

// openCodeGoServerRequestURL builds the server-function URL: for GET the
// server id and JSON-encoded args become URL-encoded query parameters; for
// POST the args travel in the request body instead.
func openCodeGoServerRequestURL(serverEndpoint, serverID string, args []string, method string) (string, error) {
	if method != http.MethodGet {
		return serverEndpoint, nil
	}
	query := url.Values{}
	query.Set("id", serverID)
	if len(args) > 0 {
		encoded, err := json.Marshal(args)
		if err != nil {
			return "", err
		}
		query.Set("args", string(encoded))
	}
	return serverEndpoint + "?" + query.Encode(), nil
}

func openCodeGoRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

func openCodeGoCookieHeader(raw string) string {
	if strings.Contains(raw, "auth=") {
		return raw
	}
	return "auth=" + raw
}

func openCodeGoLooksSignedOut(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"login", "sign in", "auth/authorize", "not associated with an account", `actor of type "public"`} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func openCodeGoIsNullPayload(text string) bool {
	trimmed := strings.TrimSpace(text)
	if strings.EqualFold(trimmed, "null") {
		return true
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return false
	}
	return value == nil
}

// parseOpenCodeGoServerUsage extracts usage windows from the subscription
// server-function response. The response is text/javascript that carries
// serialized objects, so parsing tries strict JSON first (top-level or one of
// the common wrapper keys) and then extracts window objects from the
// serialized text. The rolling window is required; weekly and monthly windows
// are included only when they are actually present and parseable. No value is
// ever guessed and page HTML is never treated as evidence.
func parseOpenCodeGoServerUsage(text string, now time.Time) []OpenCodeGoUsageWindow {
	if openCodeGoLooksSignedOut(text) || openCodeGoIsNullPayload(text) {
		return nil
	}
	if windows := parseOpenCodeGoServerUsageJSON(text, now); len(windows) > 0 {
		return windows
	}
	var windows []OpenCodeGoUsageWindow
	for i, spec := range openCodeGoWindowSpecs {
		match := openCodeGoWindowObjectRes[i].FindStringSubmatch(text)
		if match == nil {
			continue
		}
		if window, ok := openCodeGoWindowFromBody(match[1], spec.name, spec.limitUSD, now); ok {
			windows = append(windows, window)
		}
	}
	if len(windows) == 0 || windows[0].Name != "rolling" {
		return nil
	}
	return windows
}

// parseOpenCodeGoServerUsageJSON parses the strict JSON form of the response:
// an object with the usage windows at the top level, under one of the common
// wrapper keys, or under a nested "usage" object.
func parseOpenCodeGoServerUsageJSON(text string, now time.Time) []OpenCodeGoUsageWindow {
	var root map[string]any
	if err := json.Unmarshal([]byte(text), &root); err != nil {
		return nil
	}
	candidates := []map[string]any{root}
	for _, key := range []string{"data", "result", "usage", "billing", "payload"} {
		if nested, ok := root[key].(map[string]any); ok {
			candidates = append(candidates, nested)
		}
	}
	if nested, ok := root["usage"].(map[string]any); ok {
		candidates = append(candidates, nested)
	}
	for _, candidate := range candidates {
		if windows := openCodeGoWindowsFromObject(candidate, now); len(windows) > 0 {
			return windows
		}
	}
	return nil
}

func openCodeGoWindowsFromObject(object map[string]any, now time.Time) []OpenCodeGoUsageWindow {
	var windows []OpenCodeGoUsageWindow
	for _, spec := range openCodeGoWindowSpecs {
		window, ok := openCodeGoWindowFromAliases(object, spec, now)
		if !ok {
			continue
		}
		windows = append(windows, window)
	}
	if len(windows) == 0 || windows[0].Name != "rolling" {
		return nil
	}
	return windows
}

func openCodeGoWindowFromAliases(object map[string]any, spec openCodeGoWindowSpec, now time.Time) (OpenCodeGoUsageWindow, bool) {
	for _, alias := range openCodeGoWindowAliases[spec.field] {
		raw, ok := object[alias]
		if !ok {
			continue
		}
		body, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		return openCodeGoWindowFromBodyMap(body, spec.name, spec.limitUSD, now)
	}
	return OpenCodeGoUsageWindow{}, false
}

// openCodeGoWindowFromBody parses a window object body captured from
// serialized response text.
func openCodeGoWindowFromBody(body, name string, limitUSD float64, now time.Time) (OpenCodeGoUsageWindow, bool) {
	used, ok := openCodeGoCaptureNumber(body, openCodeGoPercentKeys)
	if !ok {
		return OpenCodeGoUsageWindow{}, false
	}
	reset, ok := openCodeGoCaptureNumber(body, openCodeGoResetInKeys)
	if !ok {
		return OpenCodeGoUsageWindow{}, false
	}
	return openCodeGoBuildWindow(name, limitUSD, used, reset, now), true
}

// openCodeGoWindowFromBodyMap parses a window object from parsed JSON.
func openCodeGoWindowFromBodyMap(body map[string]any, name string, limitUSD float64, now time.Time) (OpenCodeGoUsageWindow, bool) {
	used, ok := openCodeGoNumberValue(body, openCodeGoPercentKeys)
	if !ok {
		return OpenCodeGoUsageWindow{}, false
	}
	reset, ok := openCodeGoNumberValue(body, openCodeGoResetInKeys)
	if !ok {
		return OpenCodeGoUsageWindow{}, false
	}
	return openCodeGoBuildWindow(name, limitUSD, used, reset, now), true
}

// openCodeGoBuildWindow normalizes a parsed window. A directly present percent
// at or below 1.0 is treated as a fraction (mirroring CodexBar); the result is
// clamped to 0..100 and the reset countdown to >= 0. Values are never
// invented, only normalized.
func openCodeGoBuildWindow(name string, limitUSD float64, used, reset float64, now time.Time) OpenCodeGoUsageWindow {
	percent := used
	if percent >= 0 && percent <= 1.0 {
		percent *= 100
	}
	percent = math.Max(0, math.Min(100, percent))
	seconds := int64(math.Round(reset))
	if seconds < 0 {
		seconds = 0
	}
	return OpenCodeGoUsageWindow{
		Name:           name,
		UsedPercent:    percent,
		LimitUSD:       limitUSD,
		ResetInSeconds: seconds,
		ResetsAt:       now.Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339),
	}
}

func openCodeGoCaptureNumber(body string, keys []string) (float64, bool) {
	for _, key := range keys {
		re := regexp.MustCompile(`["']?` + regexp.QuoteMeta(key) + `["']?\s*:\s*"?(-?\d+(?:\.\d+)?)"?`)
		match := re.FindStringSubmatch(body)
		if match == nil {
			continue
		}
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			continue
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return 0, false
		}
		return value, true
	}
	return 0, false
}

func openCodeGoNumberValue(object map[string]any, keys []string) (float64, bool) {
	for _, key := range keys {
		value, ok := object[key]
		if !ok {
			continue
		}
		parsed, ok := openCodeGoFloat(value)
		if !ok {
			continue
		}
		if math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

func openCodeGoFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

// collectOpenCodeGoSubscription returns the subscription projection only when
// the most recent verification positively confirmed the OpenCode Go
// subscription: the subscription server-function yielded usage windows, or
// the non-generating invalid-request auth challenge yielded the exact
// invalid_request_error 400. Any negative or ambiguous outcome (missing
// credentials, auth failure, network failure, ambiguous response, no
// challenge signal) yields nil so the app can never retain or produce a Go
// card.
func (c *Collector) collectOpenCodeGoSubscription(home string) *OpenCodeGoSubscriptionUsage {
	auth, err := readOpenCodeGoAuth(home)
	if err != nil || auth.kind != "official" {
		return nil
	}
	dashboard := readOpenCodeGoDashboardCredential(home)

	ctx, cancel := context.WithTimeout(context.Background(), c.opencodeGoTimeout)
	defer cancel()
	usage, err := fetchOpenCodeGoSubscription(ctx, c.opencodeGoClient, c.opencodeGoEndpoint, c.opencodeGoChatEndpoint, c.opencodeGoServerEndpoint, auth, dashboard, c.now())
	if err != nil {
		return nil
	}
	return usage
}
