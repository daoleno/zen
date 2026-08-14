package modelprofiles

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCompileProviderConnectionCuratedOmitsModel(t *testing.T) {
	in := ProviderConnectionInput{Name: "DeepSeek", PresetID: ProviderPresetDeepSeek, Client: ClientCodex}
	profile, err := CompileProviderConnection(in)
	if err != nil {
		t.Fatal(err)
	}
	if !isAccountConnection(profile) || profile.Model != "" {
		t.Fatalf("curated account must not own model: %#v", profile)
	}
	pub := providerConnectionFromProfile(profile, false)
	raw, _ := json.Marshal(pub)
	for _, banned := range []string{`"model_id"`, `"provider_id"`, "auth_mode", "credential_env", "executor_id", "protocol", "DEEPSEEK_API_KEY"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("public leaked %q: %s", banned, raw)
		}
	}
	if pub.ManualModelID != "" || pub.BaseURL != "" {
		t.Fatalf("curated public advanced fields: %#v", pub)
	}
	_, err = CompileProviderConnection(ProviderConnectionInput{
		Name: "DS", PresetID: ProviderPresetDeepSeek, Client: ClientCodex, ModelID: "deepseek-v4-flash",
	})
	if err == nil {
		t.Fatal("curated must reject connection-level model_id")
	}
}

func TestSetProviderDefaultAtomicSingleWrite(t *testing.T) {
	owner := startTestOwner(t, func(string) (string, bool) { return "ready", true })
	creds := NewMemoryCredentialStore()
	owner.creds = creds
	conn, err := CompileProviderConnection(ProviderConnectionInput{Name: "DS", PresetID: ProviderPresetDeepSeek, Client: ClientCodex})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.UpsertProfile(conn, 0, true); err != nil {
		t.Fatal(err)
	}
	before := owner.Catalog().Revision
	proj, err := owner.SetProviderDefault(ClientCodex, conn.ID, "deepseek-v4-flash", before)
	if err != nil {
		t.Fatal(err)
	}
	if proj.Revision != before+1 {
		t.Fatalf("revision=%d want %d", proj.Revision, before+1)
	}
	def := proj.Defaults[ClientCodex]
	if def.ConnectionID != conn.ID || def.ModelID != "deepseek-v4-flash" {
		t.Fatalf("default=%#v", def)
	}

	// Deterministic persistence failure leaves both fields and revision unchanged.
	failOwner := startTestOwner(t, func(string) (string, bool) { return "ready", true })
	conn2, err := CompileProviderConnection(ProviderConnectionInput{Name: "DS2", PresetID: ProviderPresetDeepSeek, Client: ClientCodex})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failOwner.UpsertProfile(conn2, 0, true); err != nil {
		t.Fatal(err)
	}
	rev := failOwner.Catalog().Revision
	if _, err := failOwner.SetProviderDefault(ClientCodex, conn2.ID, "deepseek-v4-flash", rev); err != nil {
		t.Fatal(err)
	}
	rev = failOwner.Catalog().Revision
	failOwner.store.SetPersistHook(func(phase string) error {
		if phase == "before_write" {
			return errors.New("disk full")
		}
		return nil
	})
	_, err = failOwner.SetProviderDefault(ClientCodex, conn2.ID, "deepseek-v4-flash", rev)
	if err == nil {
		t.Fatal("expected persist failure")
	}
	if failOwner.Catalog().Revision != rev {
		t.Fatalf("revision mutated on failure: %d", failOwner.Catalog().Revision)
	}
	got := failOwner.store.DefaultModelID(ClientCodex)
	if got != "deepseek-v4-flash" {
		t.Fatalf("model rolled forward on failure: %q", got)
	}
}

func TestCredentialStoreMatrix(t *testing.T) {
	owner := startTestOwner(t, func(string) (string, bool) { return "", false })
	store := NewMemoryCredentialStore()
	owner.creds = store
	owner.router.creds = store

	conn, err := CompileProviderConnection(ProviderConnectionInput{Name: "DeepSeek", PresetID: ProviderPresetDeepSeek, Client: ClientCodex})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.UpsertProfile(conn, 0, true); err != nil {
		t.Fatal(err)
	}
	connB, err := CompileProviderConnection(ProviderConnectionInput{Name: "B", PresetID: ProviderPresetDeepSeek, Client: ClientCodex})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.UpsertProfile(connB, 1, true); err != nil {
		t.Fatal(err)
	}

	if owner.connectionReady(conn) {
		t.Fatal("expected not ready without secret/env")
	}
	res, err := owner.SetProviderCredential(conn.ID, "sk-secret-a")
	if err != nil || !res.CredentialReady {
		t.Fatalf("set: %#v err=%v", res, err)
	}
	if owner.connectionReady(connB) {
		t.Fatal("secret must isolate across connections")
	}
	rawProj, _ := json.Marshal(mustProject(t, owner))
	if strings.Contains(string(rawProj), "sk-secret-a") {
		t.Fatal("secret leaked into providers projection")
	}

	store.SetAvailable(false)
	_, err = owner.SetProviderCredential(connB.ID, "sk-b")
	if !errors.Is(err, ErrCredentialStoreUnavailable) {
		t.Fatalf("unavailable: %v", err)
	}
	store.SetAvailable(true)
}

