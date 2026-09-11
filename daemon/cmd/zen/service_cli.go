package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/watcher"
)

func runServiceCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printServiceUsage(stderr)
		return flag.ErrHelp
	}
	switch args[0] {
	case "list":
		return runServiceList(args[1:], stderr)
	case "register":
		return runServiceRegister(args[1:], stderr)
	case "unregister":
		return runServiceUnregister(args[1:], stderr)
	default:
		return fmt.Errorf("unknown service command: %s", args[0])
	}
}

func printServiceUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: zen service <list|register|unregister> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  list        List Agent-visible services (tmux sessions + registered persistent units)")
	fmt.Fprintln(w, "  register    Adopt one persistent user systemd unit without restarting it")
	fmt.Fprintln(w, "  unregister  Remove one persistent service registration (never stops the unit)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  zen service list --json")
	fmt.Fprintln(w, "  zen service register -unit dsh-web.service -name \"DeepSeek Harness\" -project dsh -port 3080")
	fmt.Fprintln(w, "  zen service unregister -unit dsh-web.service")
}

func runServiceList(args []string, stderr io.Writer) error {
	cfg, err := parseCLIConfig("zen service list", args, stderr)
	if err != nil {
		return err
	}
	resp, err := callControl(cfg, control.Request{Type: "service_list"})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runServiceRegister(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen service register", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	desc := watcher.ManagedServiceDescriptor{}
	var workID string
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&desc.Unit, "unit", "", "user systemd unit to adopt (for example dsh-web.service)")
	fs.StringVar(&desc.Name, "name", "", "display name for the service")
	fs.StringVar(&desc.Project, "project", "", "project label")
	fs.StringVar(&desc.Cwd, "cwd", "", "service working directory")
	fs.IntVar(&desc.Port, "port", 0, "expected listening port (0 matches any owned port)")
	fs.StringVar(&desc.RegisteredBy, "registered-by", "", "owning Worker id (defaults to ZEN_WORKER_ID)")
	fs.StringVar(&workID, "work", "", "Brain Work id owning this service")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen service register -unit dsh-web.service -name \"DeepSeek Harness\" [-project dsh] [-port 3080] [-cwd ~/workspace/dsh-smoke]")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(desc.RegisteredBy) == "" {
		desc.RegisteredBy = strings.TrimSpace(os.Getenv("ZEN_WORKER_ID"))
	}
	resp, err := callControl(cfg, control.Request{
		Type:     "service_register",
		Service:  &desc,
		WorkerID: desc.RegisteredBy,
		WorkID:   strings.TrimSpace(workID),
	})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runServiceUnregister(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen service unregister", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	var unit string
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&unit, "unit", "", "registered user systemd unit to remove")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen service unregister -unit dsh-web.service")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	resp, err := callControl(cfg, control.Request{Type: "service_unregister", ServiceUnit: unit})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func serviceDisplayState(service watcher.SessionService) string {
	if strings.TrimSpace(service.State) != "" {
		return strings.TrimSpace(service.State)
	}
	return watcher.ServiceStateActive
}

func serviceDisplaySource(service watcher.SessionService) string {
	if strings.TrimSpace(service.Source) != "" {
		return strings.TrimSpace(service.Source)
	}
	return watcher.ServiceSourceSession
}

func serviceDisplayName(service watcher.SessionService) string {
	name := strings.TrimSpace(service.WorkerName)
	if strings.TrimSpace(service.Unit) != "" {
		if name == "" {
			return strings.TrimSpace(service.Unit)
		}
		return name + " (" + strings.TrimSpace(service.Unit) + ")"
	}
	if name == "" {
		return strings.TrimSpace(service.WorkerID)
	}
	return name
}
