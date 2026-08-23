package telegram

import (
	"context"
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
	MessageID       int64    `json:"message_id"`
	MessageThreadID int64    `json:"message_thread_id,omitempty"`
	From            *User    `json:"from,omitempty"`
	SenderChat      *Chat    `json:"sender_chat,omitempty"`
	Chat            Chat     `json:"chat"`
	Text            string   `json:"text,omitempty"`
	Caption         string   `json:"caption,omitempty"`
	ReplyToMessage  *Message `json:"reply_to_message,omitempty"`
	Photo           []any    `json:"photo,omitempty"`
	Document        any      `json:"document,omitempty"`
	Audio           any      `json:"audio,omitempty"`
	Video           any      `json:"video,omitempty"`
	Voice           any      `json:"voice,omitempty"`
	Sticker         any      `json:"sticker,omitempty"`
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
	ChatID           int64  `json:"chat_id"`
	Text             string `json:"text"`
	ReplyToMessageID int64  `json:"-"`
}

type EditRequest struct {
	ChatID    int64  `json:"chat_id"`
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
}

type API interface {
	GetMe(ctx context.Context, token string) (User, error)
	GetWebhookInfo(ctx context.Context, token string) (WebhookInfo, error)
	GetUpdates(ctx context.Context, token string, offset int64, timeoutSeconds int, allowed []string) ([]Update, error)
	SendMessage(ctx context.Context, token string, request SendRequest) (Message, error)
	EditMessage(ctx context.Context, token string, request EditRequest) (Message, error)
}

type APIError struct {
	Code       int
	RetryAfter time.Duration
	Retryable  bool
}

func (e *APIError) Error() string {
	return "Telegram Bot API request failed"
}

type ConnectionState string

const (
	StateDisabled     ConnectionState = "disabled"
	StateSetupPending ConnectionState = "setup_pending"
	StateConnected    ConnectionState = "connected"
	StateDegraded     ConnectionState = "degraded"
)

type Status struct {
	State             ConnectionState `json:"state"`
	Enabled           bool            `json:"enabled"`
	BotName           string          `json:"bot_name,omitempty"`
	BotUsername       string          `json:"bot_username,omitempty"`
	OwnerHint         string          `json:"owner_hint,omitempty"`
	BindingPending    bool            `json:"binding_pending"`
	TopicsAvailable   bool            `json:"topics_available,omitempty"`
	LastReceiveAt     *time.Time      `json:"last_receive_at,omitempty"`
	LastSendAt        *time.Time      `json:"last_send_at,omitempty"`
	LastError         string          `json:"last_error,omitempty"`
	WebhookConflict   bool            `json:"webhook_conflict,omitempty"`
	AmbiguousDelivery int             `json:"ambiguous_delivery_count,omitempty"`
}

type BindingChallenge struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}
