//go:build linux || darwin

package watcher

import (
	"strings"

	"github.com/daoleno/zen/daemon/agentproc"
)

func (m *portableDelegatedResourceManager) Snapshot(target string) SessionResourceSnapshot {
	snap := SessionResourceSnapshot{
		Backend: resourceBackendPortableSupervisor,
	}
	if m == nil {
		return SessionResourceSnapshot{}
	}

	applySharedPoolLimits(&snap, m.limits)
	applyHostObservation(&snap, m.availableMemory, m.limits.HostReserve)

	target = strings.TrimSpace(target)
	unit := ""
	if target != "" {
		unit = m.UnitForTarget(target)
	}
	if unit == "" || !validDelegatedResourceUnit(m.owner, unit) {
		return snap
	}
	snap.Managed = true

	sampleFn := m.sampleOwnedLeases
	if sampleFn == nil {
		sampleFn = agentproc.SampleOwnedLeases
	}
	sample, err := sampleFn(m.leaseDir)
	if err != nil {
		return snap
	}
	if rss, ok := sample.RSSByLease[unit]; ok {
		snap.MemoryCurrentBytes = uint64Ptr(rss)
	}
	if tasks, ok := sample.TasksByLease[unit]; ok {
		snap.TasksCurrent = intPtr(tasks)
	}
	snap.PoolMemoryCurrentBytes = uint64Ptr(sample.AggregateRSS)
	return snap
}
