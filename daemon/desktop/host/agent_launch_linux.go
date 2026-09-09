package host

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// BrokerMayLaunchAgent allows a root broker to exec desktop-agent only from the
// reviewed installed ELF. User-writable DEV binaries are refused even if root.
func BrokerMayLaunchAgent(installed *os.File) error {
	if os.Geteuid() != 0 {
		return errors.New("broker_not_root")
	}
	selfPath, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return errors.New("broker_identity_unavailable")
	}
	if ForbiddenBrokerPath(selfPath) {
		return errors.New("user_writable_broker_refused")
	}
	var self unix.Stat_t
	if unix.Stat("/proc/self/exe", &self) != nil {
		return errors.New("broker_identity_unavailable")
	}
	info, err := installed.Stat()
	if err != nil {
		return errors.New("broker_identity_unavailable")
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint64(sys.Dev) != uint64(self.Dev) || uint64(sys.Ino) != uint64(self.Ino) {
		return errors.New("broker_not_installed_binary")
	}
	return nil
}
