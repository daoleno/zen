package modelprofiles

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// newDurableOwner starts an Owner over one durable root with a real
// FileCredentialStore, exactly like production. Restarting the Owner from the
// same root simulates a process restart: catalog, credentials, routes and
// discovery all reload from disk and StartOwner runs the orphan sweep.
func newDurableOwner(t *testing.T, root string) (*Owner, *FileCredentialStore) {
	t.Helper()
	creds, err := NewFileCredentialStore(filepath.Join(root, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath:  filepath.Join(root, "model-profiles.toml"),
		RoutesPath:    filepath.Join(root, "route-bindings.json"),
		ListenerPath:  filepath.Join(root, "route-listener.json"),
		DiscoveryPath: filepath.Join(root, "provider-discovery.json"),
		Lookup:        func(string) (string, bool) { return "", false },
		Verifier:      BuiltinEnvelopeVerifier{},
		Credentials:   creds,
	})
	if err != nil {
		t.Fatal(err)
	}
	return owner, creds
}

// crashEdit aborts a provider edit at the given edit-hook phase, leaving every
// durable write before that phase on disk exactly as a process crash would.
func crashEdit(t *testing.T, owner *Owner, phase string, in ProviderConnectionInput, apiKey string, revision int64, create bool) {
	t.Helper()
	owner.SetEditHook(func(p string) error {
		if p == phase {
			return errors.New("injected crash at " + phase)
		}
		return nil
	})
	if _, err := owner.UpsertProviderConnection(in, apiKey, revision, create); err == nil {
		t.Fatalf("edit hook at %s must abort the transaction", phase)
	}
}

// activeRef returns the ref the catalog row currently resolves.
func activeRef(t *testing.T, owner *Owner, id string) string {
	t.Helper()
	return activeCredentialRef(profileFor(t, owner, id))
}

// storedSecret reads a secret from the credential store without failing on a
// missing ref.
func storedSecret(t *testing.T, store *FileCredentialStore, ref string) (string, bool) {
	t.Helper()
	if ref == "" {
		return "", false
	}
	val, ok, err := store.Get(ref)
	if err != nil {
		t.Fatal(err)
	}
	return val, ok
}

func refsSet(t *testing.T, store *FileCredentialStore) map[string]bool {
	t.Helper()
	refs, err := store.Refs()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, ref := range refs {
		out[ref] = true
	}
	return out
}

// TestEditCrashAfterStageKeepsCompleteOldVersion proves P1-2 at the first
// durable boundary: a crash after the staged secret write but before the
// catalog commit leaves the complete old version active (name, URL, ref, key)
// and the restart sweep removes the staged orphan — no mixed state, no active
// orphan secret.
func TestEditCrashAfterStageKeepsCompleteOldVersion(t *testing.T) {
	root := t.TempDir()
	owner, _ := newDurableOwner(t, root)
	proj, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Alpha", "https://one.example/v1"), "sk-1", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connID := proj.Connections[0].ID
	oldRef := activeRef(t, owner, connID)
	oldName, oldURL := "Alpha", "https://one.example/v1"

	// Edit name/URL/key, crash right after the staged secret is durable.
	crashEdit(t, owner, "after_stage",
		codexCustomInput(connID, "Renamed", "https://two.example/v1"), "sk-2", owner.Catalog().Revision, false)
	_ = owner.Close()

	owner2, creds2 := newDurableOwner(t, root)
	t.Cleanup(func() { _ = owner2.Close() })

	got := profileFor(t, owner2, connID)
	if got.Name != oldName || got.BaseURL != oldURL {
		t.Fatalf("catalog must stay the complete old version: %#v", got)
	}
	if ref := activeCredentialRef(got); ref != oldRef {
		t.Fatalf("active ref must stay old: %q want %q", ref, oldRef)
	}
	secret, ok := storedSecret(t, creds2, oldRef)
	if !ok || secret != "sk-1" {
		t.Fatalf("old key lost: %q/%v", secret, ok)
	}
	refs := refsSet(t, creds2)
	if len(refs) != 1 || !refs[oldRef] {
		t.Fatalf("staged orphan not swept: refs=%v want {%s}", refs, oldRef)
	}
}

