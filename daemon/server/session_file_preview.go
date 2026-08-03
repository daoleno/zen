package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/gorilla/websocket"
)

const (
	maxSessionFileReferenceBytes = 4096
	maxSessionFileTextBytes      = 512 << 10
	maxSessionFileBinaryBytes    = 50 << 20
	sessionFileAuthPurpose       = "zen-session-file"
	sessionFileCapabilityTTL     = 2 * time.Minute
	maxSessionFileCapabilityBody = 16 << 10
)

var (
	errSessionFileChanged       = errors.New("session file changed")
	errStaleSessionFileIdentity = errors.New("stale Session identity")
	errSessionFileTooLarge      = errors.New("Session file exceeds the preview size limit")
)

type sessionFileMetadata struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	Kind         string `json:"kind"`
	ContentType  string `json:"content_type"`
	Size         int64  `json:"size"`
	ModifiedAt   string `json:"modified_at"`
	Generation   string `json:"generation"`
	TooLarge     bool   `json:"too_large"`
	PreviewLimit int64  `json:"preview_limit_bytes"`
}

type sessionFileTextPreview struct {
	Content    string `json:"content"`
	BytesRead  int    `json:"bytes_read"`
	Truncated  bool   `json:"truncated"`
	Generation string `json:"generation"`
}

type sessionFileCapabilityRequest struct {
	AgentID    string `json:"agent_id"`
	ProcessID  int    `json:"process_id"`
	StartedAt  int64  `json:"started_at"`
	Path       string `json:"path"`
	Generation string `json:"generation"`
}

type sessionFileCapabilityClaims struct {
	Version     int    `json:"version"`
	DaemonID    string `json:"daemon_id"`
	DeviceID    string `json:"device_id"`
	Method      string `json:"method"`
	AgentID     string `json:"agent_id"`
	ProcessID   int    `json:"process_id"`
	StartedAt   int64  `json:"started_at"`
	Path        string `json:"path"`
	Generation  string `json:"generation"`
	ExpiresAtMS int64  `json:"expires_at_ms"`
}

type resolvedSessionFile struct {
	file          *os.File
	info          os.FileInfo
	canonicalPath string
	relativePath  string
	kind          string
	contentType   string
	generation    string
}

func (s *Server) handleSessionFileMetadata(conn *websocket.Conn, raw clientMessage) {
	resolved, err := s.resolveCurrentSessionFile(raw)
	if err != nil {
		s.sendSessionFileError(conn, raw.RequestID, "session_file_metadata_failed", err)
		return
	}
	defer resolved.file.Close()
	s.sendJSON(conn, map[string]any{
		"type":       "session_file_metadata",
		"request_id": raw.RequestID,
		"metadata":   resolved.metadata(),
	})
}

func (s *Server) handleSessionFileText(conn *websocket.Conn, raw clientMessage) {
	resolved, err := s.resolveCurrentSessionFile(raw)
	if err != nil {
		s.sendSessionFileError(conn, raw.RequestID, "session_file_text_failed", err)
		return
	}
	preview, err := readSessionFileText(resolved, strings.TrimSpace(raw.FileGeneration))
	if err != nil {
		s.sendSessionFileError(conn, raw.RequestID, "session_file_text_failed", err)
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "session_file_text",
		"request_id": raw.RequestID,
		"text":       preview,
	})
}

func (s *Server) sendSessionFileError(conn *websocket.Conn, requestID, fallbackCode string, err error) {
	code := fallbackCode
	switch {
	case isSessionFileChanged(err):
		code = "session_file_changed"
	case isStaleSessionFileIdentity(err):
		code = "session_file_stale_session"
	}
	s.sendErrorWithRequestID(conn, requestID, code, err.Error())
}

