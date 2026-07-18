package watcher

import "github.com/daoleno/zen/daemon/agentproc"

const (
	resourceBackendCgroupPool         = "cgroup_pool"
	resourceBackendPortableSupervisor = "portable_supervisor"

	hostPressureOK       = "ok"
	hostPressurePressure = "pressure"
)

// SessionResourceSnapshot is one on-demand read-only projection of the current
// Session's owned resources and the daemon shared pool. Optional measurements
// use pointers so unsupported values stay absent in JSON (never invented zero).
type SessionResourceSnapshot struct {
	Backend string `json:"backend,omitempty"`
	Managed bool   `json:"managed,omitempty"`

	MemoryCurrentBytes *uint64 `json:"memory_current_bytes,omitempty"`
	MemoryPeakBytes    *uint64 `json:"memory_peak_bytes,omitempty"`
	TasksCurrent       *int    `json:"tasks_current,omitempty"`

	PoolMemoryCurrentBytes *uint64 `json:"pool_memory_current_bytes,omitempty"`
	PoolMemoryHighBytes    *uint64 `json:"pool_memory_high_bytes,omitempty"`
	PoolMemoryMaxBytes     *uint64 `json:"pool_memory_max_bytes,omitempty"`

	HostAvailableBytes *uint64 `json:"host_available_bytes,omitempty"`
	HostPressure       string  `json:"host_pressure,omitempty"`
}

func uint64Ptr(value uint64) *uint64 { return &value }

func intPtr(value int) *int { return &value }

func hostPressureState(available, hostReserve uint64) string {
	if available == 0 {
		return ""
	}
	if hostReserve > 0 && available < hostReserve {
		return hostPressurePressure
	}
	return hostPressureOK
}

func applySharedPoolLimits(snap *SessionResourceSnapshot, limits delegatedResourceLimits) {
	total := agentproc.PhysicalMemory()
	if high, err := agentproc.ParseMemoryLimit(limits.MemoryHigh, total); err == nil && high > 0 {
		snap.PoolMemoryHighBytes = uint64Ptr(high)
	}
	if max, err := agentproc.ParseMemoryLimit(limits.MemoryMax, total); err == nil && max > 0 {
		snap.PoolMemoryMaxBytes = uint64Ptr(max)
	}
}

func applyHostObservation(snap *SessionResourceSnapshot, available func() uint64, hostReserve uint64) {
	if available == nil {
		return
	}
	value := available()
	if value == 0 {
		return
	}
	snap.HostAvailableBytes = uint64Ptr(value)
	snap.HostPressure = hostPressureState(value, hostReserve)
}
