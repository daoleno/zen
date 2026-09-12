package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// inlineBackupLimit keeps small previous files directly in the journal; larger
// files (the ELF) are copied to a sidecar before the upgrade overwrites them.
const inlineBackupLimit = 65536

// upgradeIO isolates the in-place upgrade and rollback file operations so fault
// tests can inject write/backup failures without root or a real /etc tree.
type upgradeIO struct {
	lstat  func(string) (os.FileInfo, error)
	read   func(string, uint32) ([]byte, error)
	write  func(string, []byte, uint32) error
	remove func(string) error
}

func rootUpgradeIO() upgradeIO {
	return upgradeIO{
		lstat: os.Lstat,
		read: func(path string, mode uint32) ([]byte, error) {
			file, err := OpenRootFile(path, mode)
			if err != nil {
				return nil, err
			}
			defer file.Close()
			return io.ReadAll(io.LimitReader(file, (64<<20)+1))
		},
		write: writeRootFile,
		remove: func(path string) error {
			parent, name, err := safeParent(path, false)
			if err != nil {
				return err
			}
			err = unix.Unlinkat(parent, name, 0)
			unix.Close(parent)
			if err == unix.ENOENT {
				return nil
			}
			return err
		},
	}
}

// loadInstallJournal reads and validates the recorded installation state.
func loadInstallJournal() (installJournal, error) {
	file, err := OpenRootFile(installJournalPath, 0600)
	if err != nil {
		return installJournal{}, err
	}
	data, err := io.ReadAll(io.LimitReader(file, 1<<20))
	file.Close()
	var journal installJournal
	if err != nil || decodeMessage(data, &journal) != nil || journal.Version != 1 {
		return installJournal{}, errors.New("invalid_install_journal")
	}
	return journal, nil
}

// existingInstallMatches reports whether the installed state is the requested
// one. A matching config and untouched tracked files are required; the boolean
// is false when only the binary differs, which is the one upgradeable change.
// Config identity changes and administrator drift are refused here so an
// upgrade can never overwrite them.
func existingInstallMatches(config HostConfig, source string) (bool, error) {
	installed, err := LoadRootConfig("/etc/zen/desktop-host.json")
	if err != nil || installed != config {
		return false, errors.New("installed_host_config_differs; existing installation preserved")
	}
	journal, err := loadInstallJournal()
	if err != nil {
		return false, err
	}
	binaryData, err := readInstallSource(source)
	if err != nil {
		return false, err
	}
	foundBinary, binaryMatches := false, false
	for _, entry := range journal.Files {
		file, err := OpenRootFile(entry.Path, entry.Mode)
		if err != nil {
			return false, errors.New("installed_file_changed; existing installation preserved")
		}
		current, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
		file.Close()
		if err != nil || digest(current) != entry.SHA256 {
			return false, errors.New("installed_file_changed; existing installation preserved")
		}
		if entry.Path == InstalledBinary {
			foundBinary = true
			binaryMatches = digest(binaryData) == entry.SHA256
		}
	}
	if !foundBinary {
		return false, errors.New("invalid_install_journal")
	}
	return binaryMatches, nil
}

// applyPlan writes every planned file in order. A failure leaves the journal in
// place and the caller restores the previous state from it.
func applyPlan(plan []PlannedFile, io upgradeIO) error {
	for _, file := range plan {
		if err := io.write(file.Path, []byte(file.Content), file.Mode); err != nil {
			return errors.New("install_incomplete_use_rollback")
		}
	}
	return nil
}

// upgradeTransaction records how to restore the current installation and copies
// every previous file that the plan will overwrite. Small files stay inline in
// the journal; larger files go to a collision-safe sidecar path (a fixed one
// could overwrite the still-referenced backup of an earlier upgrade before the
// new journal is on disk). Created sidecars are returned
// so the caller can remove them if the transaction never starts.
func upgradeTransaction(plan []PlannedFile, io upgradeIO) (installJournal, []string, error) {
	var sidecars []string
	fail := func(reason string) (installJournal, []string, error) {
		for _, path := range sidecars {
			_ = io.remove(path)
		}
		return installJournal{}, nil, errors.New(reason)
	}
	journal := installJournal{Version: 1}
	for _, file := range plan {
		entry := installedFile{Path: file.Path, SHA256: digest([]byte(file.Content)), Mode: file.Mode}
		info, err := io.lstat(file.Path)
		switch {
		case err == nil && info.IsDir():
			return fail("install_destination_unavailable")
		case err == nil:
			current, readErr := io.read(file.Path, file.Mode)
			if readErr != nil || len(current) > (64<<20) {
				return fail("invalid_install_backup")
			}
			entry.Exists = true
			if len(current) <= inlineBackupLimit {
				entry.Before = current
			} else {
				sidecar := file.Path + ".zen-previous." + digest(current)[:12]
				if writeErr := io.write(sidecar, current, file.Mode); writeErr != nil {
					return fail("install_backup_failed")
				}
				sidecars = append(sidecars, sidecar)
				entry.BeforePath = sidecar
				entry.BeforeSHA256 = digest(current)
			}
		case !os.IsNotExist(err):
			return fail("install_destination_unavailable")
		}
		journal.Files = append(journal.Files, entry)
	}
	return journal, sidecars, nil
}

