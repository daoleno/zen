package stats

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	codexUsageEndpoint = "https://chatgpt.com/backend-api/wham/usage"
	maxCodexAuthBytes  = 2 << 20
	maxCodexUsageBytes = 1 << 20
)

type codexUsageHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type codexAuthMaterial struct {
	kind      string
	token     string
	accountID string
}

type codexAuthFile struct {
	AuthMode string `json:"auth_mode"`
	APIKey   string `json:"OPENAI_API_KEY"`
	Tokens   *struct {
		AccessToken string `json:"access_token"`
		AccountID   string `json:"account_id"`
	} `json:"tokens"`
}

type codexUsagePayload struct {
	PlanType  string `json:"plan_type"`
	RateLimit *struct {
		Primary   *codexUsageWindowPayload `json:"primary_window"`
		Secondary *codexUsageWindowPayload `json:"secondary_window"`
	} `json:"rate_limit"`
}

type codexUsageWindowPayload struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

func readCodexAuth(home, environmentAPIKey string) (codexAuthMaterial, error) {
	path := filepath.Join(home, ".codex", "auth.json")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		if strings.TrimSpace(environmentAPIKey) != "" {
			return codexAuthMaterial{kind: "api_key"}, nil
		}
		return codexAuthMaterial{kind: "absent"}, nil
	}
	if err != nil {
		return codexAuthMaterial{kind: "unknown"}, fmt.Errorf("open auth file: %w", err)
	}
	defer f.Close()

	var auth codexAuthFile
	decoder := json.NewDecoder(io.LimitReader(f, maxCodexAuthBytes))
	if err := decoder.Decode(&auth); err != nil {
		return codexAuthMaterial{kind: "unknown"}, fmt.Errorf("decode auth file: %w", err)
	}
	mode := strings.TrimSpace(auth.AuthMode)
	if mode == "apikey" || mode == "bedrockApiKey" {
		return codexAuthMaterial{kind: "api_key"}, nil
	}
	if mode != "" && mode != "chatgpt" && mode != "chatgptAuthTokens" {
		return codexAuthMaterial{kind: "unknown"}, nil
	}
	if auth.Tokens != nil && strings.TrimSpace(auth.Tokens.AccessToken) != "" {
		return codexAuthMaterial{
			kind:      "official",
			token:     strings.TrimSpace(auth.Tokens.AccessToken),
			accountID: strings.TrimSpace(auth.Tokens.AccountID),
		}, nil
	}
	if strings.TrimSpace(auth.APIKey) != "" || strings.TrimSpace(environmentAPIKey) != "" {
		return codexAuthMaterial{kind: "api_key"}, nil
	}
	return codexAuthMaterial{kind: "unknown"}, nil
}

func fetchCodexSubscription(ctx context.Context, client codexUsageHTTPClient, endpoint string, auth codexAuthMaterial, now time.Time) (*CodexSubscriptionUsage, error) {
	if auth.kind != "official" || strings.TrimSpace(auth.token) == "" {
		return nil, errors.New("official Codex authentication required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+auth.token)
	req.Header.Set("User-Agent", "zen-stats")
	if auth.accountID != "" {
		req.Header.Set("ChatGPT-Account-Id", auth.accountID)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request usage: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("usage endpoint returned status %d", resp.StatusCode)
	}

	var payload codexUsagePayload
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxCodexUsageBytes))
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode usage response: %w", err)
	}
	if payload.RateLimit == nil {
		return nil, errors.New("usage response has no rate_limit")
	}

	usage := &CodexSubscriptionUsage{
		AuthKind:  "official",
		State:     "available",
		Plan:      strings.TrimSpace(payload.PlanType),
		FetchedAt: now.UTC().Format(time.RFC3339),
	}
	for _, candidate := range []struct {
		name   string
		window *codexUsageWindowPayload
	}{{"primary", payload.RateLimit.Primary}, {"secondary", payload.RateLimit.Secondary}} {
		if candidate.window == nil {
			continue
		}
		if math.IsNaN(candidate.window.UsedPercent) || math.IsInf(candidate.window.UsedPercent, 0) || candidate.window.UsedPercent < 0 || candidate.window.UsedPercent > 100 {
			return nil, fmt.Errorf("invalid %s used_percent", candidate.name)
		}
		window := CodexUsageWindow{
			Name:          candidate.name,
			UsedPercent:   candidate.window.UsedPercent,
			WindowMinutes: candidate.window.LimitWindowSeconds / 60,
		}
		if candidate.window.ResetAt > 0 {
			window.ResetsAt = time.Unix(candidate.window.ResetAt, 0).UTC().Format(time.RFC3339)
		}
		usage.Windows = append(usage.Windows, window)
	}
	if len(usage.Windows) == 0 {
		return nil, errors.New("usage response has no windows")
	}
	return usage, nil
}

func (c *Collector) collectCodexSubscription(home string) *CodexSubscriptionUsage {
	auth, err := readCodexAuth(home, os.Getenv("OPENAI_API_KEY"))
	if err != nil || auth.kind != "official" {
		c.lastCodexSubscription = nil
		c.lastCodexAuthFingerprint = ""
		return nil
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(auth.token+"\x00"+auth.accountID)))

	ctx, cancel := context.WithTimeout(context.Background(), c.codexUsageTimeout)
	defer cancel()
	usage, err := fetchCodexSubscription(ctx, c.codexUsageClient, c.codexUsageEndpoint, auth, c.now())
	if err == nil {
		c.lastCodexSubscription = usage
		c.lastCodexAuthFingerprint = fingerprint
		return usage
	}
	if c.lastCodexSubscription != nil && c.lastCodexAuthFingerprint == fingerprint {
		stale := *c.lastCodexSubscription
		stale.Windows = append([]CodexUsageWindow(nil), stale.Windows...)
		stale.State = "available"
		stale.Stale = true
		return &stale
	}
	return &CodexSubscriptionUsage{AuthKind: "official", State: "unavailable"}
}
