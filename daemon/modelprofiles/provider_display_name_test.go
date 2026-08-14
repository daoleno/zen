package modelprofiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestProviderDisplayNameMigrationDeduplicatesDeterministically proves I6: an
// existing catalog with duplicate/empty display names migrates deterministically
// at load — IDs, revision, defaults and credentials are untouched.
func TestProviderDisplayNameMigrationDeduplicatesDeterministically(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "model-profiles.toml")
	doc := `revision = 7

[[profiles]]
id = "c-a"
name = "Alpha"
scope = "account"
client = "codex"
provider_id = "custom"
provider_label = "Custom Gateway"
base_url = "https://a.example/v1"
auth_mode = "none"
credential_env = "ZEN_PROVIDER_API_KEY"

[[profiles]]
id = "c-b"
name = "alpha"
scope = "account"
client = "codex"
provider_id = "custom"
provider_label = "Custom Gateway"
base_url = "https://b.example/v1"
auth_mode = "none"
credential_env = "ZEN_PROVIDER_API_KEY"

[[profiles]]
id = "c-c"
name = "ALPHA"
scope = "account"
client = "claude"
provider_id = "custom"
provider_label = "Custom Gateway"
base_url = "https://c.example/v1"
auth_mode = "none"
credential_env = "ZEN_PROVIDER_API_KEY"

[[profiles]]
id = "c-d"
name = ""
scope = "account"
client = "codex"
provider_id = "custom"
provider_label = "Custom Gateway"
base_url = "https://gateway.example/v1"
auth_mode = "none"
credential_env = "ZEN_PROVIDER_API_KEY"
`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Profile{}
	for _, p := range store.Catalog().Profiles {
		byID[p.ID] = p
	}
	if len(byID) != 4 {
		t.Fatalf("migration lost profiles: %d", len(byID))
	}
	want := map[string]string{
		"c-a": "Alpha",
		"c-b": "alpha (2)",
		"c-c": "ALPHA (3)",
		"c-d": "gateway.example",
	}
	for id, name := range want {
		if got := byID[id].Name; got != name {
			t.Fatalf("migrated name %s=%q want %q", id, got, name)
		}
	}
	// Revision, IDs and defaults survive the migration.
	if store.Revision() != 7 {
		t.Fatalf("revision=%d want 7", store.Revision())
	}
	// The corrected names are durable: a fresh load must not re-suffix.
	store2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	byID2 := map[string]Profile{}
	for _, p := range store2.Catalog().Profiles {
		byID2[p.ID] = p
	}
	for id, name := range want {
		if got := byID2[id].Name; got != name {
			t.Fatalf("reload %s=%q want %q", id, got, name)
		}
	}
	if store2.Revision() != 7 {
		t.Fatalf("reload revision=%d want 7", store2.Revision())
	}
}

// TestProviderDisplayNameMigrationTruncatesOverlongNames proves over-long legacy
// names are trimmed to the bounded length instead of failing startup.
func TestProviderDisplayNameMigrationTruncatesOverlongNames(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "model-profiles.toml")
	long := strings.Repeat("x", 120)
	doc := `revision = 0

[[profiles]]
id = "c-long"
name = "` + long + `"
scope = "account"
client = "codex"
provider_id = "custom"
provider_label = "Custom Gateway"
base_url = "https://long.example/v1"
auth_mode = "none"
credential_env = "ZEN_PROVIDER_API_KEY"
`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("c-long")
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(got.Name)) != MaxProviderNameLength {
		t.Fatalf("name length=%d want %d (%q)", len([]rune(got.Name)), MaxProviderNameLength, got.Name)
	}
}

func codexCustomInput(id, name, baseURL string) ProviderConnectionInput {
	return ProviderConnectionInput{
		ID: id, Name: name, Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: baseURL, Advanced: true,
	}
}

