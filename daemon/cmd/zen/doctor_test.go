package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/doctor"
)

func TestDoctorHelp(t *testing.T) {
	var stderr bytes.Buffer
	err := run([]string{"doctor", "--help"}, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want ErrHelp", err)
	}
	out := stderr.String()
	for _, want := range []string{"Usage: zen doctor", "--json", "Diagnose whether this machine can run Zen"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
}

func TestMainHelpListsDoctor(t *testing.T) {
	var stderr bytes.Buffer
	err := run([]string{"--help"}, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "doctor") {
		t.Fatalf("main help missing doctor:\n%s", stderr.String())
	}
}

func TestDoctorJSONSchemaSmoke(t *testing.T) {
	// Library-level smoke through the same Report shape the CLI encodes.
	report := doctor.Report{
		Ready: false,
		Checks: []doctor.NamedCheck{
			{ID: "tmux", Status: doctor.StatusFail, Remediation: doctor.RemediationInstallTmux, Summary: "tmux not found on PATH"},
		},
		Remediations: []doctor.Remediation{doctor.RemediationInstallTmux},
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"ready":false`) {
		t.Fatalf("unexpected JSON: %s", raw)
	}
}
