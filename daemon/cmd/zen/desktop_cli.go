package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/daoleno/zen/daemon/desktop"
	"github.com/daoleno/zen/daemon/desktop/nativebind"
)

func runDesktopIdentityCommand(stderr io.Writer) error {
	id, err := desktop.NewIdentity(nativebind.NativeLinked)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(id); err != nil {
		fmt.Fprintln(stderr, "desktop identity unavailable")
		return err
	}
	return nil
}

func runDesktopHelperCommand(args []string) error {
	argv := append([]string{os.Args[0]}, append([]string{desktop.RoleHelper}, args...)...)
	return nativebind.RunHelper(argv)
}

func runDesktopAgentCommand(args []string) error {
	argv := append([]string{os.Args[0]}, append([]string{desktop.RoleAgent}, args...)...)
	return nativebind.RunAgent(argv)
}
