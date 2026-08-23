package brain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
)

const (
	defaultPersonality = "calm, direct, warm, pragmatic"
	worklogDirName     = "worklog"
	policiesDirName    = "policies"
)

type Store struct {
	Root string

	mu      sync.Mutex
	subMu   sync.Mutex
	subs    map[int]chan WorkChange
	nextSub int
	now     func() time.Time

	// fsm is the canonical delegated-Work lifecycle engine (docs/work-lifecycle.md).
	fsm *lifecycle.Engine

	writePresentation func(string, any) error
	// projectBrainInputAdmission is an optional Store-scoped fault seam for the
	// recoverable messages.jsonl projection. Nil uses the production append.
	projectBrainInputAdmission func(BrainInputAdmission) error
	// replaceHostBindingWrite is an optional Store-scoped seam used only by
	// ReplaceHostSessionBinding. Nil means writeJSONFile (production default).
	replaceHostBindingWrite func(path string, value any) error
}

func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zen", "brain"), nil
}

func NewStore(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("brain root required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	store := &Store{
		Root:              root,
		subs:              map[int]chan WorkChange{},
		now:               time.Now,
		writePresentation: writeJSONFile,
	}
	// The canonical delegated-Work lifecycle engine owns its own append-only
	// log and current image under state/lifecycle. Startup reads the current
	// image directly; reducer determinism remains an audit/test property.
	fsm, err := lifecycle.Open(filepath.Join(root, "state", "lifecycle"))
	if err != nil {
		return nil, fmt.Errorf("open work lifecycle engine: %w", err)
	}
	store.fsm = fsm
	// The engine shares the Store clock (read dynamically through nowUTC), so
	// supervisor sweeps and event timestamps follow the same time authority.
	fsm.SetNow(store.nowUTC)
	if err := store.ensureFiles(); err != nil {
		_ = fsm.Close()
		return nil, err
	}
	if err := store.rebuildFSMProjections(); err != nil {
		// Lifecycle already recovered. Derived read-model repair must not
		// prevent the daemon from starting: a stale or historical projection
		// row is not process-fatal.
		log.Printf("brain: repair Lifecycle projections: %v", err)
	}
	return store, nil
}

// FSM exposes the canonical Work lifecycle engine for supervisor loops.
func (s *Store) FSM() *lifecycle.Engine {
	return s.fsm
}

func (s *Store) WorkspacePath() string {
	return filepath.Join(s.Root, "workspace")
}

func (s *Store) HostSessionPath() string {
	return filepath.Join(s.statePath(), "host_session.json")
}

// HostReplacementsPath is an append-only audit log for Brain host replacements.
// One JSON object per line; safe to tail for diagnosing @N → @M session loss.
func (s *Store) HostReplacementsPath() string {
	return filepath.Join(s.statePath(), "host_replacements.jsonl")
}

func (s *Store) ChatStatePath() string {
	return filepath.Join(s.statePath(), "chat_state.json")
}

// HostReplacementEvent is a durable audit record for Host identity and
// foreground-ownership replacement.
type HostReplacementEvent struct {
	At               time.Time `json:"at"`
	Reason           string    `json:"reason"`
	FromID           string    `json:"from_id,omitempty"`
	ToID             string    `json:"to_id,omitempty"`
	FromExecutorID   string    `json:"from_executor_id,omitempty"`
	FromCommand      string    `json:"from_command,omitempty"`
	ResolvedExecutor string    `json:"resolved_executor,omitempty"`
	Detail           string    `json:"detail,omitempty"`
}

