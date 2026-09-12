package host

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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

	pinnedLeafDER []byte

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
	// Sunshine's default Web UI certificate is CN-only (crypto.cpp gen_creds),
	// so standard IP hostname verification rejects the exact saved cert. Pin the
	// exact leaf instead: verification compares the presented leaf bytes with
	// the Zen-owned certificate and nothing else.
	pinnedLeafDER, err := loadPinnedLeafDER(caCertPath)
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // verification is the exact leaf pin below
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("sunshine_admin_no_peer_certificate")
			}
			if subtle.ConstantTimeCompare(rawCerts[0], pinnedLeafDER) != 1 {
				return errors.New("sunshine_admin_certificate_mismatch")
			}
			return nil
		},
	}
	return &SunshineAdmin{
		baseURL:       strings.TrimSuffix(baseURL, "/"),
		username:      username,
		password:      password,
		pinnedLeafDER: pinnedLeafDER,
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Never follow redirects: a redirected admin call would leave the
			// pinned endpoint.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{
				TLSClientConfig:     tlsConfig,
				DisableKeepAlives:   true,
				MaxIdleConnsPerHost: 1,
			},
		},
	}, nil
}

func loadPinnedLeafDER(caCertPath string) ([]byte, error) {
	if caCertPath == "" {
		return nil, errors.New("sunshine_admin_missing_certificate")
	}
	pemBytes, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("read sunshine web ui certificate: %w", err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("sunshine_admin_invalid_certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errors.New("sunshine_admin_invalid_certificate")
	}
	return certificate.Raw, nil
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
	if csrf && (response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusForbidden) {
		// The upstream CSRF token may have expired: refresh exactly once and
		// retry only this bounded invalid-token response.
		response.Body.Close()
		a.csrfMu.Lock()
		a.csrfToken = ""
		a.csrfMu.Unlock()
		refreshed, refreshErr := a.token(ctx)
		if refreshErr != nil {
			return nil, refreshErr
		}
		request2, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, func() io.Reader {
			if body == nil {
				return nil
			}
			encoded, _ := json.Marshal(body)
			return bytes.NewReader(encoded)
		}())
		if err != nil {
			return nil, err
		}
		request2.SetBasicAuth(a.username, a.password)
		request2.Header.Set("Content-Type", "application/json")
		request2.Header.Set("X-CSRF-Token", refreshed)
		response, err = a.client.Do(request2)
		if err != nil {
			return nil, err
		}
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
