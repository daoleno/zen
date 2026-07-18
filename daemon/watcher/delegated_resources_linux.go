//go:build linux

package watcher

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	tmpfsMagic     = 0x01021994
	ramfsMagic     = 0x858458f6
	hugetlbfsMagic = 0x958458f6
)

// linuxDelegatedResourceManager layers a kernel-enforced systemd scope over
// the portable supervisor. The supervisor provides the same durable lease and
// process-tree semantics used on macOS; systemd adds aggregate cgroup limits on
// the owned parent slice and control-group teardown on Linux.
type linuxDelegatedResourceManager struct {
	*portableDelegatedResourceManager
	systemdRun      string
	systemctl       string
	lastSystemdScan time.Time
	sliceReady      bool
	// normalizedChildLimits tracks live scopes whose child MemoryHigh/MemoryMax
	// were confirmed infinity (or cleared) once this manager lifetime. Failures
	// stay absent so the next bounded scan can retry.
	normalizedChildLimits map[string]struct{}
	listOwnedUnitsFn      func() ([]string, error)
	showUnitPropertiesFn  func(unit string, properties ...string) map[string]string
	// showUnitsPropertiesFn batches multi-unit inspection. When nil, production
	// uses one systemctl show over the candidate list; tests may stub either
	// this or showUnitPropertiesFn.
	showUnitsPropertiesFn func(units []string, properties ...string) map[string]map[string]string
	setUnitPropertiesFn   func(unit string, properties ...string) error
}

func newDelegatedResourceManager(owner string) delegatedResourceManager {
	portable, err := newPortableDelegatedResourceManager(owner)
	if err != nil {
		return unavailableDelegatedResourceManager{reason: err.Error()}
	}
	systemdRun, runErr := exec.LookPath("systemd-run")
	systemctl, ctlErr := exec.LookPath("systemctl")
	if runErr != nil || ctlErr != nil {
		return portable
	}
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return portable
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, systemctl, "--user", "show-environment").Run(); err != nil {
		return portable
	}
	return &linuxDelegatedResourceManager{
		portableDelegatedResourceManager: portable,
		systemdRun:                       systemdRun,
		systemctl:                        systemctl,
	}
}

func (m *linuxDelegatedResourceManager) Prepare(activeSessions int) (*delegatedResourceSpec, error) {
	if err := m.ensureSharedPoolSlice(); err != nil {
		return nil, err
	}
	if m.limits.MaxActiveSessions > 0 {
		units, err := m.listOwnedUnits()
		if err != nil {
			return nil, fmt.Errorf("inspect delegated resource units: %w", err)
		}
		activeSessions = max(activeSessions, len(units))
	}
	spec, err := m.portableDelegatedResourceManager.Prepare(activeSessions)
	if err != nil {
		return nil, err
	}
	spec.SystemdRun = m.systemdRun
	spec.Slice = delegatedResourceSlice(m.owner)
	return spec, nil
}

func (m *linuxDelegatedResourceManager) ensureSharedPoolSlice() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sliceReady {
		return nil
	}
	limits := m.limits
	slice := delegatedResourceSlice(m.owner)
	if slice == "" {
		return fmt.Errorf("delegated resource slice is unavailable")
	}
	// Ensure the owned parent slice exists, then apply runtime-only aggregate
	// memory limits once. Child scopes inherit the pool and carry no per-agent
	// MemoryHigh/MemoryMax of their own. Hold mu across bootstrap/set-property so
	// concurrent Prepare calls cannot configure the slice more than once.
	bootstrapUnit := "zen-agents-" + m.owner + "-pool-init.scope"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	bootstrapOut, bootstrapErr := exec.CommandContext(ctx, m.systemdRun,
		"--user",
		"--scope",
		"--quiet",
		"--collect",
		"--unit="+bootstrapUnit,
		"--slice="+slice,
		"/bin/true",
	).CombinedOutput()
	cancel()
	if bootstrapErr != nil && !strings.Contains(string(bootstrapOut), "already exists") {
		// Slice may already exist from a prior daemon; continue to set-property.
		if _, showErr := m.sliceLoadState(slice); showErr != nil {
			return fmt.Errorf("create delegated resource slice %s: %w: %s", slice, bootstrapErr, strings.TrimSpace(string(bootstrapOut)))
		}
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	out, err := exec.CommandContext(ctx, m.systemctl, "--user", "set-property", "--runtime",
		slice,
		"MemoryAccounting=yes",
		"MemoryHigh="+limits.MemoryHigh,
		"MemoryMax="+limits.MemoryMax,
	).CombinedOutput()
	cancel()
	if err != nil {
		return fmt.Errorf("configure delegated resource slice %s: %w: %s", slice, err, strings.TrimSpace(string(out)))
	}
	m.sliceReady = true
	return nil
}

