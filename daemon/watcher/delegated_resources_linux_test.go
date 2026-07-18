//go:build linux

package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidateDelegatedWorkspaceRejectsMemoryBackedFilesystem(t *testing.T) {
	root := "/dev/shm"
	if _, err := os.Stat(root); err != nil {
		t.Skipf("%s unavailable: %v", root, err)
	}
	dir, err := os.MkdirTemp(root, "zen-workspace-test-")
	if err != nil {
		t.Skipf("cannot create tmpfs fixture: %v", err)
	}
	defer os.RemoveAll(dir)
	if err := validateDelegatedWorkspace(dir); err == nil || !strings.Contains(err.Error(), "memory-backed temporary storage") {
		t.Fatalf("validateDelegatedWorkspace(%q) error = %v", dir, err)
	}
}

func TestLinuxResourceReconcileStopsOnlyOwnedOrphan(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	systemctl := filepath.Join(dir, "systemctl")
	owner := "abc123"
	owned := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	other := delegatedResourceUnit("other", "fedcba9876543210fedcba9876543210")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
case "$*" in
  *list-units*)
    printf '%s loaded active running owned\n' "$ZEN_TEST_OWNED_UNIT"
    printf '%s loaded active running other\n' "$ZEN_TEST_OTHER_UNIT"
    ;;
esac
exit 0
`
	if err := os.WriteFile(systemctl, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_TEST_SYSTEMCTL_LOG", logPath)
	t.Setenv("ZEN_TEST_OWNED_UNIT", owned)
	t.Setenv("ZEN_TEST_OTHER_UNIT", other)

	manager := newTestLinuxResourceManager(
		t,
		owner,
		systemctl,
		func() time.Time { return time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC) },
	)
	manager.Reconcile(nil)

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(raw)
	if !strings.Contains(calls, "stop "+owned) {
		t.Fatalf("owned orphan was not stopped:\n%s", calls)
	}
	if strings.Contains(calls, "stop "+other) {
		t.Fatalf("foreign unit was stopped:\n%s", calls)
	}
}

func TestLinuxResourceReconcileKeepsLiveOwnedUnit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	systemctl := filepath.Join(dir, "systemctl")
	systemdRun := filepath.Join(dir, "systemd-run")
	owner := "abc123"
	owned := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
case "$*" in
  *list-units*) printf '%s loaded active running owned\n' "$ZEN_TEST_OWNED_UNIT" ;;
esac
exit 0
`
	for _, path := range []string{systemctl, systemdRun} {
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ZEN_TEST_SYSTEMCTL_LOG", logPath)
	t.Setenv("ZEN_TEST_OWNED_UNIT", owned)
	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.systemdRun = systemdRun
	manager.showUnitPropertiesFn = func(string, ...string) map[string]string {
		return map[string]string{"MemoryHigh": "infinity", "MemoryMax": "infinity"}
	}
	manager.Reconcile([]tmuxWindow{{target: "main:@42", delegated: true, resourceUnit: owned}})
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "stop "+owned) {
		t.Fatalf("live unit was stopped:\n%s", raw)
	}
	if got := manager.UnitForTarget("main:@42"); got != owned {
		t.Fatalf("bound unit = %q, want %q", got, owned)
	}
}

func TestLinuxResourceReconcileKeepsInFlightReservedUnit(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	systemctl := filepath.Join(dir, "systemctl")
	owner := "abc123"
	owned := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
case "$*" in
  *list-units*) printf '%s loaded active running owned\n' "$ZEN_TEST_OWNED_UNIT" ;;
esac
exit 0
`
	if err := os.WriteFile(systemctl, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_TEST_SYSTEMCTL_LOG", logPath)
	t.Setenv("ZEN_TEST_OWNED_UNIT", owned)
	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.reserved[owned] = time.Now().Add(time.Minute)
	manager.Reconcile(nil)
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "stop "+owned) {
		t.Fatalf("in-flight reserved unit was stopped:\n%s", raw)
	}
	if manager.normalizedChildLimits != nil {
		if _, ok := manager.normalizedChildLimits[owned]; ok {
			t.Fatal("reserved-only unit was treated as a live migration candidate")
		}
	}
}

func TestLinuxResourceEnsureSharedPoolSliceIsIdempotent(t *testing.T) {
	owner := "abc123"
	script := `#!/bin/sh
