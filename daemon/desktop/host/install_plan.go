package host

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type PlannedFile struct {
	Path    string
	Mode    uint32
	Content string
}

type InstallPlan struct {
	Files        []PlannedFile
	Requirements []string
}

// PrepareLinuxInstall only renders a reviewable manifest. It never writes to
// the host or invokes systemctl, SDDM, sudo, polkit, login, or an input device.
func PrepareLinuxInstall(config HostConfig) (InstallPlan, error) {
	if err := config.Validate(); err != nil {
		return InstallPlan{}, err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return InstallPlan{}, err
	}
	plan := InstallPlan{Files: []PlannedFile{
		{Path: "/etc/zen/desktop-host.json", Mode: 0600, Content: string(data) + "\n"},
		{Path: "/usr/lib/systemd/system/zen-desktop-host.service", Mode: 0644, Content: `[Unit]
Description=Zen desktop privilege broker
After=systemd-logind.service
Requires=systemd-logind.service
Before=display-manager.service

[Service]
Type=simple
User=root
ExecStart=/usr/libexec/zen/zen desktop-host --config /etc/zen/desktop-host.json
RuntimeDirectory=zen-desktop
RuntimeDirectoryMode=0711
UMask=0077
NoNewPrivileges=yes
CapabilityBoundingSet=CAP_SETUID CAP_SETGID CAP_DAC_READ_SEARCH CAP_CHOWN CAP_KILL
AmbientCapabilities=CAP_SETUID
RestrictAddressFamilies=AF_UNIX
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=no
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
LockPersonality=yes
LimitCORE=0
TasksMax=32
MemoryMax=512M
CPUQuota=100%
KillMode=control-group
TimeoutStopSec=5
Restart=on-failure

[Install]
WantedBy=multi-user.target
`},
	}, Requirements: []string{
		"Build and audit the executable broker and UID-dropped X11 agent; rendered service files alone are not an installation.",
		"Verify root-owned immutable binaries and all parent directories; no user-writable executable, helper path or library search path. The installed zen ELF is the only broker/agent identity.",
		"Verify kernel peer UID with fresh device/scope proof, including synchronous revocation. When ownerUnit is configured, additionally require membership in that root-enrolled system unit's cgroup.",
		"Create the broker socket mode 0600 owned by the configured owner UID inside the root-owned runtime directory; keep all other runtime state root-only.",
		"Approve boot service ownership for the existing unprivileged Zen daemon with unchanged identity, state directory and network settings; do not start a duplicate owner.",
		"Use the journaled installer to preserve existing SDDM X11 display hooks and register verified display metadata with an Xauthority FD, never cookies in logs or user config.",
		"Test installation, rollback, lock, logout, reboot and greeter-to-owner handoff only in an owned disposable SDDM VM before personal-host installation approval.",
		"X11-only privilege profile: Wayland DRM capture or uinput requires a separately reviewed device/capability profile, not a silent CAP_SYS_ADMIN addition.",
	}}
	if config.OwnerUnit != "" {
		// Wants (not Requires): the broker starts after the owner and pulls
		// it in at boot, but an owner restart/rebuild must NOT stop the
		// broker — stopping would discard the in-memory X registration and
		// blind the broker until the next X start. Verified live: with
		// Requires, `systemctl restart <owner>` stopped the broker and all
		// later admissions failed session_observation.
		plan.Files[1].Content = strings.Replace(plan.Files[1].Content, "Requires=systemd-logind.service", "Requires=systemd-logind.service\nWants="+config.OwnerUnit+"\nAfter="+config.OwnerUnit, 1)
	}
	return plan, nil
}

// PrintInstallPlan writes the reviewed manifest. It is not an installation.
func PrintInstallPlan(w io.Writer, plan InstallPlan) {
	fmt.Fprintln(w, "Zen desktop-host install plan (not applied)")
	fmt.Fprintln(w, "This does not write files, start systemd, change SDDM, or capture a display.")
	fmt.Fprintln(w, "Current-session desktop does not use this plan. Lock/login after reboot does.")
	for _, file := range plan.Files {
		fmt.Fprintf(w, "\nFILE %s mode=%04o\n%s", file.Path, file.Mode, file.Content)
	}
	fmt.Fprintln(w, "\nREQUIREMENTS")
	for _, requirement := range plan.Requirements {
		fmt.Fprintf(w, "- %s\n", requirement)
	}
}
