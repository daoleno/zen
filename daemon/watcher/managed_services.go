package watcher

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Managed service discovery covers Agent-retained user services that outlive
// the tmux Sessions that created them (typically user systemd units). tmux
// discovery in services.go is unchanged; this file adds the smallest explicit
// persistent registration/attribution path on top of it.
//
// A service is attributed ONLY when all of the following hold:
//  1. its unit name was explicitly registered (no name/port heuristics), and
//  2. structured `systemctl --user show` reports the unit active, and
//  3. the listening socket PID is the unit's live MainPID or sits in the
//     unit's cgroup.
//
// Stop/replacement/foreign-unit/PID reuse can therefore never produce a stale
// positive: every discovery re-reads live unit properties and live sockets.

const (
	// ServiceSourceSession marks rows owned by a live tmux Session.
	ServiceSourceSession = "session"
	// ServiceSourcePersistent marks rows owned by a registered persistent unit.
	ServiceSourcePersistent = "persistent"

	// ServiceStateActive marks a registered unit that is active with live sockets.
	ServiceStateActive = "active"
	// ServiceStateInactive marks a registered unit that is not active (or has
	// no live owned sockets). Shown honestly; never a stale positive.
	ServiceStateInactive = "inactive"
	// ServiceStateError marks a registered unit whose live state could not be
	// determined. A broken systemd query is surfaced per row and never
	// reported as silent success.
	ServiceStateError = "error"

	managedServicesFileVersion = 1
)

// Budget knobs for managed discovery. The mobile sheet rejects after 10s,
// so the aggregate stays comfortably below it: one shared deadline covers all
// units (N hung units cannot multiply beyond it) with a shorter per-unit cap
// and a bounded pipe drain. Vars (not consts) so tests can shrink them.
// No App timeout change, no scheduler, no framework.
var (
	// managedDiscoveryBudget is the shared deadline for resolving every
	// registered unit in one snapshot.
	managedDiscoveryBudget = 6 * time.Second
	// systemdUnitTimeout caps one unit query (discovery per-unit, register).
	systemdUnitTimeout = 4 * time.Second
	// serviceProcWaitDelay bounds the pipe drain after killing a stuck query.
	serviceProcWaitDelay = 1 * time.Second
)

// ManagedServiceDescriptor is one explicitly registered persistent service.
// It is identity + provenance only; liveness always comes from live systemd
// properties and live socket ownership at discovery time.
type ManagedServiceDescriptor struct {
	Unit         string    `json:"unit"`
	Name         string    `json:"name"`
	Project      string    `json:"project,omitempty"`
	Cwd          string    `json:"cwd,omitempty"`
	Port         int       `json:"port,omitempty"`
	RegisteredBy string    `json:"registered_by,omitempty"`
	WorkID       string    `json:"work_id,omitempty"`
	RegisteredAt time.Time `json:"registered_at"`
}

type managedServicesFile struct {
	Version  int                        `json:"version"`
	Services []ManagedServiceDescriptor `json:"services"`
}

var managedServiceUnitPattern = regexp.MustCompile(`^[A-Za-z0-9_.@:-]+\.service$`)

// ValidateManagedServiceUnit rejects anything that is not a plain user unit
// name. Paths, flags and template traversals never reach systemctl.
func ValidateManagedServiceUnit(unit string) error {
	unit = strings.TrimSpace(unit)
	if unit == "" {
		return fmt.Errorf("unit name is required")
	}
	if !managedServiceUnitPattern.MatchString(unit) {
		return fmt.Errorf("invalid unit name %q: want a plain user unit like dsh-web.service", unit)
	}
	return nil
}

// SetManagedServicesPath installs the persisted registry file used by
// Register/Unregister/List and by DiscoverSessionServices. It lives in the
// daemon state dir so completed Worker cleanup or daemon restart cannot lose
// explicit registrations.
func (w *Watcher) SetManagedServicesPath(path string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.managedServicesPath = strings.TrimSpace(path)
	w.mu.Unlock()
}

