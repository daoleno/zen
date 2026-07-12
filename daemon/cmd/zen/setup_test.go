package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/setup"
)

func TestSetupHelp(t *testing.T) {
	var stderr bytes.Buffer
	err := run([]string{"setup", "--help"}, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v", err)
	}
	out := stderr.String()
	for _, want := range []string{"Usage: zen setup", "--non-interactive", "--profile", "--yes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
}

func TestMainHelpListsSetup(t *testing.T) {
	var stderr bytes.Buffer
	err := run([]string{"--help"}, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stderr.String(), "setup") {
		t.Fatalf("main help missing setup:\n%s", stderr.String())
	}
}

func TestSetupNonInteractiveConsentExit(t *testing.T) {
	// Library-level sentinel remains stable for CLI mapping.
	if !errors.Is(setup.ErrConsentRequired, setup.ErrConsentRequired) {
		t.Fatal("sentinel identity")
	}
}
