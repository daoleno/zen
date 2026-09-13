package server

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop/host"
)

type fakeDesktopAuthorization struct {
	reconciles []host.AuthorizationInput
	releases   int
	status     host.AuthorizationStatus
}

func (f *fakeDesktopAuthorization) Reconcile(_ context.Context, input host.AuthorizationInput) host.AuthorizationStatus {
	f.reconciles = append(f.reconciles, input)
	return f.status
}

func (f *fakeDesktopAuthorization) Release() host.AuthorizationStatus {
	f.releases++
	return f.status
}

func (f *fakeDesktopAuthorization) Status() host.AuthorizationStatus { return f.status }

func awaitAuthorizationWake(t *testing.T, srv *Server) {
	t.Helper()
	select {
	case <-srv.desktopAuthorizationWake:
	case <-time.After(time.Second):
		t.Fatal("desktop authorization was not nudged")
	}
}

func stubReadiness(t *testing.T, readiness host.Readiness) {
	t.Helper()
	previous := inspectHostReadiness
	inspectHostReadiness = func() host.Readiness { return readiness }
	t.Cleanup(func() { inspectHostReadiness = previous })
}

func stubSunshineConfigured(t *testing.T, configured bool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sunshine.json")
	if configured {
		body, err := json.Marshal(map[string]any{
			"binary_path": "/usr/libexec/zen/sunshine",
			"state_dir":   filepath.Join(t.TempDir(), "sunshine"),
			"host_key":    "zen-host-1",
			"http_port":   47989,
			"app_id":      1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ZEN_SUNSHINE_CONFIG", path)
}

func TestDesktopCapabilityReportsAuthorizationStatus(t *testing.T) {
	manager, _, _ := sessionFileAuthFixture(t)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	updated := time.Now().UTC()
	s.EnableDesktopAuthorization(&fakeDesktopAuthorization{status: host.AuthorizationStatus{
		Version: 1, Active: true, Reason: "authorization_active", AuthorizedDevices: 1,
		Inhibitors: host.InhibitorStatus{Active: true, IdleInhibited: true, SuspendInhibited: true, LockInhibited: true},
		UpdatedAt:  updated,
	}})
	payload := s.desktopCapability(context.Background(), &auth.TrustedDevice{ID: "dev-1", PublicKeyHex: manager.PublicKeyHex()}, desktopIngress{})
	hostBlock, ok := payload["host"].(map[string]any)
	if !ok {
		t.Fatalf("host block missing: %+v", payload["host"])
	}
	block, ok := hostBlock["authorization"].(map[string]any)
	if !ok {
		t.Fatalf("authorization block missing: %+v", hostBlock)
	}
	if block["active"] != true || block["reason"] != "authorization_active" || block["lock_inhibited"] != true || block["suspend_inhibited"] != true || block["idle_inhibited"] != true {
		t.Fatalf("authorization block = %+v", block)
	}
	if block["authorized_devices"] != 1 {
		t.Fatalf("authorized_devices = %v", block["authorized_devices"])
	}
	if block["updated_at"] != updated.Format(time.RFC3339) {
		t.Fatalf("updated_at = %v", block["updated_at"])
	}
}

func TestReconcileDesktopAuthorizationComputesCanonicalInput(t *testing.T) {
	manager, key, deviceID := sessionFileAuthFixture(t)
	devicePublicKey := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	if _, err := manager.GrantDesktopScope(deviceID, devicePublicKey, auth.DesktopScopeVersion); err != nil {
		t.Fatalf("grant scope: %v", err)
	}
	stubReadiness(t, host.Readiness{Status: host.ReadinessReady, Broker: true})
	stubSunshineConfigured(t, true)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	controller := &fakeDesktopAuthorization{status: host.AuthorizationStatus{Active: true, Reason: "authorization_active"}}
	s.EnableDesktopAuthorization(controller)
	status := s.reconcileDesktopAuthorization(context.Background())
	if status.Reason != "authorization_active" || !status.Active {
		t.Fatalf("status = %+v", status)
	}
	if len(controller.reconciles) != 1 {
		t.Fatalf("reconciles = %d", len(controller.reconciles))
	}
	input := controller.reconciles[0]
	if input.AuthorizedDevices != 1 || !input.HostConfigured || !input.Reachable {
		t.Fatalf("input = %+v", input)
	}
}

func TestDeviceRevocationRecomputesAuthorization(t *testing.T) {
	manager, key, deviceID := sessionFileAuthFixture(t)
	if _, err := manager.GrantDesktopScope(deviceID, hex.EncodeToString(key.Public().(ed25519.PublicKey)), auth.DesktopScopeVersion); err != nil {
		t.Fatalf("grant scope: %v", err)
	}
	stubReadiness(t, host.Readiness{Status: host.ReadinessReady, Broker: true})
	stubSunshineConfigured(t, true)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	controller := &fakeDesktopAuthorization{}
	s.EnableDesktopAuthorization(controller)
	if _, err := manager.RevokeDevice(deviceID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	awaitAuthorizationWake(t, s)
	s.reconcileDesktopAuthorization(context.Background())
	if len(controller.reconciles) != 1 || controller.reconciles[0].AuthorizedDevices != 0 {
		t.Fatalf("input after revoke = %+v", controller.reconciles)
	}
}

func TestDesktopAuthorizationLoopReleasesOnShutdown(t *testing.T) {
	manager, _, _ := sessionFileAuthFixture(t)
	stubReadiness(t, host.Readiness{Status: host.ReadinessReady, Broker: true})
	stubSunshineConfigured(t, false)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	controller := &fakeDesktopAuthorization{}
	s.EnableDesktopAuthorization(controller)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runDesktopAuthorization(ctx)
	}()
	// The loop reconciles once immediately with no authorized device.
	deadline := time.After(2 * time.Second)
	for len(controller.reconciles) == 0 {
		select {
		case <-deadline:
			t.Fatal("initial reconcile did not run")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("authorization loop did not stop")
	}
	if controller.releases != 1 {
		t.Fatalf("releases = %d", controller.releases)
	}
}

func TestAuthorizationCapabilityBlockWithoutController(t *testing.T) {
	manager, _, _ := sessionFileAuthFixture(t)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	payload := s.desktopCapability(context.Background(), &auth.TrustedDevice{ID: "dev-1", PublicKeyHex: manager.PublicKeyHex()}, desktopIngress{})
	hostBlock, _ := payload["host"].(map[string]any)
	if _, present := hostBlock["authorization"]; present {
		t.Fatal("authorization block present without a controller")
	}
}

func TestDesktopCapabilityAdvertisesConfiguredHostOutsideSession(t *testing.T) {
	manager, key, deviceID := sessionFileAuthFixture(t)
	devicePublicKey := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	if _, err := manager.GrantDesktopScope(deviceID, devicePublicKey, auth.DesktopScopeVersion); err != nil {
		t.Fatalf("grant scope: %v", err)
	}
	// The daemon was started over SSH: no DISPLAY and no broker socket, but the
	// supervised host is explicitly configured.
	stubReadiness(t, host.Readiness{Status: host.ReadinessSetupRequired, Surface: string(host.Unavailable)})
	stubSunshineConfigured(t, true)
	stubMoonlight(t, host.SunshineRuntimeSnapshot{
		Configured: true, Running: true, HostKey: "zen-host-1", HTTPPort: 47989, HTTPSPort: 47984, AppID: 1,
	}, nil)
	s := New(manager, nil, nil, nil, nil, nil, nil)
	defer s.shutdownAuthenticatedClients()
	payload := s.desktopCapability(context.Background(), &auth.TrustedDevice{ID: deviceID, PublicKeyHex: devicePublicKey}, desktopIngress{Trusted: true})
	connect, _ := payload["connect"].(map[string]any)
	if connect["unattended"] != true || connect["reason"] != "" {
		t.Fatalf("connect = %+v", connect)
	}
	if _, ok := payload["moonlight"]; !ok {
		t.Fatalf("configured host was not advertised: %+v", payload)
	}
}