func (s *Server) resolveCurrentSessionFile(raw clientMessage) (*resolvedSessionFile, error) {
	agentID := strings.TrimSpace(raw.AgentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	agent := s.currentSessionFileAgent(agentID)
	if agent == nil {
		return nil, fmt.Errorf("%w: Session is no longer live", errStaleSessionFileIdentity)
	}
	if err := validateSessionFileIdentity(agent, raw); err != nil {
		return nil, err
	}
	return openSessionFile(agent.Cwd, raw.Path)
}

func (s *Server) currentSessionFileAgent(agentID string) *classifier.Agent {
	if s != nil && s.sessionFileAgentLoader != nil {
		return s.sessionFileAgentLoader(agentID)
	}
	if s == nil || s.watcher == nil {
		return nil
	}
	return s.watcher.GetAgent(agentID)
}

func validateSessionFileIdentity(agent *classifier.Agent, raw clientMessage) error {
	agentID := strings.TrimSpace(raw.AgentID)
	if agent == nil || strings.TrimSpace(agent.ID) == "" || strings.TrimSpace(agent.ID) != agentID {
		return fmt.Errorf("%w: Session is no longer live", errStaleSessionFileIdentity)
	}
	if raw.ProcessID <= 0 {
		return fmt.Errorf("process_id is required")
	}
	startedAt := clientStartedAt(raw.StartedAt)
	if startedAt.IsZero() {
		return fmt.Errorf("started_at is required")
	}
	if agent.ProcessID <= 0 || agent.StartedAt.IsZero() {
		return fmt.Errorf("%w: live Session generation is unavailable", errStaleSessionFileIdentity)
	}
	if raw.ProcessID != agent.ProcessID || startedAt.UnixMilli() != agent.StartedAt.UnixMilli() {
		return fmt.Errorf("%w: Session generation changed", errStaleSessionFileIdentity)
	}
	if strings.TrimSpace(agent.Cwd) == "" {
		return fmt.Errorf("live Session CWD is unavailable")
	}
	return nil
}

func openSessionFile(cwd, reference string) (*resolvedSessionFile, error) {
	if strings.TrimSpace(cwd) == "" {
		return nil, fmt.Errorf("live Session CWD is unavailable")
	}
	if strings.TrimSpace(reference) == "" {
		return nil, fmt.Errorf("file reference is required")
	}
	if len(reference) > maxSessionFileReferenceBytes || strings.ContainsRune(reference, 0) {
		return nil, fmt.Errorf("file reference is invalid")
	}

	cwdPath := filepath.FromSlash(cwd)
	if !filepath.IsAbs(cwdPath) {
		var err error
		cwdPath, err = filepath.Abs(cwdPath)
		if err != nil {
			return nil, fmt.Errorf("resolve live Session CWD: %w", err)
		}
	}
	canonicalCWD, err := filepath.EvalSymlinks(cwdPath)
	if err != nil {
		return nil, fmt.Errorf("resolve live Session CWD: %w", err)
	}
	cwdInfo, err := os.Stat(canonicalCWD)
	if err != nil {
		return nil, fmt.Errorf("inspect live Session CWD: %w", err)
	}
	if !cwdInfo.IsDir() {
		return nil, fmt.Errorf("live Session CWD is not a directory")
	}

	referencePath := filepath.FromSlash(reference)
	candidatePath := referencePath
	if !filepath.IsAbs(referencePath) {
		// canonicalCWD names the exact live directory inode. Keep the reference
		// components intact until EvalSymlinks so aliases followed by ".." have
		// host filesystem semantics instead of filepath.Join's lexical semantics.
		candidatePath = canonicalCWD + string(filepath.Separator) + referencePath
	}
	canonicalPath, err := filepath.EvalSymlinks(candidatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("Session file is missing")
		}
		return nil, fmt.Errorf("resolve Session file: %w", err)
	}

	// Check before Open so known directories and special files (notably FIFOs)
	// are rejected without attempting a potentially blocking read. The post-open
	// Stat below remains the authoritative file type after any replacement race.
	preInfo, err := os.Stat(canonicalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("Session file is missing")
		}
		return nil, fmt.Errorf("inspect Session file: %w", err)
	}
	if !preInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("file reference is not a regular file")
	}

	file, err := os.Open(canonicalPath)
	if err != nil {
		return nil, fmt.Errorf("open Session file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("inspect Session file: %w", err)
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("file reference is not a regular file")
	}

	sniff := make([]byte, 512)
	n, readErr := file.Read(sniff)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		file.Close()
		return nil, fmt.Errorf("inspect Session file content: %w", readErr)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, fmt.Errorf("rewind Session file: %w", err)
	}
	kind, contentType := classifySessionFile(canonicalPath, sniff[:n])
	relativePath, err := filepath.Rel(canonicalCWD, canonicalPath)
	if err != nil {
		// Different Windows volumes cannot be expressed relatively. The canonical
		// absolute path remains a valid follow-up reference and display value.
		relativePath = canonicalPath
	}
	relativeSlash := filepath.ToSlash(relativePath)
	return &resolvedSessionFile{
		file:          file,
		info:          info,
		canonicalPath: canonicalPath,
		relativePath:  relativeSlash,
		kind:          kind,
		contentType:   contentType,
		generation:    sessionFileGeneration(canonicalPath, info),
	}, nil
}

