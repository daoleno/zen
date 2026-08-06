package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	"unicode/utf8"

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
		if got, _ := classifySessionFile(name, data, false); got != want {
			t.Fatalf("classify %s = %q, want %q", name, got, want)
		}
	}
}

func TestSessionFilePreviewUTF8SniffBoundary(t *testing.T) {
	// Exact Free Ride shape: 512-byte sniff ends at lead byte e6 of 明 (e6 98 8e).
	const sniffSize = 512
	ming := []byte{0xe6, 0x98, 0x8e}
	prefix := bytes.Repeat([]byte("a"), sniffSize-1)
	prefix = append(prefix, ming[0])
	if len(prefix) != sniffSize {
		t.Fatalf("prefix len=%d, want %d", len(prefix), sniffSize)
	}
	if utf8.Valid(prefix) {
		t.Fatal("expected incomplete UTF-8 at the 512-byte sniff boundary")
	}

	truncatedFile := append(append([]byte{}, prefix...), ming[1:]...)
	truncatedFile = append(truncatedFile, []byte(" more markdown\n")...)
	if got, _ := classifySessionFile("report.md", prefix, true); got != "markdown" {
		t.Fatalf("truncated boundary classify = %q, want markdown", got)
	}

	workspace := t.TempDir()
	path := filepath.Join(workspace, "report.md")
	if err := os.WriteFile(path, truncatedFile, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := openSessionFile(workspace, "report.md")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.kind != "markdown" {
		_ = resolved.file.Close()
		t.Fatalf("open truncated boundary kind=%q, want markdown", resolved.kind)
	}
	preview, err := readSessionFileText(resolved, resolved.generation)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.Content, "明") {
		t.Fatalf("text preview missing restored rune: %q", preview.Content[len(preview.Content)-32:])
	}

	if got, _ := classifySessionFile("report.md", prefix, false); got != "unsupported" {
		t.Fatalf("EOF incomplete classify = %q, want unsupported", got)
	}
	eofPath := filepath.Join(workspace, "eof-incomplete.md")
	if err := os.WriteFile(eofPath, prefix, 0o600); err != nil {
		t.Fatal(err)
	}
	eofResolved, err := openSessionFile(workspace, "eof-incomplete.md")
	if err != nil {
		t.Fatal(err)
	}
	defer eofResolved.file.Close()
	if eofResolved.kind != "unsupported" {
		t.Fatalf("EOF incomplete kind=%q, want unsupported", eofResolved.kind)
	}

	interior := append([]byte{}, prefix[:sniffSize-2]...)
	interior = append(interior, 0xff, 'x')
	if got, _ := classifySessionFile("bad.md", interior, true); got != "unsupported" {
		t.Fatalf("interior invalid truncated classify = %q, want unsupported", got)
	}
	if got, _ := classifySessionFile("bad.md", interior, false); got != "unsupported" {
		t.Fatalf("interior invalid EOF classify = %q, want unsupported", got)
	}

	withNUL := append([]byte("hello"), 0)
	withNUL = append(withNUL, []byte("world")...)
	if got, _ := classifySessionFile("nul.md", withNUL, true); got != "unsupported" {
		t.Fatalf("NUL classify = %q, want unsupported", got)
	}

	for _, tt := range []struct {
		name      string
		suffix    []byte
		truncated bool
		want      string
	}{
		{name: "one-byte partial of 2-byte rune", suffix: []byte{0xc2}, truncated: true, want: "markdown"},
		{name: "one-byte partial of 2-byte rune at EOF", suffix: []byte{0xc2}, truncated: false, want: "unsupported"},
		{name: "one-byte partial of 3-byte rune", suffix: []byte{0xe6}, truncated: true, want: "markdown"},
		{name: "two-byte partial of 3-byte rune", suffix: []byte{0xe6, 0x98}, truncated: true, want: "markdown"},
		{name: "two-byte partial of 3-byte rune at EOF", suffix: []byte{0xe6, 0x98}, truncated: false, want: "unsupported"},
		{name: "one-byte partial of 4-byte rune", suffix: []byte{0xf0}, truncated: true, want: "markdown"},
		{name: "three-byte partial of 4-byte rune", suffix: []byte{0xf0, 0x9f, 0x98}, truncated: true, want: "markdown"},
		{name: "overlong lead C0", suffix: []byte{0xc0}, truncated: true, want: "unsupported"},
		{name: "overlong lead C1", suffix: []byte{0xc1}, truncated: true, want: "unsupported"},
		{name: "out-of-range lead F5", suffix: []byte{0xf5}, truncated: true, want: "unsupported"},
		{name: "out-of-range lead F6", suffix: []byte{0xf6}, truncated: true, want: "unsupported"},
		{name: "out-of-range lead F7", suffix: []byte{0xf7}, truncated: true, want: "unsupported"},
		{name: "stray continuation", suffix: []byte{0x80}, truncated: true, want: "unsupported"},
		{name: "overlong E0 80", suffix: []byte{0xe0, 0x80}, truncated: true, want: "unsupported"},
		{name: "surrogate ED A0", suffix: []byte{0xed, 0xa0}, truncated: true, want: "unsupported"},
		{name: "overlong F0 80", suffix: []byte{0xf0, 0x80}, truncated: true, want: "unsupported"},
		{name: "out-of-range F4 90", suffix: []byte{0xf4, 0x90}, truncated: true, want: "unsupported"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sample := append(bytes.Repeat([]byte("c"), sniffSize-len(tt.suffix)), tt.suffix...)
			if len(sample) != sniffSize {
				t.Fatalf("sample len=%d, want %d", len(sample), sniffSize)
			}
			if got, _ := classifySessionFile("case.md", sample, tt.truncated); got != tt.want {
				t.Fatalf("classify = %q, want %q (FullRune=%v Valid=%v)", got, tt.want, utf8.FullRune(tt.suffix), utf8.Valid(sample))
			}
		})
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

func TestSessionFileReadCapabilitySupportsGETHEADRangeAndRetry(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "image.png")
	data := append(
		[]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
		bytes.Repeat([]byte{2}, 128)...,
	)
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
	agent := &classifier.Agent{
		ID:        "main:@capability",
		Cwd:       workspace,
		ProcessID: 512,
		StartedAt: started,
	}
	server := New(manager, nil, nil, nil, nil, nil, nil)
	server.sessionFileAgentLoader = func(id string) *classifier.Agent {
		if id != agent.ID {
			return nil
		}
		copy := *agent
		return &copy
	}

	requestBody, err := json.Marshal(map[string]any{
		"agent_id":   agent.ID,
		"process_id": agent.ProcessID,
		"started_at": started.UnixMilli(),
		"path":       "image.png",
		"generation": generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	issueHeader := sessionFileAuthorizationHeader(
		t,
		privateKey,
		manager.DaemonID(),
		deviceID,
	)
	issue := httptest.NewRequest(
		http.MethodPost,
		"/session-file-capability",
		bytes.NewReader(requestBody),
	)
	issue.Header.Set("Authorization", issueHeader)
	issue.Header.Set("Content-Type", "application/json")
	issueResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(issueResponse, issue)
	if issueResponse.Code != http.StatusOK {
		t.Fatalf(
			"capability issue status=%d body=%q",
			issueResponse.Code,
			issueResponse.Body.String(),
		)
	}
	var capability struct {
		Version       int    `json:"version"`
		DeviceID      string `json:"device_id"`
		ExpiresAtMS   int64  `json:"expires_at_ms"`
		GETSignature  string `json:"get_signature"`
		HEADSignature string `json:"head_signature"`
	}
	if err := json.Unmarshal(issueResponse.Body.Bytes(), &capability); err != nil {
		t.Fatal(err)
	}
	if capability.Version != 1 ||
		capability.DeviceID != deviceID ||
		capability.ExpiresAtMS <= time.Now().UnixMilli() ||
		len(capability.GETSignature) != ed25519.SignatureSize*2 ||
		len(capability.HEADSignature) != ed25519.SignatureSize*2 {
		t.Fatalf("invalid capability response: %#v", capability)
	}

	fileURL := "/session-file"
	queryRequest := httptest.NewRequest(http.MethodGet, fileURL, nil)
	query := queryRequest.URL.Query()
	query.Set("agent_id", agent.ID)
	query.Set("process_id", strconv.Itoa(agent.ProcessID))
	query.Set("started_at", strconv.FormatInt(started.UnixMilli(), 10))
	query.Set("path", "image.png")
	query.Set("generation", generation)
	query.Set("file_cap_device", capability.DeviceID)
	query.Set("file_cap_expires", strconv.FormatInt(capability.ExpiresAtMS, 10))
	query.Set("file_cap_get", capability.GETSignature)
	query.Set("file_cap_head", capability.HEADSignature)
	queryRequest.URL.RawQuery = query.Encode()
	fileURL = queryRequest.URL.String()

	assertRequest := func(
		method string,
		rangeHeader string,
		wantStatus int,
		wantBody []byte,
	) {
		t.Helper()
		request := httptest.NewRequest(method, fileURL, nil)
		if rangeHeader != "" {
			request.Header.Set("Range", rangeHeader)
		}
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != wantStatus {
			t.Fatalf(
				"%s %s status=%d body=%q",
				method,
				rangeHeader,
				response.Code,
				response.Body.String(),
			)
		}
		if !bytes.Equal(response.Body.Bytes(), wantBody) {
			t.Fatalf(
				"%s %s body=%v want=%v",
				method,
				rangeHeader,
				response.Body.Bytes(),
				wantBody,
			)
		}
	}

	assertRequest(http.MethodGet, "", http.StatusOK, data)
	assertRequest(http.MethodHead, "", http.StatusOK, nil)
	assertRequest(http.MethodGet, "bytes=8-15", http.StatusPartialContent, data[8:16])
	// A native loader can retry the identical Range after network recovery.
	assertRequest(http.MethodGet, "bytes=8-15", http.StatusPartialContent, data[8:16])

	tampered := httptest.NewRequest(http.MethodGet, fileURL, nil)
	tamperedQuery := tampered.URL.Query()
	tamperedQuery.Set("path", "other.png")
	tampered.URL.RawQuery = tamperedQuery.Encode()
	tamperedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(tamperedResponse, tampered)
	if tamperedResponse.Code != http.StatusUnauthorized {
		t.Fatalf(
			"cross-path capability status=%d body=%q",
			tamperedResponse.Code,
			tamperedResponse.Body.String(),
		)
	}

	wrongMethod := httptest.NewRequest(http.MethodGet, fileURL, nil)
	wrongMethodQuery := wrongMethod.URL.Query()
	wrongMethodQuery.Set("file_cap_get", capability.HEADSignature)
	wrongMethod.URL.RawQuery = wrongMethodQuery.Encode()
	wrongMethodResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(wrongMethodResponse, wrongMethod)
	if wrongMethodResponse.Code != http.StatusUnauthorized {
		t.Fatalf(
			"cross-method capability status=%d body=%q",
			wrongMethodResponse.Code,
			wrongMethodResponse.Body.String(),
		)
	}

	replayIssue := httptest.NewRequest(
		http.MethodPost,
		"/session-file-capability",
		bytes.NewReader(requestBody),
	)
	replayIssue.Header.Set("Authorization", issueHeader)
	replayIssueResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(replayIssueResponse, replayIssue)
	if replayIssueResponse.Code != http.StatusUnauthorized {
		t.Fatalf(
			"ordinary nonce replay status=%d body=%q",
			replayIssueResponse.Code,
			replayIssueResponse.Body.String(),
		)
	}

	server.sessionFileCapabilityClock = func() time.Time {
		return time.UnixMilli(capability.ExpiresAtMS + 1)
	}
	expired := httptest.NewRequest(http.MethodGet, fileURL, nil)
	expiredResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(expiredResponse, expired)
	if expiredResponse.Code != http.StatusUnauthorized {
		t.Fatalf(
			"expired capability status=%d body=%q",
			expiredResponse.Code,
			expiredResponse.Body.String(),
		)
	}

	server.sessionFileCapabilityClock = nil
	if _, err := manager.RevokeDevice(deviceID); err != nil {
		t.Fatal(err)
	}
	revoked := httptest.NewRequest(http.MethodGet, fileURL, nil)
	revokedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(revokedResponse, revoked)
	if revokedResponse.Code != http.StatusUnauthorized {
		t.Fatalf(
			"revoked capability status=%d body=%q",
			revokedResponse.Code,
			revokedResponse.Body.String(),
		)
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

func TestSessionFileBinaryDownloadServesExactTextUnderSizeBound(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "notes.md")
	data := []byte("# Notes\nexact download bytes\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := openSessionFile(workspace, "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.kind != "markdown" {
		t.Fatalf("kind=%q want markdown", resolved.kind)
	}
	generation := resolved.generation
	_ = resolved.file.Close()

	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	started := time.Date(2026, 8, 6, 4, 0, 0, 0, time.UTC)
	agent := &classifier.Agent{ID: "main:@download", Cwd: workspace, ProcessID: 713, StartedAt: started}
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
	query.Set("process_id", "713")
	query.Set("started_at", string(startedAtRaw(started)))
	query.Set("path", "notes.md")
	query.Set("generation", generation)
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Authorization", sessionFileAuthorizationHeader(t, privateKey, manager.DaemonID(), deviceID))
	response := httptest.NewRecorder()
	server.handleSessionFileBinary(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("text download status=%d body=%s", response.Code, response.Body.String())
	}
	if !bytes.Equal(response.Body.Bytes(), data) {
		t.Fatalf("text download body=%q want=%q", response.Body.Bytes(), data)
	}
	if !strings.Contains(response.Header().Get("Content-Type"), "markdown") &&
		!strings.HasPrefix(response.Header().Get("Content-Type"), "text/") {
		t.Fatalf("content type=%q", response.Header().Get("Content-Type"))
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
