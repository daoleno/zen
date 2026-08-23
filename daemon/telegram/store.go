package telegram

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const stateSchema = 1

type updateRecord struct {
	Disposition string    `json:"disposition"`
	HandledAt   time.Time `json:"handled_at"`
}

type outboxRecord struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	CanonicalID    string    `json:"canonical_id,omitempty"`
	WorkID         string    `json:"work_id,omitempty"`
	Text           string    `json:"text"`
	ReplyMessageID int64     `json:"reply_message_id,omitempty"`
	MessageID      int64     `json:"message_id,omitempty"`
	State          string    `json:"state"`
	AttemptAt      time.Time `json:"attempt_at,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type durableState struct {
	Schema             int                     `json:"schema"`
	Enabled            bool                    `json:"enabled"`
	BotID              int64                   `json:"bot_id,omitempty"`
	BotName            string                  `json:"bot_name,omitempty"`
	BotUsername        string                  `json:"bot_username,omitempty"`
	TopicsAvailable    bool                    `json:"topics_available,omitempty"`
	OwnerID            int64                   `json:"owner_id,omitempty"`
	OwnerHint          string                  `json:"owner_hint,omitempty"`
	ChatID             int64                   `json:"chat_id,omitempty"`
	ChallengeSHA256    string                  `json:"challenge_sha256,omitempty"`
	ChallengeExpiresAt time.Time               `json:"challenge_expires_at,omitempty"`
	NextOffset         int64                   `json:"next_offset,omitempty"`
	Processed          map[string]updateRecord `json:"processed,omitempty"`
	Outbox             []outboxRecord          `json:"outbox,omitempty"`
	Projection         map[string]string       `json:"projection,omitempty"`
	WorkMessages       map[string]int64        `json:"work_messages,omitempty"`
	LastReceiveAt      *time.Time              `json:"last_receive_at,omitempty"`
	LastSendAt         *time.Time              `json:"last_send_at,omitempty"`
	LastError          string                  `json:"last_error,omitempty"`
	WebhookConflict    bool                    `json:"webhook_conflict,omitempty"`
}

type store struct {
	mu        sync.Mutex
	dir       string
	statePath string
	tokenPath string
	state     durableState
}

func openStore(root string) (*store, error) {
	dir := filepath.Join(root, "telegram")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &store{dir: dir, statePath: filepath.Join(dir, "state.json"), tokenPath: filepath.Join(dir, "token")}
	s.state = newDurableState()
	data, err := os.ReadFile(s.statePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s.state); err != nil {
			return nil, fmt.Errorf("decode Telegram state: %w", err)
		}
		if s.state.Schema != stateSchema {
			return nil, fmt.Errorf("unsupported Telegram state schema")
		}
	}
	s.ensureMapsLocked()
	changed := false
	for index := range s.state.Outbox {
		if s.state.Outbox[index].State == "dispatching" {
			s.state.Outbox[index].State = "ambiguous"
			changed = true
		}
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func newDurableState() durableState {
	return durableState{Schema: stateSchema, Processed: map[string]updateRecord{}, Projection: map[string]string{}, WorkMessages: map[string]int64{}}
}

func (s *store) ensureMapsLocked() {
	if s.state.Processed == nil {
		s.state.Processed = map[string]updateRecord{}
	}
	if s.state.Projection == nil {
		s.state.Projection = map[string]string{}
	}
	if s.state.WorkMessages == nil {
		s.state.WorkMessages = map[string]int64{}
	}
}

func (s *store) snapshot() durableState {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(s.state)
	var copy durableState
	_ = json.Unmarshal(data, &copy)
	return copy
}

func (s *store) mutate(fn func(*durableState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.state); err != nil {
		return err
	}
	s.ensureMapsLocked()
	return s.saveLocked()
}

func (s *store) saveLocked() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return atomicPrivateWrite(s.statePath, append(data, '\n'))
}

func (s *store) readToken() (string, error) {
	data, err := os.ReadFile(s.tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (s *store) replaceToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("Telegram token is required")
	}
	return atomicPrivateWrite(s.tokenPath, []byte(token+"\n"))
}

func (s *store) removeToken() error {
	err := os.Remove(s.tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func atomicPrivateWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".telegram-*.partial")
	if err != nil {
		return err
	}
	partial := file.Name()
	keep := true
	defer func() {
		if keep {
			_ = os.Remove(partial)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(partial, path); err != nil {
		return err
	}
	keep = false
	if directory, err := os.Open(dir); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