func classifySessionFile(path string, sniff []byte) (kind, contentType string) {
	extension := strings.ToLower(filepath.Ext(path))
	detected := http.DetectContentType(sniff)
	if len(sniff) == 0 {
		detected = "application/octet-stream"
	}

	if bytesStartWith(sniff, "%PDF-") {
		return "pdf", "application/pdf"
	}
	if isSupportedSessionImageType(detected) {
		return "image", detected
	}
	if containsBinaryMarker(sniff) || !utf8.Valid(sniff) {
		return "unsupported", detected
	}
	if extension == ".md" || extension == ".markdown" || extension == ".mdx" {
		return "markdown", "text/markdown; charset=utf-8"
	}
	if isSessionTextExtension(extension) || strings.HasPrefix(detected, "text/") {
		return "text", sessionTextContentType(extension, detected)
	}
	return "unsupported", detected
}

func bytesStartWith(value []byte, prefix string) bool {
	return len(value) >= len(prefix) && string(value[:len(prefix)]) == prefix
}

func isSupportedSessionImageType(contentType string) bool {
	switch contentType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func containsBinaryMarker(value []byte) bool {
	for _, b := range value {
		if b == 0 {
			return true
		}
	}
	return false
}

func isSessionTextExtension(extension string) bool {
	switch extension {
	case ".c", ".cc", ".conf", ".cpp", ".css", ".csv", ".env", ".go", ".graphql", ".h", ".hpp", ".html", ".ini", ".java", ".js", ".json", ".jsx", ".kt", ".kts", ".log", ".lua", ".m", ".mm", ".php", ".plist", ".properties", ".py", ".rb", ".rs", ".sh", ".sql", ".swift", ".toml", ".ts", ".tsx", ".txt", ".xml", ".yaml", ".yml", ".zsh":
		return true
	default:
		return false
	}
}

func sessionTextContentType(extension, detected string) string {
	switch extension {
	case ".json":
		return "application/json; charset=utf-8"
	case ".yaml", ".yml":
		return "application/yaml; charset=utf-8"
	case ".log":
		return "text/plain; charset=utf-8"
	}
	if strings.HasPrefix(detected, "text/") {
		return strings.Split(detected, ";")[0] + "; charset=utf-8"
	}
	if byExtension := mime.TypeByExtension(extension); strings.HasPrefix(byExtension, "text/") {
		return strings.Split(byExtension, ";")[0] + "; charset=utf-8"
	}
	return "text/plain; charset=utf-8"
}

func sessionFileGeneration(canonicalPath string, info os.FileInfo) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%s\x00%d\x00%d\x00%d\x00%s", canonicalPath, info.Size(), info.ModTime().UnixNano(), uint32(info.Mode()), stableFileIdentity(info.Sys()))
	return hex.EncodeToString(hash.Sum(nil))
}

func stableFileIdentity(sys any) string {
	value := reflect.ValueOf(sys)
	if !value.IsValid() {
		return ""
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return ""
	}
	fields := []string{"Dev", "Ino", "VolumeSerialNumber", "FileIndexHigh", "FileIndexLow"}
	var parts []string
	for _, name := range fields {
		field := value.FieldByName(name)
		if !field.IsValid() || !field.CanInterface() {
			continue
		}
		parts = append(parts, name+"="+fmt.Sprint(field.Interface()))
	}
	return strings.Join(parts, ",")
}

