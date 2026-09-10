package host

import (
	"strings"
	"testing"
)

func TestInstallPlanHasNoDeviceTrustToggleOrCredentialStore(t *testing.T) {
	plan, err := PrepareLinuxInstall(HostConfig{Version: 1, HostID: "fixture", OwnerUID: 1000, Seat: "seat0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 2 || len(plan.Requirements) < 6 {
		t.Fatal("incomplete preparation")
	}
	for _, file := range plan.Files {
		for _, forbidden := range []string{"Autologin", "UnlockSession", "CAP_SYS_ADMIN", "/dev/uinput", "password", "grants", "enabled", "trusted-devices"} {
			if strings.Contains(file.Content, forbidden) {
				t.Fatalf("unexpected installation side effect %q", forbidden)
			}
		}
	}
	if plan.Files[0].Mode != 0600 || !strings.Contains(plan.Files[1].Content, "User=root\n") || !strings.Contains(plan.Files[1].Content, "RestrictAddressFamilies=AF_UNIX\n") {
		t.Fatal("privilege boundary missing")
	}
	if !strings.Contains(plan.Files[1].Content, "ExecStart=/usr/libexec/zen/zen desktop-host --config /etc/zen/desktop-host.json") {
		t.Fatal("same-binary desktop-host role missing")
	}
	var rendered strings.Builder
	PrintInstallPlan(&rendered, plan)
	if !strings.Contains(rendered.String(), "not applied") || !strings.Contains(rendered.String(), "Current-session desktop does not use this plan") {
		t.Fatal("plan must stay review-only")
	}
	if !strings.Contains(plan.Files[1].Content, "AmbientCapabilities=CAP_SETUID\n") || !strings.Contains(plan.Files[1].Content, "CAP_KILL") || !strings.Contains(plan.Files[1].Content, "Before=display-manager.service") {
		t.Fatal("boot ordering or dropped-agent lifecycle capability missing")
	}
	if _, err := PrepareLinuxInstall(HostConfig{Version: 1, HostID: "fixture", OwnerUID: 0, Seat: "seat0"}); err == nil {
		t.Fatal("root owner allowed")
	}
}

// The broker must order after the enrolled owner without being killed by its
// restarts: Requires= propagates a stop (losing in-memory X registration),
// Wants= only pulls the owner in at boot.
func TestInstallPlanOwnerDependencyIsWeak(t *testing.T) {
	plan, err := PrepareLinuxInstall(HostConfig{Version: 1, HostID: "fixture", OwnerUID: 1000, Seat: "seat0", OwnerUnit: "zen-fixture-owner.service"})
	if err != nil {
		t.Fatal(err)
	}
	content := plan.Files[1].Content
	if !strings.Contains(content, "Wants=zen-fixture-owner.service\n") || !strings.Contains(content, "After=zen-fixture-owner.service") {
		t.Fatal("owner boot ordering missing")
	}
	if strings.Contains(content, "Requires=zen-fixture-owner.service") || strings.Contains(content, "BindsTo=zen-fixture-owner.service") {
		t.Fatal("strong owner dependency stops the broker on owner rebuild")
	}
}
