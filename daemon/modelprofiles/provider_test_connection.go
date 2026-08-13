package modelprofiles

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TestProviderConnection validates credentials and protocol compatibility by
// issuing a bounded, SSRF-safe model-list request. It does not create a
// connection, touch the credential store, mutate discovery cache, or alter defaults.
func (o *Owner) TestProviderConnection(in ProviderConnectionTestInput) (ProviderConnectionTestResult, error) {
	out := ProviderConnectionTestResult{}
	if o == nil || !o.started {
		return out, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	client := clientFromExecutor(in.Client)
	baseURL := strings.TrimSpace(in.BaseURL)
	secret := strings.TrimSpace(in.Credential)
	if client != ClientCodex && client != ClientClaude {
		return out, fmt.Errorf("%w: client must be codex or claude", ErrInvalid)
	}
	if baseURL == "" || secret == "" {
		return out, fmt.Errorf("%w: base_url and credential are required", ErrInvalid)
	}

	// Empty ModelID: compileProviderConnectionForClient uses the preset
	// ClientModel contract id as the ephemeral probe placeholder.
	profile, err := compileProviderConnectionForClient(ProviderConnectionInput{
		ID:       "connection-test",
		Name:     "Connection test",
		Client:   client,
		PresetID: ProviderPresetCustom,
		BaseURL:  baseURL,
		Advanced: true,
	}, executorFromClient(client))
	if err != nil {
		return out, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	lookup := func(name string) (string, bool) {
		if name == profile.CredentialEnv {
			return secret, true
		}
		return "", false
	}
	startedAt := time.Now()
	ids, err := fetchUpstreamModelIDs(ctx, NewSafeHTTPClient(15*time.Second), profile, nil, lookup)
	latencyMS := time.Since(startedAt).Milliseconds()
	secret = ""
	if err != nil {
		return out, err
	}
	return ProviderConnectionTestResult{Client: client, ModelCount: len(ids), LatencyMS: latencyMS}, nil
}
