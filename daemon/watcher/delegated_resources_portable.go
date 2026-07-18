//go:build linux || darwin

package watcher

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/agentproc"
	"github.com/google/uuid"
)

const delegatedResourceReservationTTL = 2 * time.Minute

type portableDelegatedResourceManager struct {
	owner          string
	supervisor     string
	leaseDir       string
	tempRoot       string
	legacyTempRoot string // cleanup-only: ~/.zen/tmp/agent-resources/<owner>
	limits         delegatedResourceLimits

	mu                sync.Mutex
	byTarget          map[string]string
	reserved          map[string]time.Time
	lastFullScan      time.Time
	now               func() time.Time
	availableMemory   func() uint64
	listLeases        func(dir string) ([]agentproc.Lease, error)
	sampleOwnedLeases func(dir string) (agentproc.PoolSample, error)
}

func newPortableDelegatedResourceManager(owner string) (*portableDelegatedResourceManager, error) {
	owner = normalizeResourceOwner(owner)
	if owner == "" {
		return nil, fmt.Errorf("durable daemon identity is required")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate home directory: %w", err)
	}
	leaseDir := filepath.Join(home, ".zen", "run", "agent-resources", owner)
	if err := os.MkdirAll(leaseDir, 0o700); err != nil {
		return nil, fmt.Errorf("create delegated lease directory: %w", err)
	}
	// Short shared temp root keeps per-agent TMPDIR paths AF_UNIX-safe.
	tempRoot := filepath.Join(home, ".zen", "t")
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create delegated temporary root: %w", err)
	}
	// Legacy long temp root is cleanup-only for sessions launched before the
	// short marked layout. New Prepare calls never create under it.
	legacyTempRoot := filepath.Join(home, ".zen", "tmp", "agent-resources", owner)
	return &portableDelegatedResourceManager{
		owner:           owner,
		supervisor:      ZenExecutablePath(),
		leaseDir:        leaseDir,
		tempRoot:        tempRoot,
		legacyTempRoot:  legacyTempRoot,
		limits:          delegatedResourceLimitsForMemory(agentproc.PhysicalMemory()),
		byTarget:        make(map[string]string),
		reserved:        make(map[string]time.Time),
		now:             time.Now,
		availableMemory: agentproc.AvailableMemory,
	}, nil
}

func (m *portableDelegatedResourceManager) Prepare(activeSessions int) (*delegatedResourceSpec, error) {
	var ownedLeases map[string]bool
	if m.limits.MaxActiveSessions > 0 {
		leases, err := m.listOwnedLeases()
		if err != nil {
			return nil, fmt.Errorf("inspect delegated resource leases: %w", err)
		}
		ownedLeases = make(map[string]bool, len(leases))
		for _, lease := range leases {
			if validDelegatedResourceUnit(m.owner, lease.ResourceID) {
				ownedLeases[lease.ResourceID] = true
			}
		}
	}
	m.mu.Lock()
	m.expireReservationsLocked(m.now())
	if m.limits.MaxActiveSessions > 0 {
		reserved := 0
		for unit := range m.reserved {
			if !ownedLeases[unit] {
				reserved++
			}
		}
		activeSessions = max(activeSessions, len(ownedLeases)+reserved)
		if activeSessions >= m.limits.MaxActiveSessions {
			m.mu.Unlock()
			return nil, fmt.Errorf("active delegated session capacity reached (%d for this machine); reuse or close an existing delegated session, or explicitly configure ZEN_DELEGATED_MAX_SESSIONS", m.limits.MaxActiveSessions)
		}
	}
	if m.limits.HostReserve > 0 {
		available := uint64(0)
		if m.availableMemory != nil {
			available = m.availableMemory()
		} else {
			available = agentproc.AvailableMemory()
		}
		if available > 0 && available < m.limits.HostReserve {
			m.mu.Unlock()
			return nil, fmt.Errorf("delegated agent launch deferred under host memory pressure (available %d bytes is below host reserve %d); retry when memory is available", available, m.limits.HostReserve)
		}
	}
	unit := delegatedResourceUnit(m.owner, uuid.NewString())
	if unit == "" {
		m.mu.Unlock()
		return nil, fmt.Errorf("create delegated resource id")
	}
	m.reserved[unit] = m.now().Add(delegatedResourceReservationTTL)
	m.mu.Unlock()
	tempDir, err := m.createOwnedTempDir(unit)
	if err != nil {
		m.forgetUnit(unit)
		return nil, err
	}
	return &delegatedResourceSpec{
		Owner:      m.owner,
		Unit:       unit,
		Supervisor: m.supervisor,
		LeaseDir:   m.leaseDir,
		TempDir:    tempDir,
		Limits:     m.limits,
	}, nil
}

