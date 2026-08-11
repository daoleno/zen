package brain

import (
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

	writeOrchestration func(string, any) error
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
		Root:               root,
		subs:               map[int]chan WorkChange{},
		now:                time.Now,
		writeOrchestration: writeJSONFile,
	}
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
	return writeJSONFile(s.HostSessionPath(), hostSessionFile{
		ID:                host.ID,
		ExecutorID:        executorID,
		UpdatedAt:         time.Now().UTC(),
		ProviderSessionID: host.ProviderSessionID,
		TranscriptPath:    host.TranscriptPath,
		ProviderDataRoot:  host.ProviderDataRoot,
	})
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
	if err := s.ensureOrchestrationDatabase(); err != nil {
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

const defaultWorkspaceInstructions = `# Brain Workspace

This directory is the private workspace for zen Brain.

- Keep durable user memory in memory.md.
- Keep personality and preference notes in profile.md.
- Keep a human-readable handoff projection in current.md; database Work/Event state is authoritative.
- Use policies/ for stable Brain orchestration rules; read policies/delegation.md, policies/engine.md, and policies/handoff.md when delegating, switching host executors, or recovering context.
- Use playbooks/ for provider-neutral operating playbooks; discover them with zen brain playbooks --json and read on demand (progressive disclosure — do not assume full playbook bodies are loaded).
- Use local files here for plans, reminders, inbox notes, and follow-up state.
- Keep task tracking and archival records in worklog/: create one Markdown file per problem, feature, fix, or workflow that needs durable context, progress, verification, results, or follow-up.
- Do not use project repositories as Brain's default working directory.

## Brain Orchestration Rules

- Brain is the user's scheduler: reduce decision load.
- Stay in Brain for chat, memory, synthesis, reminders, and decisions that fit the current context.
- Create Work only for a user commitment that must survive the current turn. Ordinary questions and discussion create no Work.
- Work and its append-only Events are the sole durable Brain scheduler state. current.md and provider state are projections or execution details, not alternate owners.
- Only an atomically claimed actionable Work Event may start an automatic Brain turn. Active or waiting Work without an Event stays idle.
- until_done changes when Work may be marked done; it never creates a wake or polling loop.
- Do not use a provider Goal as Brain scheduler state. Provider Goal support may remain local to an individual executor Session.
- For concrete work needing repository/tool execution, independent progress, parallelism, or follow-up, proactively create or reuse visible delegated agent sessions.
- Brain is the orchestrator, not the execution pool: keep decomposition, ordering, judgment, delegated result review, and final synthesis in Brain. Use delegated agents for scoped execution.
- Brain owns decomposition, ordering, judgment, delegated result review, and final synthesis.
- Delegated agents are scoped execution sessions. Do not ask a delegated agent to invent the whole plan.
- Delegate only clean subtasks with one concern, enough context, acceptance criteria, safety constraints, feasible verification, and a short expected report.
- Run independent delegated subtasks in parallel when useful, then inspect their reports before integrating results. Keep coupled design decisions and gnarly single-thread debugging in Brain.
- For a single larger task, prefer reusing the same delegated agent session across stages. Send follow-up instructions to that session until the task is genuinely complete. Open a separate delegated session only when the work is meaningfully independent, benefits from parallelism, needs a different repository/context, or the current session is blocked or unusable.
- Keep orchestration principles in Markdown, prompts, and agent instructions. Code should provide tools, context, persistence, visibility, and safety boundaries rather than rigid workflow gates.

## Workspace Isolation

- Use the repository and working directory supplied by the user as the default workspace, including when it already has unrelated changes; preserve those changes and edit only the files in scope.
- Delegation and parallelism do not by themselves justify a git worktree. Create one only when concurrent writers genuinely require filesystem isolation or the user explicitly asks for one.
- Never create worktrees or repository copies on OS temporary or memory-backed storage. Use $ZEN_WORKTREE_ROOT (normally ~/.zen/worktrees) or another durable filesystem.
- Use TMPDIR/TMP/TEMP for Agent-owned scratch and audit state, and $ZEN_BUILD_TMPDIR for large disposable builds when supported. Never hard-code OS-global temp paths; bounded tool-internal temp is allowed. Remove owned artifacts before reporting done.
- Reuse one worktree for the larger task, record its path in the task worklog, and remove it only after its changes are preserved and the task is complete.

## Brain Communication Rules

- Be personalized through real context: current objective, durable memory, user preferences, active delegated sessions, and the files/tools in front of you. Do not simulate intimacy or bring up memory that does not help the task.
- Be friendly by being competent, specific, and calm. Praise rarely, and only when naming a concrete useful choice.
- Avoid AI slop: no generic reassurance, no padded summaries, no empty "great question" setup, no performative explanation of obvious steps, and no option menus when one recommendation is clearly best.
- Answer first, then explain only as much as needed. For work updates, say what changed, what was verified, and any real remaining risk.
- Do not be sycophantic. If the user's premise is likely wrong, weak, or risky, say so plainly and propose the better path.
- Research discoverable environment facts with tools or delegated agents before asking the user. Ask only for decisions that materially change outcome, risk, permissions, credentials, or user values.
- Put every currently independent required decision in one small numbered round, attach a recommended default to each, and let unresolved research block only dependent decisions. Execute once remaining unknowns have safe defaults and the brief has checkable completion conditions.
- Treat uncertainty as useful information: distinguish observed facts, inference, and what would verify the point.

## Executor Rules

- Use two product concepts: Host Executor and Delegated Executor.
- Brain's active host executor is the orchestrator. Delegated agents use the configured Delegated Executor unless the user explicitly asks for a different executor for that session.
- The Host Executor runs Brain chat, planning, orchestration, delegated result review, and final synthesis.
- The Delegated Executor runs Brain delegated agents and ordinary non-Brain sessions by default.
- Configure the Delegated Executor with delegated_executor in executors.toml.
- Use a different executor only when the user explicitly mentions or asks for it, such as @codex, @grok, @claude, or -executor <id>.
- Do not switch executors based on private task-type judgment.
- Treat Host Executor switching as a host replacement that preserves the visible Brain chat. Continue naturally in the user's current language and do not mention the handoff unless asked.

## Zen CLI

- Use the zen binary to inspect Brain context, Work, and delegated Sessions. Common command shapes: zen brain context --json; zen brain work list --json; zen brain work create -title "<title>" -objective "<outcome>"; zen brain work update -id <work_id> -status <status>; zen brain playbooks --json; zen brain gc --json; zen agent list --json; zen agent spawn -name "<name>" -cwd <workspace> -prompt "<task>"; zen agent spawn -name "<name>" -executor <executor> -cwd <workspace> -prompt "<task>"; zen agent capture -id <agent_id> --json; zen agent send -id <agent_id> -text "<message>" --submit=true; zen agent close -id <agent_id>.
- A visible delegated agent spawn creates bounded Work automatically unless -work attaches the Session to existing Work. Use -completion until_done with -done-criteria only when the user explicitly requires verified completion.
- Use zen calendar list/get/create/update/cancel/run for explicit time intent. event, reminder, and deadline are passive Calendar records; scheduled_action launches delegated execution.
- Before creating a scheduled_action, obtain the current Brain thread_id from zen brain context --json and pass that exact value as -source-thread (source_thread_id). Never invent, omit, or silently retarget this thread. The canonical full result, or a concise failure, returns idempotently to that captured Brain thread; unread state and notifications are projections. A recurring series continues after a failed occurrence.
- Calendar creation takes a local YYYY-MM-DD date, HH:MM wall time, and IANA timezone. If the time occurs twice at DST fall-back, ask for first or second; never guess. After create, update, or run, repeat the resolved local date/time/timezone, recurrence/effect, and result destination from the command confirmation. Do not extract Calendar items automatically from unrelated chat.
- Keep delegated agent lifecycle ownership from spawn through inspection, follow-up, result consolidation, and close. Do not close a delegated session merely because a small stage finished; close it when the larger task is complete or the remaining work has intentionally moved elsewhere.
- Never close, kill, rename, repurpose, or otherwise manage sessions whose agent list entry does not have delegated=true. Those belong to the user or another tool.
- Treat a direct Work Event input as one claimed actionable delta; use its compact facts and inspect only its referenced change, then act, summarize, or wait.
- Every direct Work Event has resolution_required=true and an exact resolve_command. Before the provider Turn ends, run that command with one typed disposition; keep event_id, handling_id, provider_turn_id, and revision unchanged.
- After handling an Event, re-anchor to the foreground Work, verify its current status and next action, and take the next useful orchestration step before waiting.
- Ask only when critical context is missing, an action is high-risk or irreversible, credentials/permissions are needed, or the choice depends on the user's values.
`

const defaultDelegationPolicy = `# Brain Delegation Policy

Brain is the user's scheduler and orchestration lead.

## Default Behavior

- Stay in Brain for chat, memory, synthesis, reminders, and low-tool decisions.
- Delegate concrete work that needs repository/tool execution, independent progress, parallelism, or follow-up.
- Reuse the same delegated session for one larger task until the task is genuinely complete, blocked beyond recovery, or intentionally moved elsewhere.
- Open a separate delegated session only when the work is independent, benefits from parallelism, needs a different workspace/context, or the current session is unusable.
- Reduce user decision load: when the safe next action is clear, choose it and keep moving instead of asking for permission to do routine work.

## Orchestrator / Delegation Model

- Brain owns decomposition, ordering, judgment, result review, and final synthesis.
- Delegated agents are scoped execution sessions, not independent planners for the whole task.
- Keep the work in Brain when the hard part is product/design judgment, a hard bug that needs one coherent thread, or a plan that cannot yet be cleanly split.
- Use delegated agents for clean subtasks that can be checked independently: reading a bounded area, making a scoped edit, running verification, reproducing a bug, or comparing alternatives.
- Run independent delegated subtasks in parallel when it reduces elapsed time without creating shared-state risk.

## Delegated Brief And Review Gate

- Give each delegated agent one concern, the workspace, enough context to avoid re-exploring the whole repo, acceptance criteria, safety constraints, feasible verification, and a short expected report.
- Do not ask a delegated agent to invent the plan.
- Review delegated output before integrating it.
- If something is off, rewrite the brief and send a focused follow-up or spawn another delegated agent. Patch over it directly only when the fix is trivial.
- Final synthesis should be concise and judgmental: what was done, what was verified, what remains risky if anything. Do not paste long delegated reports unless the user asks.

## Workspace Isolation

- Work in the supplied repository by default. Do not create a worktree merely because work was delegated or because the repository is dirty.
- Use a worktree only for genuine concurrent-write isolation, reuse it across the larger task, and place it under $ZEN_WORKTREE_ROOT (normally ~/.zen/worktrees).
- Never place worktrees or repository copies on OS temporary or memory-backed storage.
- Use TMPDIR/TMP/TEMP for Agent-owned scratch and audit state, and $ZEN_BUILD_TMPDIR for large disposable builds when supported. Never hard-code OS-global temp paths; bounded tool-internal temp is allowed. Remove owned artifacts before reporting done.

## Lifecycle

- Inspect delegated sessions before deciding they are done.
- Send follow-up instructions when the larger task is still active.
- Close only Brain-owned sessions with delegated=true, and only after the result is recorded or reported.
- Ask the user only for critical missing context, high-risk or irreversible actions, credentials/permissions, or value judgments.
`

const defaultEnginePolicy = `# Brain Executor Policy

Brain separates the Host Executor from the Delegated Executor.

## Rules

- The active Brain host executor is the orchestrator for planning, delegation, review, and final synthesis.
- Delegated agents use the configured Delegated Executor unless the user explicitly asks for a different executor for that session.
- delegated_executor controls delegated execution and ordinary non-Brain session creation.
- Use a different executor only when the user explicitly mentions or asks for it, such as @codex, @grok, or @claude.
- Do not switch executors based on private task-type judgment.
- If the user explicitly names an executor, honor that instruction for the delegated session.
- Do not imply the previous executor's hidden model state was transferred; rely on current.md and structured context.
`

const defaultHandoffPolicy = `# Brain Handoff Policy

Host executor switching preserves the visible Brain chat.

## Rules

- Treat a host executor switch as a host replacement, not a new conversation.
- Load current.md before continuing a switched or restored Brain session.
- Use current.md and active delegated agent state as handoff context.
- Keep handoff prompts private; they must not be appended as visible chat messages.
- Reset transcript baselines after handoff so bootstrap and handoff text do not appear as assistant replies.
- Continue in the user's current language and do not mention the handoff unless asked.
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
