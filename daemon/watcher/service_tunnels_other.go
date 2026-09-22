//go:build !linux

package watcher

import (
	"fmt"
	"os/exec"
)

func bindTunnelToDaemon(*exec.Cmd) error {
	return fmt.Errorf("Quick Tunnels require a Linux daemon host with parent-death process ownership")
}
