package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const maxResponseBytes = 2 << 20

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string, client *http.Client) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.telegram.org"
	}
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = 35 * time.Second
		transport.IdleConnTimeout = 60 * time.Second
		client = &http.Client{Transport: transport, Timeout: 45 * time.Second}
	}
	privateClient := *client
	// Redirects must never carry a bot credential to a different endpoint.
	privateClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &privateClient}
}

func (c *Client) GetFile(ctx context.Context, token, fileID string) (File, error) {
	var file File
	err := c.call(ctx, token, "getFile", map[string]string{"file_id": fileID}, &file)
	return file, err
}

func (c *Client) DownloadFile(ctx context.Context, token string, file File) (io.ReadCloser, error) {
	p := file.FilePath
	if token == "" || p == "" || len(p) > 1024 || path.Clean(p) != p || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\%:?#\x00\r\n") {
		return nil, fmt.Errorf("Telegram file path is invalid")
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." || segment == "." || segment == "" {
			return nil, fmt.Errorf("Telegram file path is invalid")
		}
	}
	if file.FileSize < 0 || file.FileSize > maxTelegramFileBytes {
		return nil, fmt.Errorf("Telegram file exceeds the 20 MiB download limit")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/file/bot"+url.PathEscape(token)+"/"+p, nil)
	if err != nil {
		return nil, fmt.Errorf("Telegram file request is invalid")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Telegram file download unavailable")
	}
	if response.StatusCode != http.StatusOK || response.ContentLength > maxTelegramFileBytes {
		_ = response.Body.Close()
		return nil, fmt.Errorf("Telegram file download rejected or too large")
	}
	return response.Body, nil
}

func (c *Client) SetMessageReaction(ctx context.Context, token string, request ReactionRequest) error {
	return c.call(ctx, token, "setMessageReaction", request, nil)
}

func (c *Client) PinChatMessage(ctx context.Context, token string, chatID, messageID int64) error {
	return c.call(ctx, token, "pinChatMessage", map[string]any{"chat_id": chatID, "message_id": messageID, "disable_notification": true}, nil)
}

type apiEnvelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  responseParams  `json:"parameters"`
}

type responseParams struct {
	RetryAfter int `json:"retry_after"`
}

func (c *Client) call(ctx context.Context, token, method string, body any, out any) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("Telegram credential is unavailable")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Telegram request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	requestURL := c.baseURL + "/bot" + url.PathEscape(token) + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, reader)
	if err != nil {
		return fmt.Errorf("create Telegram request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Telegram transport unavailable")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return fmt.Errorf("Telegram response unavailable")
	}
	var envelope apiEnvelope
	if json.Unmarshal(data, &envelope) != nil {
		return fmt.Errorf("Telegram response invalid")
	}
	if !envelope.OK {
		code := envelope.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &APIError{
			Code: code, RetryAfter: time.Duration(envelope.Parameters.RetryAfter) * time.Second,
			Retryable: code == http.StatusTooManyRequests || code >= 500, description: envelope.Description,
		}
	}
	if out != nil && json.Unmarshal(envelope.Result, out) != nil {
		return fmt.Errorf("Telegram result invalid")
	}
	return nil
}

func (c *Client) GetMe(ctx context.Context, token string) (User, error) {
	var result User
	err := c.call(ctx, token, "getMe", nil, &result)
	return result, err
}

func (c *Client) GetWebhookInfo(ctx context.Context, token string) (WebhookInfo, error) {
	var result WebhookInfo
	err := c.call(ctx, token, "getWebhookInfo", nil, &result)
	return result, err
}

func (c *Client) GetUpdates(ctx context.Context, token string, offset int64, timeoutSeconds int, allowed []string) ([]Update, error) {
	var result []Update
	err := c.call(ctx, token, "getUpdates", map[string]any{
		"offset": offset, "timeout": timeoutSeconds, "limit": 100, "allowed_updates": allowed,
	}, &result)
	return result, err
}

func (c *Client) SendMessage(ctx context.Context, token string, request SendRequest) (Message, error) {
	body := map[string]any{"chat_id": request.ChatID, "text": request.Text}
	if request.MessageThreadID != 0 {
		body["message_thread_id"] = request.MessageThreadID
	}
	if len(request.Entities) > 0 {
		body["entities"] = request.Entities
	}
	if request.ReplyToMessageID != 0 {
		body["reply_parameters"] = map[string]any{"message_id": request.ReplyToMessageID}
	}
	if request.ReplyMarkup != nil {
		body["reply_markup"] = request.ReplyMarkup
	}
	var result Message
	err := c.call(ctx, token, "sendMessage", body, &result)
	return result, err
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, token, callbackID, text string) error {
	body := map[string]any{"callback_query_id": callbackID}
	if strings.TrimSpace(text) != "" {
		body["text"] = text
	}
	return c.call(ctx, token, "answerCallbackQuery", body, nil)
}

// EditMessage uses editMessageText, which has no message_thread_id parameter:
// an edit addresses an existing message by id and inherits its thread.
func (c *Client) EditMessage(ctx context.Context, token string, request EditRequest) (Message, error) {
	var result Message
	err := c.call(ctx, token, "editMessageText", request, &result)
	return result, err
}

func (c *Client) SendChatAction(ctx context.Context, token string, request ChatActionRequest) error {
	return c.call(ctx, token, "sendChatAction", request, nil)
}

func (c *Client) CreateForumTopic(ctx context.Context, token string, request CreateForumTopicRequest) (ForumTopic, error) {
	var result ForumTopic
	err := c.call(ctx, token, "createForumTopic", request, &result)
	return result, err
}

func (c *Client) EditForumTopic(ctx context.Context, token string, request EditForumTopicRequest) error {
	return c.call(ctx, token, "editForumTopic", request, nil)
}

func (c *Client) CloseForumTopic(ctx context.Context, token string, request ForumTopicIDRequest) error {
	return c.call(ctx, token, "closeForumTopic", request, nil)
}

func (c *Client) ReopenForumTopic(ctx context.Context, token string, request ForumTopicIDRequest) error {
	return c.call(ctx, token, "reopenForumTopic", request, nil)
}

func (c *Client) DeleteForumTopic(ctx context.Context, token string, request ForumTopicIDRequest) error {
	return c.call(ctx, token, "deleteForumTopic", request, nil)
}

func retryDelay(err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	return 0
}

func retryable(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Retryable
}

func formattingRejected(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != http.StatusBadRequest || apiErr.Retryable {
		return false
	}
	description := strings.ToLower(apiErr.description)
	for _, phrase := range []string{
		"can't parse entities",
		"cant parse entities",
		"failed to parse entities",
		"unsupported start tag",
		"entity bounds",
		"entity length",
		"entities are too long",
		"wrong http url",
		"unsupported url protocol",
	} {
		if strings.Contains(description, phrase) {
			return true
		}
	}
	return false
}
