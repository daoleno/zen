package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/classifier"
)

func TestSessionFilePreviewResolvesAbsoluteRelativeAndSymlinkAliases(t *testing.T) {
	hostRoot := t.TempDir()
	sessionRepo := filepath.Join(hostRoot, "session-repo")
	otherRepo := filepath.Join(hostRoot, "other-repo")
	inside := filepath.Join(sessionRepo, "docs", "notes.md")
	crossRepo := filepath.Join(otherRepo, "shared.md")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(crossRepo), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("# Notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crossRepo, []byte("# Shared\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(crossRepo, filepath.Join(sessionRepo, "shared-alias.md")); err != nil {
		t.Fatal(err)
	}
	sessionAlias := filepath.Join(hostRoot, "session-alias")
	if err := os.Symlink(sessionRepo, sessionAlias); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		cwd          string
		reference    string
		wantPath     string
		wantRelative string
	}{
		{
			name:         "relative path from live CWD",
			cwd:          sessionRepo,
			reference:    filepath.Join("docs", "notes.md"),
			wantPath:     inside,
			wantRelative: "docs/notes.md",
		},
		{
			name:         "relative path from symlinked live CWD",
			cwd:          sessionAlias,
			reference:    filepath.Join("docs", "notes.md"),
			wantPath:     inside,
			wantRelative: "docs/notes.md",
		},
		{
			name:         "absolute cross-repository path",
			cwd:          sessionRepo,
			reference:    crossRepo,
			wantPath:     crossRepo,
			wantRelative: "../other-repo/shared.md",
		},
		{
			name:         "relative cross-repository path",
			cwd:          sessionRepo,
			reference:    filepath.Join("..", "other-repo", "shared.md"),
			wantPath:     crossRepo,
			wantRelative: "../other-repo/shared.md",
		},
		{
			name:         "symlink alias to cross-repository path",
			cwd:          sessionRepo,
			reference:    "shared-alias.md",
			wantPath:     crossRepo,
			wantRelative: "../other-repo/shared.md",
		},
	}

	var crossRepoGeneration string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := openSessionFile(tt.cwd, tt.reference)
			if err != nil {
				t.Fatal(err)
			}
			defer resolved.file.Close()
			metadata := resolved.metadata()
			if metadata.Path != tt.wantPath {
				t.Fatalf("canonical path = %q, want %q", metadata.Path, tt.wantPath)
			}
			if metadata.RelativePath != tt.wantRelative {
				t.Fatalf("relative path = %q, want %q", metadata.RelativePath, tt.wantRelative)
			}
			if tt.wantPath == crossRepo {
				if crossRepoGeneration == "" {
					crossRepoGeneration = metadata.Generation
				} else if metadata.Generation != crossRepoGeneration {
					t.Fatalf("alias generation = %q, want %q", metadata.Generation, crossRepoGeneration)
				}
			}
		})
	}
}

func TestSessionFilePreviewRejectsMissingAndNonRegularFiles(t *testing.T) {
	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, reference := range []string{"docs", "missing.txt"} {
		resolved, err := openSessionFile(cwd, reference)
		if err != nil {
			if reference == "missing.txt" && !strings.Contains(err.Error(), "missing") {
				t.Fatalf("missing error = %q", err)
			}
			continue
		}
		_ = resolved.file.Close()
		t.Fatalf("open %q succeeded despite the regular-file contract", reference)
	}
}

func TestSessionFilePreviewClassifiesSupportedAndUnsupportedRenderers(t *testing.T) {
	for name, want := range map[string]string{
		"README.md":   "markdown",
		"main.go":     "text",
		"data.json":   "text",
		"config.yaml": "text",
		"server.log":  "text",
		"photo.png":   "image",
		"manual.pdf":  "pdf",
		"archive.zip": "unsupported",
	} {
		data := []byte("plain text")
		switch filepath.Ext(name) {
		case ".png":
			data = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
		case ".pdf":
			data = []byte("%PDF-1.7\n")
		case ".zip":
			data = []byte{'P', 'K', 3, 4, 0}
		}
		if got, _ := classifySessionFile(name, data); got != want {
			t.Fatalf("classify %s = %q, want %q", name, got, want)
		}
	}
}