func profileFor(t *testing.T, owner *Owner, id string) Profile {
	t.Helper()
	profile, err := owner.GetProfile(id)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

// TestSameBaseURLDifferentKeysCoexistAndRouteIndependently proves I3: two
// Providers with the same Base URL and different API keys are distinct rows
// with independent credentials, discovery catalogs and route bindings, and the
// separation survives a restart.
func TestSameBaseURLDifferentKeysCoexistAndRouteIndependently(t *testing.T) {
	root := t.TempDir()
	credsPath := filepath.Join(root, "credentials.json")
	creds, err := NewFileCredentialStore(credsPath)
	if err != nil {
		t.Fatal(err)
	}
	// One shared upstream serving both keys: the two Providers point at the
	// exact same Base URL and must still be routed independently.
	var authMu sync.Mutex
	var seenAuth []string
	shared := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		authMu.Lock()
		seenAuth = append(seenAuth, auth)
		authMu.Unlock()
		_, _ = io.WriteString(w, `{"id":"r_shared","object":"response"}`)
	}))
	t.Cleanup(shared.Close)
	sharedURL := shared.URL + "/v1"

	start := func() *Owner {
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
		return owner
	}

	owner := start()
	// Same Base URL, different keys, different names.
	a, err := owner.UpsertProviderConnection(codexCustomInput("", "Alpha gateway", sharedURL), "key-alpha", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connA := a.Connections[0]
	if !strings.HasPrefix(connA.ID, "conn_") || connA.Name != "Alpha gateway" {
		t.Fatalf("conn A=%#v", connA)
	}
	if _, err := owner.UpsertProviderConnection(codexCustomInput("", "Beta gateway", sharedURL), "key-beta", a.Revision, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.UpsertProviderConnection(codexCustomInput("", "Alpha gateway", sharedURL), "key-alpha-2", owner.Catalog().Revision, true); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate case-insensitive name err=%v", err)
	}
	// Exact same URL + distinct names stays allowed (no URL uniqueness).
	connC, err := owner.UpsertProviderConnection(codexCustomInput("", "Alpha mirror", sharedURL), "key-alpha-2", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatalf("same URL second row: %v", err)
	}
	_ = connC

	gotA, _ := owner.GetProfile(connA.ID)
	proj := owner.MustProjectForTest(t)
	connBID := ""
	for _, c := range proj.Connections {
		if c.Name == "Beta gateway" {
			connBID = c.ID
		}
	}
	gotB, _ := owner.GetProfile(connBID)
	if gotA.BaseURL != gotB.BaseURL {
		t.Fatalf("same URL expected: %q vs %q", gotA.BaseURL, gotB.BaseURL)
	}

	// Independent per-connection discovery catalogs.
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put(connA.ID, []string{"alpha-model-1", "alpha-model-2"}, nil)
	owner.discovery.put(connBID, []string{"beta-model-1"}, nil)
	if owner.discoveryPath != "" {
		if serr := owner.discovery.save(owner.discoveryPath); serr != nil {
			t.Fatal(serr)
		}
	}
	owner.mu.Unlock()
	modelsA := owner.supportedModelEntriesLocked(gotA)
	modelsB := owner.supportedModelEntriesLocked(gotB)
	if len(modelsA) != 2 || len(modelsB) != 1 || modelsA[0].ID != "alpha-model-1" || modelsB[0].ID != "beta-model-1" {
		t.Fatalf("catalog isolation A=%#v B=%#v", modelsA, modelsB)
	}

	// Independent route bindings carry the right credential refs.
	planA, err := owner.PrepareLaunch(ExecutorCodex, connA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	planB, err := owner.PrepareLaunch(ExecutorCodex, connBID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if planA.State.Binding.CredentialRef != activeCredentialRef(gotA) ||
		planB.State.Binding.CredentialRef != activeCredentialRef(gotB) {
		t.Fatalf("credential refs A=%q B=%q", planA.State.Binding.CredentialRef, planB.State.Binding.CredentialRef)
	}

	// Full loopback routing: each Session's request reaches the shared upstream
	// with its own key, proving same-URL routing is ID-based, never URL-based.
	routerSrv := httptest.NewServer(owner.router.Handler())
	t.Cleanup(routerSrv.Close)
	wantAuth := map[string]string{connA.ID: "Bearer key-alpha", connBID: "Bearer key-beta"}
	for _, plan := range []*SessionLaunchPlan{&planA, &planB} {
		if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s-"+plan.State.Binding.ProfileID); err != nil {
			t.Fatal(err)
		}
		base, err := LoopbackCodexBaseURL(routerSrv.Listener.Addr().String(), plan.State.Binding.RouteID)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodPost, base+"/responses", bytes.NewReader([]byte(`{"model":"m","input":"hi"}`)))
		req.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("route %s status=%d", plan.State.Binding.ProfileID, resp.StatusCode)
		}
	}
	authMu.Lock()
	defer authMu.Unlock()
	if len(seenAuth) != 2 {
		t.Fatalf("upstream saw %d requests: %v", len(seenAuth), seenAuth)
	}
	for id, want := range wantAuth {
		matched := false
		for _, got := range seenAuth {
			if got == want {
				matched = true
			}
		}
		if !matched {
			t.Fatalf("connection %s auth %q never reached upstream: %v", id, want, seenAuth)
		}
	}

	// Restart: the same rows, secrets and catalogs survive independently.
	_ = owner.Close()
	owner2 := start()
	t.Cleanup(func() { _ = owner2.Close() })
	proj2 := owner2.MustProjectForTest(t)
	byName := map[string]ProviderConnection{}
	for _, c := range proj2.Connections {
		byName[c.Name] = c
	}
	if _, ok := byName["Alpha gateway"]; !ok {
		t.Fatalf("Alpha gateway lost on restart: %#v", proj2.Connections)
	}
	if _, ok := byName["Beta gateway"]; !ok {
		t.Fatalf("Beta gateway lost on restart: %#v", proj2.Connections)
	}
	refA, refB := activeCredentialRef(gotA), activeCredentialRef(gotB)
	valA, okA, _ := creds.Get(refA)
	valB, okB, _ := creds.Get(refB)
	if !okA || !okB || valA != "key-alpha" || valB != "key-beta" {
		t.Fatalf("restart secrets A=%q/%v B=%q/%v", valA, okA, valB, okB)
	}
	// Discovery cache survives restart and stays per-ID.
	alphaID := byName["Alpha gateway"].ID
	alphaProfile, gerr := owner2.GetProfile(alphaID)
	if gerr != nil {
		t.Fatal(gerr)
	}
	entriesA, _ := owner2.modelsForConnection(alphaProfile, false)
	if len(entriesA) != 2 || entriesA[0].ID != "alpha-model-1" {
		t.Fatalf("restart catalog A=%#v", entriesA)
	}
}