// TestEditCrashAfterCommitKeepsCompleteNewVersion proves P1-2 at the catalog
// commit boundary: a crash after the single visibility commit but before
// cleanup leaves the complete new version active (name, URL, ref, key) and the
// restart sweep removes the old secret — no mixed state, no active orphan.
func TestEditCrashAfterCommitKeepsCompleteNewVersion(t *testing.T) {
	root := t.TempDir()
	owner, _ := newDurableOwner(t, root)
	proj, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Alpha", "https://one.example/v1"), "sk-1", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connID := proj.Connections[0].ID
	oldRef := activeRef(t, owner, connID)

	// Edit name/URL/key, crash after the catalog commit before cleanup.
	crashEdit(t, owner, "after_commit",
		codexCustomInput(connID, "Renamed", "https://two.example/v1"), "sk-2", owner.Catalog().Revision, false)
	_ = owner.Close()

	owner2, creds2 := newDurableOwner(t, root)
	t.Cleanup(func() { _ = owner2.Close() })

	got := profileFor(t, owner2, connID)
	newRef := activeCredentialRef(got)
	if got.Name != "Renamed" || got.BaseURL != "https://two.example/v1" {
		t.Fatalf("catalog must stay the complete new version: %#v", got)
	}
	if newRef == "" || newRef == oldRef {
		t.Fatalf("active ref must be the committed staged ref: %q", newRef)
	}
	secret, ok := storedSecret(t, creds2, newRef)
	if !ok || secret != "sk-2" {
		t.Fatalf("new key lost: %q/%v", secret, ok)
	}
	refs := refsSet(t, creds2)
	if len(refs) != 1 || !refs[newRef] {
		t.Fatalf("old secret not swept: refs=%v want {%s}", refs, newRef)
	}
}

// TestCreateCrashAfterStageLeavesNoRowAndNoSecret proves create-path recovery:
// a crash after the staged secret write leaves no catalog row and the sweep
// removes the staged secret.
func TestCreateCrashAfterStageLeavesNoRowAndNoSecret(t *testing.T) {
	root := t.TempDir()
	owner, _ := newDurableOwner(t, root)
	crashEdit(t, owner, "after_stage",
		codexCustomInput("", "Doomed", "https://doomed.example/v1"), "sk-x", 0, true)
	_ = owner.Close()

	owner2, creds2 := newDurableOwner(t, root)
	t.Cleanup(func() { _ = owner2.Close() })
	if got := len(owner2.Catalog().Profiles); got != 0 {
		t.Fatalf("orphan row after crashed create: %d", got)
	}
	if refs := refsSet(t, creds2); len(refs) != 0 {
		t.Fatalf("staged secret not swept: %v", refs)
	}
}

// TestCreateCrashAfterCommitKeepsRowAndSecret proves a crash after the create
// commit leaves the complete new connection with its key.
func TestCreateCrashAfterCommitKeepsRowAndSecret(t *testing.T) {
	root := t.TempDir()
	owner, _ := newDurableOwner(t, root)
	crashEdit(t, owner, "after_commit",
		codexCustomInput("", "Born", "https://born.example/v1"), "sk-born", 0, true)
	_ = owner.Close()

	owner2, creds2 := newDurableOwner(t, root)
	t.Cleanup(func() { _ = owner2.Close() })
	proj := owner2.MustProjectForTest(t)
	if len(proj.Connections) != 1 || proj.Connections[0].Name != "Born" {
		t.Fatalf("connection lost after crashed create: %#v", proj.Connections)
	}
	connID := proj.Connections[0].ID
	ref := activeRef(t, owner2, connID)
	secret, ok := storedSecret(t, creds2, ref)
	if !ok || secret != "sk-born" {
		t.Fatalf("key lost after crashed create: %q/%v", secret, ok)
	}
	if refs := refsSet(t, creds2); len(refs) != 1 || !refs[ref] {
		t.Fatalf("refs after crashed create: %v", refs)
	}
}

