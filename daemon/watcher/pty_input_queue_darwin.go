//go:build darwin

package watcher

import "golang.org/x/sys/unix"

const darwinFIONREAD = 0x4004667f

func pendingPTYInputBytes(path string) (int, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOCTTY, 0)
	if err != nil {
		return 0, err
	}
	defer unix.Close(fd)
	return unix.IoctlGetInt(fd, darwinFIONREAD)
}
