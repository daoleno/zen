package watcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func requireLinuxManagedServices(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("persistent service registration requires Linux user systemd")
	}
}

const testUnitCgroup = "/user.slice/user-1000.slice/user@1000.service/app.slice/dsh-web.service"

func testManagedWatcher(t *testing.T, registry string) *Watcher {
	t.Helper()
	w := New(500 * time.Millisecond)
	w.SetManagedServicesPath(registry)
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return systemdUnitStatus{}, os.ErrNotExist
	}
	w.procCgroupFn = func(pid int) ([]string, error) { return nil, nil }
	w.procUIDFn = func(pid int) (int, error) { return os.Geteuid(), nil }
	w.listSocketsFn = func() ([]listeningSocket, error) { return nil, nil }
	return w
}

func activeUnitStatus(unit string, mainPID int) systemdUnitStatus {
	return systemdUnitStatus{
		Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running",
		MainPID: mainPID, ControlGroup: testUnitCgroup,
	}
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
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return activeUnitStatus(unit, 4242), nil
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

// Concurrent control handlers must not lose registrations: the file
// read-modify-write is serialized. -race alone cannot catch the logical loss.
func TestConcurrentRegisterUnregisterLosesNothing(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return activeUnitStatus(unit, 1), nil
	}
	const units = 8
	var wg sync.WaitGroup
	errs := make(chan error, units*2)
	for i := 0; i < units; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := w.RegisterManagedService(ManagedServiceDescriptor{
				Unit: fmt.Sprintf("svc-%d.service", i), Name: fmt.Sprintf("Svc %d", i),
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent register error = %v", err)
	}
	listed, err := w.ListManagedServices()
	if err != nil || len(listed) != units {
		t.Fatalf("after concurrent register listed=%d err=%v, want %d", len(listed), err, units)
	}
	// Concurrent unregister of disjoint units keeps the survivors exact.
	var wg2 sync.WaitGroup
	for i := 0; i < units; i += 2 {
		wg2.Add(1)
		go func(i int) {
			defer wg2.Done()
			if err := w.UnregisterManagedService(fmt.Sprintf("svc-%d.service", i)); err != nil {
				errs <- err
			}
		}(i)
	}
	wg2.Wait()
	listed, err = w.ListManagedServices()
	if err != nil || len(listed) != units/2 {
		t.Fatalf("after concurrent unregister listed=%d err=%v, want %d", len(listed), err, units/2)
	}
	for _, desc := range listed {
		n := 0
		if _, err := fmt.Sscanf(desc.Unit, "svc-%d.service", &n); err != nil || n%2 == 0 {
			t.Fatalf("survivor = %#v, want only odd units", desc)
		}
	}
}

func TestRegisterManagedServiceRejectsUnknownUnit(t *testing.T) {
	requireLinuxManagedServices(t)
	dir := t.TempDir()
	w := testManagedWatcher(t, filepath.Join(dir, "managed-services.json"))
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "nope.service", Name: "Nope"}); err == nil {
		t.Fatalf("RegisterManagedService(unknown unit) = nil, want error")
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "../evil.service", Name: "Evil"}); err == nil {
		t.Fatalf("RegisterManagedService(path traversal) = nil, want error")
	}
	if _, err := os.Stat(filepath.Join(dir, "managed-services.json")); !os.IsNotExist(err) {
		t.Fatalf("failed registration must not create a registry")
	}
}

func TestDiscoverPersistentActiveViaCgroup(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return activeUnitStatus(unit, 1610722), nil
	}
	w.procCgroupFn = func(pid int) ([]string, error) {
		if pid == 1610722 {
			return []string{testUnitCgroup}, nil
		}
		return nil, nil
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
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return activeUnitStatus(unit, 111), nil
	}
	// Same port and even the stale MainPID, but the listener lives under a
	// DIFFERENT unit's exact cgroup (same leaf name elsewhere).
	w.procCgroupFn = func(pid int) ([]string, error) {
		if pid == 999 {
			return []string{"/user.slice/user-1001.slice/user@1001.service/app.slice/dsh-web.service"}, nil
		}
		return nil, nil
	}
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