// TestEditCrashPreservesOldSecretWhileLiveBindingReferencesIt proves the
// router/launch "complete old version" guarantee for pre-existing Sessions: a
// crash after commit keeps the old secret while a route binding still
// references it (that binding keeps routing with the old key), and the sweep
// removes it only after the binding is released and the process restarts.
func TestEditCrashPreservesOldSecretWhileLiveBindingReferencesIt(t *testing.T) {
	root := t.TempDir()
	owner, _ := newDurableOwner(t, root)
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID:       "",
		Name:     "Alpha",
		Client:   ClientCodex,
		PresetID: ProviderPresetCustom,
		BaseURL:  "https://one.example/v1",
		ModelID:  "alpha-model",
		Advanced: true,
	}, "sk-1", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connID := proj.Connections[0].ID
	oldRef := activeRef(t, owner, connID)
	plan, err := owner.PrepareLaunch(ExecutorCodex, connID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s-live"); err != nil {
		t.Fatal(err)
	}
	if plan.State.Binding.CredentialRef != oldRef {
		t.Fatalf("binding ref=%q want %q", plan.State.Binding.CredentialRef, oldRef)
	}

	crashEdit(t, owner, "after_commit",
		codexCustomInput(connID, "Renamed", "https://two.example/v1"), "sk-2", owner.Catalog().Revision, false)
	_ = owner.Close()

	owner2, creds2 := newDurableOwner(t, root)
	// The restored binding still references the old ref, so the sweep must
	// keep the old secret (complete old version for that Session).
	refs := refsSet(t, creds2)
	newRef := activeRef(t, owner2, connID)
	if !refs[oldRef] || !refs[newRef] {
		t.Fatalf("old ref must stay while binding references it: refs=%v old=%q new=%q", refs, oldRef, newRef)
	}
	oldSecret, ok := storedSecret(t, creds2, oldRef)
	if !ok || oldSecret != "sk-1" {
		t.Fatalf("old binding key lost: %q/%v", oldSecret, ok)
	}
	state, ok := owner2.table.Get("s-live")
	if !ok || state.Binding.CredentialRef != oldRef {
		t.Fatalf("restored binding must keep old ref: %#v", state.Binding)
	}

	// Release the Session; the old secret is now unreferenced and the next
	// restart sweep removes it.
	if _, err := owner2.ReleaseSession("s-live"); err != nil {
		t.Fatal(err)
	}
	_ = owner2.Close()
	owner3, creds3 := newDurableOwner(t, root)
	t.Cleanup(func() { _ = owner3.Close() })
	if refs := refsSet(t, creds3); len(refs) != 1 || !refs[newRef] {
		t.Fatalf("unreferenced old secret not swept: %v", refs)
	}
}

// TestSweepRemovesPlantedOrphanRefs proves the deterministic startup cleanup
// removes every provider:* ref that no catalog row or binding references,
// including legacy canonical refs of edited connections and ghost refs.
func TestSweepRemovesPlantedOrphanRefs(t *testing.T) {
	root := t.TempDir()
	owner, creds := newDurableOwner(t, root)
	proj, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Alpha", "https://one.example/v1"), "sk-1", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connID := proj.Connections[0].ID
	activeRef := activeRef(t, owner, connID)
	// Plant a ghost ref, a dead staged ref for the live connection, and a
	// legacy canonical ref the row no longer references.
	if err := creds.Set("provider:ghost", "sk-ghost"); err != nil {
		t.Fatal(err)
	}
	if err := creds.Set("provider:"+connID+":deadbeef", "sk-dead"); err != nil {
		t.Fatal(err)
	}
	if err := creds.Set(CredentialRefFor(connID), "sk-legacy"); err != nil {
		t.Fatal(err)
	}
	_ = owner.Close()

	owner2, creds2 := newDurableOwner(t, root)
	t.Cleanup(func() { _ = owner2.Close() })
	refs := refsSet(t, creds2)
	if len(refs) != 1 || !refs[activeRef] {
		t.Fatalf("sweep kept orphans: %v want {%s}", refs, activeRef)
	}
	secret, ok := storedSecret(t, creds2, activeRef)
	if !ok || secret != "sk-1" {
		t.Fatalf("live secret damaged by sweep: %q/%v", secret, ok)
	}
}