printf '%s\n' "$0 $*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
exit 0
`
	systemctl, systemdRun, logPath := writeTestSystemdStubs(t, script)
	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.systemdRun = systemdRun
	manager.limits = delegatedResourceLimits{
		MemoryHigh: "27500000000",
		MemoryMax:  "30900000000",
		TasksMax:   1024,
	}
	if err := manager.ensureSharedPoolSlice(); err != nil {
		t.Fatal(err)
	}
	if err := manager.ensureSharedPoolSlice(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(raw)
	if !strings.Contains(calls, "set-property --runtime zen-agents-abc123.slice") {
		t.Fatalf("missing slice configuration:\n%s", calls)
	}
	if !strings.Contains(calls, "MemoryHigh=27500000000") || !strings.Contains(calls, "MemoryMax=30900000000") {
		t.Fatalf("missing aggregate memory properties:\n%s", calls)
	}
	if strings.Count(calls, "set-property --runtime") != 1 {
		t.Fatalf("slice configuration was not idempotent:\n%s", calls)
	}
}

func TestLinuxResourceEnsureSharedPoolSliceSerializesConcurrentPrepare(t *testing.T) {
	owner := "abc123"
	script := `#!/bin/sh
printf '%s\n' "$0 $*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
sleep 0.05
exit 0
`
	systemctl, systemdRun, logPath := writeTestSystemdStubs(t, script)
	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.systemdRun = systemdRun
	manager.limits = delegatedResourceLimits{
		MemoryHigh: "27500000000",
		MemoryMax:  "30900000000",
		TasksMax:   1024,
	}
	const workers = 8
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			errs <- manager.ensureSharedPoolSlice()
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("ensureSharedPoolSlice: %v", err)
		}
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(raw)
	if strings.Count(calls, "set-property --runtime") != 1 {
		t.Fatalf("concurrent callers configured slice more than once:\n%s", calls)
	}
}

func TestLinuxResourceReleaseRefusesForeignUnitWithoutCallingSystemctl(t *testing.T) {
	manager := newTestLinuxResourceManager(t, "abc123", filepath.Join(t.TempDir(), "systemctl"), time.Now)
	foreign := delegatedResourceUnit("different", "0123456789abcdef0123456789abcdef")
	if err := manager.Release("", foreign); err == nil || !strings.Contains(err.Error(), "refuse unowned") {
		t.Fatalf("Release foreign unit error = %v", err)
	}
}

func TestLinuxPrepareUnitScanFollowsSessionCap(t *testing.T) {
	script := `#!/bin/sh
printf '%s\n' "$0 $*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
exit 0
`
	cases := []struct {
		name     string
		cap      int
		wantScan int32
	}{
		{name: "disabled", cap: 0, wantScan: 0},
		{name: "enabled", cap: 4, wantScan: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			systemctl, systemdRun, _ := writeTestSystemdStubs(t, script)
			manager := newTestLinuxResourceManager(t, "abc123", systemctl, time.Now)
			manager.systemdRun = systemdRun
			manager.limits.MaxActiveSessions = tc.cap
			var unitScans atomic.Int32
			manager.listOwnedUnitsFn = func() ([]string, error) {
				unitScans.Add(1)
				return nil, nil
			}
			if _, err := manager.Prepare(0); err != nil {
				t.Fatal(err)
			}
			if got := unitScans.Load(); got != tc.wantScan {
				t.Fatalf("unit scan count = %d, want %d", got, tc.wantScan)
			}
		})
	}
}

func TestLinuxResourceReconcileClearsLegacyChildMemoryLimits(t *testing.T) {
	owner := "abc123"
	live := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	systemctl, systemdRun, logPath := writeTestSystemdStubs(t, `#!/bin/sh
printf '%s\n' "$0 $*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
exit 0
`)
	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.systemdRun = systemdRun
	var clears []string
	manager.listOwnedUnitsFn = func() ([]string, error) { return []string{live}, nil }
	manager.showUnitPropertiesFn = func(unit string, properties ...string) map[string]string {
		if unit != live {
			t.Fatalf("unexpected show unit %q", unit)
		}
		return map[string]string{"MemoryHigh": "5321266995", "MemoryMax": "6549251686"}
	}
	manager.setUnitPropertiesFn = func(unit string, properties ...string) error {
		if !manager.sliceReady {
			t.Fatal("child clear ran before parent slice was ready")
		}
		clears = append(clears, unit+"|"+strings.Join(properties, ","))
		return nil
	}

	manager.Reconcile([]tmuxWindow{{target: "main:@31", delegated: true, resourceUnit: live}})

	if len(clears) != 1 || clears[0] != live+"|MemoryHigh=infinity,MemoryMax=infinity" {
		t.Fatalf("clears = %#v", clears)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "set-property --runtime zen-agents-abc123.slice") {
		t.Fatalf("missing parent slice configuration:\n%s", raw)
	}
	if _, done := manager.normalizedChildLimits[live]; !done {
		t.Fatal("live legacy unit was not marked normalized")
	}
}

func TestLinuxResourceReconcileAlreadyInfinityChildNoOp(t *testing.T) {
	owner := "abc123"
	live := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	systemctl, systemdRun, _ := writeTestSystemdStubs(t, `#!/bin/sh