func (m *linuxDelegatedResourceManager) sliceLoadState(slice string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.systemctl, "--user", "show", slice, "--property=LoadState", "--no-pager").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("systemctl show %s: %w: %s", slice, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func (m *linuxDelegatedResourceManager) Reconcile(windows []tmuxWindow) {
	m.portableDelegatedResourceManager.Reconcile(windows)
	now := m.now()
	m.mu.Lock()
	fullScan := m.lastSystemdScan.IsZero() || now.Sub(m.lastSystemdScan) >= 10*time.Second
	if fullScan {
		m.lastSystemdScan = now
	}
	m.mu.Unlock()
	if !fullScan {
		return
	}
	// Tmux-proven live markers (migration candidates) vs live-or-reserved
	// (orphan keep set). Reserved-only ids are never migration candidates.
	liveFromTmux := make(map[string]bool)
	liveOrReserved := make(map[string]bool)
	for _, window := range windows {
		if window.delegated && validDelegatedResourceUnit(m.owner, window.resourceUnit) {
			liveFromTmux[window.resourceUnit] = true
			liveOrReserved[window.resourceUnit] = true
		}
	}
	for unit := range m.reservedUnits() {
		liveOrReserved[unit] = true
	}

	owned, err := m.listOwnedUnits()
	if err != nil {
		log.Printf("delegated systemd reconciliation: %v", err)
		return
	}
	ownedSet := make(map[string]bool, len(owned))
	for _, unit := range owned {
		if validDelegatedResourceUnit(m.owner, unit) {
			ownedSet[unit] = true
		}
	}
	// Migrate only units proven by both tmux resource markers and the current
	// systemd owned-unit list from this single scan.
	liveOwned := make(map[string]bool)
	for unit := range liveFromTmux {
		if ownedSet[unit] {
			liveOwned[unit] = true
		}
	}
	m.normalizeLiveChildMemoryLimits(liveOwned)

	for _, unit := range owned {
		if liveOrReserved[unit] {
			continue
		}
		if err := m.stopUnit(unit); err != nil {
			log.Printf("release orphaned delegated resource %s: %v", unit, err)
		}
	}
}

// normalizeLiveChildMemoryLimits clears rejected per-Session MemoryHigh/MemoryMax
// on the tmux∩owned live intersection so they inherit the shared parent pool.
// Candidates are inspected in one bounded multi-unit systemctl show (sorted for
// determinism); already-infinity blocks are marked normalized without rewrite.
// Partial property blocks stay retryable. Parent slice configuration is
// confirmed only before clearing finite legacy units. normalizedChildLimits is
// pruned to the current live-owned intersection each scan.
func (m *linuxDelegatedResourceManager) normalizeLiveChildMemoryLimits(liveOwned map[string]bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.normalizedChildLimits == nil {
		m.normalizedChildLimits = make(map[string]struct{})
	}
	for unit := range m.normalizedChildLimits {
		if !liveOwned[unit] {
			delete(m.normalizedChildLimits, unit)
		}
	}
	candidates := make([]string, 0, len(liveOwned))
	for unit := range liveOwned {
		if !validDelegatedResourceUnit(m.owner, unit) {
			continue
		}
		if _, done := m.normalizedChildLimits[unit]; done {
			continue
		}
		candidates = append(candidates, unit)
	}
	sort.Strings(candidates)
	m.mu.Unlock()
	if len(candidates) == 0 {
		return
	}

	inspected := m.readSystemdUnitsProperties(candidates, "Id", "MemoryHigh", "MemoryMax")
	if len(inspected) == 0 {
		log.Printf("delegated child memory limit migration: inspect failed for %d live units", len(candidates))
		return
	}

	legacy := make([]string, 0)
	for _, unit := range candidates {
		props, ok := inspected[unit]
		if !ok || len(props) == 0 {
			log.Printf("normalize delegated child memory limits %s: missing from batch inspect", unit)
			continue
		}
		memoryHigh, hasHigh := props["MemoryHigh"]
		memoryMax, hasMax := props["MemoryMax"]
		if !hasHigh || !hasMax {
			log.Printf("normalize delegated child memory limits %s: incomplete MemoryHigh/MemoryMax in batch inspect", unit)
			continue
		}
		if !childMemoryLimitsNeedClear(memoryHigh, memoryMax) {
			m.markChildLimitsNormalized(unit)
			continue
		}
		legacy = append(legacy, unit)
	}
	if len(legacy) == 0 {
		return
	}
	if err := m.ensureSharedPoolSlice(); err != nil {
		log.Printf("delegated child memory limit migration deferred: parent pool: %v", err)
		return
	}
	for _, unit := range legacy {
		if err := m.clearChildMemoryLimits(unit); err != nil {
			log.Printf("normalize delegated child memory limits %s: %v", unit, err)
			continue
		}
		m.markChildLimitsNormalized(unit)
	}
}

func (m *linuxDelegatedResourceManager) markChildLimitsNormalized(unit string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.normalizedChildLimits == nil {
		m.normalizedChildLimits = make(map[string]struct{})
	}
	m.normalizedChildLimits[unit] = struct{}{}
}

func (m *linuxDelegatedResourceManager) clearChildMemoryLimits(unit string) error {
	if !validDelegatedResourceUnit(m.owner, unit) {
		return fmt.Errorf("refuse unowned delegated resource unit %q", unit)
	}
	properties := []string{"MemoryHigh=infinity", "MemoryMax=infinity"}
	if m.setUnitPropertiesFn != nil {
		return m.setUnitPropertiesFn(unit, properties...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := append([]string{"--user", "set-property", "--runtime", unit}, properties...)
	out, err := exec.CommandContext(ctx, m.systemctl, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl set-property %s: %w: %s", unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func childMemoryLimitsNeedClear(memoryHigh, memoryMax string) bool {
	return !systemdMemoryLimitIsUnbounded(memoryHigh) || !systemdMemoryLimitIsUnbounded(memoryMax)
}

func systemdMemoryLimitIsUnbounded(raw string) bool {
	raw = strings.TrimSpace(raw)
	return raw == "" || raw == "[not set]" || strings.EqualFold(raw, "infinity")
}

func (m *linuxDelegatedResourceManager) Release(target, unit string) error {
	target = strings.TrimSpace(target)
	unit = strings.TrimSpace(unit)
	if unit == "" && target != "" {
		unit = m.UnitForTarget(target)
	}
	if unit == "" {
		return nil
	}
	if !validDelegatedResourceUnit(m.owner, unit) {
		return fmt.Errorf("refuse unowned delegated resource unit %q", unit)
	}
	systemdErr := m.stopUnit(unit)
	portableErr := m.portableDelegatedResourceManager.Release(target, unit)
	if systemdErr != nil && portableErr != nil {
		return fmt.Errorf("stop systemd scope: %v; stop portable lease: %w", systemdErr, portableErr)
	}
	if systemdErr != nil {
		return systemdErr
	}
	return portableErr
}

func (m *linuxDelegatedResourceManager) listOwnedUnits() ([]string, error) {
	if m.listOwnedUnitsFn != nil {
		return m.listOwnedUnitsFn()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pattern := "zen-agent-" + m.owner + "-*.scope"
	out, err := exec.CommandContext(ctx, m.systemctl, "--user", "list-units", "--all", "--type=scope", "--plain", "--no-legend", "--no-pager", pattern).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("systemctl list-units: %w: %s", err, strings.TrimSpace(string(out)))
	}
	units := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && validDelegatedResourceUnit(m.owner, fields[0]) {
			units = append(units, fields[0])
		}
	}
	return units, nil
}

func (m *linuxDelegatedResourceManager) stopUnit(unit string) error {
	if !validDelegatedResourceUnit(m.owner, unit) {
		return fmt.Errorf("refuse unowned delegated resource unit %q", unit)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	out, err := exec.CommandContext(ctx, m.systemctl, "--user", "stop", unit).CombinedOutput()
	cancel()
	if err == nil {
		return nil
	}

	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	state, showErr := exec.CommandContext(ctx, m.systemctl, "--user", "show", unit, "--property=LoadState", "--property=ActiveState", "--no-pager").CombinedOutput()
	stateText := string(state)
	if showErr == nil && (strings.Contains(stateText, "LoadState=not-found") || strings.Contains(stateText, "ActiveState=inactive")) {
		return nil
	}
	return fmt.Errorf("systemctl stop %s: %w: %s", unit, err, strings.TrimSpace(string(out)))
}

func validateDelegatedWorkspace(cwd string) error {
	resolved, err := validateDelegatedWorkspacePath(cwd)
	if err != nil {
		return err
	}
	if resolved == "" {
		return nil
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(resolved, &stat); err != nil {
		return nil
	}
	switch uint64(stat.Type) {
	case tmpfsMagic, ramfsMagic, hugetlbfsMagic:
		return fmt.Errorf("delegated agent cwd %q is on memory-backed temporary storage; use a durable workspace such as $ZEN_WORKTREE_ROOT (default ~/.zen/worktrees)", cwd)
	default:
		return nil
	}
}
