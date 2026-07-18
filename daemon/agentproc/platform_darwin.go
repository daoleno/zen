//go:build darwin

package agentproc

import (
	"os/exec"
	"strings"

	"golang.org/x/sys/unix"
)

func physicalMemory() uint64 {
	value, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return value
}

func availableMemory() uint64 {
	// One vm_stat observation: free/inactive/speculative page counts are 32-bit
	// SYSCTL_UINT on XNU, so SysctlUint64 is incorrect. Only launch admission and
	// the single elected pool leader call this path.
	out, err := exec.Command("/usr/bin/vm_stat").Output()
	if err != nil {
		return 0
	}
	available, err := parseDarwinVMStat(out)
	if err != nil {
		return 0
	}
	return available
}

func bootID() string {
	out, err := exec.Command("/usr/sbin/sysctl", "-n", "kern.boottime").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