printf '%s\n' "$0 $*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
exit 0
`)
	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.systemdRun = systemdRun
	var clears int
	manager.listOwnedUnitsFn = func() ([]string, error) { return []string{live}, nil }
	manager.showUnitPropertiesFn = func(unit string, _ ...string) map[string]string {
		return map[string]string{"MemoryHigh": "infinity", "MemoryMax": "infinity"}
	}
	manager.setUnitPropertiesFn = func(string, ...string) error {
		clears++
		return nil
	}

	manager.Reconcile([]tmuxWindow{{target: "main:@31", delegated: true, resourceUnit: live}})
	manager.lastSystemdScan = time.Time{}
	manager.Reconcile([]tmuxWindow{{target: "main:@31", delegated: true, resourceUnit: live}})

	if clears != 0 {
		t.Fatalf("already-infinity child was rewritten %d times", clears)
	}
	if _, done := manager.normalizedChildLimits[live]; !done {
		t.Fatal("already-infinity unit was not marked normalized")
	}
}

func TestLinuxResourceReconcileSkipsForeignOrphanAndMalformedChildMigration(t *testing.T) {
	owner := "abc123"
	live := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	orphan := delegatedResourceUnit(owner, "fedcba9876543210fedcba9876543210")
	foreign := delegatedResourceUnit("other", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	malformed := "zen-agent-" + owner + "-not-a-token.scope"
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	systemctl := filepath.Join(dir, "systemctl")
	systemdRun := filepath.Join(dir, "systemd-run")
	script := `#!/bin/sh
printf '%s\n' "$0 $*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
case "$*" in
  *list-units*)
    printf '%s loaded active running live\n' "$ZEN_TEST_LIVE_UNIT"
    printf '%s loaded active running orphan\n' "$ZEN_TEST_ORPHAN_UNIT"
    printf '%s loaded active running foreign\n' "$ZEN_TEST_FOREIGN_UNIT"
    ;;
  *stop*)
    ;;
esac
exit 0
`
	for _, path := range []string{systemctl, systemdRun} {
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ZEN_TEST_SYSTEMCTL_LOG", logPath)
	t.Setenv("ZEN_TEST_LIVE_UNIT", live)
	t.Setenv("ZEN_TEST_ORPHAN_UNIT", orphan)
	t.Setenv("ZEN_TEST_FOREIGN_UNIT", foreign)

	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.systemdRun = systemdRun
	shown := make([]string, 0)
	cleared := make([]string, 0)
	manager.showUnitPropertiesFn = func(unit string, _ ...string) map[string]string {
		shown = append(shown, unit)
		return map[string]string{"MemoryHigh": "1000", "MemoryMax": "2000"}
	}
	manager.setUnitPropertiesFn = func(unit string, _ ...string) error {
		cleared = append(cleared, unit)
		return nil
	}

	manager.Reconcile([]tmuxWindow{
		{target: "main:@31", delegated: true, resourceUnit: live},
		{target: "main:@99", delegated: true, resourceUnit: malformed},
		{target: "main:@7", delegated: true, resourceUnit: foreign},
	})

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(raw)
	if !strings.Contains(calls, "stop "+orphan) {
		t.Fatalf("orphan was not stopped:\n%s", calls)
	}
	if strings.Contains(calls, "stop "+live) {
		t.Fatalf("live unit was stopped:\n%s", calls)
	}
	if strings.Contains(calls, "stop "+foreign) {
		t.Fatalf("foreign unit was stopped:\n%s", calls)
	}
	if len(shown) != 1 || shown[0] != live {
		t.Fatalf("shown units = %#v, want only live", shown)
	}
	if len(cleared) != 1 || cleared[0] != live {
		t.Fatalf("cleared units = %#v, want only live", cleared)
	}
}

func TestLinuxResourceReconcileChildLimitMigrationRetriesThenOnce(t *testing.T) {
	owner := "abc123"
	live := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	systemctl, systemdRun, _ := writeTestSystemdStubs(t, `#!/bin/sh
