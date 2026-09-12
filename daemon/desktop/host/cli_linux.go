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
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop"
	"github.com/daoleno/zen/daemon/desktop/nativebind"
)

// RunLinuxCLI implements `zen desktop-host` and the historical zen-desktop-host entry.
func RunLinuxCLI(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen desktop-host", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "/etc/zen/desktop-host.json", "Root-owned host configuration")
	initConfig := fs.Bool("init-config", false, "Generate an idempotent user-owned host configuration")
	stateDir := fs.String("state-dir", "", "Zen state directory used to read the canonical daemon identity")
	ownerUnit := fs.String("owner-unit", "", "Optional root-enrolled systemd unit that owns the canonical daemon")
	register := fs.String("register", "", "SDDM registration action: start or stop")
	install := fs.Bool("install", false, "Install reviewed same-binary zen as broker/agent")
	rollback := fs.Bool("rollback", false, "Roll back unchanged installed files; broker must be stopped")
	activate := fs.Bool("activate", false, "Enable/start the installed broker; do not restart SDDM or the owner")
	planOnly := fs.Bool("plan", false, "Print the reviewed install manifest without changing the host")
	binarySource := fs.String("binary-source", "", "Reviewed desktop-capable zen ELF used for every role")
	brokerSource := fs.String("broker-source", "", "Legacy alias; must match --binary-source / --agent-source")
	agentSource := fs.String("agent-source", "", "Legacy alias; must match --binary-source / --broker-source")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *initConfig {
		return initLinuxConfig(fs, *configPath, *stateDir, *ownerUnit, stderr)
	}
	if *planOnly {
		if *install || *rollback || *activate || *register != "" {
			fmt.Fprintln(stderr, "zen desktop-host --plan cannot be combined with install, rollback, activate, or register.")
			return errors.New("invalid_desktop_host_plan")
		}
		file, err := os.Open(*configPath)
		if err != nil {
			fmt.Fprintln(stderr, "Zen desktop host operation failed.")
			return err
		}
		config, err := ReadHostConfig(file)
		file.Close()
		if err != nil {
			fmt.Fprintln(stderr, "Zen desktop host operation failed.")
			return err
		}
		plan, err := PrepareLinuxInstall(config)
		if err != nil {
			fmt.Fprintln(stderr, "Zen desktop host operation failed.")
			return err
		}
		PrintInstallPlan(stderr, plan)
		return nil
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
				// An ownerUnit is optional: with account scope there is no unit to
				// preflight, and the broker admits the owner UID directly.
				if config.OwnerUnit != "" {
					err = serviceCommand("is-active", "--quiet", config.OwnerUnit)
				}
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

func initLinuxConfig(fs *flag.FlagSet, configuredPath, stateDir, ownerUnit string, stderr io.Writer) error {
	for _, name := range []string{"register", "install", "rollback", "activate", "plan", "binary-source", "broker-source", "agent-source"} {
		if flagWasSet(fs, name) {
			return errors.New("--init-config cannot be combined with install, rollback, activate, plan, register, or binary source flags")
		}
	}
	if runtime.GOOS != "linux" || !nativebind.NativeLinked {
		return errors.New("desktop host setup requires a Linux desktop-capable zen ELF (CGO and -tags zen_desktop)")
	}
	if strings.TrimSpace(stateDir) == "" {
		var err error
		stateDir, err = auth.DefaultStorageDir()
		if err != nil {
			return fmt.Errorf("locate Zen state directory: %w", err)
		}
	}
	manager, err := auth.NewManager(stateDir)
	if err != nil {
		return fmt.Errorf("read canonical daemon identity: %w", err)
	}
	path := configuredPath
	if !flagWasSet(fs, "config") {
		path = filepath.Join(stateDir, "desktop-host.json")
	}
	config := HostConfig{Version: 1, HostID: manager.DaemonID(), OwnerUID: uint32(os.Getuid()), Seat: "seat0", OwnerUnit: strings.TrimSpace(ownerUnit)}
	created, err := InitializeConfig(path, config)
	if err != nil {
		return err
	}
	executable, err := desktop.CurrentExecutable()
	if err != nil {
		return fmt.Errorf("locate current zen executable: %w", err)
	}
	if created {
		fmt.Fprintf(stderr, "Zen desktop host config created: %s\n", path)
	} else {
		fmt.Fprintf(stderr, "Zen desktop host config already matches this daemon: %s\n", path)
	}
	fmt.Fprintf(stderr, "  owner account: uid %d, seat0\n", config.OwnerUID)
	fmt.Fprintf(stderr, "  review: zen desktop-host --plan --config %q\n", path)
	fmt.Fprintf(stderr, "  install: sudo %q desktop-host --install --config %q --binary-source %q --activate\n", executable, path, executable)
	fmt.Fprintln(stderr, "  current-session access does not need this administrator step; boot/greeter access does.")
	return nil
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func serviceCommand(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/systemctl", args...).Run()
}
