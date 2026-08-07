package stats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// opencodeGoUsageEndpoint is the official OpenCode Go chat completions
	// endpoint. The Go subscription is confirmed only by an authenticated
	// response from this service: an invalid or expired key is rejected with
	// an AuthError while a valid Go key is accepted.
	opencodeGoUsageEndpoint = "https://opencode.ai/zen/go/v1/chat/completions"
	// opencodeGoVerifyModel is a model currently served by OpenCode Go per the
	// official model catalog. If it is ever retired the check fails closed
	// (no card) rather than fabricating a subscription.
	opencodeGoVerifyModel  = "deepseek-v4-flash"
	maxOpenCodeGoAuthBytes = 2 << 20
	maxOpenCodeGoBodyBytes = 1 << 20
)

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

// fetchOpenCodeGoSubscription confirms the OpenCode Go subscription by
// authenticating against the official Go API with a request that is rejected
// before any tokens are consumed. A 400 invalid_request_error or a 200 proves
// the key was accepted by the Go service; an AuthError (401), any other
// status, an unparseable body, or a network failure proves nothing and yields
// an error so no card can be produced.
func fetchOpenCodeGoSubscription(ctx context.Context, client openCodeGoHTTPClient, endpoint string, auth openCodeGoAuthMaterial, now time.Time) (*OpenCodeGoSubscriptionUsage, error) {
	if auth.kind != "official" || strings.TrimSpace(auth.token) == "" {
		return nil, errors.New("official OpenCode Go authentication required")
	}
	payload := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"ok"}],"max_tokens":0,"stream":false}`, opencodeGoVerifyModel)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+auth.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "zen-stats")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request opencode go: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOpenCodeGoBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read opencode go response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		if !json.Valid(body) {
			return nil, errors.New("opencode go response is not valid json")
		}
	case http.StatusBadRequest:
		var errorBody struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &errorBody); err != nil {
			return nil, errors.New("opencode go rejected response is not parseable")
		}
		if errorBody.Error.Type != "invalid_request_error" {
			return nil, fmt.Errorf("opencode go rejected request with error type %q", errorBody.Error.Type)
		}
	default:
		return nil, fmt.Errorf("opencode go endpoint returned status %d", resp.StatusCode)
	}

	return &OpenCodeGoSubscriptionUsage{
		AuthKind:  "official",
		State:     "available",
		Plan:      "go",
		FetchedAt: now.UTC().Format(time.RFC3339),
	}, nil
}

// collectOpenCodeGoSubscription returns the subscription projection only when
// the most recent authoritative check against the official OpenCode Go API
// positively confirmed it. Any negative or ambiguous outcome (missing
// credentials, auth failure, expired key, network failure, malformed body)
// yields nil so the app can never retain or produce a Go card.
func (c *Collector) collectOpenCodeGoSubscription(home string) *OpenCodeGoSubscriptionUsage {
	auth, err := readOpenCodeGoAuth(home)
	if err != nil || auth.kind != "official" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.opencodeGoTimeout)
	defer cancel()
	usage, err := fetchOpenCodeGoSubscription(ctx, c.opencodeGoClient, c.opencodeGoEndpoint, auth, c.now())
	if err != nil {
		return nil
	}
	return usage
}
