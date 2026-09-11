package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrokerMayLaunchAgentRefusesUserDEVWithoutRootInstall(t *testing.T) {
	// Isolated policy proof: do not install on the personal SDDM/root host.
	if os.Geteuid() == 0 {
		t.Skip("this fixture is the non-root path; root inode matching needs an owned VM")
	}
	file, err := os.CreateTemp(t.TempDir(), "fixture-zen")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	err = BrokerMayLaunchAgent(file)
	if err == nil || !(err.Error() == "broker_not_root" || err.Error() == "user_writable_broker_refused" || err.Error() == "broker_not_installed_binary") {
		t.Fatalf("user-writable/non-root broker must be refused, got %v", err)
	}
	if !ForbiddenBrokerPath(filepath.Join(t.TempDir(), "zen-dev")) {
		t.Fatal("temp zen-dev path must be forbidden")
	}
}

func TestInstallPlanSameBinaryExecStart(t *testing.T) {
	plan, err := PrepareLinuxInstall(HostConfig{Version: 1, HostID: "fixture", OwnerUID: 1000, Seat: "seat0"})
	if err != nil {
		t.Fatal(err)
	}
	service := plan.Files[1].Content
	if !strings.Contains(service, "ExecStart=/usr/libexec/zen/zen desktop-host --config /etc/zen/desktop-host.json") {
		t.Fatal(service)
	}
	if strings.Contains(service, "zen-desktop-agent") || strings.Contains(service, "ZEN_DESKTOP_HELPER") {
		t.Fatal("separate helper/agent path leaked into the unit")
	}
}