// MustProjectForTest projects the Provider surface for tests.
func (o *Owner) MustProjectForTest(t *testing.T) ProviderCatalogProjection {
	t.Helper()
	proj, err := o.ProjectProviders()
	if err != nil {
		t.Fatal(err)
	}
	return proj
}

// TestRenamePreservesIdentityModelsDefaultsHistory proves I1/I2: renaming a
// Provider keeps its ID, discovery catalog, client default, bound Session and
// credential, and only the displayed name changes.
func TestRenamePreservesIdentityModelsDefaultsHistory(t *testing.T) {
	root := t.TempDir()
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
	t.Cleanup(func() { _ = owner.Close() })

	proj, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Old name", "https://gateway.example/v1"), "sk-rename-secret", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connID := proj.Connections[0].ID

	// Discovery catalog, default selection and credential exist before rename.
	owner.mu.Lock()
	owner.discovery = newModelDiscoveryCache()
	owner.discovery.put(connID, []string{"m1", "m2"}, nil)
	owner.discovery.setDisabled(connID, []string{"m2"})
	owner.mu.Unlock()
	proj, err = owner.SetProviderDefault(ClientCodex, connID, "m1", proj.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.PrepareLaunch(ExecutorCodex, connID, "codex"); err != nil {
		t.Fatal(err)
	}

	// Rename with an unchanged key (empty apiKey preserves the secret).
	proj, err = owner.UpsertProviderConnection(
		codexCustomInput(connID, "New name", "https://gateway.example/v1"), "", proj.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	renamed := proj.Connections[0]
	if renamed.ID != connID || renamed.Name != "New name" {
		t.Fatalf("rename row=%#v", renamed)
	}
	got, err := owner.GetProfile(connID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "New name" || got.BaseURL != "https://gateway.example/v1" {
		t.Fatalf("renamed profile=%#v", got)
	}
	secret, ok, _ := creds.Get(activeCredentialRef(got))
	if !ok || secret != "sk-rename-secret" {
		t.Fatalf("secret lost on rename: %q/%v", secret, ok)
	}
	// Models and support toggles survived (disabled m2 stays disabled).
	entries := owner.supportedModelEntriesLocked(got)
	if len(entries) != 1 || entries[0].ID != "m1" {
		t.Fatalf("support allowlist after rename=%#v", entries)
	}
	// Client default still references the same ID.
	dflt := owner.MustProjectForTest(t).Defaults[ClientCodex]
	if dflt.ConnectionID != connID || dflt.ModelID != "m1" {
		t.Fatalf("default after rename=%#v", dflt)
	}
	// Launch routes the renamed provider with the preserved credential.
	plan, err := owner.PrepareLaunch(ExecutorCodex, connID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if plan.State.Binding.ProfileName != "New name" || plan.State.Binding.CredentialRef != activeCredentialRef(got) {
		t.Fatalf("binding after rename=%#v", plan.State.Binding)
	}
}

// TestUpsertValidationFailureAppliesNothing proves I4: a validation failure
// (duplicate name) leaves name, URL and credential untouched — no partial edit.
func TestUpsertValidationFailureAppliesNothing(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	creds := NewMemoryCredentialStore()
	owner.SetCredentialStore(creds)

	proj, err := owner.UpsertProviderConnection(
		codexCustomInput("", "First", "https://one.example/v1"), "sk-first", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	first := proj.Connections[0]
	if _, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Second", "https://two.example/v1"), "sk-second", proj.Revision, true); err != nil {
		t.Fatal(err)
	}
	// Edit First → duplicate name "SECOND" (case-insensitive) with a new key.
	_, err = owner.UpsertProviderConnection(
		codexCustomInput(first.ID, "SECOND", "https://changed.example/v1"), "sk-attacker", proj.Revision+1, false)
	if !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("want ErrDuplicateName got %v", err)
	}
	got, _ := owner.GetProfile(first.ID)
	if got.Name != "First" || got.BaseURL != "https://one.example/v1" {
		t.Fatalf("partial update applied: %#v", got)
	}
	secret, ok, _ := creds.Get(activeCredentialRef(got))
	if !ok || secret != "sk-first" {
		t.Fatalf("credential mutated by failed edit: %q/%v", secret, ok)
	}
	// Blank, whitespace-only, and over-long name edits also apply nothing: the
	// daemon requires an explicit non-empty trimmed name on every mutation
	// (only load-time migration may synthesize names).
	badName := strings.Repeat("y", MaxProviderNameLength+1)
	if _, err := owner.UpsertProviderConnection(
		codexCustomInput(first.ID, badName, "https://changed.example/v1"), "sk-attacker", owner.Catalog().Revision, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("over-long name err=%v", err)
	}
	// Update without the stable id is rejected before any write.
	if _, err := owner.UpsertProviderConnection(
		codexCustomInput("", "No id", "https://changed.example/v1"), "sk-attacker", owner.Catalog().Revision, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("update without id err=%v", err)
	}
}

// TestUpsertKeyReplaceAndPreserve proves I4: an empty key preserves the stored
// secret; a non-empty key replaces it via the staged-ref commit (no partial
// state, no credential-store rollback window).
func TestUpsertKeyReplaceAndPreserve(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	creds := NewMemoryCredentialStore()
	owner.SetCredentialStore(creds)

	proj, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Keyed", "https://keyed.example/v1"), "sk-v1", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connID := proj.Connections[0].ID
	secret, ok, _ := creds.Get(activeCredentialRef(profileFor(t, owner, connID)))
	if !ok || secret != "sk-v1" {
		t.Fatalf("initial key=%q/%v", secret, ok)
	}
	// Empty key on edit preserves the secret and the active ref.
	refBefore := activeCredentialRef(profileFor(t, owner, connID))
	if _, err := owner.UpsertProviderConnection(
		codexCustomInput(connID, "Keyed", "https://keyed.example/v1"), "", proj.Revision, false); err != nil {
		t.Fatal(err)
	}
	secret, _, _ = creds.Get(activeCredentialRef(profileFor(t, owner, connID)))
	if secret != "sk-v1" {
		t.Fatalf("preserve key=%q", secret)
	}
	if got := activeCredentialRef(profileFor(t, owner, connID)); got != refBefore {
		t.Fatalf("rename without key must keep the active ref: %q -> %q", refBefore, got)
	}
	// New key replaces it atomically with the rename; the old ref is cleaned up.
	if _, err := owner.UpsertProviderConnection(
		codexCustomInput(connID, "Keyed v2", "https://keyed2.example/v1"), "sk-v2", owner.Catalog().Revision, false); err != nil {
		t.Fatal(err)
	}
	gotProfile := profileFor(t, owner, connID)
	secret, _, _ = creds.Get(activeCredentialRef(gotProfile))
	if secret != "sk-v2" {
		t.Fatalf("replace key=%q", secret)
	}
	got, _ := owner.GetProfile(connID)
	if got.Name != "Keyed v2" || got.BaseURL != "https://keyed2.example/v1" {
		t.Fatalf("atomic edit profile=%#v", got)
	}
}

// TestUpsertCredentialStoreFailureAppliesNothing proves the staged-ref design:
// a credential-store failure happens before the catalog write (stage-first), so
// the edit applies zero writes and leaves no staged or orphaned secret behind.
func TestUpsertCredentialStoreFailureAppliesNothing(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	creds := NewMemoryCredentialStore()
	owner.SetCredentialStore(creds)
	proj, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Stable", "https://stable.example/v1"), "sk-old", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connID := proj.Connections[0].ID

	failStore := &failingCredentialStore{inner: NewMemoryCredentialStore(), failAll: true}
	owner.SetCredentialStore(failStore)
	// Update with a new key: the stage fails before the catalog write, so the
	// row keeps its old name/URL/ref and no staged ref exists.
	_, err = owner.UpsertProviderConnection(
		codexCustomInput(connID, "Mutated", "https://mutated.example/v1"), "sk-new", owner.Catalog().Revision, false)
	if !errors.Is(err, ErrCredentialStoreFailed) {
		t.Fatalf("want credential failure got %v", err)
	}
	got, _ := owner.GetProfile(connID)
	if got.Name != "Stable" || got.BaseURL != "https://stable.example/v1" {
		t.Fatalf("catalog mutated by failed stage: %#v", got)
	}
	if refs, _ := failStore.Refs(); len(refs) != 0 {
		t.Fatalf("staged refs after failed stage: %v", refs)
	}

	// Create with a key whose stage fails leaves no orphan connection and no
	// credential refs.
	before := len(owner.Catalog().Profiles)
	_, err = owner.UpsertProviderConnection(
		codexCustomInput("", "Doomed", "https://doomed.example/v1"), "sk-new", owner.Catalog().Revision, true)
	if !errors.Is(err, ErrCredentialStoreFailed) {
		t.Fatalf("want credential failure got %v", err)
	}
	if got := len(owner.Catalog().Profiles); got != before {
		t.Fatalf("orphan connection created: %d -> %d", before, got)
	}
	if refs, _ := failStore.Refs(); len(refs) != 0 {
		t.Fatalf("staged refs after failed create: %v", refs)
	}
}

// TestUpsertCatalogFailureRetractsStagedSecret proves a not-applied catalog
// commit (revision conflict) retracts the private staged secret so a failed
// edit cannot leave an orphaned secret in the vault.
func TestUpsertCatalogFailureRetractsStagedSecret(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	creds := NewMemoryCredentialStore()
	owner.SetCredentialStore(creds)

	// Stage succeeds (store accepts), but the catalog create conflicts: the
	// staged secret must be retracted.
	_, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Conflicted", "https://conflict.example/v1"), "sk-staged", 99, true)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict got %v", err)
	}
	if refs, _ := creds.Refs(); len(refs) != 0 {
		t.Fatalf("staged secret not retracted: %v", refs)
	}
	if got := len(owner.Catalog().Profiles); got != 0 {
		t.Fatalf("orphan connection created: %d", got)
	}
}

type failingCredentialStore struct {
	inner    CredentialStore
	failAll  bool
	failNext bool
}

func (f *failingCredentialStore) Available() bool { return f.inner.Available() }
func (f *failingCredentialStore) Get(ref string) (string, bool, error) {
	return f.inner.Get(ref)
}
func (f *failingCredentialStore) Delete(ref string) error { return f.inner.Delete(ref) }
func (f *failingCredentialStore) Refs() ([]string, error) { return f.inner.Refs() }
func (f *failingCredentialStore) Set(ref, secret string) error {
	if f.failAll || f.failNext {
		f.failNext = false
		return ErrCredentialStoreFailed
	}
	return f.inner.Set(ref, secret)
}

// TestProviderConnectionWireNeverCarriesCredential proves the upsert reply
// never reflects the submitted key.
func TestProviderConnectionWireNeverCarriesCredential(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	creds := NewMemoryCredentialStore()
	owner.SetCredentialStore(creds)
	proj, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Secret free", "https://secret-free.example/v1"), "sk-super-secret-12345", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(proj)
	for _, banned := range []string{"sk-super-secret", "api_key", `"credential":`} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("wire leaked %q: %s", banned, raw)
		}
	}
}

// TestCuratedConnectionRenameKeepsPresetEndpoint proves editing a curated
// connection (official endpoint, no Base URL in the edit) renames it without
// flipping it into an advanced/custom gateway or changing its endpoint.
func TestCuratedConnectionRenameKeepsPresetEndpoint(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		Name: "OpenAI", Client: ClientCodex, PresetID: ProviderPresetOpenAI,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	conn := proj.Connections[0]
	if conn.Advanced || conn.BaseURL != "" {
		t.Fatalf("curated row=%#v", conn)
	}
	proj, err = owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: conn.ID, Name: "OpenAI work", Client: ClientCodex,
		PresetID: ProviderPresetOpenAI, Advanced: false,
	}, "", proj.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	renamed := proj.Connections[0]
	if renamed.ID != conn.ID || renamed.Name != "OpenAI work" || renamed.Advanced {
		t.Fatalf("renamed curated row=%#v", renamed)
	}
	got, _ := owner.GetProfile(conn.ID)
	if got.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("curated endpoint changed: %#v", got)
	}
}
