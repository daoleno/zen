package host

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// SunshineAdmin is the Zen-owned control client for the pinned Sunshine admin
// APIs (src/confighttp.cpp@dd7a1f79):
//
//	GET  /api/clients/list    -> {"status":true,"named_certs":[{"name","uuid","enabled"}]}
//	POST /api/clients/update  {"uuid","enabled"} -> {"status":bool}
//	   (disabling terminates the target's sessions by certificate upstream)
//	POST /api/clients/unpair  {"uuid"} -> {"status":bool} (only the matching UUID)
//
// Authentication is HTTP Basic with the host Web UI credentials and the real
// CSRF flow (GET /api/csrf-token + X-CSRF-Token). Requests are bounded and the
// Web UI certificate is pinned to the Zen-owned certificate file.
type SunshineAdmin struct {
	baseURL  string
	username string
	password string
	client   *http.Client

	csrfMu    sync.Mutex
	csrfToken string
}

type SunshineClient struct {
	Name    string `json:"name"`
	UUID    string `json:"uuid"`
	Enabled bool   `json:"enabled"`
}

func NewSunshineAdmin(baseURL, username, password, caCertPath string) (*SunshineAdmin, error) {
	if !strings.HasPrefix(baseURL, "https://") {
		return nil, errors.New("sunshine_admin_requires_https")
	}
	if username == "" || password == "" {
		return nil, errors.New("sunshine_admin_missing_credentials")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caCertPath != "" {
		pem, err := os.ReadFile(caCertPath)
		if err != nil {
			return nil, fmt.Errorf("read sunshine web ui certificate: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("sunshine_admin_invalid_certificate")
		}
		tlsConfig.RootCAs = pool
	}
	return &SunshineAdmin{
		baseURL:  strings.TrimSuffix(baseURL, "/"),
		username: username,
		password: password,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:     tlsConfig,
				DisableKeepAlives:   true,
				MaxIdleConnsPerHost: 1,
			},
		},
	}, nil
}

func (a *SunshineAdmin) do(ctx context.Context, method, path string, body any, csrf bool) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	request.SetBasicAuth(a.username, a.password)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if csrf {
		token, err := a.token(ctx)
		if err != nil {
			return nil, err
		}
		request.Header.Set("X-CSRF-Token", token)
	}
	response, err := a.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("sunshine admin %s %s: status %d", method, path, response.StatusCode)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("sunshine admin %s %s: %w", method, path, err)
	}
	return decoded, nil
}

func (a *SunshineAdmin) token(ctx context.Context) (string, error) {
	a.csrfMu.Lock()
	defer a.csrfMu.Unlock()
	if a.csrfToken != "" {
		return a.csrfToken, nil
	}
	payload, err := a.do(ctx, http.MethodGet, "/api/csrf-token", nil, false)
	if err != nil {
		return "", err
	}
	token, _ := payload["csrf_token"].(string)
	if token == "" {
		return "", errors.New("sunshine_admin_missing_csrf_token")
	}
	a.csrfToken = token
	return token, nil
}

func (a *SunshineAdmin) ListClients(ctx context.Context) ([]SunshineClient, error) {
	payload, err := a.do(ctx, http.MethodGet, "/api/clients/list", nil, false)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(payload["named_certs"])
	var clients []SunshineClient
	if err := json.Unmarshal(raw, &clients); err != nil {
		return nil, fmt.Errorf("sunshine admin clients list: %w", err)
	}
	return clients, nil
}

func (a *SunshineAdmin) SetClientEnabled(ctx context.Context, uuid string, enabled bool) error {
	if uuid == "" {
		return errors.New("sunshine_admin_missing_uuid")
	}
	payload, err := a.do(ctx, http.MethodPost, "/api/clients/update", map[string]any{"uuid": uuid, "enabled": enabled}, true)
	if err != nil {
		return err
	}
	if status, _ := payload["status"].(bool); !status {
		return fmt.Errorf("sunshine_admin_update_rejected: %s", uuid)
	}
	return nil
}

func (a *SunshineAdmin) UnpairClient(ctx context.Context, uuid string) error {
	if uuid == "" {
		return errors.New("sunshine_admin_missing_uuid")
	}
	payload, err := a.do(ctx, http.MethodPost, "/api/clients/unpair", map[string]any{"uuid": uuid}, true)
	if err != nil {
		return err
	}
	if status, _ := payload["status"].(bool); !status {
		return fmt.Errorf("sunshine_admin_unpair_rejected: %s", uuid)
	}
	return nil
}
