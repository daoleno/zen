//go:build darwin

package agentproc

import (
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/sys/unix"
)

func processSnapshot() (map[int]processRecord, error) {
	// Darwin's ps "sess" keyword is a kernel session pointer, not the numeric
	// POSIX session ID. Query getsid(2) for each visible PID instead.
	out, err := exec.Command("ps", "-ww", "-axo", "pid=,ppid=,pgid=,rss=,command=").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("snapshot processes: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseDarwinProcessSnapshot(out, unix.Getsid)
}

func markedProcessIDs(resourceID string) (map[int]bool, error) {
	// Apple ps uses -E (not procps' BSD-style e flag) to append environments.
	out, err := exec.Command("ps", "-E", "-ww", "-axo", "pid=,command=").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("inspect delegated process markers: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseMarkedProcessIDs(out, delegatedResourceMarker(resourceID))
}

func allMarkedProcessIDs() (map[string]map[int]bool, error) {
	out, err := exec.Command("ps", "-E", "-ww", "-axo", "pid=,command=").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("inspect delegated process markers: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseAllMarkedProcessIDs(out)
}
