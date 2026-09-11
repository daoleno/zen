package main

import (
	"os"

	"github.com/daoleno/zen/daemon/desktop/host"
)

func main() {
	if err := host.RunLinuxCLI(os.Args[1:], os.Stderr); err != nil {
		os.Exit(1)
	}
}
