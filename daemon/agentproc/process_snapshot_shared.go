//go:build linux || darwin

package agentproc

import (
	"bufio"
	"strconv"
	"strings"
)

func largeLineScanner(out []byte) *bufio.Scanner {
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	return scanner
}

func parseProcessIntegers(fields []string) ([]int64, bool) {
	values := make([]int64, len(fields))
	for index, field := range fields {
		parsed, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			return nil, false
		}
		values[index] = parsed
	}
	return values, true
}

func parseMarkedProcessIDs(out []byte, marker string) (map[int]bool, error) {
	marked := make(map[int]bool)
	scanner := largeLineScanner(out)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, marker) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err == nil && pid > 1 {
			marked[pid] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return marked, nil
}

// parseAllMarkedProcessIDs groups PIDs by ZEN_AGENT_RESOURCE_UNIT from one
// environment-bearing process listing. Callers must not issue one ps per lease.
func parseAllMarkedProcessIDs(out []byte) (map[string]map[int]bool, error) {
	const prefix = "ZEN_AGENT_RESOURCE_UNIT="
	marked := make(map[string]map[int]bool)
	scanner := largeLineScanner(out)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		index := strings.Index(line, prefix)
		if index < 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 1 {
			continue
		}
		rest := line[index+len(prefix):]
		resourceID := rest
		if end := strings.IndexAny(rest, " \t"); end >= 0 {
			resourceID = rest[:end]
		}
		resourceID = strings.TrimSpace(resourceID)
		if resourceID == "" {
			continue
		}
		if marked[resourceID] == nil {
			marked[resourceID] = make(map[int]bool)
		}
		marked[resourceID][pid] = true
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return marked, nil
}

func parseDarwinProcessSnapshot(out []byte, sessionID func(int) (int, error)) (map[int]processRecord, error) {
	processes := make(map[int]processRecord)
	scanner := largeLineScanner(out)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 {
			continue
		}
		values, valid := parseProcessIntegers(fields[:4])
		if !valid || values[0] <= 0 {
			continue
		}
		pid := int(values[0])
		sid, _ := sessionID(pid)
		processes[pid] = processRecord{
			PID:  pid,
			PPID: int(values[1]),
			PGID: int(values[2]),
			SID:  sid,
			RSS:  uint64(max(values[3], 0)) * 1024,
			Args: strings.Join(fields[4:], " "),
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return processes, nil
}
