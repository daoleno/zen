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

const stateSchema = 4

const (
	maxCallbackRoutes = 64
	formattedVariant  = "formatted"
	plainVariant      = "plain"

	// Topic mapping lifecycle states. Routing authority is the Session ID;
	// these states mirror the Session's durable lifecycle so input can fail
	// closed exactly.
	topicStateActive    = "active"
	topicStateCompleted = "completed"
	topicStateStale     = "stale"

	// Topic operation kinds and states. Ops share the outbox's
	// pending -> dispatching -> sent/failed/ambiguous discipline because
	// createForumTopic has no caller-supplied idempotency key: only a definite
	// API rejection may retry, a transport-indeterminate outcome stays
	// ambiguous and is never retried automatically.
	topicOpCreate = "create"
	topicOpRename = "rename"
	topicOpClose  = "close"
	topicOpReopen = "reopen"
	topicOpDelete = "delete"
)

type updateRecord struct {
	Disposition     string    `json:"disposition"`
	HandledAt       time.Time `json:"handled_at"`
	SessionID       string    `json:"session_id,omitempty"`
	BrainThreadID   string    `json:"brain_thread_id,omitempty"`
	MessageThreadID int64     `json:"message_thread_id,omitempty"`
}

type outboxRecord struct {
	ID              string                `json:"id"`
	Kind            string                `json:"kind"`
	CanonicalID     string                `json:"canonical_id,omitempty"`
	WorkID          string                `json:"work_id,omitempty"`
	SessionID       string                `json:"session_id,omitempty"`
	TopicKey        string                `json:"topic_key,omitempty"`
	Text            string                `json:"text"`
	PlainText       string                `json:"plain_text,omitempty"`
	Entities        []MessageEntity       `json:"entities,omitempty"`
	Variant         string                `json:"variant,omitempty"`
	ReplyMessageID  int64                 `json:"reply_message_id,omitempty"`
	ReplyMarkup     *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	MessageThreadID int64                 `json:"message_thread_id,omitempty"`
	MessageID       int64                 `json:"message_id,omitempty"`
	State           string                `json:"state"`
	AttemptAt       time.Time             `json:"attempt_at,omitempty"`
	CreatedAt       time.Time             `json:"created_at"`
}

