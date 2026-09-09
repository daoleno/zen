package host

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// RunLinuxCLI implements `zen desktop-host` and the historical zen-desktop-host entry.
func RunLinuxCLI(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen desktop-host", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "/etc/zen/desktop-host.json", "Root-owned host configuration")
	register := fs.String("register", "", "SDDM registration action: start or stop")
	install := fs.Bool("install", false, "Install reviewed same-binary zen as broker/agent")
	rollback := fs.Bool("rollback", false, "Roll back unchanged installed files; broker must be stopped")
	activate := fs.Bool("activate", false, "Enable/start the installed broker; do not restart SDDM or the owner")
	binarySource := fs.String("binary-source", "", "Reviewed desktop-capable zen ELF used for every role")
	brokerSource := fs.String("broker-source", "", "Legacy alias; must match --binary-source / --agent-source")
	agentSource := fs.String("agent-source", "", "Legacy alias; must match --binary-source / --broker-source")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var err error
	if *rollback {
		if serviceCommand("is-active", "--quiet", "zen-desktop-host.service") == nil {
			fmt.Fprintln(stderr, "Stop the installed broker before rollback.")
			return errBrokerActive
		}
		err = serviceCommand("disable", "--no-reload", "zen-desktop-host.service")
		if err == nil {
			err = RollbackLinuxInstall()
		}
		if err == nil {
			err = serviceCommand("daemon-reload")
		}
	} else if *register != "" {
		err = RegisterDisplay(*register, os.Getenv("DISPLAY"), os.Getenv("XAUTHORITY"))
	} else {
		var config HostConfig
		if *install {
			var file *os.File
			file, err = os.Open(*configPath)
			if err == nil {
				config, err = ReadHostConfig(file)
				file.Close()
			}
		} else {
			config, err = LoadRootConfig(*configPath)
		}
		if err == nil {
			if *install {
				source, sourceErr := ResolveInstallSource(*binarySource, *brokerSource, *agentSource)
				if sourceErr != nil {
					err = sourceErr
				} else {
					err = InstallLinux(config, source)
				}
			}
			if err == nil && *activate {
				err = serviceCommand("is-active", "--quiet", config.OwnerUnit)
				if err == nil {
					err = serviceCommand("daemon-reload")
				}
				if err == nil {
					err = serviceCommand("enable", "--now", "zen-desktop-host.service")
				}
			} else if !*install && !*activate {
				ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
				defer cancel()
				err = ServeBroker(ctx, config)
			}
		}
	}
	if err != nil {
		if !errors.Is(err, errBrokerActive) {
			fmt.Fprintln(stderr, "Zen desktop host operation failed.")
		}
		return err
	}
	return nil
}

func serviceCommand(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/systemctl", args...).Run()
}
