//go:build linux || darwin

package agentproc

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const (
	poolGuardInterval = 2 * time.Second
	poolGuardGrace    = 3
)

type processRecord struct {
	PID  int
	PPID int
	PGID int
	SID  int
	RSS  uint64
	Args string
}

func RunSupervisor(config SupervisorConfig) error {
	leasePath, err := LeasePath(config.LeaseDir, config.ResourceID)
	if err != nil {
		return err
	}
	if len(config.Command) == 0 || strings.TrimSpace(config.Command[0]) == "" {
		return fmt.Errorf("supervised command is required")
	}
	total := physicalMemory()
	memoryHigh, err := ParseMemoryLimit(config.MemoryHigh, total)
	if err != nil {
		return fmt.Errorf("memory high: %w", err)
	}
	memoryMax, err := ParseMemoryLimit(config.MemoryMax, total)
	if err != nil {
		return fmt.Errorf("memory max: %w", err)
	}
	if memoryMax > 0 && memoryHigh > memoryMax {
		memoryHigh = memoryMax
	}
	processGroup, sessionID, err := claimProcessGroup(firstFile(config.Stdin, os.Stdin))
	if err != nil {
		return fmt.Errorf("establish delegated process ownership: %w", err)
	}

	command := exec.Command(config.Command[0], config.Command[1:]...)
	command.Stdin = firstFile(config.Stdin, os.Stdin)
	command.Stdout = firstFile(config.Stdout, os.Stdout)
	command.Stderr = firstFile(config.Stderr, os.Stderr)
	command.Env = os.Environ()
	if err := command.Start(); err != nil {
		return fmt.Errorf("start supervised command: %w", err)
	}

	lease := Lease{
		Version:       1,
		ResourceID:    config.ResourceID,
		BootID:        bootID(),
		SupervisorPID: os.Getpid(),
		RootPID:       command.Process.Pid,
		SessionID:     sessionID,
		ProcessGroup:  processGroup,
		StartedAt:     time.Now().UTC(),
		MemoryHigh:    memoryHigh,
		MemoryMax:     memoryMax,
		TasksMax:      config.TasksMax,
	}
	if err := writeLease(leasePath, lease); err != nil {
		_ = terminateLease(lease, 2*time.Second, true)
		_, _ = command.Process.Wait()
		return fmt.Errorf("write resource lease: %w", err)
	}
	defer os.Remove(leasePath)

	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(signals)

	// systemd Linux (no pool-guard): parent cgroup + child TasksMax enforce
	// resources. Supervisors only own lifecycle — no portable host scanning.
	if !config.PoolGuard {
		return waitSupervisedLifecycle(done, signals, lease)
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastPoolSample := time.Time{}
	poolBreaches := 0
	var poolLock *os.File
	defer func() {
		if poolLock != nil {
			_ = poolLock.Close()
		}
	}()

	for {
		select {
		case err := <-done:
			_ = terminateLease(lease, 750*time.Millisecond, true)
			return err
		case <-signals:
			_ = terminateLease(lease, 2*time.Second, true)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
			}
			return nil
		case <-ticker.C:
			if poolLock == nil {
				if lock, ok := tryPoolLeaderLock(config.LeaseDir); ok {
					poolLock = lock
				}
				// Non-leaders only retry the cheap nonblocking lock.
				continue
			}
			if !lastPoolSample.IsZero() && time.Since(lastPoolSample) < poolGuardInterval {
				continue
			}
			lastPoolSample = time.Now()
			sample, sampleErr := SampleOwnedLeases(config.LeaseDir)
			if sampleErr != nil {
				poolBreaches = 0
				continue
			}
			if stopID, reason := leaseExceedingTasks(sample.TasksByLease, sample.Leases); stopID != "" {
				fmt.Fprintf(command.Stderr, "zen: stopping delegated resource %s: %s\n", stopID, reason)
				if stopID == config.ResourceID {
					_ = terminateLease(lease, 2*time.Second, true)
					select {
					case <-done:
					case <-time.After(3 * time.Second):
					}
					return fmt.Errorf("delegated resource %s: %s", config.ResourceID, reason)
				}
				if path, pathErr := LeasePath(config.LeaseDir, stopID); pathErr == nil {
					_ = StopLease(path)
				}
				poolBreaches = 0
				continue
			}
			if memoryMax == 0 {
				continue
			}
			emergencyReserve := uint64(0)
			if total > memoryMax {
				emergencyReserve = total - memoryMax
			}
			available := availableMemory()
			breached := sample.AggregateRSS > memoryMax || (emergencyReserve > 0 && available > 0 && available < emergencyReserve)
			if !breached {
				poolBreaches = 0
				continue
			}
			poolBreaches++
			if poolBreaches < poolGuardGrace {
				continue
			}
			victim := largestLeaseRSS(sample.RSSByLease)
			if victim == "" {
				poolBreaches = 0
				continue
			}
			fmt.Fprintf(command.Stderr, "zen: stopping delegated resource %s: shared pool pressure (aggregate=%d max=%d available=%d)\n", victim, sample.AggregateRSS, memoryMax, available)
			if victim == config.ResourceID {
				_ = terminateLease(lease, 2*time.Second, true)
				select {
				case <-done:
				case <-time.After(3 * time.Second):
				}
				return fmt.Errorf("delegated resource %s: shared pool pressure", config.ResourceID)
			}
			if path, pathErr := LeasePath(config.LeaseDir, victim); pathErr == nil {
				_ = StopLease(path)
			}
			poolBreaches = 0
		}
	}
}

