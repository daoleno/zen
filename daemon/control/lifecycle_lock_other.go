//go:build !linux && !darwin

package control

import "errors"

type LifecycleLock struct{}

func TryAcquireLifecycleLock(
	stateDir string,
) (*LifecycleLock, bool, error) {
	return nil, false, errors.New(
		"daemon lifecycle locking is unsupported on this platform",
	)
}

func (lock *LifecycleLock) Close() error {
	return nil
}
