package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/daoleno/zen/daemon/doctor"
)

func runDoctorCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := false
	stateDir := ""
	addr := ""
	fs.BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	fs.StringVar(&stateDir, "state-dir", "", "state directory for daemon identity and readiness checks")
	fs.StringVar(&addr, "addr", "127.0.0.1:9876", "daemon listen address to probe")
	fs.Usage = func() {
		printDoctorUsage(stderr)
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	report, err := doctor.Run(doctor.Options{
		StateDir: stateDir,
		Addr:     addr,
	})
	if err != nil {
		return err
	}

	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return err
		}
	} else {
		if err := doctor.WriteHuman(os.Stdout, report); err != nil {
			return err
		}
	}
	if !report.Ready {
		return doctor.ErrNotReady
	}
	return nil
}

func printDoctorUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: zen doctor [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Diagnose whether this machine can run Zen (tmux, state dir, listen port, executors).")
	fmt.Fprintln(w, "Never installs packages or prints credentials. Exit status is nonzero when not ready.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  zen doctor")
	fmt.Fprintln(w, "  zen doctor --json")
	fmt.Fprintln(w, "  zen doctor --state-dir /tmp/zen-state --addr 127.0.0.1:9876")
}
