//go:build !linux && !darwin

package watcher

func newDelegatedResourceManager(string) delegatedResourceManager {
	return unavailableDelegatedResourceManager{reason: "portable process supervision is not implemented on this platform"}
}

func validateDelegatedWorkspace(string) error { return nil }
