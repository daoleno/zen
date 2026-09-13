package server

import (
	"context"
	"errors"
	"testing"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop/host"
)

func stubMoonlight(t *testing.T, snapshot host.SunshineRuntimeSnapshot, ensureErr error) *int {
	return stubMoonlightWithAvailability(t, snapshot, ensureErr, false)
}

func stubMoonlightWithAvailability(t *testing.T, snapshot host.SunshineRuntimeSnapshot, ensureErr error, available bool) *int {
	t.Helper()
	calls := 0
	previousSnapshot, previousEnsure, previousAvailable := moonlightSnapshot, moonlightEnsure, moonlightAvailable
	moonlightSnapshot = func() host.SunshineRuntimeSnapshot { return snapshot }
	moonlightAvailable = func() bool { return available }
	moonlightEnsure = func(context.Context, host.SunshineSpawner) (host.SunshineRuntimeSnapshot, error) {
		calls++
		if ensureErr != nil {
			return snapshot, ensureErr
		}
		snapshot.Running = true
		return snapshot, nil
	}
	t.Cleanup(func() {
		moonlightSnapshot = previousSnapshot
		moonlightEnsure = previousEnsure
		moonlightAvailable = previousAvailable
	})
	return &calls
}

func TestMoonlightBootstrapRequiresExplicitConfiguration(t *testing.T) {
	calls := stubMoonlight(t, host.SunshineRuntimeSnapshot{}, nil)
	if got := moonlightBootstrap(context.Background(), &auth.TrustedDevice{ID: "dev-1"}); got != nil {
		t.Fatalf("unconfigured bootstrap = %+v", got)
	}
	if *calls != 0 {
		t.Fatalf("ensure called %d times for unconfigured host", *calls)
	}
}

func TestMoonlightBootstrapBindsZenIdentityAndHostKey(t *testing.T) {
	calls := stubMoonlightWithAvailability(t, host.SunshineRuntimeSnapshot{
		Configured: true,
		HostKey:    "zen-host-1",
		HTTPPort:   47989,
		HTTPSPort:  47984,
		AppID:      3,
	}, nil, true)
	got := moonlightBootstrap(context.Background(), &auth.TrustedDevice{ID: "dev-42"})
	if got == nil {
		t.Fatal("expected bootstrap")
	}
	if got["identity_key"] != "dev-42" || got["host_key"] != "zen-host-1" {
		t.Fatalf("bootstrap = %+v", got)
	}
	if _, hasHost := got["host"]; hasHost {
		t.Fatalf("block must not carry a request-derived host: %+v", got)
	}
	if got["http_port"] != 47989 || got["https_port"] != 47984 || got["app_id"] != 3 {
		t.Fatalf("bootstrap = %+v", got)
	}
	if got["available"] != true || got["reason"] != "" {
		t.Fatalf("availability = %v reason=%v", got["available"], got["reason"])
	}
	if *calls != 1 {
		t.Fatalf("ensure called %d times", *calls)
	}
}

func TestMoonlightBootstrapStaysClosedWithoutAdminBinding(t *testing.T) {
	stubMoonlightWithAvailability(t, host.SunshineRuntimeSnapshot{
		Configured: true,
		HostKey:    "zen-host-1",
		HTTPPort:   47989,
		HTTPSPort:  47984,
	}, nil, false)
	got := moonlightBootstrap(context.Background(), &auth.TrustedDevice{ID: "dev-42"})
	if got == nil || got["available"] != false || got["reason"] != "admin_binding_unavailable" {
		t.Fatalf("bootstrap = %+v", got)
	}
}

func TestMoonlightBootstrapUnavailableWhenRuntimeCannotStart(t *testing.T) {
	stubMoonlight(t, host.SunshineRuntimeSnapshot{
		Configured: true,
		HostKey:    "zen-host-1",
		HTTPPort:   47989,
		HTTPSPort:  47984,
	}, errRuntimeUnavailable{})
	got := moonlightBootstrap(context.Background(), &auth.TrustedDevice{ID: "dev-42"})
	if got == nil || got["available"] != false {
		t.Fatalf("bootstrap = %+v", got)
	}
}

type errRuntimeUnavailable struct{}

func (errRuntimeUnavailable) Error() string { return "runtime unavailable" }

func stubRevocationHooks(t *testing.T, enrolled map[string]bool, calls *int, err error) {
	t.Helper()
	previousEnrollment := moonlightEnrollment
	previousTarget := moonlightRevokeTarget
	moonlightEnrollment = func(deviceID string) (string, bool, error) {
		return "uuid-" + deviceID, enrolled[deviceID], nil
	}
	moonlightRevokeTarget = func(ctx context.Context, deviceID string) error {
		if ctx == nil || ctx.Err() != nil {
			t.Fatal("revoke hook called without a live context")
		}
		*calls++
		return err
	}
	t.Cleanup(func() {
		moonlightEnrollment = previousEnrollment
		moonlightRevokeTarget = previousTarget
	})
}

func TestDeviceRevocationTargetsOnlyTheEnrolledOwner(t *testing.T) {
	calls := 0
	stubRevocationHooks(t, map[string]bool{"device-a": true}, &calls, nil)

	// An unrelated target must not touch the owner's engine or pairing.
	if err := revokeSunshineForDeviceRevocation(context.Background(), "device-b"); err != nil {
		t.Fatalf("unrelated revoke: %v", err)
	}
	if calls != 0 {
		t.Fatalf("unrelated target revoked the engine: calls=%d", calls)
	}
	// The enrolled owner revokes its own engine.
	if err := revokeSunshineForDeviceRevocation(context.Background(), "device-a"); err != nil {
		t.Fatalf("owner revoke: %v", err)
	}
	if calls != 1 {
		t.Fatalf("owner revoke hook calls = %d", calls)
	}
}

func TestDeviceRevocationWithoutEnrollmentDoesNothing(t *testing.T) {
	calls := 0
	stubRevocationHooks(t, map[string]bool{}, &calls, nil)
	if err := revokeSunshineForDeviceRevocation(context.Background(), "device-a"); err != nil {
		t.Fatalf("un-enrolled revoke: %v", err)
	}
	if calls != 0 {
		t.Fatalf("un-enrolled target revoked the engine: calls=%d", calls)
	}
}

func TestDeviceRevocationSurfacesEngineFailure(t *testing.T) {
	calls := 0
	stubRevocationHooks(t, map[string]bool{"device-a": true}, &calls, errors.New("engine busy"))
	if err := revokeSunshineForDeviceRevocation(context.Background(), "device-a"); err == nil {
		t.Fatal("engine revoke failure was swallowed")
	}
}
