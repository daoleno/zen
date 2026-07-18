//go:build linux

package watcher

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func (m *linuxDelegatedResourceManager) Snapshot(target string) SessionResourceSnapshot {
	if m == nil || m.portableDelegatedResourceManager == nil {
		return SessionResourceSnapshot{}
	}
	snap := SessionResourceSnapshot{
		Backend: resourceBackendCgroupPool,
	}
	// Linux displayed pool high/max must reflect the live parent-slice properties
	// in this same on-demand path. Do not recompute policy thresholds here; omit
	// when systemd reports infinity/unset so the UI never claims a computed
	// target is the currently applied cgroup limit.
	applyHostObservation(&snap, m.availableMemory, m.limits.HostReserve)

	slice := delegatedResourceSlice(m.owner)
	if slice != "" {
		props := m.readSystemdProperties(slice, "MemoryCurrent", "MemoryHigh", "MemoryMax")
		if current, ok := parseSystemdUint64(props["MemoryCurrent"]); ok {
			snap.PoolMemoryCurrentBytes = uint64Ptr(current)
		}
		if high, ok := parseSystemdUint64(props["MemoryHigh"]); ok {
			snap.PoolMemoryHighBytes = uint64Ptr(high)
		}
		if max, ok := parseSystemdUint64(props["MemoryMax"]); ok {
			snap.PoolMemoryMaxBytes = uint64Ptr(max)
		}
	}

	target = strings.TrimSpace(target)
	unit := ""
	if target != "" {
		unit = m.UnitForTarget(target)
	}
	if unit == "" || !validDelegatedResourceUnit(m.owner, unit) {
		return snap
	}
	snap.Managed = true

	props := m.readSystemdProperties(unit,
		"MemoryCurrent",
		"MemoryPeak",
		"TasksCurrent",
	)
	if current, ok := parseSystemdUint64(props["MemoryCurrent"]); ok {
		snap.MemoryCurrentBytes = uint64Ptr(current)
	}
	if peak, ok := parseSystemdUint64(props["MemoryPeak"]); ok {
		snap.MemoryPeakBytes = uint64Ptr(peak)
	}
	if tasks, ok := parseSystemdInt(props["TasksCurrent"]); ok {
		snap.TasksCurrent = intPtr(tasks)
	}
	return snap
}

func (m *linuxDelegatedResourceManager) readSystemdProperties(unit string, properties ...string) map[string]string {
	if m == nil || strings.TrimSpace(unit) == "" || len(properties) == 0 {
		return nil
	}
	byUnit := m.readSystemdUnitsProperties([]string{unit}, properties...)
	if len(byUnit) == 0 {
		return nil
	}
	return byUnit[unit]
}

// readSystemdUnitsProperties issues one systemctl show over units and returns
// property maps keyed by unit Id. Blank-line-separated multi-unit output is the
// scalable inspection path for live child-limit migration. The production
// command always requests Id exactly once so blocks remain keyed even when
// callers (e.g. Snapshot) only ask for measurement fields.
func (m *linuxDelegatedResourceManager) readSystemdUnitsProperties(units []string, properties ...string) map[string]map[string]string {
	if m == nil || len(units) == 0 || len(properties) == 0 {
		return nil
	}
	if m.showUnitsPropertiesFn != nil {
		return m.showUnitsPropertiesFn(units, properties...)
	}
	if m.showUnitPropertiesFn != nil {
		out := make(map[string]map[string]string, len(units))
		for _, unit := range units {
			unit = strings.TrimSpace(unit)
			if unit == "" {
				continue
			}
			props := m.showUnitPropertiesFn(unit, properties...)
			if len(props) == 0 {
				continue
			}
			copied := make(map[string]string, len(props)+1)
			for key, value := range props {
				copied[key] = value
			}
			if strings.TrimSpace(copied["Id"]) == "" {
				copied["Id"] = unit
			}
			out[unit] = copied
		}
		return out
	}
	if strings.TrimSpace(m.systemctl) == "" {
		return nil
	}
	showProperties := systemdShowPropertiesWithID(properties)
	args := make([]string, 0, 3+len(units)+len(showProperties))
	args = append(args, "--user", "show")
	args = append(args, units...)
	args = append(args, "--no-pager")
	for _, property := range showProperties {
		args = append(args, "--property="+property)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.systemctl, args...).CombinedOutput()
	if err != nil {
		return nil
	}
	return parseSystemctlShowBlocks(out)
}

// systemdShowPropertiesWithID returns the caller's property list with Id
// present exactly once so multi-unit show blocks can be keyed by Id.
func systemdShowPropertiesWithID(properties []string) []string {
	out := make([]string, 0, len(properties)+1)
	seenID := false
	for _, property := range properties {
		property = strings.TrimSpace(property)
		if property == "" {
			continue
		}
		if property == "Id" {
			if seenID {
				continue
			}
			seenID = true
		}
		out = append(out, property)
	}
	if !seenID {
		out = append([]string{"Id"}, out...)
	}
	return out
}

func parseSystemctlShow(out []byte) map[string]string {
	props := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || key == "" {
			continue
		}
		props[key] = value
	}
	return props
}

// parseSystemctlShowBlocks parses blank-line-separated systemctl show blocks
// from a multi-unit invocation into maps keyed by Id.
func parseSystemctlShowBlocks(out []byte) map[string]map[string]string {
	byID := make(map[string]map[string]string)
	for _, block := range strings.Split(string(out), "\n\n") {
		props := parseSystemctlShow([]byte(block))
		id := strings.TrimSpace(props["Id"])
		if id == "" {
			continue
		}
		byID[id] = props
	}
	return byID
}

func parseSystemdUint64(raw string) (uint64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[not set]" || strings.EqualFold(raw, "infinity") {
		return 0, false
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func parseSystemdInt(raw string) (int, bool) {
	value, ok := parseSystemdUint64(raw)
	if !ok || value > uint64(^uint(0)>>1) {
		return 0, false
	}
	return int(value), true
}