func waitSupervisedLifecycle(done <-chan error, signals <-chan os.Signal, lease Lease) error {
	select {
	case err := <-done:
		_ = terminateLease(lease, 750*time.Millisecond, true)
		return err
	case <-signals:
		_ = terminateLease(lease, 2*time.Second, true)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
		return nil
	}
}

func tryPoolLeaderLock(leaseDir string) (*os.File, bool) {
	path := filepath.Join(leaseDir, ".pool-leader.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, false
	}
	return file, true
}

// PoolSample is one on-demand observation of every durable owned lease.
// Callers must treat missing entries as unavailable, never invent zeroes.
type PoolSample struct {
	Leases       []Lease
	RSSByLease   map[string]uint64
	TasksByLease map[string]int
	AggregateRSS uint64
}

// SampleOwnedLeases takes exactly one process snapshot and one environment
// listing, then derives ownership for every durable lease from those two sources.
func SampleOwnedLeases(leaseDir string) (PoolSample, error) {
	leases, err := ListLeases(leaseDir)
	if err != nil {
		return PoolSample{}, err
	}
	snapshot, err := processSnapshot()
	if err != nil {
		return PoolSample{}, err
	}
	markers, err := allMarkedProcessIDs()
	if err != nil {
		return PoolSample{}, err
	}
	sample := PoolSample{
		Leases:       leases,
		RSSByLease:   make(map[string]uint64, len(leases)),
		TasksByLease: make(map[string]int, len(leases)),
	}
	for _, lease := range leases {
		trusted := trustedLeaseScope(snapshot, lease)
		owned := ownedProcesses(snapshot, lease, trusted, markers[lease.ResourceID])
		var rss uint64
		for pid := range owned {
			rss += snapshot[pid].RSS
		}
		sample.RSSByLease[lease.ResourceID] = rss
		sample.TasksByLease[lease.ResourceID] = len(owned)
		sample.AggregateRSS += rss
	}
	return sample, nil
}

func leaseExceedingTasks(tasksByLease map[string]int, leases []Lease) (string, string) {
	for _, lease := range leases {
		if lease.TasksMax <= 0 {
			continue
		}
		tasks := tasksByLease[lease.ResourceID]
		if tasks > lease.TasksMax {
			return lease.ResourceID, fmt.Sprintf("process limit exceeded (%d > %d)", tasks, lease.TasksMax)
		}
	}
	return "", ""
}

func largestLeaseRSS(rssByLease map[string]uint64) string {
	victim := ""
	var best uint64
	for resourceID, rss := range rssByLease {
		if rss > best || (rss == best && (victim == "" || resourceID < victim)) {
			best = rss
			victim = resourceID
		}
	}
	if best == 0 {
		return ""
	}
	return victim
}

