//go:build linux || darwin

package agentproc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const supervisorHelperEnv = "ZEN_AGENTPROC_SUPERVISOR_HELPER"

func TestRunSupervisorOwnsAndReclaimsIsolatedSession(t *testing.T) {
	leaseDir := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestRunSupervisorHelper$")
	command.Env = append(os.Environ(), supervisorHelperEnv+"=1", "ZEN_AGENTPROC_LEASE_DIR="+leaseDir)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("isolated supervisor failed: %v\n%s", err, output)
	}
	entries, err := os.ReadDir(leaseDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("supervisor left leases behind: %+v", entries)
	}
}

func TestLargestLeaseRSS(t *testing.T) {
	got := largestLeaseRSS(map[string]uint64{
		"zen-agent-a": 100,
		"zen-agent-b": 500,
		"zen-agent-c": 500,
		"zen-agent-d": 0,
	})
	if got != "zen-agent-b" {
		t.Fatalf("largest = %q, want zen-agent-b (stable tie-break)", got)
	}
	if largestLeaseRSS(map[string]uint64{"a": 0, "b": 0}) != "" {
		t.Fatal("expected empty victim when all RSS are zero")
	}
}

func TestParseAllMarkedProcessIDsGroupsByResource(t *testing.T) {
	out := []byte(strings.Join([]string{
		"10 /bin/sh ZEN_AGENT_RESOURCE_UNIT=zen-agent-a.scope HOME=/tmp",
		"11 /bin/sh ZEN_AGENT_RESOURCE_UNIT=zen-agent-b.scope",
		"12 /usr/bin/other",
		"13 /bin/tool ZEN_AGENT_RESOURCE_UNIT=zen-agent-a.scope PWD=/",
	}, "\n"))
	marked, err := parseAllMarkedProcessIDs(out)
	if err != nil {
		t.Fatal(err)
	}
	if !marked["zen-agent-a.scope"][10] || !marked["zen-agent-a.scope"][13] {
		t.Fatalf("resource a markers = %#v", marked["zen-agent-a.scope"])
	}
	if !marked["zen-agent-b.scope"][11] || marked["zen-agent-b.scope"][10] {
		t.Fatalf("resource b markers = %#v", marked["zen-agent-b.scope"])
	}
}

func TestLeaseExceedingTasks(t *testing.T) {
	leases := []Lease{
		{ResourceID: "a", TasksMax: 2},
		{ResourceID: "b", TasksMax: 4},
	}
	stopID, reason := leaseExceedingTasks(map[string]int{"a": 2, "b": 5}, leases)
	if stopID != "b" || !strings.Contains(reason, "process limit exceeded") {
		t.Fatalf("stopID=%q reason=%q", stopID, reason)
	}
	if id, _ := leaseExceedingTasks(map[string]int{"a": 2, "b": 4}, leases); id != "" {
		t.Fatalf("unexpected stop %q", id)
	}
}

func TestPoolLeaderLockHandoff(t *testing.T) {
	dir := t.TempDir()
	first, ok := tryPoolLeaderLock(dir)
	if !ok {
		t.Fatal("first contender failed to obtain pool lock")
	}
	if second, ok := tryPoolLeaderLock(dir); ok {
		_ = second.Close()
		_ = first.Close()
		t.Fatal("second contender obtained pool lock while first held it")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, ok := tryPoolLeaderLock(dir)
	if !ok {
		t.Fatal("contender failed to obtain pool lock after leader release")
	}
	_ = next.Close()
}

func TestParseDarwinProcessSnapshotUsesNumericGetsid(t *testing.T) {
	processes, err := parseDarwinProcessSnapshot(
		[]byte("123 1 123 2048 /bin/zsh -c work\n"),
		func(pid int) (int, error) {
			if pid != 123 {
				t.Fatalf("session lookup pid = %d", pid)
			}
			return 123, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	process := processes[123]
	if process.SID != 123 || process.RSS != 2*1024*1024 || process.Args != "/bin/zsh -c work" {
		t.Fatalf("process = %+v", process)
	}
}

func TestRunSupervisorHelper(t *testing.T) {
	if os.Getenv(supervisorHelperEnv) != "1" {
		return
	}
	resourceID := "zen-agent-helper-0123456789abcdef0123456789abcdef.scope"
	err := RunSupervisor(SupervisorConfig{
		ResourceID: resourceID,
		LeaseDir:   os.Getenv("ZEN_AGENTPROC_LEASE_DIR"),
		MemoryHigh: "1G",
		MemoryMax:  "2G",
		TasksMax:   64,
		Command:    []string{"/bin/sh", "-c", "exit 0"},
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if matches, _ := filepath.Glob(filepath.Join(os.Getenv("ZEN_AGENTPROC_LEASE_DIR"), "*.json")); len(matches) != 0 {
		fmt.Fprintf(os.Stderr, "leases remain: %v\n", matches)
		os.Exit(3)
	}
	os.Exit(0)
}
