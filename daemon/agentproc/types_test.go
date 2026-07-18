package agentproc

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseMemoryLimit(t *testing.T) {
	const gib = uint64(1 << 30)
	for _, test := range []struct {
		value string
		total uint64
		want  uint64
	}{
		{value: "", total: 8 * gib, want: 0},
		{value: "4096", total: 8 * gib, want: 4096},
		{value: "4G", total: 8 * gib, want: 4 * gib},
		{value: "25%", total: 8 * gib, want: 2 * gib},
	} {
		got, err := ParseMemoryLimit(test.value, test.total)
		if err != nil {
			t.Fatalf("ParseMemoryLimit(%q): %v", test.value, err)
		}
		if got != test.want {
			t.Fatalf("ParseMemoryLimit(%q) = %d, want %d", test.value, got, test.want)
		}
	}
	for _, value := range []string{"-1", "101%", "wat", "18446744073709551615E"} {
		if _, err := ParseMemoryLimit(value, 8*gib); err == nil {
			t.Fatalf("ParseMemoryLimit(%q) unexpectedly succeeded", value)
		}
	}
}

func TestLeaseRoundTripAndListing(t *testing.T) {
	dir := t.TempDir()
	resourceID := "zen-agent-abc123-0123456789abcdef0123456789abcdef.scope"
	path, err := LeasePath(dir, resourceID)
	if err != nil {
		t.Fatal(err)
	}
	lease := Lease{
		Version:       1,
		ResourceID:    resourceID,
		BootID:        "boot",
		SupervisorPID: 100,
		RootPID:       101,
		SessionID:     100,
		ProcessGroup:  100,
		StartedAt:     time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC),
		MemoryHigh:    1024,
		MemoryMax:     2048,
		TasksMax:      64,
	}
	if err := writeLease(path, lease); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLease(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ResourceID != lease.ResourceID || got.SupervisorPID != lease.SupervisorPID || got.ProcessGroup != lease.ProcessGroup {
		t.Fatalf("lease = %+v, want %+v", got, lease)
	}
	leases, err := ListLeases(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0].ResourceID != resourceID {
		t.Fatalf("leases = %+v", leases)
	}
	if mode := fileMode(t, path); mode != 0o600 {
		t.Fatalf("lease mode = %o, want 600", mode)
	}
}

func TestLeasePathRejectsTraversal(t *testing.T) {
	for _, resourceID := range []string{"", "../escape", "unit/name", ".."} {
		if _, err := LeasePath(t.TempDir(), resourceID); err == nil {
			t.Fatalf("LeasePath accepted %q", resourceID)
		}
	}
}

func TestTrustedLeaseScopeRequiresLiveSupervisorIdentity(t *testing.T) {
	lease := Lease{
		ResourceID:    "zen-agent-abc123-0123456789abcdef0123456789abcdef.scope",
		SupervisorPID: 100,
		RootPID:       101,
		SessionID:     100,
		ProcessGroup:  100,
	}
	matching := map[int]processRecord{
		100: {PID: 100, PGID: 100, SID: 100, Args: "/usr/bin/zen agent __supervise --resource-id=" + lease.ResourceID},
	}
	if !trustedLeaseScope(matching, lease) {
		t.Fatal("matching supervisor was not trusted")
	}
	mismatched := map[int]processRecord{
		100: {PID: 100, PGID: 100, SID: 100, Args: "/usr/bin/unrelated"},
	}
	if trustedLeaseScope(mismatched, lease) {
		t.Fatal("reused supervisor pid was trusted")
	}
	orphanedGroup := map[int]processRecord{
		101: {PID: 101, PPID: 1, PGID: 100, SID: 100, Args: "/usr/bin/child"},
	}
	if !trustedLeaseScope(orphanedGroup, lease) {
		t.Fatal("still-allocated orphan process group was not trusted")
	}
}

func TestOwnedProcessesIncludesMarkedDetachedDescendants(t *testing.T) {
	lease := Lease{SupervisorPID: 100, RootPID: 101, SessionID: 100, ProcessGroup: 100}
	snapshot := map[int]processRecord{
		100: {PID: 100, PPID: 1, PGID: 100, SID: 100},
		101: {PID: 101, PPID: 100, PGID: 100, SID: 100},
		200: {PID: 200, PPID: 1, PGID: 200, SID: 200},
		201: {PID: 201, PPID: 200, PGID: 200, SID: 200},
		300: {PID: 300, PPID: 1, PGID: 300, SID: 300},
	}
	owned := ownedProcesses(snapshot, lease, false, map[int]bool{200: true})
	if !owned[200] || !owned[201] {
		t.Fatalf("marked detached tree not owned: %+v", owned)
	}
	if owned[100] || owned[101] || owned[300] {
		t.Fatalf("untrusted or unrelated processes were owned: %+v", owned)
	}
	owned = ownedProcesses(snapshot, lease, true, nil)
	if !owned[100] || !owned[101] || owned[200] || owned[300] {
		t.Fatalf("trusted scope ownership = %+v", owned)
	}
}

func TestOwnedProcessesDoesNotAdoptExternalSessionLeader(t *testing.T) {
	lease := Lease{SupervisorPID: 100, RootPID: 101, SessionID: 50, ProcessGroup: 100}
	snapshot := map[int]processRecord{
		50:  {PID: 50, PPID: 1, PGID: 50, SID: 50},
		100: {PID: 100, PPID: 50, PGID: 100, SID: 50},
		101: {PID: 101, PPID: 100, PGID: 100, SID: 50},
	}
	owned := ownedProcesses(snapshot, lease, true, nil)
	if owned[50] || !owned[100] || !owned[101] {
		t.Fatalf("ownership crossed external session leader: %+v", owned)
	}
}

func TestStopLeaseRemovesOldBootMetadataWithoutSignaling(t *testing.T) {
	dir := t.TempDir()
	resourceID := "zen-agent-abc123-fedcba9876543210fedcba9876543210.scope"
	path, err := LeasePath(dir, resourceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLease(path, Lease{
		Version:       1,
		ResourceID:    resourceID,
		BootID:        "definitely-not-the-current-boot",
		SupervisorPID: 2,
		RootPID:       3,
		SessionID:     2,
		ProcessGroup:  2,
	}); err != nil {
		t.Fatal(err)
	}
	if err := StopLease(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("old-boot lease still exists: %v", err)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