func StopLease(path string) error {
	lease, err := ReadLease(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if lease.BootID != "" && lease.BootID != bootID() {
		return os.Remove(path)
	}
	snapshot, err := processSnapshot()
	if err != nil {
		return err
	}
	trusted := trustedLeaseScope(snapshot, lease)
	marked, markerErr := markedProcessIDs(lease.ResourceID)
	if markerErr != nil && !trusted {
		return fmt.Errorf("verify delegated resource ownership: %w", markerErr)
	}
	if len(ownedProcesses(snapshot, lease, trusted, marked)) > 0 {
		if err := terminateLease(lease, 2*time.Second, trusted); err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func terminateLease(lease Lease, grace time.Duration, trustedScope bool) error {
	snapshot, err := processSnapshot()
	if err != nil {
		return err
	}
	marked, markerErr := markedProcessIDs(lease.ResourceID)
	if markerErr != nil && !trustedScope {
		return markerErr
	}
	signalOwned(ownedProcesses(snapshot, lease, trustedScope, marked), syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		snapshot, err = processSnapshot()
		if err != nil {
			continue
		}
		if len(ownedProcesses(snapshot, lease, trustedScope, marked)) == 0 {
			return nil
		}
	}
	if snapshot, err = processSnapshot(); err != nil {
		return err
	}
	if discovered, discoverErr := markedProcessIDs(lease.ResourceID); discoverErr == nil {
		for pid := range discovered {
			marked[pid] = true
		}
	}
	signalOwned(ownedProcesses(snapshot, lease, trustedScope, marked), syscall.SIGKILL)
	return nil
}

func signalOwned(owned map[int]bool, signal syscall.Signal) {
	pids := make([]int, 0, len(owned))
	for pid := range owned {
		if pid > 1 && pid != os.Getpid() {
			pids = append(pids, pid)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(pids)))
	for _, pid := range pids {
		process, err := os.FindProcess(pid)
		if err == nil {
			_ = process.Signal(signal)
		}
	}
}

func ownedProcesses(snapshot map[int]processRecord, lease Lease, trustedScope bool, marked map[int]bool) map[int]bool {
	owned := make(map[int]bool)
	// The entire session is ours only when the supervisor is its leader. Under
	// systemd-run the launcher may remain the tmux pane's session leader; killing
	// that parent from inside its child would race systemd's own scope teardown.
	if trustedScope && lease.SessionID > 1 && lease.SessionID == lease.SupervisorPID {
		for pid, process := range snapshot {
			if process.SID == lease.SessionID {
				owned[pid] = true
			}
		}
	}
	if trustedScope && lease.ProcessGroup > 1 {
		for pid, process := range snapshot {
			if process.PGID == lease.ProcessGroup {
				owned[pid] = true
			}
		}
	}
	for pid := range marked {
		if _, exists := snapshot[pid]; exists {
			owned[pid] = true
		}
	}
	roots := []int{}
	if trustedScope {
		roots = append(roots, lease.SupervisorPID, lease.RootPID)
	}
	for _, root := range roots {
		if root > 1 {
			owned[root] = true
		}
	}
	changed := true
	for changed {
		changed = false
		for pid, process := range snapshot {
			if !owned[pid] && owned[process.PPID] {
				owned[pid] = true
				changed = true
			}
		}
	}
	for pid := range owned {
		if _, exists := snapshot[pid]; !exists || pid == os.Getpid() {
			delete(owned, pid)
		}
	}
	return owned
}

func trustedLeaseScope(snapshot map[int]processRecord, lease Lease) bool {
	if lease.SupervisorPID <= 1 || lease.ProcessGroup <= 1 || lease.SessionID <= 1 {
		return false
	}
	if supervisor, exists := snapshot[lease.SupervisorPID]; exists {
		return supervisor.PGID == lease.ProcessGroup &&
			(supervisor.SID == 0 || supervisor.SID == lease.SessionID) &&
			strings.Contains(supervisor.Args, "__supervise") &&
			strings.Contains(supervisor.Args, "--resource-id="+lease.ResourceID)
	}
	// A process-group or session ID remains allocated while any member exists.
	// If the supervisor was their leader, the numeric identity cannot have been
	// recycled yet, so the surviving members still belong to this lease.
	if lease.ProcessGroup == lease.SupervisorPID {
		for _, process := range snapshot {
			if process.PGID == lease.ProcessGroup {
				return true
			}
		}
	}
	if lease.SessionID == lease.SupervisorPID {
		for _, process := range snapshot {
			if process.SID == lease.SessionID {
				return true
			}
		}
	}
	return false
}

func delegatedResourceMarker(resourceID string) string {
	return "ZEN_AGENT_RESOURCE_UNIT=" + resourceID
}

func claimProcessGroup(stdin *os.File) (int, int, error) {
	pid := os.Getpid()
	pgid, err := unix.Getpgid(0)
	if err != nil {
		return 0, 0, err
	}
	if pgid != pid {
		if err := unix.Setpgid(0, pid); err != nil {
			return 0, 0, fmt.Errorf("create owned process group: %w", err)
		}
	}
	pgid, err = unix.Getpgid(0)
	if err != nil {
		return 0, 0, err
	}
	if pgid != pid {
		return 0, 0, fmt.Errorf("owned process group is %d, want supervisor pid %d", pgid, pid)
	}
	if stdin != nil && term.IsTerminal(int(stdin.Fd())) {
		foreground, getErr := unix.IoctlGetInt(int(stdin.Fd()), unix.TIOCGPGRP)
		if getErr != nil {
			return 0, 0, fmt.Errorf("read terminal foreground group: %w", getErr)
		}
		if foreground != pgid {
			signal.Ignore(syscall.SIGTTOU)
			setErr := unix.IoctlSetPointerInt(int(stdin.Fd()), unix.TIOCSPGRP, pgid)
			signal.Reset(syscall.SIGTTOU)
			if setErr != nil {
				return 0, 0, fmt.Errorf("claim terminal foreground group: %w", setErr)
			}
		}
	}
	sessionID, err := unix.Getsid(0)
	if err != nil {
		return 0, 0, err
	}
	return pgid, sessionID, nil
}

func firstFile(value, fallback *os.File) *os.File {
	if value != nil {
		return value
	}
	return fallback
}
