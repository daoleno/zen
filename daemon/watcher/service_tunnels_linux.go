package watcher

import (
	"os/exec"
	"syscall"
)

// The tunnel cannot survive an abrupt daemon exit, including SIGKILL.
func bindTunnelToDaemon(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return nil
}
