//go:build darwin

package watcher

import (
	"time"

	"golang.org/x/sys/unix"
)

// processStartTimeFromProc derives the precise Darwin process start from the
// kernel's kern.proc.pid kinfo_proc record. P_starttime is a timeval with
// microsecond precision and is the Darwin counterpart to Linux
// /proc/<pid>/stat starttime.
func processStartTimeFromProc(pid int) (time.Time, bool) {
	if pid <= 0 {
		return time.Time{}, false
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || info == nil || int(info.Proc.P_pid) != pid {
		return time.Time{}, false
	}
	start := time.Unix(info.Proc.P_starttime.Sec, int64(info.Proc.P_starttime.Usec)*int64(time.Microsecond)).UTC()
	if start.IsZero() {
		return time.Time{}, false
	}
	return start, true
}
