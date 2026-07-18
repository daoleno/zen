//go:build linux || darwin

package watcher

import (
	"strconv"
	"testing"

	"github.com/daoleno/zen/daemon/agentproc"
)

func TestPortableSnapshotOmitsInventedZerosAndLabelsSharedPool(t *testing.T) {
	owner := "snapowner"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	other := delegatedResourceUnit(owner, "fedcba9876543210fedcba9876543210")
	manager := &portableDelegatedResourceManager{
		owner:    owner,
		leaseDir: t.TempDir(),
		limits: delegatedResourceLimits{
			MemoryHigh:  "25000000000",
			MemoryMax:   "28000000000",
			HostReserve: 4 * 1024 * 1024 * 1024,
		},
		byTarget: map[string]string{"main:@7": unit},
		availableMemory: func() uint64 {
			return 8 * 1024 * 1024 * 1024
		},
		sampleOwnedLeases: func(string) (agentproc.PoolSample, error) {
			return agentproc.PoolSample{
				RSSByLease: map[string]uint64{
					unit:  1500,
					other: 500,
				},
				TasksByLease: map[string]int{
					unit:  3,
					other: 1,
				},
				AggregateRSS: 2000,
			}, nil
		},
	}

	snap := manager.Snapshot("main:@7")
	if !snap.Managed || snap.Backend != resourceBackendPortableSupervisor {
		t.Fatalf("managed/backend = %v %q", snap.Managed, snap.Backend)
	}
	if snap.MemoryCurrentBytes == nil || *snap.MemoryCurrentBytes != 1500 {
		t.Fatalf("session memory = %#v", snap.MemoryCurrentBytes)
	}
	if snap.MemoryPeakBytes != nil {
		t.Fatalf("portable peak must stay absent, got %#v", snap.MemoryPeakBytes)
	}
	if snap.TasksCurrent == nil || *snap.TasksCurrent != 3 {
		t.Fatalf("tasks = %#v", snap.TasksCurrent)
	}
	if snap.PoolMemoryCurrentBytes == nil || *snap.PoolMemoryCurrentBytes != 2000 {
		t.Fatalf("pool current = %#v", snap.PoolMemoryCurrentBytes)
	}
	if snap.PoolMemoryHighBytes == nil || *snap.PoolMemoryHighBytes != 25000000000 {
		t.Fatalf("pool high = %#v", snap.PoolMemoryHighBytes)
	}
	if snap.PoolMemoryMaxBytes == nil || *snap.PoolMemoryMaxBytes != 28000000000 {
		t.Fatalf("pool max = %#v", snap.PoolMemoryMaxBytes)
	}
	if snap.HostAvailableBytes == nil || *snap.HostAvailableBytes != 8*1024*1024*1024 {
		t.Fatalf("host available = %#v", snap.HostAvailableBytes)
	}
	if snap.HostPressure != hostPressureOK {
		t.Fatalf("host pressure = %q", snap.HostPressure)
	}

	unmanaged := manager.Snapshot("main:@missing")
	if unmanaged.Managed || unmanaged.MemoryCurrentBytes != nil || unmanaged.TasksCurrent != nil {
		t.Fatalf("unmanaged snapshot leaked session measurements: %#v", unmanaged)
	}
	if unmanaged.PoolMemoryHighBytes == nil || unmanaged.HostAvailableBytes == nil {
		t.Fatalf("shared context should remain available for unmanaged targets: %#v", unmanaged)
	}
}

func TestPortableSnapshotHostPressureWhenBelowReserve(t *testing.T) {
	manager := &portableDelegatedResourceManager{
		owner: "pressure",
		limits: delegatedResourceLimits{
			MemoryHigh:  "1",
			MemoryMax:   "2",
			HostReserve: 4 * 1024 * 1024 * 1024,
		},
		availableMemory: func() uint64 { return 1024 },
		sampleOwnedLeases: func(string) (agentproc.PoolSample, error) {
			return agentproc.PoolSample{}, nil
		},
	}
	snap := manager.Snapshot("")
	if snap.HostPressure != hostPressurePressure {
		t.Fatalf("host pressure = %q, want %q", snap.HostPressure, hostPressurePressure)
	}
}

func TestHostPressureStateOmitsUnavailable(t *testing.T) {
	if got := hostPressureState(0, 100); got != "" {
		t.Fatalf("unavailable available memory should omit pressure, got %q", got)
	}
}

func TestApplySharedPoolLimitsOmitsEmptyAndZero(t *testing.T) {
	snap := SessionResourceSnapshot{}
	applySharedPoolLimits(&snap, delegatedResourceLimits{MemoryHigh: "", MemoryMax: "0"})
	if snap.PoolMemoryHighBytes != nil || snap.PoolMemoryMaxBytes != nil {
		t.Fatalf("empty/zero limits must stay absent, got high=%#v max=%#v", snap.PoolMemoryHighBytes, snap.PoolMemoryMaxBytes)
	}
	applySharedPoolLimits(&snap, delegatedResourceLimits{MemoryHigh: strconv.FormatUint(1234, 10), MemoryMax: "2G"})
	if snap.PoolMemoryHighBytes == nil || *snap.PoolMemoryHighBytes != 1234 {
		t.Fatalf("absolute high = %#v", snap.PoolMemoryHighBytes)
	}
	if snap.PoolMemoryMaxBytes == nil || *snap.PoolMemoryMaxBytes != 2<<30 {
		t.Fatalf("sized max = %#v", snap.PoolMemoryMaxBytes)
	}
}