func (w *Watcher) managedServicesFilePath() string {
	if w == nil {
		return ""
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.managedServicesPath
}

// ManagedServicesPathForStateDir is the registry location convention for a
// daemon state dir. No database or framework: one small JSON descriptor file
// matches existing persisted-state conventions.
func ManagedServicesPathForStateDir(stateDir string) string {
	return filepath.Join(strings.TrimSpace(stateDir), "managed-services.json")
}

// ListManagedServices returns explicitly registered persistent services in
// registration order. An absent registry is an empty list, never an error.
func (w *Watcher) ListManagedServices() ([]ManagedServiceDescriptor, error) {
	return loadManagedServices(w.managedServicesFilePath())
}

// RegisterManagedService explicitly adopts one persistent user service without
// restarting or otherwise touching it. Registration verifies the unit is
// visible to the caller's user systemd instance (read-only `systemctl show`)
// so typos and foreign units fail closed at handoff time. The read-modify-
// write is serialized: concurrent control handlers cannot lose updates.
func (w *Watcher) RegisterManagedService(desc ManagedServiceDescriptor) (ManagedServiceDescriptor, error) {
	if w == nil {
		return ManagedServiceDescriptor{}, fmt.Errorf("watcher unavailable")
	}
	desc.Unit = strings.TrimSpace(desc.Unit)
	if err := ValidateManagedServiceUnit(desc.Unit); err != nil {
		return ManagedServiceDescriptor{}, err
	}
	desc.Name = strings.TrimSpace(desc.Name)
	if desc.Name == "" {
		return ManagedServiceDescriptor{}, fmt.Errorf("service name is required")
	}
	desc.Project = strings.TrimSpace(desc.Project)
	desc.Cwd = strings.TrimSpace(desc.Cwd)
	desc.RegisteredBy = strings.TrimSpace(desc.RegisteredBy)
	desc.WorkID = strings.TrimSpace(desc.WorkID)
	if desc.Port < 0 || desc.Port > 65535 {
		return ManagedServiceDescriptor{}, fmt.Errorf("invalid port %d", desc.Port)
	}
	if runtime.GOOS != "linux" {
		return ManagedServiceDescriptor{}, fmt.Errorf("persistent service registration requires Linux user systemd; tmux discovery remains available on this platform")
	}
	path := w.managedServicesFilePath()
	if strings.TrimSpace(path) == "" {
		return ManagedServiceDescriptor{}, fmt.Errorf("managed services registry is not configured")
	}
	// Slow read-only verification stays outside the registry lock, bounded
	// like discovery queries so a stuck bus fails registration fast.
	verifyCtx, verifyCancel := context.WithTimeout(context.Background(), systemdUnitTimeout)
	defer verifyCancel()
	if _, err := w.systemctlShow(verifyCtx, desc.Unit); err != nil {
		return ManagedServiceDescriptor{}, fmt.Errorf("verify unit %q: %w", desc.Unit, err)
	}
	if desc.RegisteredAt.IsZero() {
		desc.RegisteredAt = time.Now()
	}

	w.managedMu.Lock()
	defer w.managedMu.Unlock()
	services, err := loadManagedServices(path)
	if err != nil {
		return ManagedServiceDescriptor{}, err
	}
	replaced := false
	for i, existing := range services {
		if existing.Unit == desc.Unit {
			// Preserve the original registration time on re-registration.
			if existing.RegisteredAt.IsZero() {
				existing.RegisteredAt = desc.RegisteredAt
			}
			desc.RegisteredAt = existing.RegisteredAt
			services[i] = desc
			replaced = true
			break
		}
	}
	if !replaced {
		services = append(services, desc)
	}
	if err := storeManagedServices(path, services); err != nil {
		return ManagedServiceDescriptor{}, err
	}
	return desc, nil
}

// UnregisterManagedService removes one explicit registration. The underlying
// unit is never stopped or modified. The read-modify-write is serialized with
// registration so concurrent handlers cannot resurrect or lose entries.
func (w *Watcher) UnregisterManagedService(unit string) error {
	if w == nil {
		return fmt.Errorf("watcher unavailable")
	}
	unit = strings.TrimSpace(unit)
	if err := ValidateManagedServiceUnit(unit); err != nil {
		return err
	}
	path := w.managedServicesFilePath()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("managed services registry is not configured")
	}
	w.managedMu.Lock()
	defer w.managedMu.Unlock()
	services, err := loadManagedServices(path)
	if err != nil {
		return err
	}
	kept := services[:0]
	found := false
	for _, existing := range services {
		if existing.Unit == unit {
			found = true
			continue
		}
		kept = append(kept, existing)
	}
	if !found {
		return fmt.Errorf("unit %q is not registered", unit)
	}
	return storeManagedServices(path, kept)
}

