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
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: client}
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
	var result Message
	err := c.call(ctx, token, "sendMessage", body, &result)
	return result, err
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
	} {
		if strings.Contains(description, phrase) {
			return true
		}
	}
	return false
}
