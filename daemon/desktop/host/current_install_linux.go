package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

func verifyUnchangedInstall(config HostConfig, source string) error {
	installed, err := LoadRootConfig("/etc/zen/desktop-host.json")
	if err != nil || installed != config {
		return errors.New("installed_host_config_differs; existing installation preserved")
	}
	data, err := readInstallSource(source)
	if err != nil {
		return err
	}
	file, err := OpenRootFile(installJournalPath, 0600)
	if err != nil {
		return err
	}
	var journal installJournal
	bytes, err := io.ReadAll(io.LimitReader(file, 1<<20))
	file.Close()
	if err != nil || decodeMessage(bytes, &journal) != nil || journal.Version != 1 {
		return errors.New("invalid_install_journal")
	}
	foundBinary := false
	for _, entry := range journal.Files {
		file, err := OpenRootFile(entry.Path, entry.Mode)
		if err != nil {
			return errors.New("installed_file_changed; existing installation preserved")
		}
		current, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
		file.Close()
		if err != nil || digest(current) != entry.SHA256 {
			return errors.New("installed_file_changed; existing installation preserved")
		}
		if entry.Path == InstalledBinary {
			foundBinary = true
			if digest(data) != entry.SHA256 {
				return errors.New("installed_binary_differs; existing installation preserved")
			}
		}
	}
	if !foundBinary {
		return errors.New("invalid_install_journal")
	}
	return nil
}

type currentInstallOps struct {
	install  func() error
	activate func() error
	register func() error
	probe    func() (probeResult, error)
	rollback func() error
}

// currentInstallSharedOps is the install/activate/rollback transaction shared
// by the X11 SDDM registration and the Wayland dynamic-discovery registration.
// Only the register/probe steps differ between session types.
func currentInstallSharedOps(config HostConfig, source string, existing bool) (func() error, func() error, func() error) {
	install := func() error {
		if existing {
			return verifyUnchangedInstall(config, source)
		}
		if err := installLinuxLocked(config, source); err != nil {
			if _, statErr := os.Lstat(installJournalPath); statErr == nil {
				if rollbackErr := rollbackLinuxLocked(); rollbackErr != nil {
					return fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
				}
			}
			return err
		}
		return nil
	}
	activate := func() error {
		if config.OwnerUnit != "" {
			if err := serviceCommand("is-active", "--quiet", config.OwnerUnit); err != nil {
				return errors.New("configured_owner_unit_not_active")
			}
		}
		if err := serviceCommand("daemon-reload"); err != nil {
			return errors.New("broker_daemon_reload_failed")
		}
		if err := serviceCommand("enable", "--now", "zen-desktop-host.service"); err != nil {
			return errors.New("broker_activation_failed")
		}
		return nil
	}
	rollback := func() error {
		if err := serviceCommand("stop", "zen-desktop-host.service"); err != nil {
			return err
		}
		if err := serviceCommand("disable", "--no-reload", "zen-desktop-host.service"); err != nil {
			return err
		}
		if err := rollbackLinuxLocked(); err != nil {
			return err
		}
		return serviceCommand("daemon-reload")
	}
	return install, activate, rollback
}

// The transaction is injectable for fault tests. An unchanged existing install
// is never stopped or rolled back by a failed verification.
func runCurrentInstall(existing bool, ops currentInstallOps) (result probeResult, err error) {
	if err = ops.install(); err != nil {
		return result, err
	}
	defer func() {
		if err == nil || existing || errors.Is(err, errDesktopProbeBusy) {
			return
		}
		if rollbackErr := ops.rollback(); rollbackErr != nil {
			err = fmt.Errorf("%w; rollback refused or failed: %v (review the installation journal)", err, rollbackErr)
		} else {
			err = fmt.Errorf("%w; new installation rolled back, SDDM and daemon were not restarted", err)
		}
	}()
	if err = ops.activate(); err != nil {
		return result, err
	}
	if err = ops.register(); err != nil {
		return result, err
	}
	return ops.probe()
}

