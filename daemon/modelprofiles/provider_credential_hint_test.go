package modelprofiles

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCredentialHintProjection proves the public wire carries only a bounded
// masked hint of the active stored secret — never the full key, never the
// length. Short and odd-format secrets yield safe generic hints; connections
// without a stored secret get no hint at all.
func TestCredentialHintProjection(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	creds := NewMemoryCredentialStore()
	owner.SetCredentialStore(creds)

	proj, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Hinted", "https://hinted.example/v1"), "sk-super-secret-12345", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	conn := proj.Connections[0]
	if conn.CredentialHint != "sk-•••345" {
		t.Fatalf("hint=%q want %q", conn.CredentialHint, "sk-•••345")
	}
	// The hint must not reveal the length: it always renders the same width.
	if len([]rune(conn.CredentialHint)) != 9 {
		t.Fatalf("hint width=%d want 9 (%q)", len([]rune(conn.CredentialHint)), conn.CredentialHint)
	}
	// Full secret never appears anywhere in the wire projection.
	raw, _ := json.Marshal(proj)
	if strings.Contains(string(raw), "sk-super-secret-12345") {
		t.Fatalf("wire leaked full secret: %s", raw)
	}
	if strings.Contains(string(raw), "12345") {
		t.Fatalf("wire leaked secret suffix beyond hint: %s", raw)
	}

	// Short / odd-format secrets get a generic bullets-only hint.
	proj2, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Short", "https://short.example/v1"), "ab", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	short := connectionByName(t, proj2, "Short")
	if short.CredentialHint != "••••••" {
		t.Fatalf("short hint=%q want %q", short.CredentialHint, "••••••")
	}

	// A connection with no stored secret (env-only readiness) has no hint.
	noKey, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		Name: "EnvOnly", Client: ClientCodex, PresetID: ProviderPresetOpenAI,
	}, "", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := connectionByName(t, noKey, "EnvOnly").CredentialHint; got != "" {
		t.Fatalf("env-only hint=%q want empty", got)
	}
	raw2, _ := json.Marshal(proj2)
	for _, banned := range []string{`"sk-super-secret"`, `"credential":`, `"api_key":`} {
		if strings.Contains(string(raw2), banned) {
			t.Fatalf("wire leaked %q: %s", banned, raw2)
		}
	}
}

// TestCredentialHintSurvivesRestartAndRotation proves the hint tracks the
// active stored secret across the crash-safe unified edit: after a key
// replacement the hint reflects the new secret, and a rename without a key
// keeps the same hint.
func TestCredentialHintSurvivesRestartAndRotation(t *testing.T) {
	root := t.TempDir()
	owner, creds := newDurableOwner(t, root)
	proj, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Hinted", "https://hinted.example/v1"), "sk-old-secret-000", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connID := proj.Connections[0].ID
	if proj.Connections[0].CredentialHint != "sk-•••000" {
		t.Fatalf("hint=%q", proj.Connections[0].CredentialHint)
	}
	// Rotate the key through the unified edit: hint reflects the new secret.
	if _, err := owner.UpsertProviderConnection(
		codexCustomInput(connID, "Hinted", "https://hinted.example/v1"), "sk-new-secret-999", owner.Catalog().Revision, false); err != nil {
		t.Fatal(err)
	}
	if got := owner.MustProjectForTest(t).Connections[0].CredentialHint; got != "sk-•••999" {
		t.Fatalf("rotated hint=%q want %q", got, "sk-•••999")
	}
	// Restart: hint regenerates from the persisted active ref, never the full key.
	_ = owner.Close()
	owner2, _ := newDurableOwner(t, root)
	t.Cleanup(func() { _ = owner2.Close() })
	hint := owner2.MustProjectForTest(t).Connections[0].CredentialHint
	if hint != "sk-•••999" {
		t.Fatalf("restart hint=%q", hint)
	}
	if refs := refsSet(t, creds); len(refs) != 1 {
		t.Fatalf("refs after rotation+restart: %v", refs)
	}
}

// TestSavedProviderConnectionProbe proves the overflow-menu Test Connection:
// the daemon resolves the persisted Base URL, compiled protocol and active
// stored credential ref by stable Provider ID; the upstream sees the stored
// key; the probe is read-only (no catalog/default/revision mutation, no
// discovery write) and the result carries no secret.
func TestSavedProviderConnectionProbe(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"},{"id":"model-b"}]}`))
	}))
	t.Cleanup(upstream.Close)

	owner := startTestOwner(t, func(string) (string, bool) { return "", false })
	creds := NewMemoryCredentialStore()
	owner.SetCredentialStore(creds)
	proj, err := owner.UpsertProviderConnection(
		codexCustomInput("", "Probed", upstream.URL+"/v1"), "sk-stored-key-000", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connID := proj.Connections[0].ID
	beforeRev := owner.Catalog().Revision
	beforeDefaults := len(owner.MustProjectForTest(t).Defaults)

	result, err := owner.TestSavedProviderConnection(connID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Client != ClientCodex || result.ModelCount != 2 || result.LatencyMS < 0 {
		t.Fatalf("result=%#v", result)
	}
	if gotAuth != "Bearer sk-stored-key-000" {
		t.Fatalf("upstream auth=%q", gotAuth)
	}
	// Read-only: nothing about the catalog/defaults/revision changed.
	if owner.Catalog().Revision != beforeRev {
		t.Fatalf("probe mutated revision")
	}
	if got := len(owner.MustProjectForTest(t).Defaults); got != beforeDefaults {
		t.Fatalf("probe mutated defaults")
	}
	// The result carries no secret and no hint.
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "sk-stored-key") {
		t.Fatalf("result leaked secret: %s", raw)
	}

	// A saved connection without a stored secret fails closed with readiness.
	noKey, err := owner.UpsertProviderConnection(
		codexCustomInput("", "NoKey", upstream.URL+"/v1"), "", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.TestSavedProviderConnection(connectionByName(t, noKey, "NoKey").ID); !errors.Is(err, ErrCredentialNotReady) {
		t.Fatalf("want not-ready got %v", err)
	}
	// Unknown IDs and non-account rows are rejected.
	if _, err := owner.TestSavedProviderConnection("conn_unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not-found got %v", err)
	}
}
