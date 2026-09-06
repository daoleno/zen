package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
)

func TestWorkerCLIHasNoWorkerCompatibilityCommand(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"worker", "--help"}, &output); err != nil && !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "zen worker") || strings.Contains(output.String(), "zen agent") {
		t.Fatalf("Worker help=%s", output.String())
	}
	output.Reset()
	if err := run([]string{"agent", "--help"}, &output); err == nil || errors.Is(err, flag.ErrHelp) {
		t.Fatalf("removed Agent command accepted: err=%v output=%s", err, output.String())
	}
}
