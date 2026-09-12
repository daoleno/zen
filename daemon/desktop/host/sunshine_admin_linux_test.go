package host

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type recordedRequest struct {
	method string
	path   string
	body   map[string]any
	csrf   string
	authOK bool
}

func newTestAdmin(t *testing.T, handler func(recordedRequest) (int, any)) (*SunshineAdmin, *[]recordedRequest) {
	t.Helper()
	records := &[]recordedRequest{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		var body map[string]any
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		record := recordedRequest{
			method: r.Method, path: r.URL.Path, body: body,
			csrf:   r.Header.Get("X-CSRF-Token"),
			authOK: ok && username == "zen" && password == "secret",
		}
		*records = append(*records, record)
		status, payload := handler(record)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(server.Close)
	certFile := filepath.Join(t.TempDir(), "sunshine.crt")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(certFile, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(server.Certificate().Raw)
	if err != nil || cert == nil {
		t.Fatal("test certificate unavailable")
	}
	admin, err := NewSunshineAdmin(server.URL, "zen", "secret", certFile)
	if err != nil {
		t.Fatal(err)
	}
	return admin, records
}

func TestAdminUsesBasicAuthCsrfAndTargetsExactClient(t *testing.T) {
	admin, records := newTestAdmin(t, func(record recordedRequest) (int, any) {
		switch record.path {
		case "/api/csrf-token":
			return http.StatusOK, map[string]any{"csrf_token": "token-1"}
		case "/api/clients/list":
			return http.StatusOK, map[string]any{"status": true, "named_certs": []map[string]any{
				{"name": "phone", "uuid": "device-a", "enabled": true},
			}}
		case "/api/clients/update", "/api/clients/unpair":
			return http.StatusOK, map[string]any{"status": record.body["uuid"] == "device-a"}
		default:
			return http.StatusNotFound, map[string]any{"status": false}
		}
	})

	ctx := context.Background()
	clients, err := admin.ListClients(ctx)
	if err != nil || len(clients) != 1 || clients[0].UUID != "device-a" {
		t.Fatalf("clients = %v err=%v", clients, err)
	}
	if err := admin.SetClientEnabled(ctx, "device-a", false); err != nil {
		t.Fatal(err)
	}
	if err := admin.UnpairClient(ctx, "device-a"); err != nil {
		t.Fatal(err)
	}
	for _, record := range *records {
		if !record.authOK {
			t.Fatalf("request without valid basic auth: %+v", record)
		}
		if record.method == http.MethodPost && record.csrf != "token-1" {
			t.Fatalf("POST missing csrf token: %+v", record)
		}
	}
	// Unknown client is rejected by upstream status=false and surfaced.
	if err := admin.UnpairClient(ctx, "device-b"); err == nil {
		t.Fatal("unknown client unpair was not surfaced")
	}
}

func TestRevokeTargetDisablesUnpairsAndKeepsOtherEnrollments(t *testing.T) {
	t.Setenv("ZEN_STATE_DIR", t.TempDir())
	store := NewSunshineOwnershipStore(ZenStateDir())
	if err := store.Claim("device-a", "uuid-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Claim("device-b", "uuid-b"); err != nil {
		t.Fatal(err)
	}
	admin, records := newTestAdmin(t, func(record recordedRequest) (int, any) {
		switch record.path {
		case "/api/csrf-token":
			return http.StatusOK, map[string]any{"csrf_token": "token-1"}
		default:
			return http.StatusOK, map[string]any{"status": true}
		}
	})
	if err := revokeSunshineTargetWith(admin, context.Background(), "device-b"); err != nil {
		t.Fatalf("revoke target: %v", err)
	}
	var updates, unpaired []string
	for _, record := range *records {
		if record.path == "/api/clients/update" {
			updates = append(updates, record.body["uuid"].(string))
		}
		if record.path == "/api/clients/unpair" {
			unpaired = append(unpaired, record.body["uuid"].(string))
		}
	}
	if len(updates) != 1 || updates[0] != "uuid-b" || len(unpaired) != 1 || unpaired[0] != "uuid-b" {
		t.Fatalf("target calls = %v %v", updates, unpaired)
	}
	if _, ok, _ := store.Get("device-b"); ok {
		t.Fatal("target enrollment survived revoke")
	}
	if uuid, ok, err := store.Get("device-a"); !ok || err != nil || uuid != "uuid-a" {
		t.Fatalf("other enrollment changed: %q %v %v", uuid, ok, err)
	}
	// An unrelated target makes no upstream calls at all.
	*records = (*records)[:0]
	if err := revokeSunshineTargetWith(admin, context.Background(), "device-c"); err != nil {
		t.Fatalf("unrelated revoke: %v", err)
	}
	for _, record := range *records {
		if record.path == "/api/clients/update" || record.path == "/api/clients/unpair" {
			t.Fatalf("unrelated target issued %s", record.path)
		}
	}
}

func TestAdminRequiresHttpsAndCredentials(t *testing.T) {
	if _, err := NewSunshineAdmin("http://127.0.0.1:47990", "zen", "secret", ""); err == nil {
		t.Fatal("plaintext admin accepted")
	}
	if _, err := NewSunshineAdmin("https://127.0.0.1:47990", "", "", ""); err == nil {
		t.Fatal("missing credentials accepted")
	}
}

func selfSignedPEM(t *testing.T, commonName string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func newCNOnlyAdmin(t *testing.T, handler http.HandlerFunc) (*SunshineAdmin, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Sunshine"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	pemText := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	certFile := filepath.Join(t.TempDir(), "sunshine.crt")
	if err := os.WriteFile(certFile, []byte(pemText), 0o600); err != nil {
		t.Fatal(err)
	}
	admin, err := NewSunshineAdmin(server.URL, "zen", "secret", certFile)
	if err != nil {
		t.Fatal(err)
	}
	return admin, certFile
}

func TestAdminAcceptsCNOnlyPinnedCertificate(t *testing.T) {
	admin, _ := newCNOnlyAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"csrf_token": "token-1"})
	})
	if _, err := admin.ListClients(context.Background()); err != nil {
		t.Fatalf("CN-only pinned certificate rejected: %v", err)
	}
}

func TestAdminRejectsChangedPinAndRedirects(t *testing.T) {
	admin, certFile := newCNOnlyAdmin(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"csrf_token": "token-1"})
	})
	// Replace the pin with a different certificate: the handshake must fail.
	if err := os.WriteFile(certFile, []byte(selfSignedPEM(t, "Other")), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := NewSunshineAdmin(admin.baseURL, "zen", "secret", certFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changed.ListClients(context.Background()); err == nil {
		t.Fatal("changed pinned certificate was accepted")
	}
	// Redirects never leave the pinned endpoint.
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.invalid/", http.StatusFound)
	}))
	t.Cleanup(redirect.Close)
	redirectCert := filepath.Join(t.TempDir(), "sunshine.crt")
	if err := os.WriteFile(redirectCert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: redirect.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	redirectAdmin, err := NewSunshineAdmin(redirect.URL, "zen", "secret", redirectCert)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := redirectAdmin.ListClients(context.Background()); err == nil {
		t.Fatal("redirected admin call was followed")
	}
}
