//go:build !linux && !darwin

package watcher

import "errors"

func pendingPTYInputBytes(string) (int, error) {
	return 0, errors.New("PTY input queue inspection is unsupported on this platform")
}