func installAndRegisterCurrent(config HostConfig, source string, out io.Writer, verbose bool) error {
	if err := rejectBrokerUnitOverrides("/etc/systemd/system/zen-desktop-host.service", "/run/systemd/system/zen-desktop-host.service"); err != nil {
		return err
	}
	if observation, err := InspectLinux(context.Background()); err == nil && observation.Session.Backend == "wayland" {
		return installAndRegisterWaylandCurrent(config, source, out, verbose)
	}
	display, err := selectCurrentDisplay(context.Background(), config)
	if err != nil {
		return fmt.Errorf("desktop setup preflight: %w (no installation applied)", err)
	}
	defer display.Close()
	lock, err := lockInstaller()
	if err != nil {
		return err
	}
	defer lock.Close()
	_, err = os.Lstat(installJournalPath)
	existing := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	installOp, activateOp, rollbackOp := currentInstallSharedOps(config, source, existing)
	result, err := runCurrentInstall(existing, currentInstallOps{
		install:  installOp,
		activate: activateOp,
		register: func() error {
			if err := display.Revalidate(context.Background(), config); err != nil {
				return err
			}
			_, err := registerDisplayFile("start", display.observation.Display, display.authority)
			return err
		},
		probe: func() (probeResult, error) {
			result, err := probeRegisteredDisplay(display.observation.Display)
			for deadline := time.Now().Add(3 * time.Second); err != nil && result.Error == "registered_session_unavailable" && time.Now().Before(deadline); {
				time.Sleep(100 * time.Millisecond)
				result, err = probeRegisteredDisplay(display.observation.Display)
			}
			if err != nil {
				return result, err
			}
			if err := display.Revalidate(context.Background(), config); err != nil {
				return probeResult{}, err
			}
			if result.Session != display.observation.Session.ID || result.FrameBytes == 0 {
				return probeResult{}, errors.New("broker_probe_session_mismatch")
			}
			return result, nil
		},
		rollback: rollbackOp,
	})
	if err != nil {
		return err
	}
	writeCurrentInstallSuccess(out, result, display.observation.Display, false, verbose)
	return nil
}

// writeCurrentInstallSuccess keeps the default success message brief and
// human-oriented: one verified fact and the next phone action. Probe internals
// and the rollback command stay available with --verbose. The message is always
// English and does not depend on LANG/LC_ALL.
func writeCurrentInstallSuccess(out io.Writer, result probeResult, display string, portal bool, verbose bool) {
	fmt.Fprintln(out, "Remote desktop is set up.")
	fmt.Fprintln(out, "Open Remote Desktop in the Zen app on your phone and tap Connect.")
	fmt.Fprintln(out, "The Zen daemon and the login screen were not restarted.")
	if !verbose {
		return
	}
	if portal {
		fmt.Fprintf(out, "Verified: %s Wayland desktop, session %s; the system portal asks for screen-sharing consent on the next Connect.\n", result.Surface, display)
	} else {
		fmt.Fprintf(out, "Verified: %s %s, %dx%d H.264 frame decoded and discarded; XTest available; no input sent.\n", result.Surface, display, result.Width, result.Height)
	}
	fmt.Fprintln(out, "Pairing and device-scope checks were not changed; a phone paired before desktop support may need to pair again.")
	fmt.Fprintln(out, "Rollback: sudo systemctl disable --now zen-desktop-host.service; sudo /usr/libexec/zen/zen desktop-host --rollback")
}

func rejectBrokerUnitOverrides(paths ...string) error {
	for _, path := range paths {
		if _, err := os.Lstat(path); err == nil {
			return errors.New("broker_unit_override_requires_manual_review; existing service preserved")
		} else if !os.IsNotExist(err) {
			return errors.New("broker_unit_ownership_unavailable")
		}
	}
	return nil
}
