package watcher

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func requireLinuxManagedServices(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("persistent service registration requires Linux user systemd")
	}
}

func testManagedWatcher(t *testing.T, registry string) *Watcher {
	t.Helper()
	w := New(500 * time.Millisecond)
	w.SetManagedServicesPath(registry)
	w.systemctlShowFn = func(unit string) (systemdUnitStatus, error) {
		return systemdUnitStatus{}, os.ErrNotExist
	}
	w.unitCgroupFn = func(pid int, unit string) (bool, error) { return false, nil }
	w.listSocketsFn = func() ([]listeningSocket, error) { return nil, nil }
	return w
}

func TestValidateManagedServiceUnit(t *testing.T) {
	for _, unit := range []string{"dsh-web.service", "zen-worker-abc.scope.service", "a@b.service", "x-y_z:1.service"} {
		if err := ValidateManagedServiceUnit(unit); err != nil {
			t.Fatalf("ValidateManagedServiceUnit(%q) error = %v", unit, err)
		}
	}
	for _, unit := range []string{"", "../x.service", "/etc/x.service", "x; reboot", "x.timer", "x service"} {
		if err := ValidateManagedServiceUnit(unit); err == nil {
			t.Fatalf("ValidateManagedServiceUnit(%q) = nil, want error", unit)
		}
	}
}

func TestRegisterManagedServiceRoundTrip(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(unit string) (systemdUnitStatus, error) {
		return systemdUnitStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running", MainPID: 4242}, nil
	}
	got, err := w.RegisterManagedService(ManagedServiceDescriptor{
		Unit: "dsh-web.service", Name: "DeepSeek Harness", Project: "dsh", Port: 3080,
		RegisteredBy: "main:@1", WorkID: "dde7dc7f",
	})
	if err != nil {
		t.Fatalf("RegisterManagedService error = %v", err)
	}
	if got.RegisteredAt.IsZero() {
		t.Fatalf("RegisterManagedService did not stamp registration time")
	}
	listed, err := w.ListManagedServices()
	if err != nil {
		t.Fatalf("ListManagedServices error = %v", err)
	}
	if len(listed) != 1 || listed[0].Unit != "dsh-web.service" || listed[0].Port != 3080 || listed[0].RegisteredBy != "main:@1" {
		t.Fatalf("ListManagedServices = %#v, want one dsh-web row", listed)
	}
	// Re-registration replaces in place and preserves the first timestamp.
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "Renamed"}); err != nil {
		t.Fatalf("re-register error = %v", err)
	}
	listed, _ = w.ListManagedServices()
	if len(listed) != 1 || listed[0].Name != "Renamed" || !listed[0].RegisteredAt.Equal(got.RegisteredAt) {
		t.Fatalf("re-register = %#v, want single renamed row with original timestamp", listed)
	}
	if err := w.UnregisterManagedService("dsh-web.service"); err != nil {
		t.Fatalf("UnregisterManagedService error = %v", err)
	}
	listed, _ = w.ListManagedServices()
	if len(listed) != 0 {
		t.Fatalf("after unregister ListManagedServices = %#v, want empty", listed)
	}
	if err := w.UnregisterManagedService("dsh-web.service"); err == nil {
		t.Fatalf("second unregister = nil, want error")
	}
}

func TestRegisterManagedServiceRejectsUnknownUnit(t *testing.T) {
	requireLinuxManagedServices(t)
	w := testManagedWatcher(t, filepath.Join(t.TempDir(), "managed-services.json"))
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "nope.service", Name: "Nope"}); err == nil {
		t.Fatalf("RegisterManagedService(unknown unit) = nil, want error")
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "../evil.service", Name: "Evil"}); err == nil {
		t.Fatalf("RegisterManagedService(path traversal) = nil, want error")
	}
	if _, err := os.Stat(filepath.Join(t.TempDir(), "managed-services.json")); !os.IsNotExist(err) {
		t.Fatalf("failed registration must not create a registry")
	}
}

func TestDiscoverPersistentActiveViaMainPID(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(unit string) (systemdUnitStatus, error) {
		return systemdUnitStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running", MainPID: 1610722}, nil
	}
	w.listSocketsFn = func() ([]listeningSocket, error) {
		return []listeningSocket{{pid: 1610722, port: 3080, bind: "127.0.0.1"}}, nil
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "DeepSeek Harness", Project: "dsh", Port: 3080}); err != nil {
		t.Fatalf("register error = %v", err)
	}
	rows := w.discoverPersistentServices(map[string]bool{}, nil)
	if len(rows) != 1 {
		t.Fatalf("discoverPersistentServices = %#v, want one row", rows)
	}
	row := rows[0]
	if row.Source != ServiceSourcePersistent || row.Unit != "dsh-web.service" || row.State != ServiceStateActive {
		t.Fatalf("row identity = %+v, want persistent/dsh-web.service/active", row)
	}
	if row.WorkerID != "" {
		t.Fatalf("persistent row WorkerID = %q, want empty (no invented worker)", row.WorkerID)
	}
	if row.WorkerName != "DeepSeek Harness" || row.Port != 3080 || row.PID != 1610722 {
		t.Fatalf("row = %+v, want DeepSeek Harness :3080 pid 1610722", row)
	}
	if !row.LocalOnly || len(row.URLs) != 0 {
		t.Fatalf("loopback-only row must be local_only with no LAN URLs, got %+v", row)
	}
}

