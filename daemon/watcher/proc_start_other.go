//go:build !linux && !darwin

package watcher

import "time"

// processStartTimeFromProc is unavailable on this platform: no /proc
// starttime evidence exists, so the second-granularity ps lstart value
// remains the process start evidence and the same-second instance-ownership
// limitation is a documented platform data limit (like zero startedAt).
func processStartTimeFromProc(pid int) (time.Time, bool) {
	return time.Time{}, false
}