func loadManagedServices(path string) ([]ManagedServiceDescriptor, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read managed services registry: %w", err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var file managedServicesFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("decode managed services registry: %w", err)
	}
	// A version or descriptor this daemon does not understand is a
	// recoverable read error, never silent emptiness: callers surface it
	// and no writer path overwrites the file on this failure.
	if file.Version != managedServicesFileVersion {
		return nil, fmt.Errorf("unsupported managed services registry version %d (want %d)", file.Version, managedServicesFileVersion)
	}
	services := make([]ManagedServiceDescriptor, 0, len(file.Services))
	for _, desc := range file.Services {
		if err := ValidateManagedServiceUnit(desc.Unit); err != nil {
			return nil, fmt.Errorf("invalid managed service descriptor: %w", err)
		}
		if strings.TrimSpace(desc.Name) == "" {
			return nil, fmt.Errorf("invalid managed service descriptor for unit %q: name is required", desc.Unit)
		}
		if desc.Port < 0 || desc.Port > 65535 {
			return nil, fmt.Errorf("invalid managed service descriptor for unit %q: invalid port %d", desc.Unit, desc.Port)
		}
		services = append(services, desc)
	}
	return services, nil
}

func storeManagedServices(path string, services []ManagedServiceDescriptor) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("managed services registry path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create managed services registry dir: %w", err)
	}
	if services == nil {
		services = []ManagedServiceDescriptor{}
	}
	raw, err := json.MarshalIndent(managedServicesFile{Version: managedServicesFileVersion, Services: services}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode managed services registry: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".managed-services-*.tmp")
	if err != nil {
		return fmt.Errorf("create managed services registry temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write managed services registry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close managed services registry: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("protect managed services registry: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace managed services registry: %w", err)
	}
	return nil
}

// systemdUnitStatus is the structured live state of one user unit.
type systemdUnitStatus struct {
	Unit         string
	LoadState    string
	ActiveState  string
	SubState     string
	MainPID      int
	FragmentPath string
	InvocationID string
	ControlGroup string
}

// systemctlShowFunc queries structured unit properties. Production shells out
// to the caller's user systemd instance (read-only show, bounded by the
// passed context); tests inject fakes.
type systemctlShowFunc func(ctx context.Context, unit string) (systemdUnitStatus, error)

// procCgroupFunc reports the cgroup-hierarchy paths of one process from
// /proc/<pid>/cgroup. Production parses the live file; tests inject fakes.
type procCgroupFunc func(pid int) ([]string, error)

// procUIDFunc reports the owner UID of one process from /proc/<pid>/status.
// Production parses the live file; tests inject fakes.
type procUIDFunc func(pid int) (int, error)

// errProcessGone marks a /proc lookup for a PID that no longer exists. It is
// disappearance, not a verification failure: the socket owner is simply
// skipped instead of producing an error row.
var errProcessGone = errors.New("process gone")