printf '%s\n' "$0 $*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
exit 0
`)
	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.systemdRun = systemdRun
	manager.listOwnedUnitsFn = func() ([]string, error) { return []string{live}, nil }
	manager.showUnitPropertiesFn = func(string, ...string) map[string]string {
		return map[string]string{"MemoryHigh": "5321266995", "MemoryMax": "6549251686"}
	}
	var clears atomic.Int32
	var failOnce atomic.Bool
	failOnce.Store(true)
	manager.setUnitPropertiesFn = func(string, ...string) error {
		clears.Add(1)
		if failOnce.CompareAndSwap(true, false) {
			return fmt.Errorf("transient set-property failure")
		}
		return nil
	}

	windows := []tmuxWindow{{target: "main:@31", delegated: true, resourceUnit: live}}
	manager.Reconcile(windows)
	if clears.Load() != 1 {
		t.Fatalf("first clear attempts = %d, want 1", clears.Load())
	}
	if _, done := manager.normalizedChildLimits[live]; done {
		t.Fatal("failed migration was marked normalized")
	}

	manager.lastSystemdScan = time.Time{}
	manager.Reconcile(windows)
	if clears.Load() != 2 {
		t.Fatalf("retry clear attempts = %d, want 2", clears.Load())
	}
	if _, done := manager.normalizedChildLimits[live]; !done {
		t.Fatal("successful retry was not marked normalized")
	}

	manager.lastSystemdScan = time.Time{}
	manager.Reconcile(windows)
	if clears.Load() != 2 {
		t.Fatalf("normalized unit was rewritten: clears=%d", clears.Load())
	}
}

func TestLinuxResourceReconcileBatchInspectMigratesOnlyLegacy(t *testing.T) {
	owner := "abc123"
	legacy := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	current := delegatedResourceUnit(owner, "fedcba9876543210fedcba9876543210")
	systemctl, systemdRun, _ := writeTestSystemdStubs(t, `#!/bin/sh
printf '%s\n' "$0 $*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
exit 0
`)
	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.systemdRun = systemdRun
	manager.listOwnedUnitsFn = func() ([]string, error) { return []string{legacy, current}, nil }

	var batchCalls atomic.Int32
	var seenBatch []string
	manager.showUnitsPropertiesFn = func(units []string, properties ...string) map[string]map[string]string {
		batchCalls.Add(1)
		seenBatch = append([]string(nil), units...)
		wantOrder := []string{current, legacy}
		sort.Strings(wantOrder)
		if len(units) != 2 || units[0] != wantOrder[0] || units[1] != wantOrder[1] {
			t.Fatalf("batch units = %#v, want sorted %#v", units, wantOrder)
		}
		wantProps := []string{"Id", "MemoryHigh", "MemoryMax"}
		if len(properties) != len(wantProps) {
			t.Fatalf("properties = %#v", properties)
		}
		return map[string]map[string]string{
			legacy: {
				"Id":         legacy,
				"MemoryHigh": "5321266995",
				"MemoryMax":  "6549251686",
			},
			current: {
				"Id":         current,
				"MemoryHigh": "infinity",
				"MemoryMax":  "infinity",
			},
		}
	}
	var clears []string
	manager.setUnitPropertiesFn = func(unit string, properties ...string) error {
		if !manager.sliceReady {
			t.Fatal("child clear ran before parent slice was ready")
		}
		clears = append(clears, unit+"|"+strings.Join(properties, ","))
		return nil
	}

	windows := []tmuxWindow{
		{target: "main:@31", delegated: true, resourceUnit: legacy},
		{target: "main:@93", delegated: true, resourceUnit: current},
	}
	manager.Reconcile(windows)
	if batchCalls.Load() != 1 {
		t.Fatalf("batch inspect calls = %d, want 1", batchCalls.Load())
	}
	if len(seenBatch) != 2 {
		t.Fatalf("seen batch = %#v", seenBatch)
	}
	if len(clears) != 1 || clears[0] != legacy+"|MemoryHigh=infinity,MemoryMax=infinity" {
		t.Fatalf("clears = %#v, want only legacy", clears)
	}
	if _, done := manager.normalizedChildLimits[legacy]; !done {
		t.Fatal("legacy unit was not marked normalized")
	}
	if _, done := manager.normalizedChildLimits[current]; !done {
		t.Fatal("already-infinity unit was not marked normalized")
	}

	manager.lastSystemdScan = time.Time{}
	manager.Reconcile(windows)
	if batchCalls.Load() != 1 {
		t.Fatalf("normalized units were re-inspected: batchCalls=%d", batchCalls.Load())
	}
	if len(clears) != 1 {
		t.Fatalf("already-current scope was rewritten: clears=%#v", clears)
	}
}

func TestLinuxResourceReconcileIgnoresTmuxUnitAbsentFromOwnedUnits(t *testing.T) {
	owner := "abc123"
	tmuxOnly := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	orphan := delegatedResourceUnit(owner, "fedcba9876543210fedcba9876543210")
	systemctl, systemdRun, logPath := writeTestSystemdStubs(t, `#!/bin/sh