func TestDiscoverPersistentRejectsStaleMainPIDReuse(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	// systemctl reports MainPID 111, but PID 111 was reused by an unrelated
	// process outside the unit cgroup. The bare PID match must not attribute.
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return activeUnitStatus(unit, 111), nil
	}
	w.procCgroupFn = func(pid int) ([]string, error) {
		if pid == 111 {
			return []string{"/user.slice/user-1000.slice/session-9.scope"}, nil
		}
		return nil, nil
	}
	w.listSocketsFn = func() ([]listeningSocket, error) {
		return []listeningSocket{{pid: 111, port: 3080, bind: "127.0.0.1"}}, nil
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "DeepSeek Harness", Port: 3080}); err != nil {
		t.Fatalf("register error = %v", err)
	}
	rows := w.discoverPersistentServices(map[string]bool{}, nil)
	if len(rows) != 1 || rows[0].State != ServiceStateInactive {
		t.Fatalf("reused-PID rows = %+v, want single inactive", rows)
	}
}

func TestDiscoverPersistentRejectsForeignUID(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return activeUnitStatus(unit, 111), nil
	}
	w.procCgroupFn = func(pid int) ([]string, error) {
		return []string{testUnitCgroup}, nil
	}
	w.procUIDFn = func(pid int) (int, error) { return os.Geteuid() + 10000, nil }
	w.listSocketsFn = func() ([]listeningSocket, error) {
		return []listeningSocket{{pid: 111, port: 3080, bind: "127.0.0.1"}}, nil
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "DeepSeek Harness", Port: 3080}); err != nil {
		t.Fatalf("register error = %v", err)
	}
	rows := w.discoverPersistentServices(map[string]bool{}, nil)
	if len(rows) != 1 || rows[0].State != ServiceStateInactive {
		t.Fatalf("foreign-UID rows = %+v, want single inactive", rows)
	}
}

func TestDiscoverPersistentVanishedPIDIsSkipped(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return activeUnitStatus(unit, 111), nil
	}
	w.procCgroupFn = func(pid int) ([]string, error) { return nil, errProcessGone }
	w.listSocketsFn = func() ([]listeningSocket, error) {
		return []listeningSocket{{pid: 111, port: 3080, bind: "127.0.0.1"}}, nil
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "DeepSeek Harness", Port: 3080}); err != nil {
		t.Fatalf("register error = %v", err)
	}
	rows := w.discoverPersistentServices(map[string]bool{}, nil)
	if len(rows) != 1 || rows[0].State != ServiceStateInactive {
		t.Fatalf("vanished-PID rows = %+v, want single inactive (no error)", rows)
	}
}

func TestDiscoverPersistentProcFailureIsErrorNotInactive(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return activeUnitStatus(unit, 111), nil
	}
	w.procCgroupFn = func(pid int) ([]string, error) { return nil, fmt.Errorf("cgroup unavailable") }
	w.listSocketsFn = func() ([]listeningSocket, error) {
		return []listeningSocket{{pid: 111, port: 3080, bind: "127.0.0.1"}}, nil
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "DeepSeek Harness", Port: 3080}); err != nil {
		t.Fatalf("register error = %v", err)
	}
	rows := w.discoverPersistentServices(map[string]bool{}, nil)
	if len(rows) != 1 || rows[0].State != ServiceStateError {
		t.Fatalf("proc-failure rows = %+v, want single error (never fake inactive)", rows)
	}
}

func TestDiscoverPersistentAcceptsCgroupChildPID(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		// Restarted unit: new MainPID, old PID gone.
		return activeUnitStatus(unit, 555), nil
	}
	w.procCgroupFn = func(pid int) ([]string, error) {
		if pid == 556 {
			return []string{testUnitCgroup}, nil
		}
		return nil, errProcessGone
	}
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

