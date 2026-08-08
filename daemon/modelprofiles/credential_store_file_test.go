package modelprofiles

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileCredentialStorePersistsPrivateSecretsWithoutKeyring(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "provider-credentials.json")
	store, err := NewFileCredentialStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !store.Available() {
		t.Fatal("new file store must be available")
	}
	if secret, ok, err := store.Get("provider:codex-main"); err != nil || ok || secret != "" {
		t.Fatalf("empty store get=(%q,%v,%v)", secret, ok, err)
	}

	if err := store.Set("provider:codex-main", "sk-private-value"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode=%#o want 0600", info.Mode().Perm())
	}

	reloaded, err := NewFileCredentialStore(path)
	if err != nil {
		t.Fatal(err)
	}
	secret, ok, err := reloaded.Get("provider:codex-main")
	if err != nil || !ok || secret != "sk-private-value" {
		t.Fatalf("reloaded get=(%q,%v,%v)", secret, ok, err)
	}

	if err := reloaded.Delete("provider:codex-main"); err != nil {
		t.Fatal(err)
	}
	afterDelete, err := NewFileCredentialStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if secret, ok, err := afterDelete.Get("provider:codex-main"); err != nil || ok || secret != "" {
		t.Fatalf("deleted get=(%q,%v,%v)", secret, ok, err)
	}
}

func TestFileCredentialStoreRejectsCorruptOrUnknownSchema(t *testing.T) {
	for name, raw := range map[string]string{
		"corrupt": `{`,
		"unknown": `{"schema_version":2,"secrets":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "provider-credentials.json")
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := NewFileCredentialStore(path)
			if !errors.Is(err, ErrCredentialStoreFailed) {
				t.Fatalf("error=%v want ErrCredentialStoreFailed", err)
			}
		})
	}
}