printf '%s\n' "$0 $*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
exit 0
`)
	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.systemdRun = systemdRun
	var ownedCalls atomic.Int32
	manager.listOwnedUnitsFn = func() ([]string, error) {
		ownedCalls.Add(1)
		return []string{orphan}, nil
	}
	var shown []string
	var cleared []string
	manager.showUnitPropertiesFn = func(unit string, _ ...string) map[string]string {
		shown = append(shown, unit)
		return map[string]string{"MemoryHigh": "1000", "MemoryMax": "2000"}
	}
	manager.showUnitsPropertiesFn = func(units []string, _ ...string) map[string]map[string]string {
		shown = append(shown, units...)
		return nil
	}
	manager.setUnitPropertiesFn = func(unit string, _ ...string) error {
		cleared = append(cleared, unit)
		return nil
	}

	manager.Reconcile([]tmuxWindow{{target: "main:@31", delegated: true, resourceUnit: tmuxOnly}})

	if ownedCalls.Load() != 1 {
		t.Fatalf("listOwnedUnits calls = %d, want 1", ownedCalls.Load())
	}
	if len(shown) != 0 {
		t.Fatalf("tmux-only unit was inspected: %#v", shown)
	}
	if len(cleared) != 0 {
		t.Fatalf("tmux-only unit was mutated: %#v", cleared)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "stop "+orphan) {
		t.Fatalf("orphan from owned list was not stopped:\n%s", raw)
	}
	if strings.Contains(string(raw), "stop "+tmuxOnly) {
		t.Fatalf("tmux-only unit was stopped:\n%s", raw)
	}
}

func TestLinuxResourceReconcilePartialMemoryPropsRemainRetryable(t *testing.T) {
	owner := "abc123"
	live := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	systemctl, systemdRun, _ := writeTestSystemdStubs(t, `#!/bin/sh
exit 0
`)
	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.systemdRun = systemdRun
	manager.listOwnedUnitsFn = func() ([]string, error) { return []string{live}, nil }
	var batchCalls atomic.Int32
	manager.showUnitsPropertiesFn = func(units []string, _ ...string) map[string]map[string]string {
		batchCalls.Add(1)
		return map[string]map[string]string{
			live: {"Id": live, "MemoryHigh": "infinity"}, // MemoryMax key missing
		}
	}
	var clears int
	manager.setUnitPropertiesFn = func(string, ...string) error {
		clears++
		return nil
	}

	windows := []tmuxWindow{{target: "main:@31", delegated: true, resourceUnit: live}}
	manager.Reconcile(windows)
	if _, done := manager.normalizedChildLimits[live]; done {
		t.Fatal("partial property block was marked normalized")
	}
	if clears != 0 {
		t.Fatalf("partial block was mutated: clears=%d", clears)
	}

	manager.showUnitsPropertiesFn = func(units []string, _ ...string) map[string]map[string]string {
		batchCalls.Add(1)
		return map[string]map[string]string{
			live: {"Id": live, "MemoryHigh": "infinity", "MemoryMax": "infinity"},
		}
	}
	manager.lastSystemdScan = time.Time{}
	manager.Reconcile(windows)
	if batchCalls.Load() != 2 {
		t.Fatalf("partial block was not retried: batchCalls=%d", batchCalls.Load())
	}
	if _, done := manager.normalizedChildLimits[live]; !done {
		t.Fatal("complete block was not marked normalized on retry")
	}
	if clears != 0 {
		t.Fatalf("infinity block was rewritten: clears=%d", clears)
	}
}

func TestLinuxResourceReconcilePrunesNormalizedChildLimits(t *testing.T) {
	owner := "abc123"
	stale := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	live := delegatedResourceUnit(owner, "fedcba9876543210fedcba9876543210")
	systemctl, systemdRun, _ := writeTestSystemdStubs(t, `#!/bin/sh