func (file *resolvedSessionFile) metadata() sessionFileMetadata {
	binaryPreview := file.kind == "image" || file.kind == "pdf"
	return sessionFileMetadata{
		Name:         filepath.Base(file.canonicalPath),
		Path:         file.canonicalPath,
		RelativePath: file.relativePath,
		Kind:         file.kind,
		ContentType:  file.contentType,
		Size:         file.info.Size(),
		ModifiedAt:   file.info.ModTime().UTC().Format(time.RFC3339Nano),
		Generation:   file.generation,
		TooLarge:     binaryPreview && file.info.Size() > maxSessionFileBinaryBytes,
		PreviewLimit: previewLimitForKind(file.kind),
	}
}

func previewLimitForKind(kind string) int64 {
	if kind == "image" || kind == "pdf" {
		return maxSessionFileBinaryBytes
	}
	return 0
}

func validateSessionFileBinarySize(resolved *resolvedSessionFile) error {
	if resolved == nil || resolved.info == nil {
		return fmt.Errorf("Session file is unavailable")
	}
	if resolved.info.Size() > maxSessionFileBinaryBytes {
		return fmt.Errorf(
			"%w: %d bytes; limit is %d bytes",
			errSessionFileTooLarge,
			resolved.info.Size(),
			maxSessionFileBinaryBytes,
		)
	}
	return nil
}

func readSessionFileText(resolved *resolvedSessionFile, expectedGeneration string) (sessionFileTextPreview, error) {
	if resolved == nil || resolved.file == nil {
		return sessionFileTextPreview{}, fmt.Errorf("Session file is unavailable")
	}
	defer resolved.file.Close()
	if strings.TrimSpace(expectedGeneration) == "" || expectedGeneration != resolved.generation {
		return sessionFileTextPreview{}, fmt.Errorf("%w; refresh the preview", errSessionFileChanged)
	}
	if resolved.kind != "text" && resolved.kind != "markdown" {
		return sessionFileTextPreview{}, fmt.Errorf("Session file is not text")
	}

	raw, err := io.ReadAll(io.LimitReader(resolved.file, maxSessionFileTextBytes+1))
	if err != nil {
		return sessionFileTextPreview{}, fmt.Errorf("read Session file: %w", err)
	}
	truncated := len(raw) > maxSessionFileTextBytes
	if truncated {
		raw = raw[:maxSessionFileTextBytes]
		for len(raw) > 0 && !utf8.Valid(raw) {
			raw = raw[:len(raw)-1]
		}
	}
	if containsBinaryMarker(raw) || !utf8.Valid(raw) {
		return sessionFileTextPreview{}, fmt.Errorf("Session file is not valid UTF-8 text")
	}
	if len(raw) >= 3 && raw[0] == 0xef && raw[1] == 0xbb && raw[2] == 0xbf {
		raw = raw[3:]
	}

	info, err := resolved.file.Stat()
	if err != nil {
		return sessionFileTextPreview{}, fmt.Errorf("recheck Session file: %w", err)
	}
	if sessionFileGeneration(resolved.canonicalPath, info) != expectedGeneration {
		return sessionFileTextPreview{}, fmt.Errorf("%w while reading; refresh the preview", errSessionFileChanged)
	}
	return sessionFileTextPreview{
		Content:    string(raw),
		BytesRead:  len(raw),
		Truncated:  truncated,
		Generation: expectedGeneration,
	}, nil
}

func isSessionFileChanged(err error) bool {
	return errors.Is(err, errSessionFileChanged)
}

func isStaleSessionFileIdentity(err error) bool {
	return errors.Is(err, errStaleSessionFileIdentity)
}

