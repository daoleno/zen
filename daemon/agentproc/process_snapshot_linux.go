//go:build linux

package agentproc

import (
	"fmt"
	"os/exec"
	"strings"
)

func processSnapshot() (map[int]processRecord, error) {
	out, err := exec.Command("ps", "-ww", "-axo", "pid=,ppid=,pgid=,sess=,rss=,command=").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("snapshot processes: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseLinuxProcessSnapshot(out)
}

func parseLinuxProcessSnapshot(out []byte) (map[int]processRecord, error) {
	processes := make(map[int]processRecord)
	scanner := largeLineScanner(out)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 {
			continue
		}
		values, valid := parseProcessIntegers(fields[:5])
		if !valid || values[0] <= 0 {
			continue
		}
		processes[int(values[0])] = processRecord{
			PID:  int(values[0]),
			PPID: int(values[1]),
			PGID: int(values[2]),
			SID:  int(values[3]),
			RSS:  uint64(max(values[4], 0)) * 1024,
			Args: strings.Join(fields[5:], " "),
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return processes, nil
}

func markedProcessIDs(resourceID string) (map[int]bool, error) {
	// procps uses the BSD-style "e" flag to append each process environment.
	out, err := exec.Command("ps", "eww", "-axo", "pid=,command=").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("inspect delegated process markers: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseMarkedProcessIDs(out, delegatedResourceMarker(resourceID))
}

func allMarkedProcessIDs() (map[string]map[int]bool, error) {
	out, err := exec.Command("ps", "eww", "-axo", "pid=,command=").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("inspect delegated process markers: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseAllMarkedProcessIDs(out)
}
