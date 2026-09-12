package host

import (
	"bytes"
	"encoding/json"
	"testing"
)

// Behavioral regressions for the one-step rollback contract. They use the real
// orchestrator and journal/IO helpers against an in-memory filesystem with
// injected service operations; no root, systemd or host state is touched.

func baseJournal(previous string) []byte {
	data, _ := json.Marshal(installJournal{Version: 1, Previous: previous})
	return data
}

func largePayload(prefix string) []byte {
	for len(prefix) < 6 {
		prefix += "x"
	}
	return bytes.Repeat([]byte(prefix), 12000)
}

func TestSuccessfulUpgradeThenExplicitRollbackRetainsManagement(t *testing.T) {
	fs := newFakeUpgradeFS()
	io := fs.io()
	v1 := largePayload("v1-")
	fs.files[InstalledBinary] = v1
	j0 := baseJournal("")
	fs.files[installJournalPath] = j0

	previous, err := upgradeWithIO(j0, []PlannedFile{{Path: InstalledBinary, Mode: 0755, Content: "v2"}}, io)
	if err != nil {
		t.Fatal(err)
	}
	if previous.Version != 1 {
		t.Fatalf("previous journal: %+v", previous)
	}
	current, err := loadJournalWithIO(io)
	if err != nil {
		t.Fatal(err)
	}
	if current.Previous == "" {
		t.Fatal("current journal must retain the previous journal reference")
	}
	if !bytes.Equal(fs.files[current.Previous], j0) {
		t.Fatal("previous journal metadata not durable")
	}
	if !bytes.Equal(fs.files[current.Files[0].BeforePath], v1) {
		t.Fatal("previous binary snapshot missing")
	}
	// Commit keeps the retained generation; the first upgrade has no older history.
	if err := commitUpgrade(previous, io); err != nil {
		t.Fatal(err)
	}
	if _, ok := fs.files[current.Previous]; !ok {
		t.Fatal("commit removed the retained previous journal")
	}
	if _, ok := fs.files[current.Files[0].BeforePath]; !ok {
		t.Fatal("commit removed the retained snapshot")
	}
	// Explicit rollback restores files and the previous management journal.
	if err := rollbackWithIO(io); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fs.files[InstalledBinary], v1) {
		t.Fatal("explicit rollback did not restore the previous binary")
	}
	if !bytes.Equal(fs.files[installJournalPath], j0) {
		t.Fatal("explicit rollback did not restore the previous journal")
	}
	if _, ok := fs.files[current.Previous]; ok {
		t.Fatal("explicit rollback left the consumed backup reference")
	}
	if _, ok := fs.files[current.Files[0].BeforePath]; ok {
		t.Fatal("explicit rollback left the consumed snapshot")
	}
}

func TestReinstallAfterExplicitRollbackAndSecondUpgrade(t *testing.T) {
	fs := newFakeUpgradeFS()
	io := fs.io()
	v1 := largePayload("v1-")
	fs.files[InstalledBinary] = v1
	j0 := baseJournal("")
	fs.files[installJournalPath] = j0
	if _, err := upgradeWithIO(j0, []PlannedFile{{Path: InstalledBinary, Mode: 0755, Content: "v2"}}, io); err != nil {
		t.Fatal(err)
	}
	if err := rollbackWithIO(io); err != nil {
		t.Fatal(err)
	}
	restored, err := io.read(installJournalPath, 0600)
	if err != nil {
		t.Fatal(err)
	}
	// Reinstall/second upgrade after the rollback still works and retains state.
	if _, err := upgradeWithIO(restored, []PlannedFile{{Path: InstalledBinary, Mode: 0755, Content: "v2b"}}, io); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	current, _ := loadJournalWithIO(io)
	if current.Previous == "" || !bytes.Equal(fs.files[current.Previous], restored) {
		t.Fatal("second upgrade did not retain its previous journal")
	}
	// A third upgrade prunes only the older snapshot, never the retained one.
	previous3, err := upgradeWithIO(mustJournal(t, io), []PlannedFile{{Path: InstalledBinary, Mode: 0755, Content: "v3"}}, io)
	if err != nil {
		t.Fatal(err)
	}
	current3, _ := loadJournalWithIO(io)
	retained := current3.Previous
	older := current.Previous
	if err := commitUpgrade(previous3, io); err != nil {
		t.Fatal(err)
	}
	if _, ok := fs.files[retained]; !ok {
		t.Fatal("commit pruned the retained generation")
	}
	if older == "" || older == retained {
		t.Fatalf("expected a distinct older generation: %q", older)
	}
	if _, ok := fs.files[older]; ok {
		t.Fatal("commit did not prune the older generation")
	}
}

