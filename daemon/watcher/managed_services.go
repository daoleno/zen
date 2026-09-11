package watcher

import (
	"bufio"
	"encoding/json"
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
// so typos and foreign units fail closed at handoff time.
func (w *Watcher) RegisterManagedService(desc ManagedServiceDescriptor) (ManagedServiceDescriptor, error) {
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
	if _, err := w.systemctlShow(desc.Unit); err != nil {
		return ManagedServiceDescriptor{}, fmt.Errorf("verify unit %q: %w", desc.Unit, err)
	}
	if desc.RegisteredAt.IsZero() {
		desc.RegisteredAt = time.Now()
	}

	path := w.managedServicesFilePath()
	if strings.TrimSpace(path) == "" {
		return ManagedServiceDescriptor{}, fmt.Errorf("managed services registry is not configured")
	}
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
// unit is never stopped or modified.
func (w *Watcher) UnregisterManagedService(unit string) error {
	unit = strings.TrimSpace(unit)
	if err := ValidateManagedServiceUnit(unit); err != nil {
		return err
	}
	path := w.managedServicesFilePath()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("managed services registry is not configured")
	}
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
	services := make([]ManagedServiceDescriptor, 0, len(file.Services))
	for _, desc := range file.Services {
		if err := ValidateManagedServiceUnit(desc.Unit); err != nil {
			continue
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
// to the caller's user systemd instance (read-only show); tests inject fakes.
type systemctlShowFunc func(unit string) (systemdUnitStatus, error)

// unitCgroupFunc reports whether pid sits in unit's cgroup. Production reads
// /proc/<pid>/cgroup; tests inject fakes.
type unitCgroupFunc func(pid int, unit string) (bool, error)

func (w *Watcher) systemctlShow(unit string) (systemdUnitStatus, error) {
	if w == nil {
		return systemdUnitStatus{}, fmt.Errorf("watcher unavailable")
	}
	w.mu.RLock()
	fn := w.systemctlShowFn
	w.mu.RUnlock()
	if fn != nil {
		return fn(unit)
	}
	return querySystemdUnit(unit)
}

func (w *Watcher) unitInCgroup(pid int, unit string) (bool, error) {
	if w == nil || pid <= 0 {
		return false, nil
	}
	w.mu.RLock()
	fn := w.unitCgroupFn
	w.mu.RUnlock()
	if fn != nil {
		return fn(pid, unit)
	}
	return pidInUnitCgroup(pid, unit)
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
// starts, stops or modifies the unit. Non-zero exit (unknown unit, broken
// bus) is a hard error so callers surface it instead of guessing.
func querySystemdUnit(unit string) (systemdUnitStatus, error) {
	if err := ValidateManagedServiceUnit(unit); err != nil {
		return systemdUnitStatus{}, err
	}
	out, err := exec.Command("systemctl", "--user", "show", unit,
		"--property=LoadState,ActiveState,SubState,MainPID,FragmentPath,InvocationID,ControlGroup").CombinedOutput()
	if err != nil {
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

// pidInUnitCgroup reports structured cgroup membership: pid belongs to unit
// when any /proc/<pid>/cgroup line names the exact unit. Names, descriptions
// and ports are never consulted.
func pidInUnitCgroup(pid int, unit string) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	file, err := os.Open(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return false, nil
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		for _, field := range strings.Split(scanner.Text(), ":") {
			if cgroupPathNamesUnit(strings.TrimSpace(field), unit) {
				return true, nil
			}
		}
	}
	return false, nil
}

func cgroupPathNamesUnit(path, unit string) bool {
	if path == "" || unit == "" {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == unit {
			return true
		}
	}
	return false
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
	sockets, socketsErr := w.listeningSocketsForServices()
	services := make([]SessionService, 0, len(descriptors))
	for _, desc := range descriptors {
		rows, rowErr := w.resolveManagedService(desc, sockets, socketsErr, claimed, interfaces)
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

func (w *Watcher) resolveManagedService(desc ManagedServiceDescriptor, sockets []listeningSocket, socketsErr error, claimed map[string]bool, interfaces []SessionServiceInterface) ([]SessionService, error) {
	status, err := w.systemctlShow(desc.Unit)
	if err != nil {
		return nil, err
	}
	if socketsErr != nil {
		return nil, fmt.Errorf("listening sockets: %w", socketsErr)
	}
	if !strings.EqualFold(strings.TrimSpace(status.ActiveState), "active") {
		return []SessionService{inactiveManagedService(desc, status)}, nil
	}
	processes := map[int]processInfo{}
	if w != nil {
		_, _, snapshot := w.pollReaders()
		if snapshot != nil {
			processes = snapshot()
		} else {
			processes = snapshotProcesses()
		}
	}
	owned := ownedManagedSockets(desc, status, sockets, processes, w)
	byPort := make(map[int]*SessionService)
	for _, socket := range owned {
		if socket.port <= 0 {
			continue
		}
		if desc.Port > 0 && socket.port != desc.Port {
			continue
		}
		if claimed[fmt.Sprintf("%d|%d", socket.pid, socket.port)] {
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
		rows = append(rows, inactiveManagedService(desc, status))
	}
	return rows, nil
}

// ownedManagedSockets keeps only sockets whose PID is the unit's live MainPID
// or sits in the unit's cgroup. A same-port lookalike from another cgroup is
// never attributed.
func ownedManagedSockets(desc ManagedServiceDescriptor, status systemdUnitStatus, sockets []listeningSocket, processes map[int]processInfo, w *Watcher) []listeningSocket {
	var owned []listeningSocket
	for _, socket := range sockets {
		if socket.pid <= 0 {
			continue
		}
		if status.MainPID > 0 && socket.pid == status.MainPID {
			owned = append(owned, socket)
			continue
		}
		inCgroup := false
		if w != nil {
			ok, _ := w.unitInCgroup(socket.pid, desc.Unit)
			inCgroup = ok
		} else {
			ok, _ := pidInUnitCgroup(socket.pid, desc.Unit)
			inCgroup = ok
		}
		if inCgroup {
			owned = append(owned, socket)
		}
	}
	return owned
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
