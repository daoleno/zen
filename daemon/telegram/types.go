package telegram

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/daoleno/zen/daemon/attachment"
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
	Entities          []MessageEntity    `json:"entities,omitempty"`
	CaptionEntities   []MessageEntity    `json:"caption_entities,omitempty"`
	MediaGroupID      string             `json:"media_group_id,omitempty"`
	Photo             []PhotoSize        `json:"photo,omitempty"`
	Document          *MediaFile         `json:"document,omitempty"`
	Audio             *MediaFile         `json:"audio,omitempty"`
	Video             *MediaFile         `json:"video,omitempty"`
	Voice             *MediaFile         `json:"voice,omitempty"`
	Animation         *MediaFile         `json:"animation,omitempty"`
	VideoNote         *MediaFile         `json:"video_note,omitempty"`
	Sticker           *Sticker           `json:"sticker,omitempty"`
}

type File struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
	FilePath     string `json:"file_path,omitempty"`
}

type PhotoSize struct {
	File
	Width  int `json:"width"`
	Height int `json:"height"`
}

type MediaFile struct {
	File
	FileName string `json:"file_name,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
}

type Sticker struct {
	File
	IsAnimated bool   `json:"is_animated,omitempty"`
	IsVideo    bool   `json:"is_video,omitempty"`
	Emoji      string `json:"emoji,omitempty"`
}

type FileAPI interface {
	GetFile(context.Context, string, string) (File, error)
	DownloadFile(context.Context, string, File) (io.ReadCloser, error)
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

func (m Message) hasMedia() bool {
	return len(m.Photo) > 0 || m.Document != nil || m.Audio != nil || m.Video != nil ||
		m.Voice != nil || m.Sticker != nil || m.Animation != nil || m.VideoNote != nil
}

type Update struct {
	UpdateID        int64                   `json:"update_id"`
	Message         *Message                `json:"message,omitempty"`
	EditedMessage   *Message                `json:"edited_message,omitempty"`
	CallbackQuery   *CallbackQuery          `json:"callback_query,omitempty"`
	MessageReaction *MessageReactionUpdated `json:"message_reaction,omitempty"`
}

type ReactionType struct {
	Type          string `json:"type"`
	Emoji         string `json:"emoji,omitempty"`
	CustomEmojiID string `json:"custom_emoji_id,omitempty"`
}

type MessageReactionUpdated struct {
	Chat        Chat           `json:"chat"`
	MessageID   int64          `json:"message_id"`
	User        *User          `json:"user,omitempty"`
	ActorChat   *Chat          `json:"actor_chat,omitempty"`
	Date        int64          `json:"date"`
	OldReaction []ReactionType `json:"old_reaction"`
	NewReaction []ReactionType `json:"new_reaction"`
}

type ReactionRequest struct {
	ChatID    int64          `json:"chat_id"`
	MessageID int64          `json:"message_id"`
	Reaction  []ReactionType `json:"reaction"`
}

type InteractionAPI interface {
	SetMessageReaction(context.Context, string, ReactionRequest) error
	PinChatMessage(context.Context, string, int64, int64) error
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from,omitempty"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

type WebhookInfo struct {
	URL                string `json:"url"`
	PendingUpdateCount int    `json:"pending_update_count"`
}

type SendRequest struct {
	ChatID           int64                 `json:"chat_id"`
	MessageThreadID  int64                 `json:"message_thread_id,omitempty"`
	Text             string                `json:"text"`
	Entities         []MessageEntity       `json:"entities,omitempty"`
	ReplyToMessageID int64                 `json:"-"`
	ReplyMarkup      *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
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

type MessageEntity = attachment.Entity

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
	AnswerCallbackQuery(ctx context.Context, token, callbackID, text string) error
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
	UsersCreateTopics   bool            `json:"users_create_topics,omitempty"`
	BrainTopicID        int64           `json:"brain_topic_id,omitempty"`
	BrainThreadID       string          `json:"brain_thread_id,omitempty"`
	TopicNotice         string          `json:"topic_notice,omitempty"`
	TopicMappings       int             `json:"topic_mappings,omitempty"`
	RecipientID         string          `json:"recipient_id,omitempty"`
	RecipientLabel      string          `json:"recipient_label,omitempty"`
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
