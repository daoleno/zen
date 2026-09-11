package host

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SYSTEM owner contract (optional unit scope): the unattended desktop broker
// admits members of the enrolled system unit's cgroup carrying the enrolled
// UID. The real owner is a watcher-spawned daemon child (zen-dev starts the
// daemon with exec.Command/Start and rebuilds it in place), so peerPID ==
// MainPID must NOT be required: it would reject every legitimate rebuild while
// adding nothing over the cgroup proof. The unit's current MainPID only proves
// the unit is active. A user-manager unit is never canonical, and an unrelated
// same-UID process outside the enrolled cgroup is rejected. Generation,
// challenge, device and scope authority apply unchanged on top.
//
// ACCOUNT owner contract (default when no unit is configured): the kernel peer
// must carry the enrolled UID and nothing else is required. The broker socket
// is mode 0600 owned by that UID, so other accounts cannot connect; processes
// running as the owner account are inside that account's trust domain because
// the daemon state (~/.zen) and its identity key are user-owned and readable by
// the same UID. The one-use signed challenge, generation/session binding and
// device/scope/revocation checks still apply on top. Process-level exclusivity
// against same-UID outsiders is not claimed in this mode; configure ownerUnit
// to opt into the unit scope when a root-enrolled system unit exists.

// validSystemUnitCgroup reports whether a systemd ControlGroup path can host
// the canonical owner. Only the system slice qualifies; user-manager cgroups
// (user.slice, user-*.slice) and the root scope never do.
func validSystemUnitCgroup(group string) bool {
	if group == "" || strings.Contains(group, "..") || !filepath.IsAbs(group) {
		return false
	}
	return group == "/system.slice" || strings.HasPrefix(group, "/system.slice/")
}

// ownerCgroupMember reports whether pid currently lives in the enrolled
// unit's cgroup. procRoot is /proc in production and a fixture in tests.
func ownerCgroupMember(procRoot string, pid uint32, group string) bool {
	if pid == 0 || !validSystemUnitCgroup(group) {
		return false
	}
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.FormatUint(uint64(pid), 10), "cgroup"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 3 {
			continue
		}
		path := fields[2]
		if path == "" {
			path = "/"
		}
		if path == group || strings.HasPrefix(path, group+"/") {
			return true
		}
	}
	return false
}

// verifyCanonicalAccount is the account-scope owner decision: the kernel peer
// must carry the enrolled UID. See the package comment above for the exact
// boundary enforced in this mode.
func verifyCanonicalAccount(peerPid int32, peerUid uint32, ownerUID uint32) bool {
	return peerPid > 0 && ownerUID != 0 && peerUid == ownerUID
}

// verifyCanonicalOwner is the optional unit-scope decision: the kernel peer
// must carry the enrolled UID, the enrolled unit must be active (non-zero
// MainPID), and the peer must live in the enrolled system unit's cgroup.
// Watcher-spawned daemon children and in-place rebuilds are admitted by
// cgroup membership; unrelated same-UID processes and foreign/replaced units
// fail closed.
func verifyCanonicalOwner(peerPid int32, peerUid uint32, mainPID uint32, ownerUID uint32, unitCgroup string, cgroupMember bool) bool {
	if peerPid <= 0 || mainPID == 0 || ownerUID == 0 {
		return false
	}
	if peerUid != ownerUID {
		return false
	}
	if !validSystemUnitCgroup(unitCgroup) || !cgroupMember {
		return false
	}
	return true
}
