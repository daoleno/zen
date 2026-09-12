package host

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeFileInfo struct {
	name string
	size int64
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() os.FileMode  { return 0 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() interface{}   { return nil }

type fakeUpgradeFS struct {
	files           map[string][]byte
	failWrite       map[string]bool
	failWritePrefix string
}

func newFakeUpgradeFS() *fakeUpgradeFS {
	return &fakeUpgradeFS{files: map[string][]byte{}, failWrite: map[string]bool{}}
}

func (f *fakeUpgradeFS) io() upgradeIO {
	return upgradeIO{
		lstat: func(path string) (os.FileInfo, error) {
			data, ok := f.files[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return fakeFileInfo{name: path, size: int64(len(data))}, nil
		},
		read: func(path string, mode uint32) ([]byte, error) {
			data, ok := f.files[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return append([]byte(nil), data...), nil
		},
		write: func(path string, data []byte, mode uint32) error {
			if f.failWrite[path] || (f.failWritePrefix != "" && strings.HasPrefix(path, f.failWritePrefix)) {
				return errors.New("write refused")
			}
			f.files[path] = append([]byte(nil), data...)
			return nil
		},
		remove: func(path string) error {
			delete(f.files, path)
			return nil
		},
	}
}

func TestUpgradeBacksUpPreviousFilesAndRestoresThem(t *testing.T) {
	fs := newFakeUpgradeFS()
	large := bytes.Repeat([]byte("old-elf-"), 9000)
	fs.files["/etc/sddm.conf"] = []byte("old-conf")
	fs.files[InstalledBinary] = large
	plan := []PlannedFile{
		{Path: "/etc/sddm.conf", Mode: 0644, Content: "new-conf"},
		{Path: InstalledBinary, Mode: 0755, Content: "new-elf"},
		{Path: "/usr/libexec/zen/sddm-start", Mode: 0755, Content: "new-start"},
	}
	io := fs.io()
	journal, sidecars, err := upgradeTransaction(plan, io)
	if err != nil {
		t.Fatal(err)
	}
	if len(sidecars) != 1 || !strings.HasPrefix(sidecars[0], InstalledBinary+".zen-previous.") {
		t.Fatalf("sidecars: %v", sidecars)
	}
	if len(journal.Files) != 3 {
		t.Fatalf("journal files: %d", len(journal.Files))
	}
	if !bytes.Equal(journal.Files[0].Before, []byte("old-conf")) || journal.Files[0].BeforePath != "" {
		t.Fatalf("small file should back up inline: %+v", journal.Files[0])
	}
	if !strings.HasPrefix(journal.Files[1].BeforePath, InstalledBinary+".zen-previous.") || journal.Files[1].BeforeSHA256 != digest(large) {
		t.Fatalf("large file should back up to a collision-safe sidecar: %+v", journal.Files[1])
	}
	if journal.Files[2].Exists {
		t.Fatal("new file must not be recorded as existing")
	}
	if err := applyPlan(plan, io); err != nil {
		t.Fatal(err)
	}
	if string(fs.files["/etc/sddm.conf"]) != "new-conf" || string(fs.files[InstalledBinary]) != "new-elf" {
		t.Fatal("plan not applied")
	}
	if err := restoreJournal(journal, io); err != nil {
		t.Fatal(err)
	}
	if string(fs.files["/etc/sddm.conf"]) != "old-conf" {
		t.Fatalf("config not restored: %q", fs.files["/etc/sddm.conf"])
	}
	if !bytes.Equal(fs.files[InstalledBinary], large) {
		t.Fatal("previous binary not restored from the sidecar")
	}
	if _, ok := fs.files[sidecars[0]]; ok {
		t.Fatal("sidecar not consumed after restore")
	}
	if _, ok := fs.files["/usr/libexec/zen/sddm-start"]; ok {
		t.Fatal("new file not removed by restore")
	}
}

func TestUpgradePartialWriteRollsBackPreviousState(t *testing.T) {
	fs := newFakeUpgradeFS()
	fs.files["/etc/sddm.conf"] = []byte("old-conf")
	fs.files[InstalledBinary] = bytes.Repeat([]byte("old-elf"), 4096)
	plan := []PlannedFile{
		{Path: "/etc/sddm.conf", Mode: 0644, Content: "new-conf"},
		{Path: InstalledBinary, Mode: 0755, Content: "new-elf"},
	}
	io := fs.io()
	journal, _, err := upgradeTransaction(plan, io)
	if err != nil {
		t.Fatal(err)
	}
	fs.failWrite[InstalledBinary] = true
	if err := applyPlan(plan, io); err == nil {
		t.Fatal("expected the partial write to fail")
	}
	delete(fs.failWrite, InstalledBinary)
	if err := restoreJournal(journal, io); err != nil {
		t.Fatal(err)
	}
	if string(fs.files["/etc/sddm.conf"]) != "old-conf" || string(fs.files[InstalledBinary]) != strings.Repeat("old-elf", 4096) {
		t.Fatal("failed upgrade did not restore the previous state")
	}
}

func TestUpgradeRefusesAdministratorDrift(t *testing.T) {
	fs := newFakeUpgradeFS()
	fs.files["/etc/sddm.conf"] = []byte("old-conf")
	plan := []PlannedFile{{Path: "/etc/sddm.conf", Mode: 0644, Content: "new-conf"}}
	io := fs.io()
	journal, _, err := upgradeTransaction(plan, io)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPlan(plan, io); err != nil {
		t.Fatal(err)
	}
	fs.files["/etc/sddm.conf"] = []byte("admin-edit")
	if err := restoreJournal(journal, io); err == nil || !strings.Contains(err.Error(), "installed_file_changed") {
		t.Fatalf("drift not refused: %v", err)
	}
	if string(fs.files["/etc/sddm.conf"]) != "admin-edit" {
		t.Fatal("administrator change was overwritten")
	}
}

func TestUpgradeBackupFailureLeavesInstallationUntouched(t *testing.T) {
	fs := newFakeUpgradeFS()
	old := bytes.Repeat([]byte("old-elf-"), 9000)
	fs.files[InstalledBinary] = old
	fs.failWritePrefix = InstalledBinary + ".zen-previous"
	plan := []PlannedFile{{Path: InstalledBinary, Mode: 0755, Content: "new-elf"}}
	if _, sidecars, err := upgradeTransaction(plan, fs.io()); err == nil || !strings.Contains(err.Error(), "install_backup_failed") || len(sidecars) != 0 {
		t.Fatalf("backup failure not surfaced: %v %v", err, sidecars)
	}
	if !bytes.Equal(fs.files[InstalledBinary], old) {
		t.Fatal("installation changed before the journal was written")
	}
}

func TestUpgradeFailureRollsBackUnlikeUnchangedExisting(t *testing.T) {
	var rolled bool
	_, err := runCurrentInstall(true, currentInstallOps{
		install:  func() error { return nil },
		activate: func() error { return errors.New("activation") },
		changed:  func() bool { return true },
		rollback: func() error { rolled = true; return nil },
	})
	if err == nil || !rolled {
		t.Fatalf("upgrade failure must roll back: err=%v rolled=%v", err, rolled)
	}
	rolled = false
	_, err = runCurrentInstall(true, currentInstallOps{
		install:  func() error { return nil },
		activate: func() error { return errors.New("activation") },
		changed:  func() bool { return false },
		rollback: func() error { rolled = true; return nil },
	})
	if err == nil || rolled {
		t.Fatalf("unchanged existing install must not roll back: err=%v rolled=%v", err, rolled)
	}
}

func TestRollbackRequiredPolicy(t *testing.T) {
	cases := []struct {
		existing, changed, want bool
	}{
		{false, false, true}, // a fresh install must roll back on failure
		{false, true, true},
		{true, true, true},   // an upgrade must roll back
		{true, false, false}, // an unchanged existing install is never touched
	}
	for _, c := range cases {
		if got := rollbackRequired(c.existing, c.changed); got != c.want {
			t.Fatalf("rollbackRequired(%v,%v)=%v want %v", c.existing, c.changed, got, c.want)
		}
	}
}

func TestInstallOutcomeIsTheSharedChangedSource(t *testing.T) {
	fresh := &installOutcome{}
	fresh.markInstalled()
	upgrade := &installOutcome{}
	upgrade.markUpgraded()
	unchanged := &installOutcome{}
	if !fresh.changed() || !upgrade.changed() || unchanged.changed() {
		t.Fatalf("outcome changed flags: %v %v %v", fresh.changed(), upgrade.changed(), unchanged.changed())
	}
	// The shared wiring must satisfy the rollback policy for all three cases.
	if !rollbackRequired(false, fresh.changed()) ||
		!rollbackRequired(true, upgrade.changed()) ||
		rollbackRequired(true, unchanged.changed()) {
		t.Fatal("shared changed wiring does not satisfy the rollback policy")
	}
}

func TestRestoreJournalValidatesBackupsBeforeAnyWrite(t *testing.T) {
	fs := newFakeUpgradeFS()
	large := bytes.Repeat([]byte("old-a-"), 12000)
	fs.files["/a"] = large
	fs.files["/b"] = bytes.Repeat([]byte("old-b-"), 12000)
	plan := []PlannedFile{
		{Path: "/a", Mode: 0644, Content: "new-a"},
		{Path: "/b", Mode: 0644, Content: "new-b"},
	}
	io := fs.io()
	journal, sidecars, err := upgradeTransaction(plan, io)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPlan(plan, io); err != nil {
		t.Fatal(err)
	}
	delete(fs.files, sidecars[1])
	if err := restoreJournal(journal, io); err == nil || !strings.Contains(err.Error(), "install_backup_missing") {
		t.Fatalf("missing backup not refused before writes: %v", err)
	}
	if string(fs.files["/a"]) != "new-a" || string(fs.files["/b"]) != "new-b" {
		t.Fatal("restore wrote before validating every backup")
	}
	if _, ok := fs.files[sidecars[0]]; !ok {
		t.Fatal("valid sidecar was consumed by the refused restore")
	}
}

func TestRestoreJournalMidFailureStaysRetryable(t *testing.T) {
	fs := newFakeUpgradeFS()
	old := bytes.Repeat([]byte("old-x-"), 12000)
	fs.files["/x"] = old
	plan := []PlannedFile{{Path: "/x", Mode: 0644, Content: "new-x"}}
	io := fs.io()
	journal, sidecars, err := upgradeTransaction(plan, io)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPlan(plan, io); err != nil {
		t.Fatal(err)
	}
	fs.failWrite["/x"] = true
	if err := restoreJournal(journal, io); err == nil {
		t.Fatal("mid-restore write failure not surfaced")
	}
	if _, ok := fs.files[sidecars[0]]; !ok {
		t.Fatal("sidecar was deleted by a failed restore")
	}
	delete(fs.failWrite, "/x")
	if err := restoreJournal(journal, io); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fs.files["/x"], old) {
		t.Fatal("retry did not restore the previous content")
	}
	if _, ok := fs.files[sidecars[0]]; ok {
		t.Fatal("sidecar not consumed after a complete restore")
	}
}

func TestSecondUpgradeJournalFailureKeepsOldBackup(t *testing.T) {
	fs := newFakeUpgradeFS()
	original := bytes.Repeat([]byte("v1-original"), 12000)
	current := bytes.Repeat([]byte("v2-current"), 12000)
	fs.files[InstalledBinary] = current
	// A previous upgrade left this backup and its journal referencing it.
	fs.files[InstalledBinary+".zen-previous"] = original
	plan := []PlannedFile{{Path: InstalledBinary, Mode: 0755, Content: "v3-new"}}
	io := fs.io()
	_, sidecars, err := upgradeTransaction(plan, io)
	if err != nil {
		t.Fatal(err)
	}
	if len(sidecars) != 1 || sidecars[0] == InstalledBinary+".zen-previous" {
		t.Fatalf("second upgrade reused the fixed sidecar: %v", sidecars)
	}
	// The new journal write "fails": the caller removes only its own new
	// sidecars, so the previous journal and its backup stay recoverable.
	for _, sidecar := range sidecars {
		_ = io.remove(sidecar)
	}
	if !bytes.Equal(fs.files[InstalledBinary+".zen-previous"], original) {
		t.Fatal("previous rollback backup was overwritten")
	}
	if !bytes.Equal(fs.files[InstalledBinary], current) {
		t.Fatal("installation changed before the journal was durable")
	}
}

func TestRestorePreviousInstallWritesPreviousJournal(t *testing.T) {
	fs := newFakeUpgradeFS()
	old := bytes.Repeat([]byte("old-x-"), 12000)
	fs.files["/x"] = old
	plan := []PlannedFile{{Path: "/x", Mode: 0644, Content: "new-x"}}
	io := fs.io()
	journal, _, err := upgradeTransaction(plan, io)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPlan(plan, io); err != nil {
		t.Fatal(err)
	}
	previous := []byte(`{"version":1,"files":[]}`)
	backup := previousJournalBackup(previous)
	journal.Previous = backup
	fs.files[backup] = previous
	if err := restorePreviousInstall(journal, io); err != nil {
		t.Fatal(err)
	}
	if _, ok := fs.files[backup]; ok {
		t.Fatal("previous journal backup not consumed after restore")
	}
	if !bytes.Equal(fs.files["/x"], old) {
		t.Fatal("previous file not restored")
	}
	if !bytes.Equal(fs.files[installJournalPath], previous) {
		t.Fatal("previous journal metadata not restored")
	}
}
