//go:build linux

package watcher

import "golang.org/x/sys/unix"

func pendingPTYInputBytes(path string) (int, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOCTTY, 0)
	if err != nil {
		return 0, err
	}
	defer unix.Close(fd)
	return unix.IoctlGetInt(fd, unix.TIOCINQ)
}
