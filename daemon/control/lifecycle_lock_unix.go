//go:build linux || darwin

package control

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

const lifecycleLockName = "daemon.lock"

// LifecycleLock proves exclusive ownership of one daemon state directory.
// The lock file is intentionally persistent: removing a locked inode would
// allow another process to lock a replacement path concurrently.
type LifecycleLock struct {
	file      *os.File
	closeOnce sync.Once
	closeErr  error
}

func TryAcquireLifecycleLock(
	stateDir string,
) (*LifecycleLock, bool, error) {
	socketPath, err := DefaultSocketPath(stateDir)
	if err != nil {
		return nil, false, err
	}
	runDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, false, fmt.Errorf("create daemon runtime directory: %w", err)
	}
	path := filepath.Join(runDir, lifecycleLockName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open daemon lifecycle lock: %w", err)
	}
	if err := unix.Flock(
		int(file.Fd()),
		unix.LOCK_EX|unix.LOCK_NB,
	); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) ||
			errors.Is(err, unix.EAGAIN) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock daemon lifecycle: %w", err)
	}
	return &LifecycleLock{file: file}, true, nil
}

func (lock *LifecycleLock) Close() error {
	if lock == nil {
		return nil
	}
	lock.closeOnce.Do(func() {
		if lock.file == nil {
			return
		}
		unlockErr := unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
		closeErr := lock.file.Close()
		switch {
		case unlockErr != nil:
			lock.closeErr = fmt.Errorf("unlock daemon lifecycle: %w", unlockErr)
		case closeErr != nil:
			lock.closeErr = fmt.Errorf("close daemon lifecycle lock: %w", closeErr)
		}
	})
	return lock.closeErr
}
