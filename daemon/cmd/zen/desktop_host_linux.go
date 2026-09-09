package main

import (
	"io"

	"github.com/daoleno/zen/daemon/desktop/host"
)

func runDesktopHostCommand(args []string, stderr io.Writer) error {
	return host.RunLinuxCLI(args, stderr)
}