func (w *Watcher) systemctlShow(ctx context.Context, unit string) (systemdUnitStatus, error) {
	if w == nil {
		return systemdUnitStatus{}, fmt.Errorf("watcher unavailable")
	}
	w.mu.RLock()
	fn := w.systemctlShowFn
	w.mu.RUnlock()
	if fn != nil {
		return fn(ctx, unit)
	}
	return querySystemdUnit(ctx, unit)
}

func (w *Watcher) procCgroupPaths(pid int) ([]string, error) {
	if w == nil || pid <= 0 {
		return nil, nil
	}
	w.mu.RLock()
	fn := w.procCgroupFn
	w.mu.RUnlock()
	if fn != nil {
		return fn(pid)
	}
	return pidCgroupPaths(pid)
}

func (w *Watcher) procOwnerUID(pid int) (int, error) {
	if w == nil || pid <= 0 {
		return 0, nil
	}
	w.mu.RLock()
	fn := w.procUIDFn
	w.mu.RUnlock()
	if fn != nil {
		return fn(pid)
	}
	return pidOwnerUID(pid)
}

func (w *Watcher) listeningSocketsForServices() ([]listeningSocket, error) {
	if w == nil {
		return nil, fmt.Errorf("watcher unavailable")
	}
	w.mu.RLock()
	fn := w.listSocketsFn
	w.mu.RUnlock()
	if fn != nil {
		return fn()
	}
	return listListeningSockets()
}

// querySystemdUnit reads structured properties for one user unit. It never
// starts, stops or modifies the unit. The passed context bounds the query
// (shared discovery deadline or per-call cap): expiry fails this unit while
// tmux rows stay intact. Non-zero exit (unknown unit, broken bus) is a hard
// error so callers surface it instead of guessing.
func querySystemdUnit(ctx context.Context, unit string) (systemdUnitStatus, error) {
	if err := ValidateManagedServiceUnit(unit); err != nil {
		return systemdUnitStatus{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queryCtx, cancel := context.WithTimeout(ctx, systemdUnitTimeout)
	defer cancel()
	cmd := exec.CommandContext(queryCtx, "systemctl", "--user", "show", unit,
		"--property=LoadState,ActiveState,SubState,MainPID,FragmentPath,InvocationID,ControlGroup")
	// A killed query can leave grandchildren holding the output pipe; stop
	// waiting for them on a short bound instead of hanging on I/O.
	cmd.WaitDelay = serviceProcWaitDelay
	out, err := cmd.CombinedOutput()
	if err != nil {
		if queryCtx.Err() == context.DeadlineExceeded {
			if ctx.Err() == context.DeadlineExceeded {
				return systemdUnitStatus{}, fmt.Errorf("systemctl show %s: managed discovery budget exceeded", unit)
			}
			return systemdUnitStatus{}, fmt.Errorf("systemctl show %s: timed out after %s", unit, systemdUnitTimeout)
		}
		return systemdUnitStatus{}, fmt.Errorf("systemctl show %s: %w: %s", unit, err, strings.TrimSpace(string(out)))
	}
	status := systemdUnitStatus{Unit: unit}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "LoadState":
			status.LoadState = strings.TrimSpace(value)
		case "ActiveState":
			status.ActiveState = strings.TrimSpace(value)
		case "SubState":
			status.SubState = strings.TrimSpace(value)
		case "MainPID":
			status.MainPID, _ = strconv.Atoi(strings.TrimSpace(value))
		case "FragmentPath":
			status.FragmentPath = strings.TrimSpace(value)
		case "InvocationID":
			status.InvocationID = strings.TrimSpace(value)
		case "ControlGroup":
			status.ControlGroup = strings.TrimSpace(value)
		}
	}
	if strings.TrimSpace(status.LoadState) == "" || status.LoadState == "not-found" {
		return systemdUnitStatus{}, fmt.Errorf("unit %q not found in user systemd", unit)
	}
	return status, nil
}

