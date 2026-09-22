package work

import (
	"os/exec"
	"syscall"
)

func bindDSHProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
