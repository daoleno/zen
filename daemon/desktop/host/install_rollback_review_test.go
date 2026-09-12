package host

import (
	"bytes"
	"encoding/json"
	"testing"
)

func review1061Initial(t *testing.T, fs *fakeUpgradeFS, value []byte) {
	t.Helper()
	fs.files[InstalledBinary] = value
	data, err := json.Marshal(installJournal{Version: 1, Files: []installedFile{{
		Path: InstalledBinary, SHA256: digest(value), Mode: 0755,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	fs.files[installJournalPath] = data
}

func TestBrainReview1061AlternatingVersionsPreserveRollback(t *testing.T) {
	fs := newFakeUpgradeFS()
	io := fs.io()
	a := largePayload("version-a")
	review1061Initial(t, fs, a)
	for _, value := range [][]byte{largePayload("version-b"), a, largePayload("version-c")} {
		previous, err := upgradeWithIO(mustJournal(t, io), []PlannedFile{{
			Path: InstalledBinary, Mode: 0755, Content: string(value),
		}}, io)
		if err != nil {
			t.Fatal(err)
		}
		if err := commitUpgrade(previous, io); err != nil {
			t.Fatal(err)
		}
	}
	if err := rollbackWithIO(io); err != nil {
		t.Fatalf("successful A -> B -> A -> C upgrades lost rollback to A: %v", err)
	}
	if !bytes.Equal(fs.files[InstalledBinary], a) {
		t.Fatal("rollback did not restore A")
	}
}

func TestBrainReview1061MissingMetadataRefusesBeforeWrites(t *testing.T) {
	fs := newFakeUpgradeFS()
	io := fs.io()
	review1061Initial(t, fs, largePayload("version-a"))
	b := largePayload("version-b")
	if _, err := upgradeWithIO(mustJournal(t, io), []PlannedFile{{
		Path: InstalledBinary, Mode: 0755, Content: string(b),
	}}, io); err != nil {
		t.Fatal(err)
	}
	current, err := loadJournalWithIO(io)
	if err != nil {
		t.Fatal(err)
	}
	delete(fs.files, current.Previous)
	if err := rollbackWithIO(io); err == nil {
		t.Fatal("missing previous journal was accepted")
	}
	if !bytes.Equal(fs.files[InstalledBinary], b) {
		t.Fatal("rollback changed the installed binary before rejecting missing previous journal")
	}
}

func review1061Plan(value []byte) []PlannedFile {
	return []PlannedFile{{Path: InstalledBinary, Mode: 0755, Content: string(value)}}
}

func TestBrainReview1061MalformedMetadataRefusesBeforeWrites(t *testing.T) {
	fs := newFakeUpgradeFS()
	io := fs.io()
	review1061Initial(t, fs, largePayload("version-a"))
	b := largePayload("version-b")
	if _, err := upgradeWithIO(mustJournal(t, io), review1061Plan(b), io); err != nil {
		t.Fatal(err)
	}
	current, err := loadJournalWithIO(io)
	if err != nil {
		t.Fatal(err)
	}
	fs.files[current.Previous] = []byte("{not-json")
	if err := rollbackWithIO(io); err == nil {
		t.Fatal("malformed previous journal was accepted")
	}
	if !bytes.Equal(fs.files[InstalledBinary], b) {
		t.Fatal("rollback changed the installed binary before rejecting malformed previous journal")
	}
}

func TestReview1061RestorePreviousInstallMetadataFailureIsRetryable(t *testing.T) {
	fs := newFakeUpgradeFS()
	io := fs.io()
	a := largePayload("version-a")
	review1061Initial(t, fs, a)
	if _, err := upgradeWithIO(mustJournal(t, io), review1061Plan(largePayload("version-b")), io); err != nil {
		t.Fatal(err)
	}
	current, err := loadJournalWithIO(io)
	if err != nil {
		t.Fatal(err)
	}
	previousBytes := append([]byte(nil), fs.files[current.Previous]...)
	fs.failWrite[installJournalPath] = true
	if err := restorePreviousInstall(current, io); err == nil {
		t.Fatal("metadata failure not surfaced")
	}
	if _, ok := fs.files[current.Previous]; !ok {
		t.Fatal("backup consumed before metadata was durable")
	}
	if _, ok := fs.files[current.Files[0].BeforePath]; !ok {
		t.Fatal("snapshot consumed before metadata was durable")
	}
	delete(fs.failWrite, installJournalPath)
	if err := restorePreviousInstall(current, io); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fs.files[InstalledBinary], a) {
		t.Fatal("retry did not restore the previous binary")
	}
	if !bytes.Equal(fs.files[installJournalPath], previousBytes) {
		t.Fatal("previous journal metadata not restored")
	}
	if _, ok := fs.files[current.Previous]; ok {
		t.Fatal("previous journal backup not consumed after restore")
	}
}

func TestReview1061FailedLaterUpgradeSnapshotIsRecoverable(t *testing.T) {
	fs := newFakeUpgradeFS()
	io := fs.io()
	a := largePayload("version-a")
	b := largePayload("version-b")
	review1061Initial(t, fs, a)
	previous, err := upgradeWithIO(mustJournal(t, io), review1061Plan(b), io)
	if err != nil {
		t.Fatal(err)
	}
	if err := commitUpgrade(previous, io); err != nil {
		t.Fatal(err)
	}
	retainedJournal := append([]byte(nil), fs.files[installJournalPath]...)
	current, err := loadJournalWithIO(io)
	if err != nil {
		t.Fatal(err)
	}
	retainedBackup := current.Previous
	retainedSnapshot := current.Files[0].BeforePath

	fs.failWritePrefix = InstalledBinary + ".zen-previous."
	if _, err := upgradeWithIO(retainedJournal, review1061Plan(largePayload("version-c")), io); err == nil {
		t.Fatal("snapshot write failure not surfaced")
	}
	fs.failWritePrefix = ""
	if !bytes.Equal(fs.files[InstalledBinary], b) {
		t.Fatal("failed later upgrade changed the installed binary")
	}
	if !bytes.Equal(fs.files[installJournalPath], retainedJournal) {
		t.Fatal("failed later upgrade changed the journal")
	}
	if _, ok := fs.files[retainedBackup]; !ok {
		t.Fatal("failed later upgrade deleted the retained backup")
	}
	if _, ok := fs.files[retainedSnapshot]; !ok {
		t.Fatal("failed later upgrade deleted the retained snapshot")
	}
	if _, err := upgradeWithIO(retainedJournal, review1061Plan(largePayload("version-c")), io); err != nil {
		t.Fatalf("retry after failed snapshot write: %v", err)
	}
}