// pidCgroupPaths parses the hierarchy paths from /proc/<pid>/cgroup. A
// vanished PID reports errProcessGone (disappearance, not failure); any other
// read/parse error is returned so callers surface verification failure
// instead of guessing inactive.
func pidCgroupPaths(pid int) ([]string, error) {
	if pid <= 0 {
		return nil, nil
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errProcessGone
		}
		return nil, fmt.Errorf("read cgroup for pid %d: %w", pid, err)
	}
	var paths []string
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format is hierarchy-ID:controllers:path; only the path binds a
		// process to a unit scope.
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 || strings.TrimSpace(fields[2]) == "" {
			return nil, fmt.Errorf("malformed cgroup line for pid %d: %q", pid, line)
		}
		paths = append(paths, strings.TrimSpace(fields[2]))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan cgroup for pid %d: %w", pid, err)
	}
	return paths, nil
}

// pidOwnerUID parses the real UID from /proc/<pid>/status. A vanished PID
// reports errProcessGone; any other error is a verification failure.
func pidOwnerUID(pid int) (int, error) {
	if pid <= 0 {
		return 0, nil
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, errProcessGone
		}
		return 0, fmt.Errorf("read status for pid %d: %w", pid, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "Uid:" {
			uid, convErr := strconv.Atoi(fields[1])
			if convErr != nil {
				return 0, fmt.Errorf("malformed uid for pid %d: %q", pid, line)
			}
			return uid, nil
		}
	}
	return 0, fmt.Errorf("no uid in status for pid %d", pid)
}

// cgroupPathAccepts reports whether a /proc cgroup path belongs to the exact
// ControlGroup returned for the unit: the unit scope itself or a delegated
// child scope beneath it. A different unit that merely shares the leaf name,
// a sibling prefix (unit.service.d), or an empty group never matches. Names,
// descriptions and ports are never consulted.
func cgroupPathAccepts(path, controlGroup string) bool {
	path = strings.TrimSpace(path)
	controlGroup = strings.TrimSpace(controlGroup)
	if path == "" || controlGroup == "" || !strings.HasPrefix(controlGroup, "/") {
		return false
	}
	if path == controlGroup {
		return true
	}
	return strings.HasPrefix(path, controlGroup+"/")
}

// SetManagedDiscoveryFunc installs a test-only wholesale replacement for
// persistent service resolution (cross-package suites such as the server WS
// tests use it to stage deterministic rows). Production leaves it nil and
// resolves the persisted registry against live systemd state.
func (w *Watcher) SetManagedDiscoveryFunc(fn func(claimed map[string]bool, interfaces []SessionServiceInterface) []SessionService) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.managedDiscoveryFn = fn
	w.mu.Unlock()
}

