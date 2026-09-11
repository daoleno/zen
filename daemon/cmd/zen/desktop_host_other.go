//go:build !linux

package main

import (
	"errors"
	"io"
)

func runDesktopHostCommand(args []string, stderr io.Writer) error {
	_ = args
	_ = stderr
	return errors.New("Linux unattended desktop host is not supported on this platform")
}