func (m *portableDelegatedResourceManager) listOwnedLeases() ([]agentproc.Lease, error) {
	if m.listLeases != nil {
		return m.listLeases(m.leaseDir)
	}
	return agentproc.ListLeases(m.leaseDir)
}

func (m *portableDelegatedResourceManager) Bind(target, unit string) {
	target = strings.TrimSpace(target)
	unit = strings.TrimSpace(unit)
	if target == "" || !validDelegatedResourceUnit(m.owner, unit) {
		return
	}
	m.mu.Lock()
	delete(m.reserved, unit)
	m.byTarget[target] = unit
	m.mu.Unlock()
}

func (m *portableDelegatedResourceManager) UnitForTarget(target string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byTarget[strings.TrimSpace(target)]
}

func (m *portableDelegatedResourceManager) reservedUnits() map[string]bool {
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireReservationsLocked(now)
	units := make(map[string]bool, len(m.reserved))
	for unit := range m.reserved {
		units[unit] = true
	}
	return units
}

func (m *portableDelegatedResourceManager) Reconcile(windows []tmuxWindow) {
	now := m.now()
	liveTargets := make(map[string]bool)
	liveUnits := make(map[string]bool)
	for _, window := range windows {
		if !window.delegated || !validDelegatedResourceUnit(m.owner, window.resourceUnit) {
			continue
		}
		liveTargets[window.target] = true
		liveUnits[window.resourceUnit] = true
		m.Bind(window.target, window.resourceUnit)
	}

	toStop := make(map[string]bool)
	m.mu.Lock()
	m.expireReservationsLocked(now)
	for target, unit := range m.byTarget {
		if !liveTargets[target] {
			toStop[unit] = true
		}
	}
	fullScan := m.lastFullScan.IsZero() || now.Sub(m.lastFullScan) >= 10*time.Second
	if fullScan {
		m.lastFullScan = now
	}
	for unit := range m.reserved {
		liveUnits[unit] = true
	}
	m.mu.Unlock()

	if fullScan {
		leases, err := agentproc.ListLeases(m.leaseDir)
		if err != nil {
			log.Printf("delegated lease reconciliation: %v", err)
		} else {
			for _, lease := range leases {
				if validDelegatedResourceUnit(m.owner, lease.ResourceID) && !liveUnits[lease.ResourceID] {
					toStop[lease.ResourceID] = true
				}
			}
		}
		entries, err := os.ReadDir(m.tempRoot)
		if err != nil && !os.IsNotExist(err) {
			log.Printf("delegated temporary directory reconciliation: %v", err)
		} else {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				tempDir := filepath.Join(m.tempRoot, entry.Name())
				unit, ok := readDelegatedTempMarker(tempDir)
				if !ok || !validDelegatedResourceUnit(m.owner, unit) || liveUnits[unit] {
					continue
				}
				toStop[unit] = true
			}
		}
		if strings.TrimSpace(m.legacyTempRoot) != "" {
			legacyEntries, legacyErr := os.ReadDir(m.legacyTempRoot)
			if legacyErr != nil && !os.IsNotExist(legacyErr) {
				log.Printf("delegated legacy temporary directory reconciliation: %v", legacyErr)
			} else {
				for _, entry := range legacyEntries {
					unit := entry.Name()
					if entry.IsDir() && validDelegatedResourceUnit(m.owner, unit) && !liveUnits[unit] {
						toStop[unit] = true
					}
				}
			}
		}
	}

	for unit := range toStop {
		if err := m.stopLease(unit); err != nil {
			log.Printf("release orphaned delegated lease %s: %v", unit, err)
			continue
		}
		m.forgetUnit(unit)
	}
}

func (m *portableDelegatedResourceManager) Release(target, unit string) error {
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
	if err := m.stopLease(unit); err != nil {
		return err
	}
	m.forgetUnit(unit)
	return nil
}

func (m *portableDelegatedResourceManager) stopLease(unit string) error {
	path, err := agentproc.LeasePath(m.leaseDir, unit)
	if err != nil {
		return err
	}
	if err := agentproc.StopLease(path); err != nil {
		return err
	}
	return m.removeOwnedTempDir(unit)
}

