//go:build linux

package watcher

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// processStartTimeFromProc derives the precise start time of pid from
// /proc/<pid>/stat field 22 (starttime, clock ticks since boot) converted
// with btime from /proc/stat and the system clock tick rate. ps lstart only
// carries whole-second precision; the proc derivation is typically 10ms, so
// instance-ownership arms can compare against the true process start instead
// of its rounded second.
func processStartTimeFromProc(pid int) (time.Time, bool) {
	if pid <= 0 {
		return time.Time{}, false
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return time.Time{}, false
	}
	startTicks, ok := parseProcStatStartTicks(stat)
	if !ok {
		return time.Time{}, false
	}
	hz := sysconfClockTicks()
	if hz <= 0 {
		return time.Time{}, false
	}
	bootSeconds, ok := bootTimeSecondsFromProcStat()
	if !ok {
		return time.Time{}, false
	}
	start := time.Unix(
		bootSeconds+startTicks/hz,
		(startTicks%hz)*int64(time.Second)/hz,
	).UTC()
	if start.IsZero() {
		return time.Time{}, false
	}
	return start, true
}

// parseProcStatStartTicks extracts field 22 (starttime) from /proc/<pid>/stat
// content. The comm field may contain spaces or parentheses, so parsing
// starts after the final ')'.
func parseProcStatStartTicks(stat []byte) (int64, bool) {
	close := bytes.LastIndexByte(stat, ')')
	if close < 0 {
		return 0, false
	}
	fields := strings.Fields(string(stat[close+1:]))
	// After ')', the first field is state (proc field 3); starttime is proc
	// field 22, i.e. index 19 in this slice.
	if len(fields) <= 19 {
		return 0, false
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil || ticks < 0 {
		return 0, false
	}
	return ticks, true
}

// sysconfClockTicks returns the system clock tick rate (USER_HZ, typically
// 100) via getconf CLK_TCK, cached after the first successful read. Zero
// means the tick rate could not be proven; callers must then fall back to
// the observed start evidence rather than guessing.
func sysconfClockTicks() int64 {
	ticksOnce.Do(func() {
		out, err := exec.Command("getconf", "CLK_TCK").Output()
		if err != nil {
			return
		}
		value, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		if err != nil || value <= 0 {
			return
		}
		ticks = value
	})
	return ticks
}

var (
	ticksOnce sync.Once
	ticks     int64
)

// bootTimeSecondsFromProcStat reads btime (boot time in Unix seconds) from
// /proc/stat.
func bootTimeSecondsFromProcStat() (int64, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "btime ") {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "btime ")), 10, 64)
		if err != nil || value <= 0 {
			return 0, false
		}
		return value, true
	}
	return 0, false
}