func TestSessionFileTextReadIsBoundedAndGenerationChecked(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "large.log")
	payload := bytes.Repeat([]byte("x"), maxSessionFileTextBytes+128)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := openSessionFile(workspace, "large.log")
	if err != nil {
		t.Fatal(err)
	}
	generation := resolved.generation
	preview, err := readSessionFileText(resolved, generation)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Truncated || len(preview.Content) != maxSessionFileTextBytes {
		t.Fatalf("preview bytes=%d truncated=%v", len(preview.Content), preview.Truncated)
	}

	if err := os.WriteFile(path, []byte("changed generation"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := openSessionFile(workspace, "large.log")
	if err != nil {
		t.Fatal(err)
	}
	defer changed.file.Close()
	if _, err := readSessionFileText(changed, generation); !isSessionFileChanged(err) {
		t.Fatalf("changed read error = %v, want changed generation", err)
	}
}

func TestSessionFilePreviewRejectsStaleSessionIdentity(t *testing.T) {
	started := time.Date(2026, 7, 20, 4, 0, 0, 123_000_000, time.UTC)
	agent := &classifier.Agent{
		ID:        "main:@7",
		Cwd:       "/repo/zen",
		ProcessID: 412,
		StartedAt: started,
	}
	valid := clientMessage{
		AgentID:   agent.ID,
		ProcessID: agent.ProcessID,
		StartedAt: startedAtRaw(started),
	}
	if err := validateSessionFileIdentity(agent, valid); err != nil {
		t.Fatalf("valid identity: %v", err)
	}
	staleAgent := valid
	staleAgent.AgentID = "main:@8"
	if err := validateSessionFileIdentity(agent, staleAgent); !isStaleSessionFileIdentity(err) {
		t.Fatalf("stale agent error = %v", err)
	}
	staleProcess := valid
	staleProcess.ProcessID++
	if err := validateSessionFileIdentity(agent, staleProcess); !isStaleSessionFileIdentity(err) {
		t.Fatalf("stale process error = %v", err)
	}
	staleStart := valid
	staleStart.StartedAt = startedAtRaw(started.Add(time.Millisecond))
	if err := validateSessionFileIdentity(agent, staleStart); !isStaleSessionFileIdentity(err) {
		t.Fatalf("stale start error = %v", err)
	}
}

func TestSessionFileBinaryHandlerAuthenticatesRangesAndDisablesCache(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "image.png")
	data := append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, bytes.Repeat([]byte{1}, 128)...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := openSessionFile(workspace, "image.png")
	if err != nil {
		t.Fatal(err)
	}
	generation := resolved.generation
	_ = resolved.file.Close()

	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	started := time.Date(2026, 7, 20, 4, 0, 0, 0, time.UTC)
	agent := &classifier.Agent{ID: "main:@7", Cwd: workspace, ProcessID: 412, StartedAt: started}
	server := New(manager, nil, nil, nil, nil, nil, nil)
	server.sessionFileAgentLoader = func(id string) *classifier.Agent {
		if id == agent.ID {
			copy := *agent
			return &copy
		}
		return nil
	}

	request := httptest.NewRequest(http.MethodGet, "/session-file", nil)
	query := request.URL.Query()
	query.Set("agent_id", agent.ID)
	query.Set("process_id", "412")
	query.Set("started_at", "178451?bad")
	query.Set("path", "image.png")
	query.Set("generation", generation)
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Authorization", sessionFileAuthorizationHeader(t, privateKey, manager.DaemonID(), deviceID))
	bad := httptest.NewRecorder()
	server.handleSessionFileBinary(bad, request)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("malformed identity status=%d body=%s", bad.Code, bad.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/session-file", nil)
	query = request.URL.Query()
	query.Set("agent_id", agent.ID)
	query.Set("process_id", "412")
	query.Set("path", "image.png")
	query.Set("generation", generation)
	query.Set("started_at", string(startedAtRaw(started)))
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Range", "bytes=8-15")
	request.Header.Set("Authorization", sessionFileAuthorizationHeader(t, privateKey, manager.DaemonID(), deviceID))
	response := httptest.NewRecorder()
	server.handleSessionFileBinary(response, request)
	if response.Code != http.StatusPartialContent {
		t.Fatalf("range status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("stream headers = %#v", response.Header())
	}
	if got := response.Body.Bytes(); !bytes.Equal(got, data[8:16]) {
		t.Fatalf("range bytes=%v want=%v", got, data[8:16])
	}

	outOfBoundsRequest := httptest.NewRequest(http.MethodGet, request.URL.String(), nil)
	outOfBoundsRequest.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", len(data), len(data)))
	outOfBoundsRequest.Header.Set("Authorization", sessionFileAuthorizationHeader(t, privateKey, manager.DaemonID(), deviceID))
	outOfBounds := httptest.NewRecorder()
	server.handleSessionFileBinary(outOfBounds, outOfBoundsRequest)
	if outOfBounds.Code != http.StatusRequestedRangeNotSatisfiable || outOfBounds.Header().Get("Content-Range") != fmt.Sprintf("bytes */%d", len(data)) {
		t.Fatalf("out-of-bounds range status=%d content-range=%q body=%q", outOfBounds.Code, outOfBounds.Header().Get("Content-Range"), outOfBounds.Body.String())
	}

	staleGenerationRequest := httptest.NewRequest(http.MethodGet, request.URL.String(), nil)
	staleQuery := staleGenerationRequest.URL.Query()
	staleQuery.Set("generation", "stale-generation")
	staleGenerationRequest.URL.RawQuery = staleQuery.Encode()
	staleGenerationRequest.Header.Set("Authorization", sessionFileAuthorizationHeader(t, privateKey, manager.DaemonID(), deviceID))
	staleGeneration := httptest.NewRecorder()
	server.handleSessionFileBinary(staleGeneration, staleGenerationRequest)
	if staleGeneration.Code != http.StatusConflict {
		t.Fatalf("stale generation status=%d body=%s", staleGeneration.Code, staleGeneration.Body.String())
	}

	agent.ProcessID++
	staleSessionRequest := httptest.NewRequest(http.MethodGet, request.URL.String(), nil)
	staleSessionRequest.Header.Set("Authorization", sessionFileAuthorizationHeader(t, privateKey, manager.DaemonID(), deviceID))
	staleSession := httptest.NewRecorder()
	server.handleSessionFileBinary(staleSession, staleSessionRequest)
	if staleSession.Code != http.StatusConflict {
		t.Fatalf("stale Session status=%d body=%s", staleSession.Code, staleSession.Body.String())
	}
	agent.ProcessID--

	unauthorized := httptest.NewRecorder()
	server.handleSessionFileBinary(unauthorized, httptest.NewRequest(http.MethodGet, request.URL.String(), nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}
}

func TestSessionFileBinaryPreviewSizeBoundary(t *testing.T) {
	workspace := t.TempDir()
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	started := time.Date(2026, 7, 20, 4, 0, 0, 0, time.UTC)
	agent := &classifier.Agent{ID: "main:@7", Cwd: workspace, ProcessID: 412, StartedAt: started}
	server := New(manager, nil, nil, nil, nil, nil, nil)
	server.sessionFileAgentLoader = func(id string) *classifier.Agent {
		if id != agent.ID {
			return nil
		}
		copy := *agent
		return &copy
	}

	tests := []struct {
		name     string
		size     int64
		tooLarge bool
		status   int
	}{
		{name: "just-below.pdf", size: maxSessionFileBinaryBytes - 1, status: http.StatusPartialContent},
		{name: "at.pdf", size: maxSessionFileBinaryBytes, status: http.StatusPartialContent},
		{name: "above.pdf", size: maxSessionFileBinaryBytes + 1, tooLarge: true, status: http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(workspace, tt.name)
			file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteString("%PDF-1.7\n"); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if err := file.Truncate(tt.size); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}

			resolved, err := openSessionFile(workspace, tt.name)
			if err != nil {
				t.Fatal(err)
			}
			metadata := resolved.metadata()
			generation := resolved.generation
			_ = resolved.file.Close()
			if metadata.TooLarge != tt.tooLarge {
				t.Fatalf("too_large=%v, want %v", metadata.TooLarge, tt.tooLarge)
			}
			if metadata.PreviewLimit != maxSessionFileBinaryBytes {
				t.Fatalf("preview_limit_bytes=%d, want %d", metadata.PreviewLimit, maxSessionFileBinaryBytes)
			}

			request := httptest.NewRequest(http.MethodGet, "/session-file", nil)
			query := request.URL.Query()
			query.Set("agent_id", agent.ID)
			query.Set("process_id", strconv.Itoa(agent.ProcessID))
			query.Set("started_at", string(startedAtRaw(started)))
			query.Set("path", tt.name)
			query.Set("generation", generation)
			request.URL.RawQuery = query.Encode()
			request.Header.Set("Range", "bytes=0-0")
			request.Header.Set("Authorization", sessionFileAuthorizationHeader(t, privateKey, manager.DaemonID(), deviceID))
			response := httptest.NewRecorder()
			server.handleSessionFileBinary(response, request)
			if response.Code != tt.status {
				t.Fatalf("status=%d body=%s, want %d", response.Code, response.Body.String(), tt.status)
			}
			if !tt.tooLarge && response.Body.Len() != 1 {
				t.Fatalf("bounded range response bytes=%d, want 1", response.Body.Len())
			}
		})
	}
}

func TestSessionFileBinaryReaderCannotGrowPastInspectedSize(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "growing.pdf")
	original := []byte("%PDF-1.7\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := openSessionFile(workspace, "growing.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer resolved.file.Close()

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(bytes.Repeat([]byte("x"), 1024)); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := io.ReadAll(boundedSessionFileBinaryReader(resolved))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("bounded reader returned %d bytes, want original %d", len(got), len(original))
	}
}

func startedAtRaw(value time.Time) []byte {
	return []byte(strconv.FormatInt(value.UnixMilli(), 10))
}

func sessionFileAuthFixture(t *testing.T) (*auth.Manager, ed25519.PrivateKey, string) {
	t.Helper()
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "session-file-device"
	token, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.EnrollDevice(token.Value, manager.DaemonID(), manager.PublicKeyHex(), deviceID, "Session file test", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	return manager, privateKey, deviceID
}

func sessionFileAuthorizationHeader(t *testing.T, privateKey ed25519.PrivateKey, daemonID, deviceID string) string {
	t.Helper()
	nonce := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatal(err)
	}
	timestamp := time.Now().UnixMilli()
	nonceHex := hex.EncodeToString(nonce)
	timestampText := strconv.FormatInt(timestamp, 10)
	payload := auth.BuildSignaturePayload(sessionFileAuthPurpose, daemonID, deviceID, timestampText, nonceHex)
	signature := ed25519.Sign(privateKey, payload)
	return auth.AuthorizationHeaderPrefix + "v1:" + deviceID + ":" + daemonID + ":" + timestampText + ":" + nonceHex + ":" + hex.EncodeToString(signature)
}