func (s *Server) handleSessionFileCapability(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	device, ok := s.authenticateRequest(w, r, sessionFileAuthPurpose)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSessionFileCapabilityBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request sessionFileCapabilityRequest
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid Session file capability request", http.StatusBadRequest)
		return
	}
	if err := ensureJSONBodyEnded(decoder); err != nil {
		http.Error(w, "invalid Session file capability request", http.StatusBadRequest)
		return
	}

	raw := clientMessage{
		AgentID:        request.AgentID,
		ProcessID:      request.ProcessID,
		StartedAt:      json.RawMessage(strconv.FormatInt(request.StartedAt, 10)),
		Path:           request.Path,
		FileGeneration: request.Generation,
	}
	resolved, err := s.resolveCurrentSessionFile(raw)
	if err != nil {
		writeSessionFileHTTPError(w, err)
		return
	}
	defer resolved.file.Close()
	if strings.TrimSpace(raw.FileGeneration) == "" ||
		raw.FileGeneration != resolved.generation {
		writeSessionFileHTTPError(
			w,
			fmt.Errorf("%w; refresh the preview", errSessionFileChanged),
		)
		return
	}
	if resolved.kind != "image" && resolved.kind != "pdf" {
		http.Error(
			w,
			"file is not a supported streamed preview",
			http.StatusUnsupportedMediaType,
		)
		return
	}
	if err := validateSessionFileBinarySize(resolved); err != nil {
		writeSessionFileHTTPError(w, err)
		return
	}

	expiresAtMS := s.sessionFileCapabilityNow().
		Add(sessionFileCapabilityTTL).
		UnixMilli()
	getSignature, err := s.createSessionFileCapabilitySignature(
		device.ID,
		http.MethodGet,
		raw,
		expiresAtMS,
	)
	if err != nil {
		http.Error(w, "failed to create Session file capability", http.StatusInternalServerError)
		return
	}
	headSignature, err := s.createSessionFileCapabilitySignature(
		device.ID,
		http.MethodHead,
		raw,
		expiresAtMS,
	)
	if err != nil {
		http.Error(w, "failed to create Session file capability", http.StatusInternalServerError)
		return
	}
	s.writeJSONWithAssertion(
		w,
		http.StatusOK,
		"zen-session-file-capability",
		map[string]any{
			"version":        1,
			"device_id":      device.ID,
			"expires_at_ms":  expiresAtMS,
			"get_signature":  getSignature,
			"head_signature": headSignature,
		},
	)
}

