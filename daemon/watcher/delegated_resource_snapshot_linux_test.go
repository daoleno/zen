//go:build linux

package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/agentproc"
)

func TestLinuxSnapshotUsesCgroupPropertiesWithoutProcessPolling(t *testing.T) {
	owner := "linuxsnap"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	slice := delegatedResourceSlice(owner)
	calls := 0
	manager := &linuxDelegatedResourceManager{
		portableDelegatedResourceManager: &portableDelegatedResourceManager{
			owner: owner,
			limits: delegatedResourceLimits{
				// Policy intentionally differs from live slice so the snapshot
				// cannot silently fall back to recomputed thresholds.
				MemoryHigh:  "11111111111",
				MemoryMax:   "22222222222",
				HostReserve: 4 * 1024 * 1024 * 1024,
			},
			byTarget:        map[string]string{"main:@9": unit},
			availableMemory: func() uint64 { return 10 * 1024 * 1024 * 1024 },
			sampleOwnedLeases: func(string) (agentproc.PoolSample, error) {
				t.Fatal("linux snapshot must not fall back to whole-host process sampling")
				return agentproc.PoolSample{}, nil
			},
		},
		systemctl: "/bin/true",
		showUnitPropertiesFn: func(name string, properties ...string) map[string]string {
			calls++
			switch name {
			case slice:
				return map[string]string{
					"MemoryCurrent": "9000",
					"MemoryHigh":    "27500000000",
					"MemoryMax":     "30900000000",
				}
			case unit:
				return map[string]string{
					"MemoryCurrent": "1200",
					"MemoryPeak":    "3400",
					"TasksCurrent":  "7",
				}
			default:
				t.Fatalf("unexpected unit %q", name)
				return nil
			}
		},
	}

	snap := manager.Snapshot("main:@9")
	if !snap.Managed || snap.Backend != resourceBackendCgroupPool {
		t.Fatalf("managed/backend = %v %q", snap.Managed, snap.Backend)
	}
	if snap.MemoryCurrentBytes == nil || *snap.MemoryCurrentBytes != 1200 {
		t.Fatalf("session memory = %#v", snap.MemoryCurrentBytes)
	}
	if snap.MemoryPeakBytes == nil || *snap.MemoryPeakBytes != 3400 {
		t.Fatalf("session peak = %#v", snap.MemoryPeakBytes)
	}
	if snap.TasksCurrent == nil || *snap.TasksCurrent != 7 {
		t.Fatalf("tasks = %#v", snap.TasksCurrent)
	}
	if snap.PoolMemoryCurrentBytes == nil || *snap.PoolMemoryCurrentBytes != 9000 {
		t.Fatalf("pool current = %#v", snap.PoolMemoryCurrentBytes)
	}
	if snap.PoolMemoryHighBytes == nil || *snap.PoolMemoryHighBytes != 27500000000 {
		t.Fatalf("pool high must come from live slice, got %#v", snap.PoolMemoryHighBytes)
	}
	if snap.PoolMemoryMaxBytes == nil || *snap.PoolMemoryMaxBytes != 30900000000 {
		t.Fatalf("pool max must come from live slice, got %#v", snap.PoolMemoryMaxBytes)
	}
	if calls < 2 {
		t.Fatalf("expected systemd property reads, got %d", calls)
	}
}

func TestLinuxSnapshotOmitsInfinityPoolThresholds(t *testing.T) {
	owner := "linuxinf"
	slice := delegatedResourceSlice(owner)
	manager := &linuxDelegatedResourceManager{
		portableDelegatedResourceManager: &portableDelegatedResourceManager{
			owner: owner,
			limits: delegatedResourceLimits{
				MemoryHigh: "27500000000",
				MemoryMax:  "30900000000",
			},
			availableMemory: func() uint64 { return 0 },
		},
		systemctl: "/bin/true",
		showUnitPropertiesFn: func(name string, properties ...string) map[string]string {
			if name != slice {
				t.Fatalf("unexpected unit %q", name)
			}
			return map[string]string{
				"MemoryCurrent": "100",
				"MemoryHigh":    "infinity",
				"MemoryMax":     "infinity",
			}
		},
	}

	snap := manager.Snapshot("")
	if snap.PoolMemoryCurrentBytes == nil || *snap.PoolMemoryCurrentBytes != 100 {
		t.Fatalf("pool current = %#v", snap.PoolMemoryCurrentBytes)
	}
	if snap.PoolMemoryHighBytes != nil || snap.PoolMemoryMaxBytes != nil {
		t.Fatalf("infinity thresholds must stay absent, got high=%#v max=%#v", snap.PoolMemoryHighBytes, snap.PoolMemoryMaxBytes)
	}
}

func TestParseSystemctlShowAndUint(t *testing.T) {
	props := parseSystemctlShow([]byte("MemoryCurrent=42\nMemoryPeak=[not set]\nTasksCurrent=3\n"))
	if props["MemoryCurrent"] != "42" || props["TasksCurrent"] != "3" {
		t.Fatalf("props = %#v", props)
	}
	if _, ok := parseSystemdUint64(props["MemoryPeak"]); ok {
		t.Fatal("not-set peak must be unavailable")
	}
	if value, ok := parseSystemdUint64("42"); !ok || value != 42 {
		t.Fatalf("parse uint = %d ok=%v", value, ok)
	}
}