// AppendHostReplacement writes one audit line. Failures are non-fatal for callers.
func (s *Store) AppendHostReplacement(event HostReplacementEvent) error {
	if s == nil {
		return fmt.Errorf("brain store is not configured")
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	event.Reason = strings.TrimSpace(event.Reason)
	if event.Reason == "" {
		return fmt.Errorf("host replacement reason required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.statePath(), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.HostReplacementsPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(s.HostReplacementsPath()))
}

func (s *Store) Snapshot() (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked(nil)
}

func (s *Store) HostSessionID() (string, error) {
	session, err := s.HostSession()
	if err != nil {
		return "", err
	}
	return session.ID, nil
}

type HostSession struct {
	ID                string
	ExecutorID        string
	UpdatedAt         time.Time
	ProviderSessionID string
	TranscriptPath    string
	ProviderDataRoot  string
}

func (s *Store) HostSession() (HostSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readHostSessionLocked()
}

func (s *Store) SetHostSessionID(id string) error {
	return s.SetHostSession(id, "")
}

func (s *Store) SetHostSession(id, executorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	executorID = strings.TrimSpace(executorID)
	if id == "" {
		return writeJSONFile(s.HostSessionPath(), hostSessionFile{})
	}
	previous, _ := s.readHostSessionLocked()
	host := hostSessionFile{
		ID:         id,
		ExecutorID: executorID,
		UpdatedAt:  time.Now().UTC(),
	}
	if previous.ID == id {
		host.ProviderSessionID = previous.ProviderSessionID
		host.TranscriptPath = previous.TranscriptPath
		host.ProviderDataRoot = previous.ProviderDataRoot
	}
	return writeJSONFile(s.HostSessionPath(), host)
}

// ReplaceHostSessionBinding atomically writes a new host tmux target together
// with its provider binding in one JSON write. On failure the previous file is
// unchanged (writeAtomic rename).
func (s *Store) ReplaceHostSessionBinding(id, executorID, providerSessionID, transcriptPath, providerDataRoot string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("host session id required")
	}
	return s.writeReplaceHostSessionBinding(s.HostSessionPath(), hostSessionFile{
		ID:                id,
		ExecutorID:        strings.TrimSpace(executorID),
		UpdatedAt:         time.Now().UTC(),
		ProviderSessionID: strings.TrimSpace(providerSessionID),
		TranscriptPath:    strings.TrimSpace(transcriptPath),
		ProviderDataRoot:  strings.TrimSpace(providerDataRoot),
	})
}

func (s *Store) writeReplaceHostSessionBinding(path string, value any) error {
	if s != nil && s.replaceHostBindingWrite != nil {
		return s.replaceHostBindingWrite(path, value)
	}
	return writeJSONFile(path, value)
}

func (s *Store) SetHostExecutorID(executorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	executorID = strings.TrimSpace(executorID)
	host, err := s.readHostSessionLocked()
	if err != nil {
		return err
	}
	next := hostSessionFile{
		ID:         host.ID,
		ExecutorID: executorID,
		UpdatedAt:  time.Now().UTC(),
	}
	if strings.TrimSpace(host.ExecutorID) == executorID || strings.TrimSpace(host.ExecutorID) == "" {
		next.ProviderSessionID = host.ProviderSessionID
		next.TranscriptPath = host.TranscriptPath
		next.ProviderDataRoot = host.ProviderDataRoot
	}
	return writeJSONFile(s.HostSessionPath(), next)
}

// SetHostProviderTranscript persists the stable Host Executor Session provider
// transcript identity. Empty values clear the binding.
func (s *Store) SetHostProviderTranscript(providerSessionID, transcriptPath, providerDataRoot string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	host, err := s.readHostSessionLocked()
	if err != nil {
		return err
	}
	if strings.TrimSpace(host.ID) == "" {
		return fmt.Errorf("host session required before binding provider transcript")
	}
	return writeJSONFile(s.HostSessionPath(), hostSessionFile{
		ID:                host.ID,
		ExecutorID:        host.ExecutorID,
		UpdatedAt:         time.Now().UTC(),
		ProviderSessionID: strings.TrimSpace(providerSessionID),
		TranscriptPath:    strings.TrimSpace(transcriptPath),
		ProviderDataRoot:  strings.TrimSpace(providerDataRoot),
	})
}

func (s *Store) snapshotLocked(agents []AgentRef) (Snapshot, error) {
	memory, err := readTextFile(s.memoryPath())
	if err != nil {
		return Snapshot{}, err
	}
	profile, err := s.readProfileLocked()
	if err != nil {
		return Snapshot{}, err
	}
	profileNotes, err := readTextFile(s.profileNotesPath())
	if err != nil {
		return Snapshot{}, err
	}
	current, err := readTextFile(s.currentPath())
	if err != nil {
		return Snapshot{}, err
	}
	if agents == nil {
		agents = []AgentRef{}
	}
	return Snapshot{
		Memory:      memory,
		Profile:     profileNotes,
		Current:     current,
		Personality: firstNonEmpty(profile.Personality, defaultPersonality),
		Agents:      agents,
		Workspace:   s.WorkspacePath(),
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func (s *Store) ensureFiles() error {
	if err := os.MkdirAll(s.statePath(), 0o700); err != nil {
		return err
	}
	if err := s.ensurePresentationDatabase(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.WorkspacePath(), 0o700); err != nil {
		return err
	}
	if err := s.reconcileManagedWorkspace(); err != nil {
		return err
	}
	if err := ensureFile(s.memoryPath(), []byte("# Brain Memory\n\n")); err != nil {
		return err
	}
	if err := ensureFile(s.currentPath(), []byte(defaultCurrentContext)); err != nil {
		return err
	}
	if err := s.ensurePlaybooks(); err != nil {
		return err
	}
	if err := s.ensureWorklog(); err != nil {
		return err
	}
	if err := ensureFile(s.remindersPath(), []byte("[]\n")); err != nil {
		return err
	}
	if err := ensureFile(s.HostSessionPath(), []byte("{}\n")); err != nil {
		return err
	}
	if err := ensureFile(s.ChatStatePath(), []byte("{}\n")); err != nil {
		return err
	}
	profilePath := s.profilePath()
	if _, err := os.Stat(profilePath); errors.Is(err, os.ErrNotExist) {
		profile := profileFile{
			Personality: defaultPersonality,
		}
		return writeJSONFile(profilePath, profile)
	} else if err != nil {
		return err
	}
	return nil
}

func (s *Store) ChatThreadID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readChatStateLocked("")
	if err != nil {
		return "", err
	}
	return state.ThreadID, nil
}

func (s *Store) ChatThreadIDs() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readChatStateLocked("")
	if err != nil {
		return nil, err
	}
	return append([]string(nil), state.ThreadIDs...), nil
}

func (s *Store) HasChatThread(threadID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return false, nil
	}
	state, err := s.loadChatStateLocked("")
	if err != nil {
		return false, err
	}
	for _, known := range state.ThreadIDs {
		if known == threadID {
			return true, nil
		}
	}
	return false, nil
}

type ChatState struct {
	ThreadID  string
	ThreadIDs []string
}

func (s *Store) ChatState(threadID string) (ChatState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readChatStateLocked(threadID)
}

func (s *Store) SetChatState(state ChatState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setChatStateLocked(state)
}

func (s *Store) readChatStateLocked(threadID string) (ChatState, error) {
	return s.loadChatStateLocked(threadID)
}

func (s *Store) loadChatStateLocked(threadID string) (ChatState, error) {
	state, err := s.readChatStateFileLocked()
	if err != nil {
		return ChatState{}, err
	}
	changed := false
	if strings.TrimSpace(state.ThreadID) == "" {
		if strings.TrimSpace(threadID) != "" {
			state.ThreadID = strings.TrimSpace(threadID)
		} else {
			state.ThreadID = newChatThreadID()
		}
		changed = true
	}
	state.ThreadIDs = normalizeUniqueStrings(append(state.ThreadIDs, state.ThreadID))
	if changed {
		if err := s.writeChatStateLocked(state); err != nil {
			return ChatState{}, err
		}
	}
	return state, nil
}

func (s *Store) setChatStateLocked(state ChatState) error {
	previous, err := s.readChatStateFileLocked()
	if err != nil {
		return err
	}
	state.ThreadID = strings.TrimSpace(state.ThreadID)
	if state.ThreadID == "" {
		state.ThreadID = strings.TrimSpace(previous.ThreadID)
	}
	state.ThreadIDs = normalizeUniqueStrings(append(append(append(
		[]string{}, previous.ThreadIDs...), previous.ThreadID), state.ThreadIDs...))
	return s.writeChatStateLocked(state)
}

func (s *Store) statePath() string {
	return filepath.Join(s.Root, "state")
}

func (s *Store) memoryPath() string {
	return filepath.Join(s.WorkspacePath(), "memory.md")
}

func (s *Store) remindersPath() string {
	return filepath.Join(s.statePath(), "reminders.json")
}

func (s *Store) profilePath() string {
	return filepath.Join(s.statePath(), "profile.json")
}

func (s *Store) profileNotesPath() string {
	return filepath.Join(s.WorkspacePath(), "profile.md")
}

func (s *Store) currentPath() string {
	return filepath.Join(s.WorkspacePath(), "current.md")
}

func (s *Store) workspaceInstructionsPath() string {
	return filepath.Join(s.WorkspacePath(), "AGENTS.md")
}

func (s *Store) policiesPath() string {
	return filepath.Join(s.WorkspacePath(), policiesDirName)
}

func (s *Store) policyPath(name string) string {
	return filepath.Join(s.policiesPath(), name)
}

func (s *Store) worklogPath() string {
	return filepath.Join(s.WorkspacePath(), worklogDirName)
}

func (s *Store) worklogReadmePath() string {
	return filepath.Join(s.worklogPath(), "README.md")
}

type profileFile struct {
	Personality string `json:"personality"`
}

type hostSessionFile struct {
	ID                string    `json:"id,omitempty"`
	ExecutorID        string    `json:"executor_id,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
	ProviderSessionID string    `json:"provider_session_id,omitempty"`
	TranscriptPath    string    `json:"transcript_path,omitempty"`
	ProviderDataRoot  string    `json:"provider_data_root,omitempty"`
}

type chatStateFile struct {
	ThreadID  string   `json:"thread_id,omitempty"`
	ThreadIDs []string `json:"thread_ids,omitempty"`
}

func (s *Store) readHostSessionLocked() (HostSession, error) {
	raw, err := os.ReadFile(s.HostSessionPath())
	if errors.Is(err, os.ErrNotExist) {
		return HostSession{}, nil
	}
	if err != nil {
		return HostSession{}, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return HostSession{}, nil
	}
	var host hostSessionFile
	if err := json.Unmarshal(raw, &host); err != nil {
		return HostSession{}, err
	}
	return HostSession{
		ID:                strings.TrimSpace(host.ID),
		ExecutorID:        strings.TrimSpace(host.ExecutorID),
		UpdatedAt:         host.UpdatedAt,
		ProviderSessionID: strings.TrimSpace(host.ProviderSessionID),
		TranscriptPath:    strings.TrimSpace(host.TranscriptPath),
		ProviderDataRoot:  strings.TrimSpace(host.ProviderDataRoot),
	}, nil
}

func (s *Store) readChatStateFileLocked() (ChatState, error) {
	raw, err := os.ReadFile(s.ChatStatePath())
	if errors.Is(err, os.ErrNotExist) {
		return ChatState{}, nil
	}
	if err != nil {
		return ChatState{}, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return ChatState{}, nil
	}
	file, err := decodeChatStateFile(raw)
	if err != nil {
		return ChatState{}, err
	}
	return ChatState{
		ThreadID:  strings.TrimSpace(file.ThreadID),
		ThreadIDs: normalizeUniqueStrings(file.ThreadIDs),
	}, nil
}

func decodeChatStateFile(raw []byte) (chatStateFile, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return chatStateFile{}, nil
	}
	if trimmed[0] != '{' {
		return chatStateFile{}, fmt.Errorf("decode Brain chat state: expected a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var encoded struct {
		ThreadID  json.RawMessage `json:"thread_id"`
		ThreadIDs json.RawMessage `json:"thread_ids"`
	}
	if err := decoder.Decode(&encoded); err != nil {
		return chatStateFile{}, fmt.Errorf("decode Brain chat state: %w", err)
	}
	if trailing := bytes.TrimSpace(trimmed[decoder.InputOffset():]); len(trailing) != 0 {
		return chatStateFile{}, fmt.Errorf("decode Brain chat state: trailing data after JSON object")
	}
	var file chatStateFile
	if encoded.ThreadID != nil {
		if bytes.Equal(bytes.TrimSpace(encoded.ThreadID), []byte("null")) {
			return chatStateFile{}, fmt.Errorf("decode Brain chat state: thread_id must be a JSON string")
		}
		if err := json.Unmarshal(encoded.ThreadID, &file.ThreadID); err != nil {
			return chatStateFile{}, fmt.Errorf("decode Brain chat state: thread_id must be a JSON string: %w", err)
		}
	}
	if encoded.ThreadIDs != nil {
		if bytes.Equal(bytes.TrimSpace(encoded.ThreadIDs), []byte("null")) {
			return chatStateFile{}, fmt.Errorf("decode Brain chat state: thread_ids must be a JSON array of strings")
		}
		var values []json.RawMessage
		if err := json.Unmarshal(encoded.ThreadIDs, &values); err != nil {
			return chatStateFile{}, fmt.Errorf("decode Brain chat state: thread_ids must be a JSON array of strings: %w", err)
		}
		file.ThreadIDs = make([]string, 0, len(values))
		for index, raw := range values {
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return chatStateFile{}, fmt.Errorf("decode Brain chat state: thread_ids[%d] must be a JSON string", index)
			}
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return chatStateFile{}, fmt.Errorf("decode Brain chat state: thread_ids[%d] must be a JSON string: %w", index, err)
			}
			file.ThreadIDs = append(file.ThreadIDs, value)
		}
	}
	return file, nil
}

func (s *Store) readProfileLocked() (profileFile, error) {
	raw, err := os.ReadFile(s.profilePath())
	if errors.Is(err, os.ErrNotExist) {
		return profileFile{Personality: defaultPersonality}, nil
	}
	if err != nil {
		return profileFile{}, err
	}
	var profile profileFile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return profileFile{}, err
	}
	if strings.TrimSpace(profile.Personality) == "" {
		profile.Personality = defaultPersonality
	}
	return profile, nil
}

func (s *Store) writeChatStateLocked(state ChatState) error {
	state.ThreadID = strings.TrimSpace(state.ThreadID)
	if state.ThreadID == "" {
		state.ThreadID = newChatThreadID()
	}
	state.ThreadIDs = normalizeUniqueStrings(append(state.ThreadIDs, state.ThreadID))
	return writeJSONFile(s.ChatStatePath(), chatStateFile{
		ThreadID:  state.ThreadID,
		ThreadIDs: state.ThreadIDs,
	})
}

func normalizeUniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func newChatThreadID() string {
	return fmt.Sprintf("brain_%d", time.Now().UTC().UnixNano())
}

func ensureFile(path string, initial []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if initial == nil {
		initial = []byte{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, initial, 0o600)
}

func (s *Store) ensureWorklog() error {
	if err := os.MkdirAll(s.worklogPath(), 0o700); err != nil {
		return err
	}
	return ensureFile(s.worklogReadmePath(), []byte(defaultWorklogReadme))
}

func readTextFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

const defaultProfileNotes = `# Brain Profile

Record user-authored preferences, background, and working style here.
`

const defaultCurrentContext = `# Current Brain Context

## Active Objective

None recorded yet.

## Decisions

- Brain's current host executor is the orchestrator for planning, delegation, review, and final synthesis.
- Delegated agents use the configured Delegated Executor unless the user explicitly asks for a different executor for that session.
- delegated_executor controls delegated execution and ordinary non-Brain session creation.
- Use a different executor for a session only when the user explicitly mentions or asks for it.
- Switching Brain host executors preserves the visible chat and uses private handoff context.

## Open Threads

- Summarize only context useful for executor handoff. Durable status, ownership, next action, and wait state live in Brain Work.

## Next

- Refresh this projection when handoff context materially changes; do not duplicate Work/Event state manually.
`

const defaultWorklogReadme = `# Brain Worklog

This directory stores one Markdown record for each problem, feature, fix, or workflow Brain tracks for the user. Use it as a durable archive and as lightweight task progress state.

Suggested filename: ` + "`YYYY-MM-DD-short-title.md`" + `

## Task Record Template

` + "```markdown" + `
# <Task Title>

- Status: planned | in_progress | blocked | done
- Date: YYYY-MM-DD

## Context

## Objective

## Todo

- [ ]

## Progress

## Verification

## Result

## Follow-up
` + "```" + `
`

func writeJSONFile(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeAtomic(path, raw, 0o600)
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".zen-brain-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(perm); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
