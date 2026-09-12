package host

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// The orchestrator tests below drive the real shared transaction
// (currentInstallSharedOpsWith + runCurrentInstall) with an in-memory IO and
// fake services, so install writes, activation/probe failures, rollback
// metadata and consecutive upgrades are exercised end to end.

func orchestratorFakeIO() (*fakeUpgradeFS, upgradeIO) {
	fs := newFakeUpgradeFS()
	return fs, fs.io()
}

func enabledActiveOutput(args ...string) (string, error) {
	switch args[0] {
	case "is-enabled":
		return "enabled", nil
	case "is-active":
		return "active", nil
	}
	return "", nil
}

func TestSharedOrchestratorUpgradeActivationFailureRestoresEverything(t *testing.T) {
	fs, io := orchestratorFakeIO()
	previous := []byte(`{"version":1,"files":[{"path":"/x","sha256":"old","mode":420,"existed":true,"beforePath":"/x.zen-previous.old"}]}`)
	backup := previousJournalBackup(previous)
	fs.files[backup] = previous
	fs.files["/x.zen-previous.old"] = []byte("v1")
	// After the upgrade step: /x is v3 and the new journal backs it up.
	upgradedJournal := installJournal{Version: 1, Previous: backup, Files: []installedFile{{
		Path: "/x", SHA256: digest([]byte("v3")), Mode: 0644, Exists: true,
		BeforePath: "/x.zen-previous.new", BeforeSHA256: digest([]byte("v2")),
	}}}
	fs.files["/x.zen-previous.new"] = []byte("v2")
	fs.files["/x"] = []byte("v3")

	var commands []string
	steps := installSteps{
		matches:      func(HostConfig, string) (bool, error) { return false, nil },
		upgrade:      func(HostConfig, string) (installJournal, error) { return installJournal{Version: 1}, nil },
		readJournal:  func() ([]byte, error) { return previous, nil },
		loadJournal:  func() (installJournal, error) { return upgradedJournal, nil },
		restore:      func(current installJournal) error { return restorePreviousInstall(current, io) },
		rollbackNew:  func() error { return errors.New("fresh rollback must not run") },
		freshInstall: func(HostConfig, string) error { return errors.New("fresh install must not run") },
		verify:       func() error { return nil },
		output:       enabledActiveOutput,
		io:           io,
		command: func(args ...string) error {
			commands = append(commands, strings.Join(args, " "))
			if args[0] == "restart" {
				return errors.New("restart refused")
			}
			return nil
		},
	}
	installOp, activateOp, rollbackOp, commitOp, changedOp := currentInstallSharedOpsWith(HostConfig{}, "src", true, steps)
	_, err := runCurrentInstall(true, currentInstallOps{install: installOp, activate: activateOp, rollback: rollbackOp, commit: commitOp, changed: changedOp})
	if err == nil {
		t.Fatal("activation failure must fail the transaction")
	}
	joined := strings.Join(commands, ",")
	for _, want := range []string{"stop zen-desktop-host.service", "daemon-reload", "enable zen-desktop-host.service", "start zen-desktop-host.service"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("prior service policy not restored, want %q in %q", want, joined)
		}
	}
	if strings.Contains(joined, "disable") {
		t.Fatal("upgrade rollback must not disable the previously enabled unit")
	}
	if string(fs.files["/x"]) != "v2" {
		t.Fatalf("previous files not restored: %q", fs.files["/x"])
	}
	if !bytes.Equal(fs.files[installJournalPath], previous) {
		t.Fatal("previous journal metadata not restored")
	}
	if _, ok := fs.files[backup]; ok {
		t.Fatal("previous journal backup not consumed")
	}
	if _, ok := fs.files["/x.zen-previous.new"]; ok {
		t.Fatal("current backup not consumed after restore")
	}
	if !bytes.Equal(fs.files["/x.zen-previous.old"], []byte("v1")) {
		t.Fatal("previous installation's own backup was lost")
	}
}

func TestSharedOrchestratorFreshInstallProbeFailureRollsBack(t *testing.T) {
	fs, io := orchestratorFakeIO()
	freshCalled, rolledBack := false, false
	var commands []string
	steps := installSteps{
		freshInstall: func(HostConfig, string) error {
			freshCalled = true
			fs.files[installJournalPath] = []byte("fresh-journal")
			return nil
		},
		rollbackNew: func() error { rolledBack = true; return nil },
		command: func(args ...string) error {
			commands = append(commands, strings.Join(args, " "))
			return nil
		},
		output: enabledActiveOutput,
		verify: func() error { return nil },
		io:     io,
	}
	installOp, activateOp, rollbackOp, commitOp, changedOp := currentInstallSharedOpsWith(HostConfig{}, "src", false, steps)
	_, err := runCurrentInstall(false, currentInstallOps{
		install:  installOp,
		activate: activateOp,
		register: func() error { return nil },
		probe:    func() (probeResult, error) { return probeResult{}, errors.New("probe failed") },
		rollback: rollbackOp,
		commit:   commitOp,
		changed:  changedOp,
	})
	if err == nil {
		t.Fatal("probe failure must fail a fresh install")
	}
	if !freshCalled || !rolledBack {
		t.Fatalf("fresh install rollback did not run: fresh=%v rolled=%v", freshCalled, rolledBack)
	}
	joined := strings.Join(commands, ",")
	if !strings.Contains(joined, "disable --no-reload zen-desktop-host.service") {
		t.Fatalf("fresh rollback did not uninstall: %q", joined)
	}
}
