package agentproc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var resourceIDRE = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

type SupervisorConfig struct {
	ResourceID string
	LeaseDir   string
	MemoryHigh string
	MemoryMax  string
	TasksMax   int
	PoolGuard  bool
	Command    []string
	Stdin      *os.File
	Stdout     *os.File
	Stderr     *os.File
}

type Lease struct {
	Version       int       `json:"version"`
	ResourceID    string    `json:"resource_id"`
	BootID        string    `json:"boot_id"`
	SupervisorPID int       `json:"supervisor_pid"`
	RootPID       int       `json:"root_pid"`
	SessionID     int       `json:"session_id"`
	ProcessGroup  int       `json:"process_group"`
	StartedAt     time.Time `json:"started_at"`
	MemoryHigh    uint64    `json:"memory_high,omitempty"`
	MemoryMax     uint64    `json:"memory_max,omitempty"`
	TasksMax      int       `json:"tasks_max,omitempty"`
}

// PhysicalMemory returns installed physical memory in bytes when the current
// platform exposes it. Resource policy callers fall back conservatively when
// it returns zero.
func PhysicalMemory() uint64 {
	return physicalMemory()
}

// AvailableMemory returns currently available memory in bytes when the current
// platform exposes it. Callers treat zero as "observation unavailable".
func AvailableMemory() uint64 {
	return availableMemory()
}

func LeasePath(dir, resourceID string) (string, error) {
	dir = strings.TrimSpace(dir)
	resourceID = strings.TrimSpace(resourceID)
	if dir == "" {
		return "", fmt.Errorf("lease directory is required")
	}
	if !resourceIDRE.MatchString(resourceID) || strings.Contains(resourceID, "..") {
		return "", fmt.Errorf("invalid resource id %q", resourceID)
	}
	return filepath.Join(dir, resourceID+".json"), nil
}

func ReadLease(path string) (Lease, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Lease{}, err
	}
	var lease Lease
	if err := json.Unmarshal(raw, &lease); err != nil {
		return Lease{}, fmt.Errorf("decode lease: %w", err)
	}
	if lease.Version != 1 || !resourceIDRE.MatchString(lease.ResourceID) {
		return Lease{}, fmt.Errorf("invalid lease metadata")
	}
	return lease, nil
}

func ListLeases(dir string) ([]Lease, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	leases := make([]Lease, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		lease, err := ReadLease(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		leases = append(leases, lease)
	}
	return leases, nil
}

func writeLease(path string, lease Lease) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(lease, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".lease-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// ParseMemoryLimit converts a systemd-style memory limit string into bytes.
// Percentage forms require a positive total; absolute sizes do not.
func ParseMemoryLimit(value string, total uint64) (uint64, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" || value == "0" {
		return 0, nil
	}
	if strings.HasSuffix(value, "%") {
		var percent uint64
		if _, err := fmt.Sscanf(value, "%d%%", &percent); err != nil || percent == 0 || percent > 100 {
			return 0, fmt.Errorf("invalid memory percentage %q", value)
		}
		return total * percent / 100, nil
	}
	multiplier := uint64(1)
	if last := value[len(value)-1]; last < '0' || last > '9' {
		switch last {
		case 'K':
			multiplier = 1 << 10
		case 'M':
			multiplier = 1 << 20
		case 'G':
			multiplier = 1 << 30
		case 'T':
			multiplier = 1 << 40
		case 'P':
			multiplier = 1 << 50
		case 'E':
			multiplier = 1 << 60
		default:
			return 0, fmt.Errorf("invalid memory size %q", value)
		}
		value = strings.TrimSpace(value[:len(value)-1])
	}
	var amount uint64
	if _, err := fmt.Sscanf(value, "%d", &amount); err != nil || amount == 0 {
		return 0, fmt.Errorf("invalid memory size %q", value)
	}
	if amount > ^uint64(0)/multiplier {
		return 0, fmt.Errorf("memory size overflows")
	}
	return amount * multiplier, nil
}
