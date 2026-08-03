package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
)

const reportedLargeUploadBytes int64 = 1_532_564_736

func TestUploadPolicyAdmitsReportedLargeFile(t *testing.T) {
	if maxUploadFileBytes != 2<<30 {
		t.Fatalf("file limit = %d, want %d", maxUploadFileBytes, int64(2<<30))
	}
	if maxUploadStoreBytes != 8<<30 {
		t.Fatalf("store limit = %d, want %d", maxUploadStoreBytes, int64(8<<30))
	}
	if maxUploadFileBytes < reportedLargeUploadBytes {
		t.Fatalf("file limit = %d, want at least %d", maxUploadFileBytes, reportedLargeUploadBytes)
	}
	if maxUploadStoreBytes < reportedLargeUploadBytes {
		t.Fatalf("store limit = %d, want at least %d", maxUploadStoreBytes, reportedLargeUploadBytes)
	}
}

func TestUploadAcceptsUnknownLengthRawStreamBeyondSyntheticFormerLimit(t *testing.T) {
	stateDir := t.TempDir()
	deviceID := "device-beyond-former-limit"
	authManager, privateKey := uploadAuthFixture(t, stateDir, deviceID)
	server := New(authManager, nil, nil, nil, nil, nil, nil)
	// Exercise the streaming boundary with scaled limits. Production-size
	// policy is asserted above without leaving a multi-GiB fixture.
	const syntheticFormerLimit = 2 << 20
	wantSize := int64(syntheticFormerLimit + (1 << 20))
	reader, writeDone := rawUploadPipe(wantSize)

	request := rawUploadRequest(t, authManager, privateKey, deviceID, reader, "larger-than-before.bin", "application/octet-stream")
	if request.ContentLength != -1 {
		t.Fatalf("content length = %d, want unknown", request.ContentLength)
	}
	response := httptest.NewRecorder()
	server.handleUploadWithLimits(response, request, uploadLimits{
		fileBytes:  4 << 20,
		storeBytes: 8 << 20,
		retention:  uploadRetention,
	})
	_ = reader.Close()
	writeErr := <-writeDone
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s writer=%v", response.Code, response.Body.String(), writeErr)
	}
	if writeErr != nil {
		t.Fatal(writeErr)
	}

	payload := decodeUploadResponse(t, response)
	info, err := os.Stat(payload["path"])
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != wantSize {
		t.Fatalf("upload size = %d, want %d", info.Size(), wantSize)
	}
}

func TestUploadStreamsRawBodyWithUTF8PathLikeNameAndMode(t *testing.T) {
	stateDir := t.TempDir()
	deviceID := "device-upload"
	authManager, privateKey := uploadAuthFixture(t, stateDir, deviceID)
	server := New(authManager, nil, nil, nil, nil, nil, nil)
	originalName := "../../报告 2026.ZIP"
	body := bytes.NewBufferString("durable raw upload")
	request := rawUploadRequest(t, authManager, privateKey, deviceID, body, originalName, "application/zip")
	response := httptest.NewRecorder()
	server.handleUpload(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}

	payload := decodeUploadResponse(t, response)
	wantRoot := filepath.Join(stateDir, "uploads") + string(filepath.Separator)
	if !strings.HasPrefix(payload["path"], wantRoot) || !strings.HasSuffix(payload["path"], ".zip") {
		t.Fatalf("upload path = %q, want under %q with sanitized extension", payload["path"], wantRoot)
	}
	if payload["name"] != originalName {
		t.Fatalf("original name = %q, want %q", payload["name"], originalName)
	}
	raw, err := os.ReadFile(payload["path"])
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "durable raw upload" {
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

func TestUploadRejectsInvalidMetadataWithoutReadingBody(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		headerValues []string
		contentType  string
		wantBody     string
	}{
		{name: "missing name", contentType: "application/octet-stream", wantBody: "upload filename header is required"},
		{name: "duplicate name", headerValues: []string{"one.txt", "two.txt"}, contentType: "text/plain", wantBody: "upload filename header must appear once"},
		{name: "malformed encoding", headerValues: []string{"bad%GGname"}, contentType: "text/plain", wantBody: "upload filename header is invalid"},
		{name: "invalid utf8", headerValues: []string{"%FF.txt"}, contentType: "text/plain", wantBody: "upload filename header is invalid"},
		{name: "control character", headerValues: []string{"bad%0Aname.txt"}, contentType: "text/plain", wantBody: "upload filename header is invalid"},
		{name: "encoded name too long", headerValues: []string{strings.Repeat("a", maxUploadNameHeaderBytes+1)}, contentType: "text/plain", wantBody: "upload filename header is too long"},
		{name: "missing content type", headerValues: []string{"notes.txt"}, wantBody: "Content-Type header is required"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stateDir := t.TempDir()
			deviceID := "device-metadata-" + strings.ReplaceAll(testCase.name, " ", "-")
			authManager, privateKey := uploadAuthFixture(t, stateDir, deviceID)
			server := New(authManager, nil, nil, nil, nil, nil, nil)
			body := &countingReadCloser{}
			request := httptest.NewRequest(http.MethodPost, "/upload", body)
			request.ContentLength = 1
			request.Header.Set("Authorization", calendarAuthHeader(privateKey, authManager.DaemonID(), deviceID, "zen-upload"))
			for _, value := range testCase.headerValues {
				request.Header.Add(uploadNameHeader, value)
			}
			if testCase.contentType != "" {
				request.Header.Set("Content-Type", testCase.contentType)
			}
			response := httptest.NewRecorder()
			server.handleUpload(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), testCase.wantBody) {
				t.Fatalf("body = %q, want %q", response.Body.String(), testCase.wantBody)
			}
			if body.reads != 0 {
				t.Fatalf("body reads = %d, want 0", body.reads)
			}
			if _, err := os.Stat(server.uploadDir); !os.IsNotExist(err) {
				t.Fatalf("invalid metadata created upload storage: %v", err)
			}
		})
	}
}

