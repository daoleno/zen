package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/daoleno/zen/daemon/desktop/host"
)

func main() {
	configPath := flag.String("config", "/etc/zen/desktop-host.json", "Root-owned host configuration")
	register := flag.String("register", "", "SDDM registration action: start or stop")
	install := flag.Bool("install", false, "Install reviewed broker/agent and preserve SDDM hooks")
	rollback := flag.Bool("rollback", false, "Roll back unchanged installed files; broker must be stopped")
	activate := flag.Bool("activate", false, "Enable/start the installed broker; do not restart SDDM or the owner")
	brokerSource := flag.String("broker-source", "", "Reviewed broker ELF binary")
	agentSource := flag.String("agent-source", "", "Reviewed UID-dropped agent ELF binary")
	flag.Parse()
	var err error
	if *rollback {
		if serviceCommand("is-active", "--quiet", "zen-desktop-host.service") == nil {
			fmt.Fprintln(os.Stderr, "Stop the installed broker before rollback.")
			os.Exit(1)
		}
		err = serviceCommand("disable", "--no-reload", "zen-desktop-host.service")
		if err == nil {
			err = host.RollbackLinuxInstall()
		}
		if err == nil {
			err = serviceCommand("daemon-reload")
		}
	} else if *register != "" {
		err = host.RegisterDisplay(*register, os.Getenv("DISPLAY"), os.Getenv("XAUTHORITY"))
	} else {
		var config host.HostConfig
		if *install {
			// The explicit installer input is unprivileged data, not live authority.
			var file *os.File
			file, err = os.Open(*configPath)
			if err == nil {
				config, err = host.ReadHostConfig(file)
				file.Close()
			}
		} else {
			config, err = host.LoadRootConfig(*configPath)
		}
		if err == nil {
			if *install {
				err = host.InstallLinux(config, *brokerSource, *agentSource)
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
				err = host.ServeBroker(ctx, config)
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Zen desktop host operation failed.")
		os.Exit(1)
	}
}

func serviceCommand(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/systemctl", args...).Run()
}