type topicMapping struct {
	SessionID       string    `json:"session_id"`
	ThreadID        string    `json:"thread_id,omitempty"`
	WorkID          string    `json:"work_id,omitempty"`
	ChatID          int64     `json:"chat_id"`
	MessageThreadID int64     `json:"message_thread_id"`
	Label           string    `json:"label,omitempty"`
	State           string    `json:"state"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type topicOpRecord struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	SessionID       string    `json:"session_id,omitempty"`
	MessageThreadID int64     `json:"message_thread_id,omitempty"`
	Label           string    `json:"label,omitempty"`
	ThreadID        string    `json:"thread_id,omitempty"`
	WorkID          string    `json:"work_id,omitempty"`
	State           string    `json:"state"`
	AttemptAt       time.Time `json:"attempt_at,omitempty"`
	Attempts        int       `json:"attempts,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type durableState struct {
	Schema             int                     `json:"schema"`
	Enabled            bool                    `json:"enabled"`
	BotID              int64                   `json:"bot_id,omitempty"`
	BotName            string                  `json:"bot_name,omitempty"`
	BotUsername        string                  `json:"bot_username,omitempty"`
	TopicsAvailable    bool                    `json:"topics_available,omitempty"`
	UsersCreateTopics  bool                    `json:"users_create_topics,omitempty"`
	BrainTopicID       int64                   `json:"brain_topic_id,omitempty"`
	BrainReplyTopicID  int64                   `json:"brain_reply_topic_id,omitempty"`
	BrainTopics        []int64                 `json:"brain_topics,omitempty"`
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
	Topics             []topicMapping          `json:"topics,omitempty"`
	TopicOps           []topicOpRecord         `json:"topic_ops,omitempty"`
	TopicProjection    map[string]string       `json:"topic_projection,omitempty"`
	TopicMessages      map[string]int64        `json:"topic_messages,omitempty"`
	FallbackSessionID  string                  `json:"fallback_session_id,omitempty"`
	FallbackStartedAt  time.Time               `json:"fallback_started_at,omitempty"`
	TopicNotice        string                  `json:"topic_notice,omitempty"`
	CallbackRoutes     map[string]string       `json:"callback_routes,omitempty"`
	SessionChoices     []string                `json:"session_choices,omitempty"`
	CallbackIDs        map[string]int64        `json:"callback_ids,omitempty"`
	ReplySessions      map[int64]string        `json:"reply_sessions,omitempty"`
	RetryAt            time.Time               `json:"retry_at,omitempty"`
	DeliveryStartedAt  time.Time               `json:"delivery_started_at,omitempty"`
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
	migrated := false
	data, err := os.ReadFile(s.statePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s.state); err != nil {
			return nil, fmt.Errorf("decode Telegram state: %w", err)
		}
		switch s.state.Schema {
		case 1:
			for index := range s.state.Outbox {
				s.state.Outbox[index].PlainText = s.state.Outbox[index].Text
				s.state.Outbox[index].Variant = "plain"
			}
			s.state.Schema = stateSchema
			migrated = true
		case 2:
			// Schema 2 -> 3 only introduces topic state maps/slices, which
			// did not exist before; every existing row is preserved as-is.
			s.state.Schema = stateSchema
			migrated = true
		case 3:
			// Schema 3 already contains durable topic state. Schema 4 adds the
			// non-topic private-chat recipient and a non-fatal capability notice.
			s.state.Schema = stateSchema
			migrated = true
		case stateSchema:
		default:
			return nil, fmt.Errorf("unsupported Telegram state schema")
		}
	}
	s.ensureMapsLocked()
	changed := migrated
	for index := range s.state.Outbox {
		if s.state.Outbox[index].State == "dispatching" {
			s.state.Outbox[index].State = "ambiguous"
			changed = true
		}
	}
	for index := range s.state.TopicOps {
		if s.state.TopicOps[index].State == "dispatching" {
			s.state.TopicOps[index].State = "ambiguous"
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
	return durableState{Schema: stateSchema, Processed: map[string]updateRecord{}, Projection: map[string]string{}, WorkMessages: map[string]int64{},
		TopicProjection: map[string]string{}, TopicMessages: map[string]int64{}}
}

func (s *store) ensureMapsLocked() {
	ensureDurableMaps(&s.state)
}

func (s *store) snapshot() durableState {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(s.state)
	var copy durableState
	_ = json.Unmarshal(data, &copy)
	ensureDurableMaps(&copy)
	return copy
}

func (s *store) mutate(fn func(*durableState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	var next durableState
	if err := json.Unmarshal(raw, &next); err != nil {
		return err
	}
	ensureDurableMaps(&next)
	if err := fn(&next); err != nil {
		return err
	}
	ensureDurableMaps(&next)
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicPrivateWrite(s.statePath, append(data, '\n')); err != nil {
		return err
	}
	s.state = next
	return nil
}

func ensureDurableMaps(state *durableState) {
	if state.Processed == nil {
		state.Processed = map[string]updateRecord{}
	}
	if state.Projection == nil {
		state.Projection = map[string]string{}
	}
	if state.WorkMessages == nil {
		state.WorkMessages = map[string]int64{}
	}
	if state.TopicProjection == nil {
		state.TopicProjection = map[string]string{}
	}
	if state.TopicMessages == nil {
		state.TopicMessages = map[string]int64{}
	}
	if state.CallbackRoutes == nil {
		state.CallbackRoutes = map[string]string{}
	}
	if state.CallbackIDs == nil {
		state.CallbackIDs = map[string]int64{}
	}
	if state.ReplySessions == nil {
		state.ReplySessions = map[int64]string{}
	}
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