func TestParseSystemctlShowBlocksBlankLineSeparated(t *testing.T) {
	oldUnit := "zen-agent-ff992d773e637a2859b1d466b0417c81-95f28a77fff94e8da964146d3e968502.scope"
	newUnit := "zen-agent-ff992d773e637a2859b1d466b0417c81-0ab0b633f5c44a31ae3baecd1850ad36.scope"
	raw := []byte("Id=" + oldUnit + "\nMemoryHigh=5321266995\nMemoryMax=6549251686\n\nId=" + newUnit + "\nMemoryHigh=infinity\nMemoryMax=infinity\n")
	byID := parseSystemctlShowBlocks(raw)
	if len(byID) != 2 {
		t.Fatalf("blocks = %#v", byID)
	}
	if byID[oldUnit]["MemoryHigh"] != "5321266995" || byID[oldUnit]["MemoryMax"] != "6549251686" {
		t.Fatalf("old block = %#v", byID[oldUnit])
	}
	if byID[newUnit]["MemoryHigh"] != "infinity" || byID[newUnit]["MemoryMax"] != "infinity" {
		t.Fatalf("new block = %#v", byID[newUnit])
	}
	if childMemoryLimitsNeedClear(byID[oldUnit]["MemoryHigh"], byID[oldUnit]["MemoryMax"]) != true {
		t.Fatal("old block should need clear")
	}
	if childMemoryLimitsNeedClear(byID[newUnit]["MemoryHigh"], byID[newUnit]["MemoryMax"]) != false {
		t.Fatal("new block should not need clear")
	}
}

func TestSystemdShowPropertiesWithIDExactlyOnce(t *testing.T) {
	got := systemdShowPropertiesWithID([]string{"MemoryCurrent", "MemoryPeak", "TasksCurrent"})
	if len(got) != 4 || got[0] != "Id" || got[1] != "MemoryCurrent" || got[2] != "MemoryPeak" || got[3] != "TasksCurrent" {
		t.Fatalf("without Id = %#v", got)
	}
	got = systemdShowPropertiesWithID([]string{"Id", "MemoryHigh", "MemoryMax", "Id"})
	if len(got) != 3 || got[0] != "Id" || got[1] != "MemoryHigh" || got[2] != "MemoryMax" {
		t.Fatalf("with duplicate Id = %#v", got)
	}
}

func TestReadSystemdPropertiesProductionIncludesIDForMemoryCurrent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "args.log")
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	systemctl := filepath.Join(dir, "systemctl")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
case "$*" in
  *show*)
    printf 'Id=%s\nMemoryCurrent=1200\n' "$ZEN_TEST_UNIT"
    ;;
esac
exit 0
`
	if err := os.WriteFile(systemctl, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_TEST_SYSTEMCTL_LOG", logPath)
	t.Setenv("ZEN_TEST_UNIT", unit)

	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	// Production path: no injected show hooks.
	manager.showUnitPropertiesFn = nil
	manager.showUnitsPropertiesFn = nil

	props := manager.readSystemdProperties(unit, "MemoryCurrent")
	if props["MemoryCurrent"] != "1200" {
		t.Fatalf("MemoryCurrent props = %#v", props)
	}
	if props["Id"] != unit {
		t.Fatalf("Id = %q, want %q", props["Id"], unit)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(raw)
	if strings.Count(calls, "--property=Id") != 1 {
		t.Fatalf("Id property count want 1:\n%s", calls)
	}
	if !strings.Contains(calls, "--property=MemoryCurrent") {
		t.Fatalf("missing MemoryCurrent property:\n%s", calls)
	}
	if !strings.Contains(calls, "show "+unit) {
		t.Fatalf("missing unit in show args:\n%s", calls)
	}
}

func TestReadSystemdUnitsPropertiesProductionDedupesRequestedID(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "args.log")
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	systemctl := filepath.Join(dir, "systemctl")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$ZEN_TEST_SYSTEMCTL_LOG"
case "$*" in
  *show*)
    printf 'Id=%s\nMemoryHigh=infinity\nMemoryMax=infinity\n' "$ZEN_TEST_UNIT"
    ;;
esac
exit 0
`
	if err := os.WriteFile(systemctl, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZEN_TEST_SYSTEMCTL_LOG", logPath)
	t.Setenv("ZEN_TEST_UNIT", unit)

	manager := newTestLinuxResourceManager(t, owner, systemctl, time.Now)
	manager.showUnitPropertiesFn = nil
	manager.showUnitsPropertiesFn = nil

	byUnit := manager.readSystemdUnitsProperties([]string{unit}, "Id", "MemoryHigh", "MemoryMax")
	if byUnit[unit]["MemoryHigh"] != "infinity" || byUnit[unit]["MemoryMax"] != "infinity" {
		t.Fatalf("props = %#v", byUnit[unit])
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "--property=Id") != 1 {
		t.Fatalf("Id must appear exactly once when already requested:\n%s", raw)
	}
}