func (s *Server) handleSessionFileBinary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	usingCapability := hasSessionFileCapability(r)
	if !usingCapability {
		if _, ok := s.authenticateRequest(
			w,
			r,
			sessionFileAuthPurpose,
		); !ok {
			return
		}
	}
	raw, err := sessionFileMessageFromRequest(r)
	if err != nil {
		if usingCapability {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if usingCapability {
		if err := s.verifySessionFileCapability(r, raw); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	resolved, err := s.resolveCurrentSessionFile(raw)
	if err != nil {
		writeSessionFileHTTPError(w, err)
		return
	}
	defer resolved.file.Close()
	if strings.TrimSpace(raw.FileGeneration) == "" || raw.FileGeneration != resolved.generation {
		writeSessionFileHTTPError(w, fmt.Errorf("%w; refresh the preview", errSessionFileChanged))
		return
	}
	if resolved.kind != "image" && resolved.kind != "pdf" {
		http.Error(w, "file is not a supported streamed preview", http.StatusUnsupportedMediaType)
		return
	}
	if err := validateSessionFileBinarySize(resolved); err != nil {
		writeSessionFileHTTPError(w, err)
		return
	}

	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", resolved.contentType)
	w.Header().Set("Content-Disposition", contentDispositionInline(resolved.info.Name()))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(
		w,
		r,
		resolved.info.Name(),
		resolved.info.ModTime(),
		boundedSessionFileBinaryReader(resolved),
	)
}

func sessionFileMessageFromRequest(r *http.Request) (clientMessage, error) {
	processID, err := strconv.Atoi(
		strings.TrimSpace(r.URL.Query().Get("process_id")),
	)
	if err != nil || processID <= 0 {
		return clientMessage{}, errors.New("process_id is invalid")
	}
	startedAt := strings.TrimSpace(r.URL.Query().Get("started_at"))
	startedAtMS, err := strconv.ParseInt(startedAt, 10, 64)
	if err != nil || startedAtMS <= 0 {
		return clientMessage{}, errors.New("started_at is invalid")
	}
	return clientMessage{
		AgentID:        r.URL.Query().Get("agent_id"),
		ProcessID:      processID,
		StartedAt:      json.RawMessage(strconv.FormatInt(startedAtMS, 10)),
		Path:           r.URL.Query().Get("path"),
		FileGeneration: r.URL.Query().Get("generation"),
	}, nil
}

func hasSessionFileCapability(r *http.Request) bool {
	query := r.URL.Query()
	for _, key := range []string{
		"file_cap_device",
		"file_cap_expires",
		"file_cap_get",
		"file_cap_head",
	} {
		if strings.TrimSpace(query.Get(key)) != "" {
			return true
		}
	}
	return false
}

func (s *Server) createSessionFileCapabilitySignature(
	deviceID string,
	method string,
	raw clientMessage,
	expiresAtMS int64,
) (string, error) {
	payload, err := s.sessionFileCapabilityPayload(
		deviceID,
		method,
		raw,
		expiresAtMS,
	)
	if err != nil {
		return "", err
	}
	return s.auth.CreateSessionFileCapabilitySignature(payload), nil
}

func (s *Server) verifySessionFileCapability(
	r *http.Request,
	raw clientMessage,
) error {
	query := r.URL.Query()
	deviceID := strings.TrimSpace(query.Get("file_cap_device"))
	expiresAtMS, err := strconv.ParseInt(
		strings.TrimSpace(query.Get("file_cap_expires")),
		10,
		64,
	)
	if err != nil || expiresAtMS <= 0 || deviceID == "" {
		return errors.New("invalid Session file capability")
	}
	now := s.sessionFileCapabilityNow()
	expiresAt := time.UnixMilli(expiresAtMS)
	if !now.Before(expiresAt) ||
		expiresAt.After(now.Add(sessionFileCapabilityTTL+time.Second)) {
		return errors.New("expired Session file capability")
	}

	var signature string
	switch r.Method {
	case http.MethodGet:
		signature = strings.TrimSpace(query.Get("file_cap_get"))
	case http.MethodHead:
		signature = strings.TrimSpace(query.Get("file_cap_head"))
	default:
		return errors.New("invalid Session file capability method")
	}
	if signature == "" {
		return errors.New("missing Session file capability signature")
	}
	payload, err := s.sessionFileCapabilityPayload(
		deviceID,
		r.Method,
		raw,
		expiresAtMS,
	)
	if err != nil {
		return err
	}
	return s.auth.VerifySessionFileCapabilitySignature(
		deviceID,
		payload,
		signature,
	)
}

func (s *Server) sessionFileCapabilityPayload(
	deviceID string,
	method string,
	raw clientMessage,
	expiresAtMS int64,
) ([]byte, error) {
	startedAtMS, err := strconv.ParseInt(
		strings.TrimSpace(string(raw.StartedAt)),
		10,
		64,
	)
	if err != nil || startedAtMS <= 0 {
		return nil, errors.New("started_at is invalid")
	}
	return json.Marshal(sessionFileCapabilityClaims{
		Version:     1,
		DaemonID:    s.auth.DaemonID(),
		DeviceID:    strings.TrimSpace(deviceID),
		Method:      method,
		AgentID:     raw.AgentID,
		ProcessID:   raw.ProcessID,
		StartedAt:   startedAtMS,
		Path:        raw.Path,
		Generation:  raw.FileGeneration,
		ExpiresAtMS: expiresAtMS,
	})
}

func (s *Server) sessionFileCapabilityNow() time.Time {
	if s != nil && s.sessionFileCapabilityClock != nil {
		return s.sessionFileCapabilityClock().UTC()
	}
	return time.Now().UTC()
}

func ensureJSONBodyEnded(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("unexpected trailing JSON value")
}

func boundedSessionFileBinaryReader(resolved *resolvedSessionFile) io.ReadSeeker {
	return io.NewSectionReader(resolved.file, 0, resolved.info.Size())
}

func contentDispositionInline(name string) string {
	value := mime.FormatMediaType("inline", map[string]string{
		"filename": filepath.Base(name),
	})
	if value == "" {
		return "inline"
	}
	return value
}

func writeSessionFileHTTPError(w http.ResponseWriter, err error) {
	switch {
	case isSessionFileChanged(err), isStaleSessionFileIdentity(err):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, errSessionFileTooLarge):
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
	case strings.Contains(err.Error(), "missing"):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
