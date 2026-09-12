package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type currentInstallOps struct {
	install  func() error
	activate func() error
	register func() error
	probe    func() (probeResult, error)
	rollback func() error
	// commit retains the previous installation state for one explicit
	// rollback and prunes only older snapshots after the whole transaction is
	// verified. It is best-effort and never rolls back a healthy broker.
	commit func() error
	// changed reports whether install wrote files (a fresh install or an
	// upgrade). An unchanged existing install still skips rollback.
	changed func() bool
}

// rollbackRequired is the shared transaction policy: a fresh install always
// restores on failure, an upgrade restores because it changed files, and an
// unchanged existing install is never touched.
func rollbackRequired(existing, changed bool) bool { return !existing || changed }

// installOutcome is the single source of truth for what the install step did.
type installOutcome struct{ installed, upgraded bool }

func (o *installOutcome) markInstalled() { o.installed = true }
func (o *installOutcome) markUpgraded()  { o.upgraded = true }
func (o *installOutcome) changed() bool  { return o.installed || o.upgraded }

// verifyBrokerExecutable proves the active unit runs the installed broker
// content, not merely a matching path: /proc/<pid>/exe still maps the previous
// inode after a replacement, so both sides are hashed and compared.
func verifyBrokerExecutable(output func(args ...string) (string, error)) error {
	pid, err := output("show", "-p", "MainPID", "--value", "zen-desktop-host.service")
	if err != nil || pid == "" || pid == "0" {
		return errors.New("broker_not_running")
	}
	running, err := os.ReadFile("/proc/" + pid + "/exe")
	if err != nil {
		return errors.New("broker_executable_unreadable")
	}
	installed, err := os.ReadFile(InstalledBinary)
	if err != nil {
		return errors.New("broker_executable_missing")
	}
	if digest(running) != digest(installed) {
		return errors.New("broker_executable_mismatch")
	}
	return nil
}

// currentInstallSharedOps is the install/activate/rollback transaction shared
// by the X11 SDDM registration and the Wayland dynamic-discovery registration.
// Only the register/probe steps differ between session types. The steps bundle
// keeps the orchestrator fault-testable without root; production passes the
// real functions and services.
type installSteps struct {
	matches      func(config HostConfig, source string) (bool, error)
	freshInstall func(config HostConfig, source string) error
	upgrade      func(config HostConfig, source string) (installJournal, error)
	readJournal  func() ([]byte, error)
	loadJournal  func() (installJournal, error)
	rollbackNew  func() error
	restore      func(current installJournal) error
	verify       func() error
	command      func(args ...string) error
	output       func(args ...string) (string, error)
	io           upgradeIO
}

func realInstallSteps() installSteps {
	return installSteps{
		matches:      existingInstallMatches,
		freshInstall: installLinuxLocked,
		upgrade:      upgradeLinuxLocked,
		readJournal:  readInstallJournalBytes,
		loadJournal:  loadInstallJournal,
		rollbackNew:  rollbackLinuxLocked,
		restore:      func(current installJournal) error { return restorePreviousInstall(current, rootUpgradeIO()) },
		verify:       func() error { return verifyBrokerExecutable(serviceOutput) },
		command:      serviceCommand,
		output:       serviceOutput,
		io:           rootUpgradeIO(),
	}
}

// readServiceState records the actual pre-upgrade unit policy so a rollback
// restores it instead of enabling a unit the administrator had disabled.
func readServiceState(output func(args ...string) (string, error)) (enabled, active bool) {
	if value, err := output("is-enabled", "zen-desktop-host.service"); err == nil && strings.TrimSpace(value) == "enabled" {
		enabled = true
	}
	if value, err := output("is-active", "zen-desktop-host.service"); err == nil && strings.TrimSpace(value) == "active" {
		active = true
	}
	return enabled, active
}

// serviceRestoreCommands is the exact policy restoration for a previous unit.
func serviceRestoreCommands(enabled, active bool) [][]string {
	var commands [][]string
	if enabled {
		commands = append(commands, []string{"enable", "zen-desktop-host.service"})
	}
	if active {
		commands = append(commands, []string{"start", "zen-desktop-host.service"})
	}
	return commands
}