// TestBlankNameMutationsAreZeroWrite proves P1-1: create/update mutations with
// blank or whitespace-only names fail before any catalog or credential write.
func TestBlankNameMutationsAreZeroWrite(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	creds := NewMemoryCredentialStore()
	owner.SetCredentialStore(creds)

	before := owner.Catalog()
	for _, blank := range []string{"", "   ", "\t\n"} {
		_, err := owner.UpsertProviderConnection(
			codexCustomInput("", blank, "https://blank.example/v1"), "sk-x", 0, true)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("blank create name %q err=%v", blank, err)
		}
	}
	after := owner.Catalog()
	if after.Revision != before.Revision || len(after.Profiles) != len(before.Profiles) {
		t.Fatalf("blank create mutated catalog: %#v -> %#v", before, after)
	}
	if refs, _ := creds.Refs(); len(refs) != 0 {
		t.Fatalf("blank create wrote credentials: %v", refs)
	}

	// A valid connection exists; blank-name edits must also be zero-write.
	proj, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Valid", "https://valid.example/v1"), "sk-valid", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connID := proj.Connections[0].ID
	refBefore := activeRef(t, owner, connID)
	revBefore := owner.Catalog().Revision
	for _, blank := range []string{"", " \t "} {
		_, err := owner.UpsertProviderConnection(
			codexCustomInput(connID, blank, "https://mutated.example/v1"), "sk-attacker", owner.Catalog().Revision, false)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("blank update name %q err=%v", blank, err)
		}
	}
	got := profileFor(t, owner, connID)
	if got.Name != "Valid" || got.BaseURL != "https://valid.example/v1" {
		t.Fatalf("blank update mutated row: %#v", got)
	}
	if activeRef(t, owner, connID) != refBefore {
		t.Fatalf("blank update changed active ref: %q -> %q", refBefore, activeRef(t, owner, connID))
	}
	if owner.Catalog().Revision != revBefore {
		t.Fatalf("blank update mutated revision: %d -> %d", revBefore, owner.Catalog().Revision)
	}
	secret, ok, _ := creds.Get(refBefore)
	if !ok || secret != "sk-valid" {
		t.Fatalf("blank update damaged secret: %q/%v", secret, ok)
	}
}

// TestBlankNameWireMutatingProbe compiles blank-name mutations through the
// wire-input compiler directly (the API boundary) and proves the error is
// invalid-name, never a preset-label synthesis.
func TestBlankNameWireMutatingProbe(t *testing.T) {
	for _, preset := range []string{ProviderPresetOpenAI, ProviderPresetDeepSeek, ProviderPresetCustom} {
		in := ProviderConnectionInput{Name: "  ", PresetID: preset, Client: ClientCodex}
		if preset == ProviderPresetCustom {
			in.BaseURL = "https://gateway.example/v1"
			in.Advanced = true
		}
		if _, err := CompileProviderConnection(in); !errors.Is(err, ErrInvalid) {
			t.Fatalf("preset %s blank name err=%v", preset, err)
		}
	}
	// The preset label must never be substituted into a mutation: the caller
	// (App prefill) must carry it explicitly.
	if _, err := CompileProviderConnection(ProviderConnectionInput{
		Name: "OpenAI", PresetID: ProviderPresetOpenAI, Client: ClientCodex,
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains("provider", "x") {
		t.Fatal("unreachable")
	}
}