exit 0
`)
	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.systemdRun = systemdRun
	manager.normalizedChildLimits = map[string]struct{}{stale: {}, live: {}}
	manager.listOwnedUnitsFn = func() ([]string, error) { return []string{live}, nil }
	manager.showUnitsPropertiesFn = func(units []string, _ ...string) map[string]map[string]string {
		return map[string]map[string]string{
			live: {"Id": live, "MemoryHigh": "infinity", "MemoryMax": "infinity"},
		}
	}

	manager.Reconcile([]tmuxWindow{{target: "main:@93", delegated: true, resourceUnit: live}})

	if _, ok := manager.normalizedChildLimits[stale]; ok {
		t.Fatal("stale normalized entry was not pruned")
	}
	if _, ok := manager.normalizedChildLimits[live]; !ok {
		t.Fatal("live normalized entry was dropped")
	}
}

func TestLinuxResourceReconcileFailsClosedOnOwnedUnitsError(t *testing.T) {
	owner := "abc123"
	live := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	systemctl := filepath.Join(dir, "systemctl")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
exit 0
`
	if err := os.WriteFile(systemctl, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_TEST_SYSTEMCTL_LOG", logPath)
	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.listOwnedUnitsFn = func() ([]string, error) {
		return nil, fmt.Errorf("list-units unavailable")
	}
	var shown int
	var cleared int
	manager.showUnitsPropertiesFn = func([]string, ...string) map[string]map[string]string {
		shown++
		return nil
	}
	manager.setUnitPropertiesFn = func(string, ...string) error {
		cleared++
		return nil
	}

	manager.Reconcile([]tmuxWindow{{target: "main:@31", delegated: true, resourceUnit: live}})

	if shown != 0 || cleared != 0 {
		t.Fatalf("fail-open on listOwnedUnits error: shown=%d cleared=%d", shown, cleared)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "stop ") {
		t.Fatalf("orphan stop ran after listOwnedUnits failure:\n%s", raw)
	}
}

func TestChildMemoryLimitsNeedClear(t *testing.T) {
	cases := []struct {
		high, max string
		want      bool
	}{
		{"infinity", "infinity", false},
		{"", "infinity", false},
		{"[not set]", "[not set]", false},
		{"5321266995", "6549251686", true},
		{"infinity", "6549251686", true},
		{"5321266995", "infinity", true},
	}
	for _, tc := range cases {
		if got := childMemoryLimitsNeedClear(tc.high, tc.max); got != tc.want {
			t.Fatalf("childMemoryLimitsNeedClear(%q,%q)=%v, want %v", tc.high, tc.max, got, tc.want)
		}
	}
}

func writeTestSystemdStubs(t *testing.T, script string) (systemctl, systemdRun, logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "calls.log")
	systemctl = filepath.Join(dir, "systemctl")
	systemdRun = filepath.Join(dir, "systemd-run")
	for _, path := range []string{systemctl, systemdRun} {
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ZEN_TEST_SYSTEMCTL_LOG", logPath)
	return systemctl, systemdRun, logPath
}

func newTestLinuxResourceManager(t *testing.T, owner, systemctl string, now func() time.Time) *linuxDelegatedResourceManager {
	t.Helper()
	leaseDir := filepath.Join(t.TempDir(), "leases")
	tempRoot := filepath.Join(t.TempDir(), "tmp")
	if err := os.MkdirAll(leaseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	portable := &portableDelegatedResourceManager{
		owner:      owner,
		supervisor: "/usr/bin/zen",
		leaseDir:   leaseDir,
		tempRoot:   tempRoot,
		limits:     delegatedResourceLimits{MemoryHigh: "27500000000", MemoryMax: "30900000000", TasksMax: 1024, MaxActiveSessions: 0},
		byTarget:   make(map[string]string),
		reserved:   make(map[string]time.Time),
		now:        now,
	}
	return &linuxDelegatedResourceManager{
		portableDelegatedResourceManager: portable,
		systemdRun:                       "/usr/bin/systemd-run",
		systemctl:                        systemctl,
	}
}
