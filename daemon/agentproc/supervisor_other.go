//go:build !linux && !darwin

package agentproc

import "fmt"

func RunSupervisor(SupervisorConfig) error {
	return fmt.Errorf("delegated process supervision is unsupported on this platform")
}

func StopLease(string) error {
	return fmt.Errorf("delegated process supervision is unsupported on this platform")
}

func physicalMemory() uint64 { return 0 }

func availableMemory() uint64 { return 0 }

func bootID() string { return "" }
