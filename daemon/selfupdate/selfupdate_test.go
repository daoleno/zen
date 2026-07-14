package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSemVersionPrecedenceSingleStream(t *testing.T) {
	ordered := []string{
		"0.1.0-beta.2",
		"0.1.0-beta.3",
		"0.1.0-beta.10",
		"0.1.0-rc.1",
		"0.1.0",
		"0.1.1-alpha.1",
	}
	for i := 1; i < len(ordered); i++ {
		left, err := parseSemVersion(ordered[i-1])
		if err != nil {
			t.Fatal(err)
		}
		right, err := parseSemVersion(ordered[i])
		if err != nil {
			t.Fatal(err)
		}
		if compareSemVersion(left, right) >= 0 {
			t.Fatalf("%s must precede %s", ordered[i-1], ordered[i])
		}
	}
}

func TestPlatformArtifactName(t *testing.T) {
	tests := map[string]string{
		"linux/amd64":  "zen-linux-amd64.tar.gz",
		"linux/arm64":  "zen-linux-arm64.tar.gz",
		"darwin/arm64": "zen-darwin-arm64.tar.gz",
	}
	for platform, want := range tests {
		parts := strings.Split(platform, "/")
		got, err := PlatformArtifactName(parts[0], parts[1])
		if err != nil || got != want {
			t.Fatalf("PlatformArtifactName(%s) = %q, %v; want %q", platform, got, err, want)
		}
	}
	if _, err := PlatformArtifactName("windows", "amd64"); err == nil {
		t.Fatal("windows/amd64 unexpectedly supported")
	}
}

func TestVerifyManifestSignatureAndSchema(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schema_version":2,"product":"zen","version":"1.2.3-beta.1","artifacts":[]}`)
	signature := ed25519.Sign(privateKey, raw)
	manifest, err := VerifyManifest(raw, signature, publicKey)
	if err != nil || manifest.Version != "1.2.3-beta.1" {
		t.Fatalf("VerifyManifest() = %#v, %v", manifest, err)
	}
	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)-2] ^= 1
	if _, err := VerifyManifest(tampered, signature, publicKey); err == nil {
		t.Fatal("tampered manifest verified")
	}
	signature[0] ^= 1
	if _, err := VerifyManifest(raw, signature, publicKey); err == nil {
		t.Fatal("tampered signature verified")
	}
}

func TestLatestSelectsHighestUpdaterEligiblePrerelease(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifests := map[string][]byte{}
	signatures := map[string][]byte{}
	for _, version := range []string{"0.1.0-beta.4", "0.1.0-beta.10"} {
		manifest := Manifest{
			SchemaVersion: 2,
			Product:       "zen",
			Version:       version,
			Artifacts: []Artifact{{
				Path: "zen-linux-amd64.tar.gz", Role: "daemon_archive", SHA256: strings.Repeat("a", 64), Size: 42, GOOS: "linux", GOARCH: "amd64",
			}},
		}
		raw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		manifests[version] = raw
		signatures[version] = ed25519.Sign(privateKey, raw)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		switch r.URL.Path {
		case "/releases":
			_ = json.NewEncoder(w).Encode([]release{
				testRelease("0.1.0-beta.4", true, base),
				testRelease("0.1.0-beta.10", true, base),
			})
		case "/0.1.0-beta.4/manifest":
			_, _ = w.Write(manifests["0.1.0-beta.4"])
		case "/0.1.0-beta.4/signature":
			_, _ = w.Write(signatures["0.1.0-beta.4"])
		case "/0.1.0-beta.10/manifest":
			_, _ = w.Write(manifests["0.1.0-beta.10"])
		case "/0.1.0-beta.10/signature":
			_, _ = w.Write(signatures["0.1.0-beta.10"])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &Client{HTTPClient: server.Client(), APIURL: server.URL + "/releases", PublicKey: publicKey, GOOS: "linux", GOARCH: "amd64"}
	candidate, err := client.Latest(context.Background(), "0.1.0-beta.3")
	if err != nil {
		t.Fatal(err)
	}
	if candidate == nil || candidate.Version != "0.1.0-beta.10" || !candidate.Prerelease {
		t.Fatalf("candidate = %#v", candidate)
	}
}

func TestDownloadBinaryRejectsArtifactChecksumMismatch(t *testing.T) {
	archive := []byte("not the authenticated archive")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	client := &Client{HTTPClient: server.Client()}
	_, err := client.DownloadBinary(context.Background(), Candidate{
		ArtifactURL: server.URL,
		Artifact: Artifact{
			Path: "zen-linux-amd64.tar.gz", Size: int64(len(archive)), SHA256: strings.Repeat("0", 64),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("DownloadBinary() error = %v", err)
	}
}

func testRelease(version string, prerelease bool, base string) release {
	prefix := base + "/" + version
	return release{
		TagName: "v" + version, Prerelease: prerelease,
		Assets: []releaseAsset{
			{Name: ManifestAsset, URL: prefix + "/manifest"},
			{Name: ManifestSignature, URL: prefix + "/signature"},
			{Name: "zen-linux-amd64.tar.gz", URL: prefix + "/archive"},
		},
	}
}

func TestReplaceExecutableRenameFailurePreservesOriginal(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "zen")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	originalRename := renameFile
	renameFile = func(string, string) error { return errors.New("injected rename failure") }
	t.Cleanup(func() { renameFile = originalRename })
	if err := ReplaceExecutable(target, []byte("new")); err == nil {
		t.Fatal("ReplaceExecutable unexpectedly succeeded")
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "old" {
		t.Fatalf("original executable changed to %q", raw)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".zen-update-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files remain: %v, %v", matches, err)
	}
}

func TestCacheFreshnessAndNotice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-check.json")
	now := time.Date(2026, 7, 14, 1, 2, 3, 0, time.UTC)
	if err := WriteCache(path, Cache{CheckedAt: now, LatestVersion: "0.1.0-beta.10"}); err != nil {
		t.Fatal(err)
	}
	cache, fresh := ReadCache(path, now.Add(CacheTTL-time.Second))
	if !fresh || cache.LatestVersion != "0.1.0-beta.10" {
		t.Fatalf("fresh cache = %#v, %v", cache, fresh)
	}
	if _, fresh := ReadCache(path, now.Add(CacheTTL+time.Second)); fresh {
		t.Fatal("expired cache reported fresh")
	}
	if got := NoticeLine("0.1.0-beta.3", "0.1.0-beta.10"); got != "Zen 0.1.0-beta.10 is available; run: zen update" {
		t.Fatalf("notice = %q", got)
	}
	if got := NoticeLine("0.1.0", "0.1.0-beta.10"); got != "" {
		t.Fatalf("downgrade notice = %q", got)
	}
}
