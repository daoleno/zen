//go:build linux || darwin

package agentproc

import (
	"fmt"
	"strconv"
	"strings"
)

// parseDarwinVMStat parses Apple vm_stat output into an available-memory estimate:
// (Pages free + Pages inactive + Pages speculative) * page_size.
// Pages purgeable is intentionally ignored so purgeable memory is not double-counted
// when it already appears in another page class.
func parseDarwinVMStat(out []byte) (uint64, error) {
	var pageSize, free, inactive, speculative uint64
	sawFree, sawInactive, sawSpeculative := false, false, false
	scanner := largeLineScanner(out)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if size, ok := parseDarwinVMStatPageSize(line); ok {
			pageSize = size
			continue
		}
		key, value, ok := parseDarwinVMStatPagesLine(line)
		if !ok {
			continue
		}
		switch key {
		case "Pages free":
			free = value
			sawFree = true
		case "Pages inactive":
			inactive = value
			sawInactive = true
		case "Pages speculative":
			speculative = value
			sawSpeculative = true
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	if pageSize == 0 {
		return 0, fmt.Errorf("vm_stat page size missing")
	}
	if !sawFree && !sawInactive && !sawSpeculative {
		return 0, fmt.Errorf("vm_stat page counters missing")
	}
	return (free + inactive + speculative) * pageSize, nil
}

func parseDarwinVMStatPageSize(line string) (uint64, bool) {
	const prefix = "page size of "
	index := strings.Index(strings.ToLower(line), prefix)
	if index < 0 {
		return 0, false
	}
	rest := line[index+len(prefix):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	value, err := strconv.ParseUint(rest[:end], 10, 64)
	if err != nil || value == 0 {
		return 0, false
	}
	return value, true
}

func parseDarwinVMStatPagesLine(line string) (string, uint64, bool) {
	if !strings.HasPrefix(line, "Pages ") {
		return "", 0, false
	}
	colon := strings.IndexByte(line, ':')
	if colon <= 0 {
		return "", 0, false
	}
	key := strings.TrimSpace(line[:colon])
	raw := strings.TrimSpace(line[colon+1:])
	raw = strings.TrimSuffix(raw, ".")
	raw = strings.ReplaceAll(raw, ",", "")
	if raw == "" {
		return "", 0, false
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return key, value, true
}
