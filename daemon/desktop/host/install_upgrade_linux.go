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

// previousJournalPath holds a durable copy of the previous transaction journal
// across an upgrade, so a crash or a metadata-write failure can always restore
// the old managed installation instead of relying on process memory.
const previousJournalPath = installJournalPath + ".zen-previous"

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

// restoreJournal reverses one installation or upgrade transaction and then
// consumes its backups. See restoreJournalWithBackups for the shared core.
func restoreJournal(journal installJournal, io upgradeIO) error {
	backups, err := restoreJournalWithBackups(journal, io)
	if err != nil {
		return err
	}
	for _, backup := range backups {
		if err := io.remove(backup); err != nil {
			return err
		}
	}
	return nil
}

// restoreJournalWithBackups reverses one installation or upgrade transaction
// and returns the sidecar backups that must survive until the caller has made
// the previous transaction journal durable. Every backup is validated before
// the first write and kept on any failure, so a mid-restore failure stays
// retryable.
func restoreJournalWithBackups(journal installJournal, io upgradeIO) ([]string, error) {
	backups := map[string][]byte{}
	for _, entry := range journal.Files {
		if entry.BeforePath != "" {
			backup, err := io.read(entry.BeforePath, entry.Mode)
			if err != nil || digest(backup) != entry.BeforeSHA256 {
				return nil, errors.New("install_backup_missing")
			}
			backups[entry.BeforePath] = backup
		}
		if _, err := io.lstat(entry.Path); os.IsNotExist(err) {
			continue
		}
		current, err := io.read(entry.Path, entry.Mode)
		if err != nil {
			return nil, errors.New("installed_file_changed")
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
			return nil, errors.New("installed_file_changed")
		}
	}
	for i := len(journal.Files) - 1; i >= 0; i-- {
		entry := journal.Files[i]
		switch {
		case entry.BeforePath != "":
			if err := io.write(entry.Path, backups[entry.BeforePath], entry.Mode); err != nil {
				return nil, err
			}
		case entry.Exists:
			if err := io.write(entry.Path, entry.Before, entry.Mode); err != nil {
				return nil, err
			}
		default:
			if err := io.remove(entry.Path); err != nil {
				return nil, err
			}
		}
	}
	restored := make([]string, 0, len(backups))
	for _, entry := range journal.Files {
		if entry.BeforePath != "" {
			restored = append(restored, entry.BeforePath)
		}
	}
	return restored, nil
}

// restorePreviousInstall reverses an upgrade, restores the previous journal
// metadata from its durable backup, and only then consumes the backups. A
// metadata-write failure before that point stays retryable.
func restorePreviousInstall(current installJournal, io upgradeIO) error {
	previous, err := io.read(previousJournalPath, 0600)
	if err != nil {
		return errors.New("previous_install_journal_missing")
	}
	var parsed installJournal
	if decodeMessage(previous, &parsed) != nil || parsed.Version != 1 {
		return errors.New("invalid_previous_install_journal")
	}
	restored, err := restoreJournalWithBackups(current, io)
	if err != nil {
		return err
	}
	if err := io.write(installJournalPath, previous, 0600); err != nil {
		return err
	}
	for _, backup := range restored {
		if err := io.remove(backup); err != nil {
			return err
		}
	}
	return io.remove(previousJournalPath)
}

// upgradeWithIO runs the whole upgrade file transaction: the previous journal
// is made durable first, the new journal is written before any file changes,
// and an apply failure restores both the previous files and the previous
// journal before returning. The returned paths are the superseded journal's
// sidecars; they are removed by commitUpgrade only after activation and the
// health probe have succeeded.
func upgradeWithIO(previousBytes []byte, plan []PlannedFile, io upgradeIO) ([]string, error) {
	var previous installJournal
	if decodeMessage(previousBytes, &previous) != nil || previous.Version != 1 {
		return nil, errors.New("invalid_install_journal")
	}
	if err := io.write(previousJournalPath, previousBytes, 0600); err != nil {
		return nil, err
	}
	journal, sidecars, err := upgradeTransaction(plan, io)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(journal)
	if err := io.write(installJournalPath, data, 0600); err != nil {
		for _, sidecar := range sidecars {
			_ = io.remove(sidecar)
		}
		return nil, err
	}
	if err := applyPlan(plan, io); err != nil {
		if rollbackErr := restorePreviousInstall(journal, io); rollbackErr != nil {
			return nil, fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
		}
		return nil, err
	}
	var superseded []string
	for _, entry := range previous.Files {
		if entry.BeforePath != "" {
			superseded = append(superseded, entry.BeforePath)
		}
	}
	return superseded, nil
}

// commitUpgrade removes the resources of the superseded installation and the
// temporary previous journal only after the whole transaction is verified.
func commitUpgrade(superseded []string, io upgradeIO) error {
	for _, path := range superseded {
		if err := io.remove(path); err != nil {
			return err
		}
	}
	return io.remove(previousJournalPath)
}

// readInstallJournalBytes returns the raw previous journal so an upgrade can
// keep it durable across the whole transaction and restore its metadata.
func readInstallJournalBytes() ([]byte, error) {
	file, err := OpenRootFile(installJournalPath, 0600)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, 1<<20))
}

// upgradeLinuxLocked replaces the binary of an existing, unchanged
// installation while preserving its exact config. Nothing of the superseded
// installation (journal metadata or sidecar backups) is removed until the
// caller commits after activation and the health probe.
func upgradeLinuxLocked(config HostConfig, binarySource string) ([]string, error) {
	previousBytes, err := readInstallJournalBytes()
	if err != nil {
		return nil, err
	}
	plan, err := buildInstallPlan(config, binarySource)
	if err != nil {
		return nil, err
	}
	return upgradeWithIO(previousBytes, plan.Files, rootUpgradeIO())
}
