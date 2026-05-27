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
	UpdatedAt time.Time
}

func (s *Store) HostSession() (HostSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readHostSessionLocked()
}

func (s *Store) SetHostSessionID(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return writeJSONFile(s.HostSessionPath(), hostSessionFile{})
	}
	return writeJSONFile(s.HostSessionPath(), hostSessionFile{
		ID:        id,
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
	UpdatedAt time.Time `json:"updated_at,omitempty"`
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
		UpdatedAt: host.UpdatedAt,
	}, nil
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
- Create visible agent sessions only when the user asks Brain to delegate real work.
- Use the zen binary to delegate, send, and inspect agents. Do not call tmux directly.
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