// restoreJournal reverses one installation or upgrade transaction. It refuses
// to overwrite administrator changes made after the transaction: every tracked
// file must still match the recorded new state or the recorded previous state.
// Every sidecar backup is validated before the first write and kept until the
// whole restore has succeeded, so a mid-restore failure stays retryable.
func restoreJournal(journal installJournal, io upgradeIO) error {
	backups := map[string][]byte{}
	for _, entry := range journal.Files {
		if entry.BeforePath != "" {
			backup, err := io.read(entry.BeforePath, entry.Mode)
			if err != nil || digest(backup) != entry.BeforeSHA256 {
				return errors.New("install_backup_missing")
			}
			backups[entry.BeforePath] = backup
		}
		if _, err := io.lstat(entry.Path); os.IsNotExist(err) {
			continue
		}
		current, err := io.read(entry.Path, entry.Mode)
		if err != nil {
			return errors.New("installed_file_changed")
		}
		matchesNew := digest(current) == entry.SHA256
		matchesOld := false
		if entry.Exists {
			if entry.BeforePath != "" {
				matchesOld = digest(current) == entry.BeforeSHA256
			} else {
				matchesOld = bytes.Equal(current, entry.Before)
			}
		}
		if !matchesNew && !matchesOld {
			return errors.New("installed_file_changed")
		}
	}
	for i := len(journal.Files) - 1; i >= 0; i-- {
		entry := journal.Files[i]
		switch {
		case entry.BeforePath != "":
			if err := io.write(entry.Path, backups[entry.BeforePath], entry.Mode); err != nil {
				return err
			}
		case entry.Exists:
			if err := io.write(entry.Path, entry.Before, entry.Mode); err != nil {
				return err
			}
		default:
			if err := io.remove(entry.Path); err != nil {
				return err
			}
		}
	}
	// The installation is fully restored; backups are no longer needed.
	for _, entry := range journal.Files {
		if entry.BeforePath != "" {
			if err := io.remove(entry.BeforePath); err != nil {
				return err
			}
		}
	}
	return nil
}

// restorePreviousInstall reverses an upgrade and puts the previous transaction
// journal back, so the old installation stays managed and rollbackable.
func restorePreviousInstall(current installJournal, previous []byte, io upgradeIO) error {
	if err := restoreJournal(current, io); err != nil {
		return err
	}
	if len(previous) == 0 {
		return errors.New("previous_install_journal_missing")
	}
	return io.write(installJournalPath, previous, 0600)
}

// readInstallJournalBytes returns the raw previous journal so an upgrade
// rollback can restore the old installation metadata instead of deleting it.
func readInstallJournalBytes() ([]byte, error) {
	file, err := OpenRootFile(installJournalPath, 0600)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, 1<<20))
}

// upgradeLinuxLocked replaces the binary of an existing, unchanged
// installation while preserving its exact config. The new journal is written
// before any file is replaced, sidecars are collision-safe, and on success the
// superseded journal's sidecars are removed. The caller has already verified
// config identity and file drift.
func upgradeLinuxLocked(config HostConfig, binarySource string) error {
	previous, err := loadInstallJournal()
	if err != nil {
		return err
	}
	plan, err := buildInstallPlan(config, binarySource)
	if err != nil {
		return err
	}
	io := rootUpgradeIO()
	journal, sidecars, err := upgradeTransaction(plan.Files, io)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(journal)
	if err := writeRootFile(installJournalPath, data, 0600); err != nil {
		for _, sidecar := range sidecars {
			_ = io.remove(sidecar)
		}
		return err
	}
	if err := applyPlan(plan.Files, io); err != nil {
		if rollbackErr := restoreJournal(journal, io); rollbackErr != nil {
			return fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
		}
		return err
	}
	for _, entry := range previous.Files {
		if entry.BeforePath != "" {
			_ = io.remove(entry.BeforePath)
		}
	}
	return nil
}
