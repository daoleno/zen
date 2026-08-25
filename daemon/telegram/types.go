package telegram

import (
	"context"
	"fmt"
	"time"
)

type User struct {
	ID         int64  `json:"id"`
	IsBot      bool   `json:"is_bot"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name,omitempty"`
	Username   string `json:"username,omitempty"`
	Topics     bool   `json:"has_topics_enabled,omitempty"`
	UserTopics bool   `json:"allows_users_to_create_topics,omitempty"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type Message struct {
	MessageID         int64              `json:"message_id"`
	MessageThreadID   int64              `json:"message_thread_id,omitempty"`
	IsTopicMessage    bool               `json:"is_topic_message,omitempty"`
	ForumTopicCreated *ForumTopicCreated `json:"forum_topic_created,omitempty"`
	ForumTopicEdited  *ForumTopicEdited  `json:"forum_topic_edited,omitempty"`
	From              *User              `json:"from,omitempty"`
	SenderChat        *Chat              `json:"sender_chat,omitempty"`
	Chat              Chat               `json:"chat"`
	Text              string             `json:"text,omitempty"`
	Caption           string             `json:"caption,omitempty"`
	ReplyToMessage    *Message           `json:"reply_to_message,omitempty"`
	Photo             []any              `json:"photo,omitempty"`
	Document          any                `json:"document,omitempty"`
	Audio             any                `json:"audio,omitempty"`
	Video             any                `json:"video,omitempty"`
	Voice             any                `json:"voice,omitempty"`
	Sticker           any                `json:"sticker,omitempty"`
}

type ForumTopicCreated struct {
	Name              string `json:"name"`
	IconColor         int    `json:"icon_color"`
	IconCustomEmojiID string `json:"icon_custom_emoji_id,omitempty"`
}

type ForumTopicEdited struct {
	Name              string `json:"name,omitempty"`
	IconCustomEmojiID string `json:"icon_custom_emoji_id,omitempty"`
}

type ForumTopic struct {
	MessageThreadID   int64  `json:"message_thread_id"`
	Name              string `json:"name"`
	IconColor         int    `json:"icon_color,omitempty"`
	IconCustomEmojiID string `json:"icon_custom_emoji_id,omitempty"`
}

func (m Message) hasUnsupportedMedia() bool {
	return len(m.Photo) > 0 || m.Document != nil || m.Audio != nil || m.Video != nil ||
		m.Voice != nil || m.Sticker != nil
}

type Update struct {
	UpdateID      int64    `json:"update_id"`
	Message       *Message `json:"message,omitempty"`
	EditedMessage *Message `json:"edited_message,omitempty"`
}

type WebhookInfo struct {
	URL                string `json:"url"`
	PendingUpdateCount int    `json:"pending_update_count"`
}

type SendRequest struct {
	ChatID           int64           `json:"chat_id"`
	MessageThreadID  int64           `json:"message_thread_id,omitempty"`
	Text             string          `json:"text"`
	Entities         []MessageEntity `json:"entities,omitempty"`
	ReplyToMessageID int64           `json:"-"`
}

type EditRequest struct {
	ChatID    int64           `json:"chat_id"`
	MessageID int64           `json:"message_id"`
	Text      string          `json:"text"`
	Entities  []MessageEntity `json:"entities,omitempty"`
}

type CreateForumTopicRequest struct {
	ChatID int64  `json:"chat_id"`
	Name   string `json:"name"`
}

type EditForumTopicRequest struct {
	ChatID          int64  `json:"chat_id"`
	MessageThreadID int64  `json:"message_thread_id"`
	Name            string `json:"name,omitempty"`
}

type ForumTopicIDRequest struct {
	ChatID          int64 `json:"chat_id"`
	MessageThreadID int64 `json:"message_thread_id"`
}

type MessageEntity struct {
	Type     string `json:"type"`
	Offset   int    `json:"offset"`
	Length   int    `json:"length"`
	URL      string `json:"url,omitempty"`
	Language string `json:"language,omitempty"`
}

type ChatActionRequest struct {
	ChatID          int64  `json:"chat_id"`
	MessageThreadID int64  `json:"message_thread_id,omitempty"`
	Action          string `json:"action"`
}

type API interface {
	GetMe(ctx context.Context, token string) (User, error)
	GetWebhookInfo(ctx context.Context, token string) (WebhookInfo, error)
	GetUpdates(ctx context.Context, token string, offset int64, timeoutSeconds int, allowed []string) ([]Update, error)
	SendMessage(ctx context.Context, token string, request SendRequest) (Message, error)
	EditMessage(ctx context.Context, token string, request EditRequest) (Message, error)
	SendChatAction(ctx context.Context, token string, request ChatActionRequest) error
	CreateForumTopic(ctx context.Context, token string, request CreateForumTopicRequest) (ForumTopic, error)
	EditForumTopic(ctx context.Context, token string, request EditForumTopicRequest) error
	CloseForumTopic(ctx context.Context, token string, request ForumTopicIDRequest) error
	ReopenForumTopic(ctx context.Context, token string, request ForumTopicIDRequest) error
	DeleteForumTopic(ctx context.Context, token string, request ForumTopicIDRequest) error
}

type APIError struct {
	Code        int
	RetryAfter  time.Duration
	Retryable   bool
	description string
}

func (e *APIError) Error() string {
	return "Telegram Bot API request failed"
}

func (e *APIError) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte(e.Error()))
}

type ConnectionState string

const (
	StateDisabled     ConnectionState = "disabled"
	StateSetupPending ConnectionState = "setup_pending"
	StateConnected    ConnectionState = "connected"
	StateDegraded     ConnectionState = "degraded"
)

type Status struct {
	State               ConnectionState `json:"state"`
	Enabled             bool            `json:"enabled"`
	BotName             string          `json:"bot_name,omitempty"`
	BotUsername         string          `json:"bot_username,omitempty"`
	OwnerHint           string          `json:"owner_hint,omitempty"`
	BindingPending      bool            `json:"binding_pending"`
	TopicsAvailable     bool            `json:"topics_available,omitempty"`
	TopicMappings       int             `json:"topic_mappings,omitempty"`
	TopicAmbiguousOps   int             `json:"topic_ambiguous_ops_count,omitempty"`
	TopicFailedOps      int             `json:"topic_failed_ops_count,omitempty"`
	TopicFailedMessages int             `json:"topic_failed_messages_count,omitempty"`
	LastReceiveAt       *time.Time      `json:"last_receive_at,omitempty"`
	LastSendAt          *time.Time      `json:"last_send_at,omitempty"`
	LastError           string          `json:"last_error,omitempty"`
	WebhookConflict     bool            `json:"webhook_conflict,omitempty"`
	AmbiguousDelivery   int             `json:"ambiguous_delivery_count,omitempty"`
}

type BindingChallenge struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}