func (m *portableDelegatedResourceManager) createOwnedTempDir(unit string) (string, error) {
	tempDir, err := m.resourceTempDir(unit)
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(tempDir, 0o700); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("delegated temporary directory collision for %s", unit)
		}
		return "", fmt.Errorf("create delegated temporary directory: %w", err)
	}
	markerPath := filepath.Join(tempDir, delegatedTempMarkerName)
	if err := os.WriteFile(markerPath, []byte(unit+"\n"), 0o600); err != nil {
		_ = os.RemoveAll(tempDir)
		return "", fmt.Errorf("write delegated temporary ownership marker: %w", err)
	}
	if err := os.Chmod(markerPath, 0o600); err != nil {
		_ = os.RemoveAll(tempDir)
		return "", fmt.Errorf("chmod delegated temporary ownership marker: %w", err)
	}
	return tempDir, nil
}

func (m *portableDelegatedResourceManager) removeOwnedTempDir(unit string) error {
	if err := m.removeShortOwnedTempDir(unit); err != nil {
		return err
	}
	return m.removeLegacyOwnedTempDir(unit)
}

func (m *portableDelegatedResourceManager) removeShortOwnedTempDir(unit string) error {
	tempDir, err := m.resourceTempDir(unit)
	if err != nil {
		return err
	}
	markedUnit, ok := readDelegatedTempMarker(tempDir)
	if !ok || markedUnit != unit {
		// Foreign or corrupt short entries remain untouched.
		return nil
	}
	info, err := os.Lstat(tempDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat delegated temporary directory: %w", err)
	}
	// Never follow or mutate a symlink-replaced root.
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	// Re-check the marker after Lstat so a swapped foreign root stays untouched.
	markedUnit, ok = readDelegatedTempMarker(tempDir)
	if !ok || markedUnit != unit {
		return nil
	}
	if err := removeOwnedTree(tempDir); err != nil {
		return fmt.Errorf("remove delegated temporary directory: %w", err)
	}
	return nil
}

func (m *portableDelegatedResourceManager) removeLegacyOwnedTempDir(unit string) error {
	if strings.TrimSpace(m.legacyTempRoot) == "" || !validDelegatedResourceUnit(m.owner, unit) {
		return nil
	}
	legacy := filepath.Join(m.legacyTempRoot, unit)
	if filepath.Base(legacy) != unit {
		return nil
	}
	info, err := os.Lstat(legacy)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat legacy delegated temporary directory: %w", err)
	}
	// Exact owner/unit path only; never follow a symlink root into foreign content.
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if err := removeOwnedTree(legacy); err != nil {
		return fmt.Errorf("remove legacy delegated temporary directory: %w", err)
	}
	return nil
}

// removeOwnedTree deletes an already-validated owned temp tree. Nested tool
// caches (for example Go module dirs) may be mode 0555/000 with 0444 files;
// restore owner rwx on directories first so WalkDir can traverse and RemoveAll
// can unlink them.
func removeOwnedTree(root string) error {
	if err := makeOwnedTreeDirsOwnerAccessible(root); err != nil {
		return err
	}
	return os.RemoveAll(root)
}

// makeOwnedTreeDirsOwnerAccessible walks root without following symlinks and
// adds owner read+write+execute (mode|0700) only to real directories inside
// that tree. Owner-write alone is not enough: mode 000/0444 directories cannot
// be traversed until execute (and usually read) are restored. Callers must
// already have validated ownership of root; this never chmods sibling or
// foreign paths.
func makeOwnedTreeDirsOwnerAccessible(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode&0o700 == 0o700 {
			return nil
		}
		return os.Chmod(path, mode|0o700)
	})
}

func readDelegatedTempMarker(tempDir string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(tempDir, delegatedTempMarkerName))
	if err != nil {
		return "", false
	}
	unit := strings.TrimSpace(string(raw))
	if unit == "" {
		return "", false
	}
	return unit, true
}

func (m *portableDelegatedResourceManager) resourceTempDir(unit string) (string, error) {
	if !validDelegatedResourceUnit(m.owner, unit) {
		return "", fmt.Errorf("refuse unowned delegated resource unit %q", unit)
	}
	if strings.TrimSpace(m.tempRoot) == "" {
		return "", fmt.Errorf("delegated temporary root is unavailable")
	}
	digest := shortDelegatedTempDigest(unit)
	if digest == "" {
		return "", fmt.Errorf("delegated temporary digest unavailable")
	}
	return filepath.Join(m.tempRoot, digest), nil
}

func (m *portableDelegatedResourceManager) forgetUnit(unit string) {
	m.mu.Lock()
	delete(m.reserved, unit)
	for target, bound := range m.byTarget {
		if bound == unit {
			delete(m.byTarget, target)
		}
	}
	m.mu.Unlock()
}

func (m *portableDelegatedResourceManager) expireReservationsLocked(now time.Time) {
	for unit, deadline := range m.reserved {
		if !deadline.After(now) {
			delete(m.reserved, unit)
		}
	}
}
