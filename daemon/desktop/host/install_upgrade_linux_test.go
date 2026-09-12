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
	files     map[string][]byte
	failWrite map[string]bool
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
			if f.failWrite[path] {
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
	if len(sidecars) != 1 || sidecars[0] != InstalledBinary+".zen-previous" {
		t.Fatalf("sidecars: %v", sidecars)
	}
	if len(journal.Files) != 3 {
		t.Fatalf("journal files: %d", len(journal.Files))
	}
	if !bytes.Equal(journal.Files[0].Before, []byte("old-conf")) || journal.Files[0].BeforePath != "" {
		t.Fatalf("small file should back up inline: %+v", journal.Files[0])
	}
	if journal.Files[1].BeforePath != InstalledBinary+".zen-previous" || journal.Files[1].BeforeSHA256 != digest(large) {
		t.Fatalf("large file should back up to a sidecar: %+v", journal.Files[1])
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
	if _, ok := fs.files[InstalledBinary+".zen-previous"]; ok {
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
	fs.failWrite[InstalledBinary+".zen-previous"] = true
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
