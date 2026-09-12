package server

import (
	"testing"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop/host"
)

func stubMoonlight(t *testing.T, snapshot host.SunshineRuntimeSnapshot, ensureErr error) *int {
	t.Helper()
	calls := 0
	previousSnapshot, previousEnsure := moonlightSnapshot, moonlightEnsure
	moonlightSnapshot = func() host.SunshineRuntimeSnapshot { return snapshot }
	moonlightEnsure = func(host.SunshineSpawner) (host.SunshineRuntimeSnapshot, error) {
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
	})
	return &calls
}

func TestMoonlightBootstrapRequiresExplicitConfiguration(t *testing.T) {
	calls := stubMoonlight(t, host.SunshineRuntimeSnapshot{}, nil)
	if got := moonlightBootstrap(&auth.TrustedDevice{ID: "dev-1"}, "192.0.2.5:9876"); got != nil {
		t.Fatalf("unconfigured bootstrap = %+v", got)
	}
	if *calls != 0 {
		t.Fatalf("ensure called %d times for unconfigured host", *calls)
	}
}

func TestMoonlightBootstrapBindsZenIdentityAndHostKey(t *testing.T) {
	calls := stubMoonlight(t, host.SunshineRuntimeSnapshot{
		Configured: true,
		HostKey:    "zen-host-1",
		HTTPPort:   47989,
		HTTPSPort:  47984,
		AppID:      3,
	}, nil)
	got := moonlightBootstrap(&auth.TrustedDevice{ID: "dev-42"}, "192.0.2.5:9876")
	if got == nil {
		t.Fatal("expected bootstrap")
	}
	if got["identity_key"] != "dev-42" || got["host_key"] != "zen-host-1" {
		t.Fatalf("bootstrap = %+v", got)
	}
	if got["host"] != "192.0.2.5" || got["http_port"] != 47989 || got["https_port"] != 47984 || got["app_id"] != 3 {
		t.Fatalf("bootstrap = %+v", got)
	}
	if got["available"] != true {
		t.Fatalf("available = %v", got["available"])
	}
	if *calls != 1 {
		t.Fatalf("ensure called %d times", *calls)
	}
}

func TestMoonlightBootstrapUnavailableWhenRuntimeCannotStart(t *testing.T) {
	stubMoonlight(t, host.SunshineRuntimeSnapshot{
		Configured: true,
		HostKey:    "zen-host-1",
		HTTPPort:   47989,
		HTTPSPort:  47984,
	}, errRuntimeUnavailable{})
	got := moonlightBootstrap(&auth.TrustedDevice{ID: "dev-42"}, "192.0.2.5:9876")
	if got == nil || got["available"] != false {
		t.Fatalf("bootstrap = %+v", got)
	}
}

type errRuntimeUnavailable struct{}

func (errRuntimeUnavailable) Error() string { return "runtime unavailable" }
