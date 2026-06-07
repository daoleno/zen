package brain

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const defaultPersonality = "calm, direct, warm, pragmatic"

type Store struct {
	Root string

	mu sync.Mutex
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
	store := &Store{Root: root}
	if err := store.ensureFiles(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) WorkspacePath() string {
	return filepath.Join(s.Root, "workspace")
}

func (s *Store) HostSessionPath() string {
	return filepath.Join(s.statePath(), "host_session.json")
}

func (s *Store) ChatStatePath() string {
	return filepath.Join(s.statePath(), "chat_state.json")
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
	ID        string
	AdapterID string
	UpdatedAt time.Time
}

func (s *Store) HostSession() (HostSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readHostSessionLocked()
}

func (s *Store) SetHostSessionID(id string) error {
	return s.SetHostSession(id, "")
}

func (s *Store) SetHostSession(id, adapterID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	adapterID = strings.TrimSpace(adapterID)
	if id == "" {
		return writeJSONFile(s.HostSessionPath(), hostSessionFile{})
	}
	return writeJSONFile(s.HostSessionPath(), hostSessionFile{
		ID:        id,
		AdapterID: adapterID,
		UpdatedAt: time.Now().UTC(),
	})
}

func (s *Store) SetHostAdapterID(adapterID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	adapterID = strings.TrimSpace(adapterID)
	host, err := s.readHostSessionLocked()
	if err != nil {
		return err
	}
	return writeJSONFile(s.HostSessionPath(), hostSessionFile{
		ID:        host.ID,
		AdapterID: adapterID,
		UpdatedAt: time.Now().UTC(),
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
	if agents == nil {
		agents = []AgentRef{}
	}
	return Snapshot{
		Memory:      memory,
		Profile:     profileNotes,
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
	if err := os.MkdirAll(s.WorkspacePath(), 0o700); err != nil {
		return err
	}
	if err := s.migrateLegacyFiles(); err != nil {
		return err
	}
	if err := ensureFile(s.messagesPath(), nil); err != nil {
		return err
	}
	if err := ensureFile(s.memoryPath(), []byte("# Brain Memory\n\n")); err != nil {
		return err
	}
	if err := ensureFile(s.profileNotesPath(), []byte("# Brain Profile\n\n")); err != nil {
		return err
	}
	if err := ensureFile(s.workspaceInstructionsPath(), []byte(defaultWorkspaceInstructions)); err != nil {
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
			Notes:       "# Brain Profile\n\n",
		}
		return writeJSONFile(profilePath, profile)
	} else if err != nil {
		return err
	}
	return nil
}

func (s *Store) AppendChatMessage(message ChatMessage) (ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	message.ID = strings.TrimSpace(message.ID)
	if message.ID == "" {
		message.ID = fmt.Sprintf("msg_%d", time.Now().UTC().UnixNano())
	}
	message.ThreadID = strings.TrimSpace(message.ThreadID)
	message.SessionID = strings.TrimSpace(message.SessionID)
	message.AdapterID = strings.TrimSpace(message.AdapterID)
	message.Role = strings.TrimSpace(message.Role)
	message.Body = strings.TrimSpace(message.Body)
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}
	message.ThreadID = strings.TrimSpace(message.ThreadID)
	if message.ThreadID == "" {
		return ChatMessage{}, fmt.Errorf("message thread id required")
	}
	if message.SessionID == "" {
		return ChatMessage{}, fmt.Errorf("message session id required")
	}
	if message.Role == "" {
		return ChatMessage{}, fmt.Errorf("message role required")
	}
	if message.Body == "" {
		return ChatMessage{}, fmt.Errorf("message body required")
	}
	if err := s.touchChatSessionLocked(message.ThreadID, message.SessionID); err != nil {
		return ChatMessage{}, err
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return ChatMessage{}, err
	}
	if err := os.MkdirAll(filepath.Dir(s.messagesPath()), 0o700); err != nil {
		return ChatMessage{}, err
	}
	file, err := os.OpenFile(s.messagesPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return ChatMessage{}, err
	}
	defer file.Close()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return ChatMessage{}, err
	}
	return message, nil
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

func (s *Store) ChatMessages(threadID string, limit int) ([]ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chatMessagesLocked(threadID, limit)
}

func (s *Store) chatMessagesLocked(threadID string, limit int) ([]ChatMessage, error) {
	state, err := s.readChatStateLocked(threadID)
	if err != nil {
		return nil, err
	}
	sessionIDs := make(map[string]struct{}, len(state.SessionIDs))
	for _, sessionID := range state.SessionIDs {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			continue
		}
		sessionIDs[sessionID] = struct{}{}
	}
	file, err := os.Open(s.messagesPath())
	if errors.Is(err, os.ErrNotExist) {
		return []ChatMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	out := []ChatMessage{}
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var message ChatMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			continue
		}
		message.ThreadID = strings.TrimSpace(message.ThreadID)
		message.SessionID = strings.TrimSpace(message.SessionID)
		if message.ThreadID == state.ThreadID {
			out = append(out, message)
			continue
		}
		if message.ThreadID == "" {
			if _, ok := sessionIDs[message.SessionID]; ok {
				out = append(out, message)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

type ChatState struct {
	ThreadID       string
	SessionIDs     []string
	LastTranscript string
	UpdatedAt      time.Time
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

func (s *Store) TouchChatSession(threadID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.touchChatSessionLocked(strings.TrimSpace(threadID), strings.TrimSpace(sessionID))
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
	state.SessionIDs = normalizeUniqueStrings(state.SessionIDs)
	if len(state.SessionIDs) == 0 {
		state.SessionIDs = s.collectChatSessionIDsLocked()
		changed = true
	}
	if len(state.SessionIDs) == 0 {
		if host, hostErr := s.readHostSessionLocked(); hostErr == nil && strings.TrimSpace(host.ID) != "" {
			state.SessionIDs = []string{strings.TrimSpace(host.ID)}
			changed = true
		}
	}
	if strings.TrimSpace(state.ThreadID) == "" {
		state.ThreadID = newChatThreadID()
		changed = true
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
		changed = true
	}
	if changed {
		if err := s.writeChatStateLocked(state); err != nil {
			return ChatState{}, err
		}
	}
	return state, nil
}

func (s *Store) setChatStateLocked(state ChatState) error {
	if strings.TrimSpace(state.ThreadID) == "" {
		loaded, err := s.loadChatStateLocked("")
		if err != nil {
			return err
		}
		state.ThreadID = loaded.ThreadID
	}
	state.ThreadID = strings.TrimSpace(state.ThreadID)
	state.SessionIDs = normalizeUniqueStrings(state.SessionIDs)
	if len(state.SessionIDs) == 0 {
		state.SessionIDs = s.collectChatSessionIDsLocked()
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	}
	return s.writeChatStateLocked(state)
}

func (s *Store) touchChatSessionLocked(threadID, sessionID string) error {
	if threadID == "" || sessionID == "" {
		return nil
	}
	state, err := s.loadChatStateLocked(threadID)
	if err != nil {
		return err
	}
	if appendUniqueString(&state.SessionIDs, sessionID) {
		state.UpdatedAt = time.Now().UTC()
		return s.writeChatStateLocked(state)
	}
	return nil
}

func (s *Store) migrateLegacyFiles() error {
	migrations := []struct {
		from string
		to   string
	}{
		{filepath.Join(s.Root, "messages.jsonl"), s.messagesPath()},
		{filepath.Join(s.Root, "reminders.json"), s.remindersPath()},
		{filepath.Join(s.Root, "profile.json"), s.profilePath()},
		{filepath.Join(s.Root, "memory.md"), s.memoryPath()},
	}
	for _, migration := range migrations {
		if err := moveIfMissing(migration.from, migration.to); err != nil {
			return err
		}
	}
	if _, err := os.Stat(s.profileNotesPath()); errors.Is(err, os.ErrNotExist) {
		if profile, readErr := s.readProfileLocked(); readErr == nil && strings.TrimSpace(profile.Notes) != "" {
			if err := os.MkdirAll(filepath.Dir(s.profileNotesPath()), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(s.profileNotesPath(), []byte(profile.Notes), 0o600); err != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	}
	return nil
}

func (s *Store) statePath() string {
	return filepath.Join(s.Root, "state")
}

func (s *Store) messagesPath() string {
	return filepath.Join(s.statePath(), "messages.jsonl")
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

func (s *Store) workspaceInstructionsPath() string {
	return filepath.Join(s.WorkspacePath(), "AGENTS.md")
}

type profileFile struct {
	Personality string `json:"personality"`
	Notes       string `json:"notes,omitempty"`
}

type hostSessionFile struct {
	ID        string    `json:"id,omitempty"`
	AdapterID string    `json:"adapter_id,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type chatStatesFile struct {
	Sessions map[string]legacyChatStateFile `json:"sessions,omitempty"`
}

type chatStateFile struct {
	ThreadID       string    `json:"thread_id,omitempty"`
	SessionIDs     []string  `json:"session_ids,omitempty"`
	LastTranscript string    `json:"last_transcript,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

type legacyChatStateFile struct {
	SessionID      string    `json:"session_id,omitempty"`
	LastTranscript string    `json:"last_transcript,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
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
		ID:        strings.TrimSpace(host.ID),
		AdapterID: strings.TrimSpace(host.AdapterID),
		UpdatedAt: host.UpdatedAt,
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
	var file chatStateFile
	if err := json.Unmarshal(raw, &file); err == nil {
		if strings.TrimSpace(file.ThreadID) == "" && len(file.SessionIDs) == 0 && strings.TrimSpace(file.LastTranscript) == "" && file.UpdatedAt.IsZero() {
			// Fall through to legacy / bootstrap handling.
		} else {
			return ChatState{
				ThreadID:       strings.TrimSpace(file.ThreadID),
				SessionIDs:     normalizeUniqueStrings(file.SessionIDs),
				LastTranscript: file.LastTranscript,
				UpdatedAt:      file.UpdatedAt,
			}, nil
		}
	}
	var legacy chatStatesFile
	if err := json.Unmarshal(raw, &legacy); err == nil && len(legacy.Sessions) > 0 {
		state := ChatState{
			ThreadID:   newChatThreadID(),
			SessionIDs: make([]string, 0, len(legacy.Sessions)),
			UpdatedAt:  time.Now().UTC(),
		}
		var newest legacyChatStateFile
		for sessionID, legacyState := range legacy.Sessions {
			sessionID = strings.TrimSpace(sessionID)
			if sessionID != "" {
				state.SessionIDs = append(state.SessionIDs, sessionID)
			}
			if legacyState.UpdatedAt.After(newest.UpdatedAt) || newest.UpdatedAt.IsZero() {
				newest = legacyState
			}
		}
		state.SessionIDs = normalizeUniqueStrings(state.SessionIDs)
		state.LastTranscript = newest.LastTranscript
		if !newest.UpdatedAt.IsZero() {
			state.UpdatedAt = newest.UpdatedAt
		}
		if err := s.writeChatStateLocked(state); err != nil {
			return ChatState{}, err
		}
		return state, nil
	}
	return ChatState{}, nil
}

func (s *Store) readProfileLocked() (profileFile, error) {
	raw, err := os.ReadFile(s.profilePath())
	if errors.Is(err, os.ErrNotExist) {
		return profileFile{Personality: defaultPersonality, Notes: "# Brain Profile\n\n"}, nil
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
	state.SessionIDs = normalizeUniqueStrings(state.SessionIDs)
	if state.ThreadID == "" {
		state.ThreadID = newChatThreadID()
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	}
	return writeJSONFile(s.ChatStatePath(), chatStateFile{
		ThreadID:       state.ThreadID,
		SessionIDs:     state.SessionIDs,
		LastTranscript: state.LastTranscript,
		UpdatedAt:      state.UpdatedAt,
	})
}

func (s *Store) collectChatSessionIDsLocked() []string {
	file, err := os.Open(s.messagesPath())
	if errors.Is(err, os.ErrNotExist) || err != nil {
		return nil
	}
	defer file.Close()
	seen := map[string]struct{}{}
	out := []string{}
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var message ChatMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			continue
		}
		sessionID := strings.TrimSpace(message.SessionID)
		if sessionID == "" {
			continue
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		seen[sessionID] = struct{}{}
		out = append(out, sessionID)
	}
	return out
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

func appendUniqueString(values *[]string, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, existing := range *values {
		if strings.TrimSpace(existing) == value {
			return false
		}
	}
	*values = append(*values, value)
	return true
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

func moveIfMissing(from string, to string) error {
	if from == to {
		return nil
	}
	if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(to); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	raw, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.WriteFile(to, raw, 0o600); err != nil {
		return err
	}
	return os.Remove(from)
}

const defaultWorkspaceInstructions = `# Brain Workspace

This directory is the private workspace for zen Brain.

- Keep durable user memory in memory.md.
- Keep personality and preference notes in profile.md.
- Use local files here for plans, reminders, inbox notes, and follow-up state.
- Do not use project repositories as Brain's default working directory.
- Brain is the user's scheduler: reduce decision load. For concrete work needing repository/tool execution, independent progress, parallelism, or follow-up, proactively create or reuse visible delegated agent sessions; stay here for chat, memory, synthesis, reminders, and decisions that fit the current context.
- Use the zen binary to delegate, send, inspect, and close agents. Do not call tmux directly. Common command shapes: zen agent list --json; zen agent spawn -name "<name>" -executor <executor> -cwd <workspace> -prompt "<task>"; zen agent capture -id <agent_id> --json; zen agent send -id <agent_id> -text "<message>" --submit=true; zen agent close -id <agent_id>.
- Keep delegated agent lifecycle ownership from spawn through inspection, follow-up, result consolidation, and close. Do not leave completed delegated sessions open after their output is no longer needed.
- Keep orchestration principles in Markdown, prompts, and agent instructions. Code should provide tools, context, persistence, visibility, and safety boundaries rather than rigid workflow gates.
- Treat Heartbeat wake messages as compact actionable deltas; inspect only what is needed, then act, summarize, or sleep.
- Ask only when critical context is missing, an action is high-risk or irreversible, credentials/permissions are needed, or the choice depends on the user's values; otherwise continue low-risk next steps and consolidate options with a recommendation.
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
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