// A socket tmux already represents must not reappear as a bogus persistent
// inactive duplicate: it is omitted, not lied about.
func TestDiscoverPersistentOmitsClaimedTmuxSocket(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return systemdUnitStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running", MainPID: 77, ControlGroup: "/user.slice/app.slice/web.service"}, nil
	}
	w.procCgroupFn = func(pid int) ([]string, error) {
		return []string{"/user.slice/app.slice/web.service"}, nil
	}
	w.listSocketsFn = func() ([]listeningSocket, error) {
		return []listeningSocket{{pid: 77, port: 3000, bind: "0.0.0.0"}}, nil
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "web.service", Name: "Web"}); err != nil {
		t.Fatalf("register error = %v", err)
	}
	claimed := map[string]bool{"77|3000": true}
	rows := w.discoverPersistentServices(claimed, nil)
	if len(rows) != 0 {
		t.Fatalf("claimed socket rows = %+v, want omitted (no duplicate)", rows)
	}
}

// Merged authoritative snapshot: one tmux row wins, no persistent duplicate,
// no inactive ghost.
func TestMergedSnapshotKeepsSingleTmuxRow(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.servicePanesFn = func() ([]servicePane, error) {
		return []servicePane{{target: "main:@1", paneID: "%1", name: "Main", cwd: "/repo", command: "bun", panePID: 100, active: true}}, nil
	}
	w.snapshotProcesses = func() map[int]processInfo {
		return map[int]processInfo{100: {pid: 100, ppid: 1}, 200: {pid: 200, ppid: 100, comm: "bun", args: "bun dev"}}
	}
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return systemdUnitStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running", MainPID: 200, ControlGroup: "/user.slice/app.slice/web.service"}, nil
	}
	w.procCgroupFn = func(pid int) ([]string, error) {
		return []string{"/user.slice/app.slice/web.service"}, nil
	}
	w.listSocketsFn = func() ([]listeningSocket, error) {
		return []listeningSocket{{pid: 200, port: 3000, bind: "0.0.0.0"}}, nil
	}
	if err := storeManagedServices(path, []ManagedServiceDescriptor{{Unit: "web.service", Name: "Web"}}); err != nil {
		t.Fatalf("store registry: %v", err)
	}
	snapshot, err := w.DiscoverSessionServices()
	if err != nil {
		t.Fatalf("DiscoverSessionServices error = %v", err)
	}
	if len(snapshot.Services) != 1 {
		t.Fatalf("merged services = %#v, want exactly one row", snapshot.Services)
	}
	row := snapshot.Services[0]
	if row.Source != ServiceSourceSession || row.WorkerID != "main:@1" || row.Port != 3000 {
		t.Fatalf("merged row = %+v, want the tmux session row", row)
	}
}

func TestDiscoverPersistentStoppedUnitIsInactive(t *testing.T) {
	requireLinuxManagedServices(t)
	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
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
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return activeUnitStatus(unit, 1), nil
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "DeepSeek Harness", Port: 3080}); err != nil {
		t.Fatalf("register error = %v", err)
	}
	// ...then the bus breaks. Discovery must surface error state, not a
	// silent active/inactive guess.
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
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
	first.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return activeUnitStatus(unit, 9), nil
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

func TestCgroupPathAccepts(t *testing.T) {
	unit := "/user.slice/user-1000.slice/user@1000.service/app.slice/dsh-web.service"
	cases := []struct {
		name  string
		path  string
		group string
		want  bool
	}{
		{name: "exact scope", path: unit, group: unit, want: true},
		{name: "delegated child scope", path: unit + "/child", group: unit, want: true},
		{name: "same leaf under different cgroup", path: "/user.slice/user-1001.slice/user@1001.service/app.slice/dsh-web.service", group: unit, want: false},
		{name: "system unit same leaf", path: "/system.slice/dsh-web.service", group: unit, want: false},
		{name: "prefix sibling", path: unit + ".d", group: unit, want: false},
		{name: "sibling scope", path: "/user.slice/user-1000.slice/user@1000.service/app.slice/other.service", group: unit, want: false},
		{name: "empty path", path: "", group: unit, want: false},
		{name: "empty group", path: unit, group: "", want: false},
		{name: "relative group", path: unit, group: "app.slice/dsh-web.service", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cgroupPathAccepts(tc.path, tc.group); got != tc.want {
				t.Fatalf("cgroupPathAccepts(%q, %q) = %v, want %v", tc.path, tc.group, got, tc.want)
			}
		})
	}
}

func TestRegistryRejectsUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "managed-services.json")
	before := []byte("{\"version\":99,\"services\":[{\"unit\":\"dsh-web.service\",\"name\":\"X\"}]}\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadManagedServices(path); err == nil || !strings.Contains(err.Error(), "version 99") {
		t.Fatalf("load version-99 registry err = %v, want version error", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatalf("failed load rewrote the registry")
	}
}

func TestRegistryRejectsInvalidEntries(t *testing.T) {
	for _, entry := range []string{
		`{"version":1,"services":[{"unit":"../evil.service","name":"X"}]}`,
		`{"version":1,"services":[{"unit":"dsh-web.service","name":""}]}`,
		`{"version":1,"services":[{"unit":"dsh-web.service","name":"X","port":99999}]}`,
		`{"version":1,"services":[{"name":"NoUnit"}]}`,
		`not json at all`,
	} {
		path := filepath.Join(t.TempDir(), "managed-services.json")
		before := append([]byte(entry), '\n')
		if err := os.WriteFile(path, before, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadManagedServices(path); err == nil {
			t.Fatalf("load %q = nil, want error", entry)
		}
		if after, _ := os.ReadFile(path); string(after) != string(before) {
			t.Fatalf("failed load rewrote the registry for %q", entry)
		}
	}
}

func TestRegistryFailureBlocksOverwrite(t *testing.T) {
	requireLinuxManagedServices(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "managed-services.json")
	before := []byte("{\"version\":99,\"services\":[]}\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		return activeUnitStatus(unit, 1), nil
	}
	if _, err := w.RegisterManagedService(ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "X"}); err == nil {
		t.Fatalf("register over unsupported registry = nil, want error")
	}
	if err := w.UnregisterManagedService("dsh-web.service"); err == nil {
		t.Fatalf("unregister over unsupported registry = nil, want error")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatalf("failed registry operation rewrote data")
	}
}

