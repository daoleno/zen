package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
)

func TestUploadStreamsIntoBoundedStateStorage(t *testing.T) {
	stateDir := t.TempDir()
	authManager, privateKey := uploadAuthFixture(t, stateDir, "device-upload")
	server := New(authManager, nil, nil, nil, nil, nil, nil)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "Notes.TXT")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "durable upload"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-upload", "zen-upload"))
	response := httptest.NewRecorder()
	server.handleUpload(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(stateDir, "uploads") + string(filepath.Separator)
	if !strings.HasPrefix(payload["path"], wantRoot) || !strings.HasSuffix(payload["path"], ".txt") {
		t.Fatalf("upload path = %q, want under %q with sanitized extension", payload["path"], wantRoot)
	}
	if payload["name"] != "Notes.TXT" {
		t.Fatalf("original name = %q", payload["name"])
	}
	raw, err := os.ReadFile(payload["path"])
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "durable upload" {
		t.Fatalf("upload contents = %q", raw)
	}
	info, err := os.Stat(payload["path"])
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("upload mode = %o, want 600", info.Mode().Perm())
	}
}

func TestUploadRejectsOversizeStreamWithoutLeavingFile(t *testing.T) {
	stateDir := t.TempDir()
	authManager, privateKey := uploadAuthFixture(t, stateDir, "device-large-upload")
	server := New(authManager, nil, nil, nil, nil, nil, nil)

	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	writeDone := make(chan error, 1)
	go func() {
		part, err := multipartWriter.CreateFormFile("file", "large.bin")
		if err == nil {
			_, err = io.Copy(part, io.LimitReader(zeroReader{}, maxUploadFileBytes+(2<<20)))
		}
		if closeErr := multipartWriter.Close(); err == nil {
			err = closeErr
		}
		if closeErr := writer.CloseWithError(err); err == nil {
			err = closeErr
		}
		writeDone <- err
	}()
	request := httptest.NewRequest(http.MethodPost, "/upload", reader)
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	request.Header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), "device-large-upload", "zen-upload"))
	response := httptest.NewRecorder()
	server.handleUpload(response, request)
	_ = reader.Close()
	<-writeDone
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("oversize upload left files: %+v", entries)
	}
}

func TestCleanupUploadStoreRemovesExpiredRegularFilesOnly(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	current := filepath.Join(dir, "current.txt")
	expired := filepath.Join(dir, "expired.txt")
	for path, value := range map[string]string{current: "current", expired: "expired"} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(current, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(expired, now.Add(-uploadRetention-time.Hour), now.Add(-uploadRetention-time.Hour)); err != nil {
		t.Fatal(err)
	}
	total, err := cleanupUploadStore(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if total != int64(len("current")) {
		t.Fatalf("stored bytes = %d", total)
	}
	if _, err := os.Stat(expired); !os.IsNotExist(err) {
		t.Fatalf("expired upload still exists: %v", err)
	}
	if _, err := os.Stat(current); err != nil {
		t.Fatalf("current upload was removed: %v", err)
	}
}

func TestSafeUploadExtensionRejectsPathAndCompoundPunctuation(t *testing.T) {
	for name, want := range map[string]string{
		"photo.PNG":              ".png",
		"../../secret.env":       ".env",
		"archive.tar.gz":         ".gz",
		"script.user.js~":        "",
		"extension.toolongextxx": "",
	} {
		if got := safeUploadExtension(name); got != want {
			t.Fatalf("safeUploadExtension(%q) = %q, want %q", name, got, want)
		}
	}
}

func uploadAuthFixture(t *testing.T, stateDir, deviceID string) (*auth.Manager, ed25519.PrivateKey) {
	t.Helper()
	manager, err := auth.NewManager(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(), deviceID, "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	return manager, privateKey
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}