func TestDeleteProviderConnectionNonOrphaning(t *testing.T) {
	newOwner := func(t *testing.T) (*Owner, *MemoryCredentialStore) {
		t.Helper()
		owner := startTestOwner(t, func(string) (string, bool) { return "", false })
		store := NewMemoryCredentialStore()
		owner.creds = store
		owner.router.creds = store
		return owner, store
	}
	seed := func(t *testing.T, owner *Owner, store *MemoryCredentialStore, name string) Profile {
		t.Helper()
		conn, err := CompileProviderConnection(ProviderConnectionInput{Name: name, PresetID: ProviderPresetDeepSeek, Client: ClientCodex})
		if err != nil {
			t.Fatal(err)
		}
		rev := owner.Catalog().Revision
		if _, err := owner.UpsertProfile(conn, rev, true); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.SetProviderCredential(conn.ID, "sk-"+name); err != nil {
			t.Fatal(err)
		}
		got, err := owner.GetProfile(conn.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	hasRef := func(store *MemoryCredentialStore, id string) bool {
		ref := CredentialRefFor(id)
		for _, r := range store.SnapshotRefs() {
			if r == ref {
				return true
			}
		}
		return false
	}

	t.Run("preflight_default_key_untouched", func(t *testing.T) {
		owner, store := newOwner(t)
		conn := seed(t, owner, store, "def")
		rev := owner.Catalog().Revision
		if _, err := owner.SetProviderDefault(ClientCodex, conn.ID, "deepseek-v4-flash", rev); err != nil {
			t.Fatal(err)
		}
		rev = owner.Catalog().Revision
		_, err := owner.DeleteProviderConnection(conn.ID, rev)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("err=%v", err)
		}
		if !hasRef(store, conn.ID) {
			t.Fatal("key must be untouched on default preflight")
		}
		if _, err := owner.GetProfile(conn.ID); err != nil {
			t.Fatal("connection must remain")
		}
		if owner.Catalog().Revision != rev {
			t.Fatal("revision mutated")
		}
	})

	t.Run("preflight_in_use_key_untouched", func(t *testing.T) {
		owner := startTestOwner(t, readyLookup("x"))
		store := NewMemoryCredentialStore()
		owner.creds = store
		owner.router.creds = store
		conn := seed(t, owner, store, "inuse")
		target, err := CompileConnectionTarget(conn, ClientCodex, "deepseek-v4-flash", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := owner.table.BindLaunch("s-inuse", target, owner.Catalog().Revision, ContractAuth{Verifier: owner.verifier}); err != nil {
			t.Fatal(err)
		}
		rev := owner.Catalog().Revision
		_, err = owner.DeleteProviderConnection(conn.ID, rev)
		if !errors.Is(err, ErrProfileInUse) {
			t.Fatalf("err=%v", err)
		}
		if !hasRef(store, conn.ID) {
			t.Fatal("key must be untouched on in-use preflight")
		}
		if owner.Catalog().Revision != rev {
			t.Fatal("revision mutated")
		}
	})

	t.Run("preflight_revision_key_untouched", func(t *testing.T) {
		owner, store := newOwner(t)
		conn := seed(t, owner, store, "rev")
		_, err := owner.DeleteProviderConnection(conn.ID, owner.Catalog().Revision+9)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("err=%v", err)
		}
		if !hasRef(store, conn.ID) {
			t.Fatal("key must be untouched on revision preflight")
		}
	})

	t.Run("credential_delete_failure_catalog_untouched", func(t *testing.T) {
		owner, store := newOwner(t)
		conn := seed(t, owner, store, "kfail")
		rev := owner.Catalog().Revision
		store.SetFail(nil, nil, ErrCredentialStoreFailed)
		proj, err := owner.DeleteProviderConnection(conn.ID, rev)
		if !errors.Is(err, ErrCredentialStoreFailed) {
			t.Fatalf("err=%v", err)
		}
		if PersistResultFromError(err).Applied {
			t.Fatal("credential delete failure must be not-applied")
		}
		if !hasRef(store, conn.ID) {
			t.Fatal("key must remain when delete fails")
		}
		if _, gerr := owner.GetProfile(conn.ID); gerr != nil {
			t.Fatal("catalog must be untouched")
		}
		if owner.Catalog().Revision != rev || proj.Revision != rev {
			t.Fatalf("revision mutated: catalog=%d proj=%d", owner.Catalog().Revision, proj.Revision)
		}
	})

	t.Run("catalog_failure_after_key_delete_retained", func(t *testing.T) {
		owner, store := newOwner(t)
		conn := seed(t, owner, store, "cfail")
		rev := owner.Catalog().Revision
		owner.store.SetPersistHook(func(phase string) error {
			if phase == "before_write" {
				return errors.New("disk full")
			}
			return nil
		})
		proj, err := owner.DeleteProviderConnection(conn.ID, rev)
		if err == nil {
			t.Fatal("expected catalog persist failure")
		}
		if hasRef(store, conn.ID) {
			t.Fatal("key must be deleted; no orphan secret after catalog failure")
		}
		if _, gerr := owner.GetProfile(conn.ID); gerr != nil {
			t.Fatal("connection must remain in catalog")
		}
		if owner.Catalog().Revision != rev {
			t.Fatal("revision must be unchanged")
		}
		ready := false
		for _, c := range proj.Connections {
			if c.ID == conn.ID {
				ready = c.CredentialReady
			}
		}
		if ready {
			t.Fatal("connection must be credential-not-ready after key delete")
		}
	})

	t.Run("success", func(t *testing.T) {
		owner, store := newOwner(t)
		conn := seed(t, owner, store, "ok")
		rev := owner.Catalog().Revision
		proj, err := owner.DeleteProviderConnection(conn.ID, rev)
		if err != nil {
			t.Fatal(err)
		}
		if hasRef(store, conn.ID) {
			t.Fatal("key must be gone")
		}
		if _, gerr := owner.GetProfile(conn.ID); !errors.Is(gerr, ErrNotFound) {
			t.Fatalf("connection must be gone: %v", gerr)
		}
		if proj.Revision != rev+1 {
			t.Fatalf("revision=%d", proj.Revision)
		}
	})

	t.Run("not_found_key", func(t *testing.T) {
		owner, store := newOwner(t)
		conn, err := CompileProviderConnection(ProviderConnectionInput{Name: "nokey", PresetID: ProviderPresetDeepSeek, Client: ClientCodex})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := owner.UpsertProfile(conn, 0, true); err != nil {
			t.Fatal(err)
		}
		if hasRef(store, conn.ID) {
			t.Fatal("no key expected")
		}
		rev := owner.Catalog().Revision
		if _, err := owner.DeleteProviderConnection(conn.ID, rev); err != nil {
			t.Fatalf("missing key must not block delete: %v", err)
		}
		if _, err := owner.GetProfile(conn.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("connection must be deleted")
		}
	})
}

func mustProject(t *testing.T, o *Owner) ProviderCatalogProjection {
	t.Helper()
	p, err := o.ProjectProviders()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDiscoveryCacheHonestPersistence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "provider-discovery.json")
	c := newModelDiscoveryCache()
	c.put("c1", []string{"deepseek-v4-flash"}, nil)
	if err := c.save(path); err != nil {
		t.Fatal(err)
	}
	c2 := newModelDiscoveryCache()
	if err := c2.load(path); err != nil {
		t.Fatal(err)
	}
	e, ok := c2.get("c1")
	if !ok || len(e.LastGood) != 1 {
		t.Fatalf("lkg=%#v", e)
	}

	// Corrupt / unsupported schema
	_ = os.WriteFile(path, []byte(`{"schema_version":99,"entries":{}}`), 0o600)
	if err := newModelDiscoveryCache().load(path); !errors.Is(err, ErrDiscoveryCacheInvalid) {
		t.Fatalf("schema err=%v", err)
	}
	_ = os.WriteFile(path, []byte(`{not-json`), 0o600)
	if err := newModelDiscoveryCache().load(path); !errors.Is(err, ErrDiscoveryCacheInvalid) {
		t.Fatalf("corrupt err=%v", err)
	}

	// Concurrent updates under saveMu: every writer succeeds; final content is
	// some writer's snapshot (serialization makes stale rename impossible).
	c3 := newModelDiscoveryCache()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c3.put("c1", []string{strings.Repeat("m", i+1)}, nil)
			errs <- c3.save(path)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("serialized save must succeed: %v", err)
		}
	}
	c4 := newModelDiscoveryCache()
	if err := c4.load(path); err != nil {
		t.Fatal(err)
	}
	final, ok := c4.get("c1")
	if !ok || len(final.LastGood) != 1 {
		t.Fatalf("final durable=%#v", final)
	}
}