func writePathShim(t *testing.T, name, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A hung user bus must fail the unit quickly instead of blocking discovery.
// Uses a PATH shim (no real bus sabotage) with a shrunk timeout.
func TestQuerySystemdUnitHungBusTimesOut(t *testing.T) {
	writePathShim(t, "systemctl", "#!/bin/sh\nexec sleep 30\n")
	oldUnit, oldBudget, oldDrain := systemdUnitTimeout, managedDiscoveryBudget, serviceProcWaitDelay
	systemdUnitTimeout, managedDiscoveryBudget, serviceProcWaitDelay = 100*time.Millisecond, 200*time.Millisecond, 50*time.Millisecond
	defer func() {
		systemdUnitTimeout, managedDiscoveryBudget, serviceProcWaitDelay = oldUnit, oldBudget, oldDrain
	}()
	start := time.Now()
	_, err := querySystemdUnit(context.Background(), "dsh-web.service")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("hung bus err = %v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("hung bus took %v, want fast timeout", elapsed)
	}
}

func TestQuerySystemdUnitErrorExit(t *testing.T) {
	writePathShim(t, "systemctl", "#!/bin/sh\necho 'bus broken' >&2\nexit 1\n")
	if _, err := querySystemdUnit(context.Background(), "dsh-web.service"); err == nil || !strings.Contains(err.Error(), "bus broken") {
		t.Fatalf("error-exit err = %v, want bus output", err)
	}
}

func TestQuerySystemdUnitParsesProperties(t *testing.T) {
	writePathShim(t, "systemctl", "#!/bin/sh\nprintf 'LoadState=loaded\\nActiveState=active\\nSubState=running\\nMainPID=42\\nFragmentPath=/x.service\\nInvocationID=abc\\nControlGroup=/user.slice/app.slice/x.service\\n'\n")
	status, err := querySystemdUnit(context.Background(), "x.service")
	if err != nil {
		t.Fatalf("query err = %v", err)
	}
	if status.MainPID != 42 || status.ControlGroup != "/user.slice/app.slice/x.service" || status.ActiveState != "active" {
		t.Fatalf("status = %+v", status)
	}
}

func TestPidCgroupPathsParsesHierarchy(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("proc cgroup layout is Linux-specific")
	}
	paths, err := pidCgroupPaths(1)
	if err != nil {
		t.Fatalf("pidCgroupPaths(1) err = %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("pidCgroupPaths(1) empty")
	}
	for _, path := range paths {
		if !strings.HasPrefix(path, "/") || strings.Contains(path, ":") {
			t.Fatalf("unparsed cgroup path %q", path)
		}
	}
	if _, err := pidCgroupPaths(1 << 30); !errors.Is(err, errProcessGone) {
		t.Fatalf("bogus pid err = %v, want errProcessGone", err)
	}
	if uid, err := pidOwnerUID(os.Getpid()); err != nil || uid != os.Geteuid() {
		t.Fatalf("self uid = %d err = %v, want %d", uid, err, os.Geteuid())
	}
}

// Aggregate budget: TWO hung units plus one healthy unit must still resolve
// comfortably before the 10s frontend deadline. The shared deadline (not
// per-unit timeouts) prevents multiplication; healthy rows are preserved and
// hung units degrade to explicit error rows.
func TestDiscoverPersistentSharedBudgetAcrossHungUnits(t *testing.T) {
	requireLinuxManagedServices(t)
	oldUnit, oldBudget := systemdUnitTimeout, managedDiscoveryBudget
	systemdUnitTimeout, managedDiscoveryBudget = 100*time.Millisecond, 400*time.Millisecond
	defer func() { systemdUnitTimeout, managedDiscoveryBudget = oldUnit, oldBudget }()

	path := filepath.Join(t.TempDir(), "managed-services.json")
	w := testManagedWatcher(t, path)
	w.systemctlShowFn = func(ctx context.Context, unit string) (systemdUnitStatus, error) {
		if strings.HasPrefix(unit, "hung-") {
			<-ctx.Done()
			return systemdUnitStatus{}, ctx.Err()
		}
		return activeUnitStatus(unit, 1610722), nil
	}
	w.procCgroupFn = func(pid int) ([]string, error) {
		if pid == 1610722 {
			return []string{testUnitCgroup}, nil
		}
		return nil, errProcessGone
	}
	w.listSocketsFn = func() ([]listeningSocket, error) {
		return []listeningSocket{{pid: 1610722, port: 3080, bind: "127.0.0.1"}}, nil
	}
	for _, unit := range []string{"hung-a.service", "dsh-web.service", "hung-b.service"} {
		if err := storeManagedServices(path, append(mustLoadForTest(t, path), ManagedServiceDescriptor{Unit: unit, Name: unit})); err != nil {
			t.Fatalf("store: %v", err)
		}
	}

	start := time.Now()
	rows := w.discoverPersistentServices(map[string]bool{}, nil)
	elapsed := time.Since(start)
	// Comfortably below the 10s frontend rejection: budget 400ms + drain.
	if elapsed > 5*time.Second {
		t.Fatalf("shared-budget discovery took %v, want well under frontend deadline", elapsed)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want healthy + 2 hung-error rows", rows)
	}
	byUnit := map[string]SessionService{}
	for _, row := range rows {
		byUnit[row.Unit] = row
	}
	healthy, ok := byUnit["dsh-web.service"]
	if !ok || healthy.State != ServiceStateActive || healthy.Port != 3080 {
		t.Fatalf("healthy row = %+v, want active :3080", healthy)
	}
	for _, unit := range []string{"hung-a.service", "hung-b.service"} {
		row, ok := byUnit[unit]
		if !ok || row.State != ServiceStateError || strings.TrimSpace(row.StatusDetail) == "" {
			t.Fatalf("hung row %q = %+v, want explicit error", unit, row)
		}
	}
}

func mustLoadForTest(t *testing.T, path string) []ManagedServiceDescriptor {
	t.Helper()
	services, err := loadManagedServices(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return services
}