func (w *Watcher) managedDiscoverySeam() func(map[string]bool, []SessionServiceInterface) []SessionService {
	if w == nil {
		return nil
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.managedDiscoveryFn
}

// discoverPersistentServices resolves every registered unit against live
// systemd properties and live socket ownership. claimed marks pid|port pairs
// already attributed to tmux sessions so one socket is never reported twice.
// A broken systemd query yields a per-row error state, never silent success
// and never a corrupted tmux list.
func (w *Watcher) discoverPersistentServices(claimed map[string]bool, interfaces []SessionServiceInterface) []SessionService {
	if seam := w.managedDiscoverySeam(); seam != nil {
		return seam(claimed, interfaces)
	}
	descriptors, err := loadManagedServices(w.managedServicesFilePath())
	if err != nil {
		return []SessionService{{
			ID:           "persistent:registry:error",
			WorkerName:   "persistent services",
			Source:       ServiceSourcePersistent,
			State:        ServiceStateError,
			StatusDetail: err.Error(),
			Protocol:     "tcp",
			Binds:        []string{},
			URLs:         []SessionServiceURL{},
			LocalOnly:    true,
		}}
	}
	if len(descriptors) == 0 {
		return nil
	}
	// One shared deadline for every unit: N hung queries cannot multiply
	// beyond the aggregate budget, and expiry degrades to per-row errors
	// while tmux rows stay intact.
	budgetCtx, budgetCancel := context.WithTimeout(context.Background(), managedDiscoveryBudget)
	defer budgetCancel()
	sockets, socketsErr := w.listeningSocketsForServices()
	// One shared process snapshot per discovery, not one per unit.
	processes := map[int]processInfo{}
	if w != nil {
		_, _, snapshot := w.pollReaders()
		if snapshot != nil {
			processes = snapshot()
		} else {
			processes = snapshotProcesses()
		}
	}
	services := make([]SessionService, 0, len(descriptors))
	for _, desc := range descriptors {
		rows, rowErr := w.resolveManagedService(budgetCtx, desc, sockets, socketsErr, processes, claimed, interfaces)
		if rowErr != nil {
			services = append(services, SessionService{
				ID:           "persistent:" + desc.Unit + ":error",
				WorkerName:   displayManagedServiceName(desc),
				Project:      desc.Project,
				Cwd:          desc.Cwd,
				Source:       ServiceSourcePersistent,
				Unit:         desc.Unit,
				State:        ServiceStateError,
				StatusDetail: rowErr.Error(),
				Port:         desc.Port,
				Protocol:     "tcp",
				Binds:        []string{},
				URLs:         []SessionServiceURL{},
				LocalOnly:    true,
			})
			continue
		}
		services = append(services, rows...)
	}
	sort.Slice(services, func(i, j int) bool {
		if services[i].Project != services[j].Project {
			return services[i].Project < services[j].Project
		}
		if services[i].WorkerName != services[j].WorkerName {
			return services[i].WorkerName < services[j].WorkerName
		}
		if services[i].Port != services[j].Port {
			return services[i].Port < services[j].Port
		}
		return services[i].PID < services[j].PID
	})
	return services
}

func (w *Watcher) resolveManagedService(ctx context.Context, desc ManagedServiceDescriptor, sockets []listeningSocket, socketsErr error, processes map[int]processInfo, claimed map[string]bool, interfaces []SessionServiceInterface) ([]SessionService, error) {
	status, err := w.systemctlShow(ctx, desc.Unit)
	if err != nil {
		return nil, err
	}
	if socketsErr != nil {
		return nil, fmt.Errorf("listening sockets: %w", socketsErr)
	}
	if !strings.EqualFold(strings.TrimSpace(status.ActiveState), "active") {
		return []SessionService{inactiveManagedService(desc, status)}, nil
	}
	owned, err := w.ownedManagedSockets(status, sockets)
	if err != nil {
		return nil, err
	}
	byPort := make(map[int]*SessionService)
	claimedSkipped := false
	for _, socket := range owned {
		if socket.port <= 0 {
			continue
		}
		if desc.Port > 0 && socket.port != desc.Port {
			continue
		}
		if claimed[fmt.Sprintf("%d|%d", socket.pid, socket.port)] {
			claimedSkipped = true
			continue
		}
		service := byPort[socket.port]
		if service == nil {
			process := ""
			if proc, ok := processes[socket.pid]; ok {
				process = strings.TrimSpace(proc.args)
				if process == "" {
					process = strings.TrimSpace(proc.comm)
				}
			}
			service = &SessionService{
				ID:         fmt.Sprintf("persistent:%s:%d:%d", desc.Unit, socket.pid, socket.port),
				WorkerName: displayManagedServiceName(desc),
				Project:    desc.Project,
				Cwd:        desc.Cwd,
				Process:    process,
				PID:        socket.pid,
				Port:       socket.port,
				Protocol:   "tcp",
				Source:     ServiceSourcePersistent,
				Unit:       desc.Unit,
				State:      ServiceStateActive,
			}
			byPort[socket.port] = service
		}
		service.Binds = appendUnique(service.Binds, socket.bind)
	}
	rows := make([]SessionService, 0, len(byPort))
	for _, service := range byPort {
		sort.Strings(service.Binds)
		if service.Binds == nil {
			service.Binds = []string{}
		}
		service.URLs = buildServiceURLs(service.Binds, service.Port, interfaces)
		if service.URLs == nil {
			service.URLs = []SessionServiceURL{}
		}
		service.LocalOnly = len(service.URLs) == 0
		rows = append(rows, *service)
	}
	if len(rows) == 0 {
		// A live owned socket that tmux already represents is omitted, not
		// duplicated as a bogus inactive row. Only a unit with genuinely no
		// live owned socket reports inactive.
		if claimedSkipped {
			return nil, nil
		}
		rows = append(rows, inactiveManagedService(desc, status))
	}
	return rows, nil
}

// ownedManagedSockets keeps only sockets whose PID provably lives in the
// exact ControlGroup returned for this unit sample, owned by our UID. The
// MainPID fast path is gone on purpose: a bare PID match from an earlier
// sample can misattribute a reused PID, so every candidate — including the
// current MainPID — passes the same cgroup+UID verification. A same-port
// lookalike from another cgroup, another user, or a vanished PID is never
// attributed. Genuine /proc verification failures (anything but a vanished
// PID) abort the unit with an error, never a fake inactive row.
func (w *Watcher) ownedManagedSockets(status systemdUnitStatus, sockets []listeningSocket) ([]listeningSocket, error) {
	controlGroup := strings.TrimSpace(status.ControlGroup)
	if controlGroup == "" {
		return nil, fmt.Errorf("unit %q reports no control group", strings.TrimSpace(status.Unit))
	}
	selfUID := os.Geteuid()
	var owned []listeningSocket
	for _, socket := range sockets {
		if socket.pid <= 0 {
			continue
		}
		paths, err := w.procCgroupPaths(socket.pid)
		if err != nil {
			if errors.Is(err, errProcessGone) {
				continue
			}
			return nil, err
		}
		matched := false
		for _, path := range paths {
			if cgroupPathAccepts(path, controlGroup) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		uid, err := w.procOwnerUID(socket.pid)
		if err != nil {
			if errors.Is(err, errProcessGone) {
				continue
			}
			return nil, err
		}
		if uid != selfUID {
			continue
		}
		owned = append(owned, socket)
	}
	return owned, nil
}

func inactiveManagedService(desc ManagedServiceDescriptor, status systemdUnitStatus) SessionService {
	detail := fmt.Sprintf("unit %s (%s)", strings.TrimSpace(status.ActiveState), strings.TrimSpace(status.SubState))
	if strings.TrimSpace(status.ActiveState) == "" {
		detail = "unit state unknown"
	}
	if strings.TrimSpace(status.ActiveState) == "active" {
		detail = "unit active but no owned listening socket"
	}
	return SessionService{
		ID:           "persistent:" + desc.Unit + ":inactive",
		WorkerName:   displayManagedServiceName(desc),
		Project:      desc.Project,
		Cwd:          desc.Cwd,
		Port:         desc.Port,
		Protocol:     "tcp",
		Source:       ServiceSourcePersistent,
		Unit:         desc.Unit,
		State:        ServiceStateInactive,
		StatusDetail: strings.TrimSpace(detail),
		Binds:        []string{},
		URLs:         []SessionServiceURL{},
		LocalOnly:    true,
	}
}

func displayManagedServiceName(desc ManagedServiceDescriptor) string {
	if strings.TrimSpace(desc.Name) != "" {
		return strings.TrimSpace(desc.Name)
	}
	return strings.TrimSpace(desc.Unit)
}