func currentInstallSharedOps(config HostConfig, source string, existing bool) (func() error, func() error, func() error, func() error, func() bool) {
	return currentInstallSharedOpsWith(config, source, existing, realInstallSteps())
}

func currentInstallSharedOpsWith(config HostConfig, source string, existing bool, steps installSteps) (func() error, func() error, func() error, func() error, func() bool) {
	outcome := &installOutcome{}
	var previous installJournal
	var previousEnabled, previousActive bool
	install := func() error {
		if existing {
			matches, err := steps.matches(config, source)
			if err != nil {
				return err
			}
			if matches {
				return nil
			}
			previousEnabled, previousActive = readServiceState(steps.output)
			retained, err := steps.upgrade(config, source)
			if err != nil {
				return err
			}
			previous = retained
			outcome.markUpgraded()
			return nil
		}
		if err := steps.freshInstall(config, source); err != nil {
			return err
		}
		outcome.markInstalled()
		return nil
	}
	activate := func() error {
		if config.OwnerUnit != "" {
			if err := steps.command("is-active", "--quiet", config.OwnerUnit); err != nil {
				return errors.New("configured_owner_unit_not_active")
			}
		}
		if err := steps.command("daemon-reload"); err != nil {
			return errors.New("broker_daemon_reload_failed")
		}
		if err := steps.command("enable", "--now", "zen-desktop-host.service"); err != nil {
			return errors.New("broker_activation_failed")
		}
		if outcome.changed() {
			// The unit may already be active with the previous ELF: enable
			// --now does not replace it. Switch the broker explicitly and prove
			// the running executable content is the installed file.
			if err := steps.command("restart", "zen-desktop-host.service"); err != nil {
				return errors.New("broker_restart_failed")
			}
			if err := steps.verify(); err != nil {
				return err
			}
		}
		return nil
	}
	rollback := func() error {
		if !outcome.changed() {
			return nil
		}
		if outcome.upgraded {
			// Restore the previous files, their journal metadata and the exact
			// previous service policy instead of uninstalling the old install.
			if err := steps.command("stop", "zen-desktop-host.service"); err != nil {
				return err
			}
			current, err := steps.loadJournal()
			if err != nil {
				return err
			}
			if err := steps.restore(current); err != nil {
				return err
			}
			if err := steps.command("daemon-reload"); err != nil {
				return err
			}
			for _, args := range serviceRestoreCommands(previousEnabled, previousActive) {
				if err := steps.command(args...); err != nil {
					return err
				}
			}
			return nil
		}
		if err := steps.command("stop", "zen-desktop-host.service"); err != nil {
			return err
		}
		if err := steps.command("disable", "--no-reload", "zen-desktop-host.service"); err != nil {
			return err
		}
		if err := steps.rollbackNew(); err != nil {
			return err
		}
		return steps.command("daemon-reload")
	}
	commit := func() error {
		if !outcome.upgraded {
			return nil
		}
		// Runs for the first upgrade too, when the previous generation has no
		// sidecars (zero-superseded); retention must not depend on that.
		return commitUpgrade(previous, steps.io)
	}
	return install, activate, rollback, commit, outcome.changed
}

// The transaction is injectable for fault tests. An unchanged existing install
// is never stopped or rolled back by a failed verification.
func runCurrentInstall(existing bool, ops currentInstallOps) (result probeResult, err error) {
	if err = ops.install(); err != nil {
		return result, err
	}
	rollbackOnFailure := rollbackRequired(existing, ops.changed != nil && ops.changed())
	defer func() {
		if err == nil || !rollbackOnFailure || errors.Is(err, errDesktopProbeBusy) {
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
	result, err = ops.probe()
	if err != nil {
		return result, err
	}
	// The transaction is healthy: only now discard the superseded backups.
	// Cleanup failure leaves extra files but never rolls back a healthy broker.
	if ops.commit != nil {
		_ = ops.commit()
	}
	return result, nil
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
	installOp, activateOp, rollbackOp, commitOp, changedOp := currentInstallSharedOps(config, source, existing)
	result, err := runCurrentInstall(existing, currentInstallOps{
		install:  installOp,
		activate: activateOp,
		changed:  changedOp,
		commit:   commitOp,
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