func TestUploadRejectsKnownOversizeRawBodyWithoutReading(t *testing.T) {
	stateDir := t.TempDir()
	deviceID := "device-known-oversize"
	authManager, privateKey := uploadAuthFixture(t, stateDir, deviceID)
	server := New(authManager, nil, nil, nil, nil, nil, nil)
	limits := smallUploadLimits()
	body := &countingReadCloser{}
	request := rawUploadRequest(t, authManager, privateKey, deviceID, body, "large.bin", "application/octet-stream")
	request.ContentLength = limits.fileBytes + 1
	response := httptest.NewRecorder()
	server.handleUploadWithLimits(response, request, limits)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "1 MiB file limit") {
		t.Fatalf("body = %q, want configured file limit", response.Body.String())
	}
	if body.reads != 0 {
		t.Fatalf("oversize body reads = %d, want 0", body.reads)
	}
	if _, err := os.Stat(server.uploadDir); !os.IsNotExist(err) {
		t.Fatalf("known oversize upload created storage: %v", err)
	}
}

func TestUploadRejectsDeclaredLengthMismatchWithoutFile(t *testing.T) {
	stateDir := t.TempDir()
	deviceID := "device-length-mismatch"
	authManager, privateKey := uploadAuthFixture(t, stateDir, deviceID)
	server := New(authManager, nil, nil, nil, nil, nil, nil)
	body := bytes.NewBufferString("short")
	request := rawUploadRequest(t, authManager, privateKey, deviceID, body, "short.txt", "text/plain")
	request.ContentLength = int64(body.Len() + 1)
	response := httptest.NewRecorder()
	server.handleUpload(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "does not match Content-Length") {
		t.Fatalf("body = %q, want length mismatch", response.Body.String())
	}
	assertUploadDirEntries(t, server.uploadDir, nil)
}

func TestUploadRejectsUnknownLengthOversizeRawStreamWithoutFile(t *testing.T) {
	stateDir := t.TempDir()
	deviceID := "device-unknown-oversize"
	authManager, privateKey := uploadAuthFixture(t, stateDir, deviceID)
	server := New(authManager, nil, nil, nil, nil, nil, nil)
	limits := smallUploadLimits()
	reader, writeDone := rawUploadPipe(limits.fileBytes + (1 << 20))
	request := rawUploadRequest(t, authManager, privateKey, deviceID, reader, "large.bin", "application/octet-stream")
	response := httptest.NewRecorder()
	server.handleUploadWithLimits(response, request, limits)
	_ = reader.Close()
	<-writeDone
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "1 MiB file limit") {
		t.Fatalf("body = %q, want configured file limit", response.Body.String())
	}
	assertUploadDirEntries(t, server.uploadDir, nil)
}

func TestUploadRejectsRemainingStoreForKnownAndUnknownBodies(t *testing.T) {
	for _, knownLength := range []bool{true, false} {
		name := "unknown length"
		if knownLength {
			name = "known length"
		}
		t.Run(name, func(t *testing.T) {
			stateDir := t.TempDir()
			deviceID := "device-store-" + strings.ReplaceAll(name, " ", "-")
			authManager, privateKey := uploadAuthFixture(t, stateDir, deviceID)
			server := New(authManager, nil, nil, nil, nil, nil, nil)
			limits := uploadLimits{fileBytes: 1 << 20, storeBytes: 1 << 20, retention: uploadRetention}
			if err := os.MkdirAll(server.uploadDir, 0o700); err != nil {
				t.Fatal(err)
			}
			existingPath := filepath.Join(server.uploadDir, "existing.bin")
			if err := os.WriteFile(existingPath, make([]byte, 768<<10), 0o600); err != nil {
				t.Fatal(err)
			}

			var body io.Reader
			var reader *io.PipeReader
			var writeDone <-chan error
			countedBody := &countingReadCloser{}
			if knownLength {
				body = countedBody
			} else {
				reader, writeDone = rawUploadPipe(512 << 10)
				body = reader
			}
			request := rawUploadRequest(t, authManager, privateKey, deviceID, body, "does-not-fit.bin", "application/octet-stream")
			if knownLength {
				request.ContentLength = 512 << 10
			}
			response := httptest.NewRecorder()
			server.handleUploadWithLimits(response, request, limits)
			if reader != nil {
				_ = reader.Close()
				<-writeDone
			}
			if response.Code != http.StatusInsufficientStorage {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), "remaining storage capacity") {
				t.Fatalf("body = %q, want remaining capacity error", response.Body.String())
			}
			if knownLength && countedBody.reads != 0 {
				t.Fatalf("known body reads = %d, want 0", countedBody.reads)
			}
			assertUploadDirEntries(t, server.uploadDir, []string{filepath.Base(existingPath)})
		})
	}
}