func TestDiscoverPersistentRejectsForeignCgroupLookalike(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(unit string) (systemdUnitStatus, error) {
		return systemdUnitStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running", MainPID: 111}, nil
	}
	// Same port, but the listener is neither MainPID nor in the unit cgroup.
	w.listSocketsFn = func() ([]listeningSocket, error) {
		return []listeningSocket{{pid: 999, port: 3080, bind: "127.0.0.1"}}, nil
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "DeepSeek Harness", Port: 3080}); err != nil {
		t.Fatalf("register error = %v", err)
	}
	rows := w.discoverPersistentServices(map[string]bool{}, nil)
	if len(rows) != 1 {
		t.Fatalf("discoverPersistentServices = %#v, want one row", rows)
	}
	if rows[0].State != ServiceStateInactive {
		t.Fatalf("foreign-cgroup lookalike row = %+v, want inactive (no stale positive)", rows[0])
	}
	if rows[0].PID != 0 {
		t.Fatalf("inactive row PID = %d, want 0", rows[0].PID)
	}
}

func TestDiscoverPersistentAcceptsCgroupChildPID(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(unit string) (systemdUnitStatus, error) {
		// Restarted unit: new MainPID, old PID gone.
		return systemdUnitStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running", MainPID: 555}, nil
	}
	w.unitCgroupFn = func(pid int, unit string) (bool, error) { return pid == 556 && unit == "dsh-web.service", nil }
	w.listSocketsFn = func() ([]listeningSocket, error) {
		return []listeningSocket{{pid: 556, port: 3080, bind: "127.0.0.1"}}, nil
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "DeepSeek Harness", Port: 3080}); err != nil {
		t.Fatalf("register error = %v", err)
	}
	rows := w.discoverPersistentServices(map[string]bool{}, nil)
	if len(rows) != 1 || rows[0].State != ServiceStateActive || rows[0].PID != 556 {
		t.Fatalf("cgroup-child rows = %+v, want active pid 556", rows)
	}
}

func TestDiscoverPersistentSkipsClaimedTmuxSocket(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(unit string) (systemdUnitStatus, error) {
		return systemdUnitStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running", MainPID: 77}, nil
	}
	w.listSocketsFn = func() ([]listeningSocket, error) {
		return []listeningSocket{{pid: 77, port: 3000, bind: "0.0.0.0"}}, nil
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "web.service", Name: "Web"}); err != nil {
		t.Fatalf("register error = %v", err)
	}
	claimed := map[string]bool{"77|3000": true}
	rows := w.discoverPersistentServices(claimed, nil)
	if len(rows) != 1 || rows[0].State != ServiceStateInactive {
		t.Fatalf("claimed socket rows = %+v, want single inactive (no duplicate)", rows)
	}
}

func TestDiscoverPersistentStoppedUnitIsInactive(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(unit string) (systemdUnitStatus, error) {
		return systemdUnitStatus{Unit: unit, LoadState: "loaded", ActiveState: "inactive", SubState: "dead"}, nil
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "DeepSeek Harness", Port: 3080}); err != nil {
		t.Fatalf("register error = %v", err)
	}
	rows := w.discoverPersistentServices(map[string]bool{}, nil)
	if len(rows) != 1 || rows[0].State != ServiceStateInactive || rows[0].Port != 3080 {
		t.Fatalf("stopped rows = %+v, want inactive :3080", rows)
	}
	if rows[0].WorkerID != "" || rows[0].Unit != "dsh-web.service" {
		t.Fatalf("stopped rows = %+v, want empty worker, unit kept", rows)
	}
}

func TestDiscoverPersistentBrokenQueryIsErrorNotSuccess(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	// Registration succeeds against a live unit...
	w.systemctlShowFn = func(unit string) (systemdUnitStatus, error) {
		return systemdUnitStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running", MainPID: 1}, nil
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "DeepSeek Harness", Port: 3080}); err != nil {
		t.Fatalf("register error = %v", err)
	}
	// ...then the bus breaks. Discovery must surface error state, not a
	// silent active/inactive guess.
	w.systemctlShowFn = func(unit string) (systemdUnitStatus, error) {
		return systemdUnitStatus{}, os.ErrPermission
	}
	rows := w.discoverPersistentServices(map[string]bool{}, nil)
	if len(rows) != 1 || rows[0].State != ServiceStateError {
		t.Fatalf("broken-query rows = %+v, want single error row", rows)
	}
	if strings.TrimSpace(rows[0].StatusDetail) == "" {
		t.Fatalf("error row must carry status_detail")
	}
}

func TestDiscoverPersistentSurvivesDaemonRestart(t *testing.T) {
	requireLinuxManagedServices(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "managed-services.json")
	first := testManagedWatcher(t, path)
	first.systemctlShowFn = func(unit string) (systemdUnitStatus, error) {
		return systemdUnitStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running", MainPID: 9}, nil
	}
	if _, err := first.RegisterManagedService(ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "DeepSeek Harness", Port: 3080}); err != nil {
		t.Fatalf("register error = %v", err)
	}
	// A fresh watcher (new daemon process) on the same state dir keeps it.
	second := testManagedWatcher(t, path)
	listed, err := second.ListManagedServices()
	if err != nil || len(listed) != 1 || listed[0].Unit != "dsh-web.service" {
		t.Fatalf("ListManagedServices after restart = %#v, %v", listed, err)
	}
}

func TestCgroupPathNamesUnit(t *testing.T) {
	if !cgroupPathNamesUnit("/user.slice/user-1000.slice/user@1000.service/app.slice/dsh-web.service", "dsh-web.service") {
		t.Fatalf("exact cgroup segment must match")
	}
	if cgroupPathNamesUnit("/user.slice/app.slice/dsh-web.service.d", "dsh-web.service") {
		t.Fatalf("prefix segment must not match")
	}
	if cgroupPathNamesUnit("/user.slice/other.service", "dsh-web.service") {
		t.Fatalf("foreign unit must not match")
	}
}
