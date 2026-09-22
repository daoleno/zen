//go:build !linux

package work

import "os/exec"

func bindDSHProcess(*exec.Cmd) {}