func TestUploadReadFailureAndDisconnectLeaveNoFile(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
	}{
		{name: "read failure", err: io.ErrUnexpectedEOF},
		{name: "disconnect", err: context.Canceled},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stateDir := t.TempDir()
			deviceID := "device-" + strings.ReplaceAll(testCase.name, " ", "-")
			authManager, privateKey := uploadAuthFixture(t, stateDir, deviceID)
			server := New(authManager, nil, nil, nil, nil, nil, nil)
			body := io.MultiReader(strings.NewReader("partial contents"), readError{err: testCase.err})
			request := rawUploadRequest(t, authManager, privateKey, deviceID, body, "partial.bin", "application/octet-stream")
			response := httptest.NewRecorder()
			server.handleUpload(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			assertUploadDirEntries(t, server.uploadDir, nil)
		})
	}
}

func TestUploadCancelledContextDoesNotFinalizeFile(t *testing.T) {
	stateDir := t.TempDir()
	deviceID := "device-cancelled-context"
	authManager, privateKey := uploadAuthFixture(t, stateDir, deviceID)
	server := New(authManager, nil, nil, nil, nil, nil, nil)
	body := bytes.NewBufferString("complete body on a cancelled request")
	request := rawUploadRequest(t, authManager, privateKey, deviceID, body, "cancelled.bin", "application/octet-stream")
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()
	server.handleUpload(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	assertUploadDirEntries(t, server.uploadDir, nil)
}

func TestCleanupUploadStoreRemovesExpiredAndOrphanPartialFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	current := filepath.Join(dir, "current.txt")
	expired := filepath.Join(dir, "expired.txt")
	partial := filepath.Join(dir, ".upload-orphan.partial")
	for path, value := range map[string]string{current: "current", expired: "expired", partial: "partial"} {
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
	total, err := cleanupUploadStore(dir, now, productionUploadLimits())
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
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("partial upload still exists: %v", err)
	}
}

func TestSafeUploadExtensionRejectsPathAndCompoundPunctuation(t *testing.T) {
	for name, want := range map[string]string{
		"photo.PNG":                ".png",
		"../../报告 2026.ZIP":        ".zip",
		`..\..\windows-secret.ENV`: ".env",
		"archive.tar.gz":           ".gz",
		"script.user.js~":          "",
		"extension.toolongextxx":   "",
	} {
		if got := safeUploadExtension(name); got != want {
			t.Fatalf("safeUploadExtension(%q) = %q, want %q", name, got, want)
		}
	}
}

func rawUploadRequest(
	t *testing.T,
	manager *auth.Manager,
	privateKey ed25519.PrivateKey,
	deviceID string,
	body io.Reader,
	originalName string,
	contentType string,
) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/upload", body)
	request.Header.Set("Authorization", calendarAuthHeader(privateKey, manager.DaemonID(), deviceID, "zen-upload"))
	request.Header.Set(uploadNameHeader, url.PathEscape(originalName))
	request.Header.Set("Content-Type", contentType)
	return request
}

func rawUploadPipe(size int64) (*io.PipeReader, <-chan error) {
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := io.CopyN(writer, zeroReader{}, size)
		if closeErr := writer.CloseWithError(err); err == nil {
			err = closeErr
		}
		done <- err
	}()
	return reader, done
}

func decodeUploadResponse(t *testing.T, response *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func assertUploadDirEntries(t *testing.T, dir string, want []string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("upload directory entries = %q, want %q", got, want)
	}
}

func smallUploadLimits() uploadLimits {
	return uploadLimits{fileBytes: 1 << 20, storeBytes: 4 << 20, retention: uploadRetention}
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

type readError struct {
	err error
}

func (reader readError) Read([]byte) (int, error) {
	return 0, reader.err
}

type countingReadCloser struct {
	reads int
}

func (reader *countingReadCloser) Read([]byte) (int, error) {
	reader.reads++
	return 0, io.EOF
}

func (*countingReadCloser) Close() error {
	return nil
}