func mustJournal(t *testing.T, io upgradeIO) []byte {
	t.Helper()
	data, err := io.read(installJournalPath, 0600)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestExplicitRollbackMetadataFailureIsRetryable(t *testing.T) {
	fs := newFakeUpgradeFS()
	io := fs.io()
	v1 := largePayload("v1-")
	fs.files[InstalledBinary] = v1
	fs.files[installJournalPath] = baseJournal("")
	if _, err := upgradeWithIO(fs.files[installJournalPath], []PlannedFile{{Path: InstalledBinary, Mode: 0755, Content: "v2"}}, io); err != nil {
		t.Fatal(err)
	}
	current, _ := loadJournalWithIO(io)
	if err := commitUpgrade(installJournal{Version: 1}, io); err != nil {
		t.Fatal(err)
	}
	fs.failWrite[installJournalPath] = true
	if err := rollbackWithIO(io); err == nil {
		t.Fatal("metadata failure not surfaced")
	}
	if _, ok := fs.files[current.Previous]; !ok {
		t.Fatal("backup consumed before metadata was durable")
	}
	if _, ok := fs.files[current.Files[0].BeforePath]; !ok {
		t.Fatal("snapshot consumed before metadata was durable")
	}
	delete(fs.failWrite, installJournalPath)
	if err := rollbackWithIO(io); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fs.files[InstalledBinary], v1) {
		t.Fatal("retry did not restore the previous binary")
	}
}

func TestFirstUpgradeWithoutSidecarsRetainsPreviousJournal(t *testing.T) {
	fs := newFakeUpgradeFS()
	io := fs.io()
	// Small file: the previous state is inline in the journal, so the upgrade
	// has zero superseded sidecars.
	fs.files["/etc/sddm.conf"] = []byte("old-conf")
	j0, _ := json.Marshal(installJournal{Version: 1, Files: []installedFile{{
		Path: "/etc/sddm.conf", SHA256: digest([]byte("old-conf")), Mode: 0644, Exists: true, Before: []byte("older-conf"),
	}}})
	fs.files[installJournalPath] = j0
	previous, err := upgradeWithIO(j0, []PlannedFile{{Path: "/etc/sddm.conf", Mode: 0644, Content: "new-conf"}}, io)
	if err != nil {
		t.Fatal(err)
	}
	if len(previous.Files) != 1 || previous.Files[0].BeforePath != "" {
		t.Fatalf("expected inline previous state: %+v", previous)
	}
	if err := commitUpgrade(previous, io); err != nil {
		t.Fatal(err)
	}
	current, _ := loadJournalWithIO(io)
	if current.Previous == "" {
		t.Fatal("first upgrade did not retain the previous journal reference")
	}
	if err := rollbackWithIO(io); err != nil {
		t.Fatal(err)
	}
	if string(fs.files["/etc/sddm.conf"]) != "old-conf" {
		t.Fatal("rollback did not restore the small file")
	}
	if !bytes.Equal(fs.files[installJournalPath], j0) {
		t.Fatal("rollback did not restore the previous journal")
	}
}
