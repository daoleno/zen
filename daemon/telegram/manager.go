package telegram

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/daoleno/zen/daemon/brain"
)

const (
	challengeTTL       = 10 * time.Minute
	maxMessageText     = 4096
	maxOutboxRows      = 512
	maxProcessedUpdate = 512
)

type brainOwner interface {
	SubmitExternalUserInput(receipt, body string) (brain.ExternalInputDisposition, error)
	ChatThreadID() (string, error)
	ThreadTimeline(threadID string, limit int) ([]brain.TimelineItem, error)
	NewChat() (brain.Snapshot, error)
}

type Options struct {
	API         API
	Now         func() time.Time
	PollTimeout int
	Backoff     time.Duration
}

type Manager struct {
	store       *store
	api         API
	brain       brainOwner
	now         func() time.Time
	pollTimeout int
	backoff     time.Duration
	wake        chan struct{}
}

func NewManager(root string, owner *brain.Service) (*Manager, error) {
	return NewManagerWithOptions(root, owner, Options{})
}

func NewManagerWithOptions(root string, owner brainOwner, options Options) (*Manager, error) {
	state, err := openStore(root)
	if err != nil {
		return nil, err
	}
	api := options.API
	if api == nil {
		api = NewClient("", nil)
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	pollTimeout := options.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = 30
	}
	backoff := options.Backoff
	if backoff <= 0 {
		backoff = time.Second
	}
	manager := &Manager{store: state, api: api, brain: owner, now: now, pollTimeout: pollTimeout, backoff: backoff, wake: make(chan struct{}, 1)}
	if err := manager.initializeDeliveryBoundary(); err != nil {
		return nil, err
	}
	return manager, nil
}

// initializeDeliveryBoundary upgrades an already-bound connection without
// replaying timeline rows that predate this delivery contract. Only unsent
// canonical projections are discarded; direct replies and indeterminate
// dispatches retain their existing delivery semantics.
func (m *Manager) initializeDeliveryBoundary() error {
	state := m.store.snapshot()
	if state.OwnerID == 0 || state.ChatID == 0 || !state.DeliveryStartedAt.IsZero() {
		return nil
	}
	return m.store.mutate(func(current *durableState) error {
		if current.OwnerID == 0 || current.ChatID == 0 || !current.DeliveryStartedAt.IsZero() {
			return nil
		}
		current.DeliveryStartedAt = m.now().UTC()
		outbox := current.Outbox[:0]
		for _, row := range current.Outbox {
			if row.State == "pending" && row.CanonicalID != "" {
				continue
			}
			outbox = append(outbox, row)
		}
		current.Outbox = outbox
		return nil
	})
}

func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) Configure(ctx context.Context, token string) (Status, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return m.Status(), fmt.Errorf("Telegram token is required")
	}
	bot, err := m.api.GetMe(ctx, token)
	if err != nil || !bot.IsBot || bot.ID == 0 || strings.TrimSpace(bot.Username) == "" {
		return m.Status(), fmt.Errorf("Telegram bot credential could not be verified")
	}
	webhook, err := m.api.GetWebhookInfo(ctx, token)
	if err != nil {
		return m.Status(), fmt.Errorf("Telegram webhook status could not be verified")
	}
	oldToken, readErr := m.store.readToken()
	if readErr != nil {
		return m.Status(), fmt.Errorf("Telegram credential store unavailable")
	}
	if err := m.store.replaceToken(token); err != nil {
		return m.Status(), fmt.Errorf("Telegram credential store unavailable")
	}
	stateErr := m.store.mutate(func(state *durableState) error {
		if state.BotID != 0 && state.BotID != bot.ID {
			state.OwnerID, state.ChatID, state.OwnerHint = 0, 0, ""
			state.DeliveryStartedAt = time.Time{}
			state.NextOffset = 0
			state.Processed = map[string]updateRecord{}
			state.Outbox = nil
			state.Projection = map[string]string{}
			state.WorkMessages = map[string]int64{}
		}
		state.Enabled = true
		state.BotID = bot.ID
		state.BotName = displayName(bot)
		state.BotUsername = strings.TrimSpace(bot.Username)
		state.TopicsAvailable = bot.Topics
		state.WebhookConflict = strings.TrimSpace(webhook.URL) != ""
		state.LastError = ""
		if state.WebhookConflict {
			state.LastError = "A Telegram webhook is configured; remove it explicitly before long polling."
		}
		return nil
	})
	if stateErr != nil {
		if oldToken == "" {
			_ = m.store.removeToken()
		} else {
			_ = m.store.replaceToken(oldToken)
		}
		return m.Status(), fmt.Errorf("Telegram configuration could not be persisted")
	}
	m.signal()
	return m.Status(), nil
}

func (m *Manager) BeginBinding() (BindingChallenge, error) {
	state := m.store.snapshot()
	if !state.Enabled || state.BotID == 0 || strings.TrimSpace(state.BotUsername) == "" {
		return BindingChallenge{}, fmt.Errorf("Telegram bot must be configured first")
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return BindingChallenge{}, fmt.Errorf("create Telegram binding challenge")
	}
	challenge := base64.RawURLEncoding.EncodeToString(raw)
	expires := m.now().UTC().Add(challengeTTL)
	digest := sha256.Sum256([]byte(challenge))
	if err := m.store.mutate(func(current *durableState) error {
		current.ChallengeSHA256 = hex.EncodeToString(digest[:])
		current.ChallengeExpiresAt = expires
		return nil
	}); err != nil {
		return BindingChallenge{}, err
	}
	return BindingChallenge{URL: "https://t.me/" + state.BotUsername + "?start=" + challenge, ExpiresAt: expires}, nil
}

func (m *Manager) Disable() error {
	err := m.store.mutate(func(state *durableState) error {
		state.Enabled = false
		state.LastError = ""
		return nil
	})
	m.signal()
	return err
}

func (m *Manager) Enable() error {
	token, err := m.store.readToken()
	if err != nil || strings.TrimSpace(token) == "" {
		return fmt.Errorf("Telegram bot must be configured first")
	}
	err = m.store.mutate(func(state *durableState) error {
		if state.BotID == 0 {
			return fmt.Errorf("Telegram bot must be configured first")
		}
		state.Enabled = true
		state.LastError = ""
		return nil
	})
	m.signal()
	return err
}

func (m *Manager) RevokeOwner() error {
	return m.store.mutate(func(state *durableState) error {
		state.OwnerID, state.ChatID, state.OwnerHint = 0, 0, ""
		state.DeliveryStartedAt = time.Time{}
		state.ChallengeSHA256 = ""
		state.ChallengeExpiresAt = time.Time{}
		state.Outbox = nil
		state.Projection = map[string]string{}
		state.WorkMessages = map[string]int64{}
		return nil
	})
}

func (m *Manager) Remove() error {
	if err := m.store.removeToken(); err != nil {
		return err
	}
	if err := m.store.mutate(func(state *durableState) error {
		*state = newDurableState()
		return nil
	}); err != nil {
		return err
	}
	m.signal()
	return nil
}

func (m *Manager) Status() Status {
	state := m.store.snapshot()
	status := Status{Enabled: state.Enabled, BotName: state.BotName, BotUsername: state.BotUsername,
		OwnerHint: state.OwnerHint, TopicsAvailable: state.TopicsAvailable, LastReceiveAt: state.LastReceiveAt,
		LastSendAt: state.LastSendAt, LastError: state.LastError, WebhookConflict: state.WebhookConflict,
		BindingPending: state.Enabled && state.OwnerID == 0 && state.ChallengeSHA256 != "" && m.now().Before(state.ChallengeExpiresAt)}
	for _, row := range state.Outbox {
		if row.State == "ambiguous" {
			status.AmbiguousDelivery++
		}
	}
	switch {
	case !state.Enabled:
		status.State = StateDisabled
	case state.WebhookConflict || state.LastError != "" || status.AmbiguousDelivery > 0:
		status.State = StateDegraded
	case state.OwnerID == 0 || state.ChatID == 0:
		status.State = StateSetupPending
	default:
		status.State = StateConnected
	}
	return status
}

func (m *Manager) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		state := m.store.snapshot()
		if !state.Enabled || state.WebhookConflict {
			if state.Enabled && state.WebhookConflict {
				token, tokenErr := m.store.readToken()
				if tokenErr == nil && token != "" {
					_ = m.refreshWebhookState(ctx, token)
				}
			}
			if !m.wait(ctx, 30*time.Second) {
				return ctx.Err()
			}
			continue
		}
		token, err := m.store.readToken()
		if err != nil || token == "" {
			m.recordError("Telegram credential is unavailable.")
			if !m.wait(ctx, m.backoff) {
				return ctx.Err()
			}
			continue
		}
		if err := m.refreshWebhookState(ctx, token); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			m.recordError("Telegram webhook status is temporarily unavailable.")
			if !m.wait(ctx, m.jitteredBackoff()) {
				return ctx.Err()
			}
			continue
		}
		state = m.store.snapshot()
		if state.WebhookConflict {
			continue
		}
		updates, err := m.api.GetUpdates(ctx, token, state.NextOffset, m.pollTimeout, []string{"message"})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			m.recordError("Telegram receive is temporarily unavailable.")
			delay := retryDelay(err)
			if delay <= 0 {
				delay = m.jitteredBackoff()
			}
			if !m.wait(ctx, delay) {
				return ctx.Err()
			}
			continue
		}
		for _, update := range updates {
			if err := m.handleUpdate(ctx, token, update); err != nil {
				m.recordError("Telegram update could not be processed.")
			}
		}
		if state.OwnerID != 0 {
			if err := m.projectTimeline(); err != nil {
				m.recordError("Brain timeline projection is temporarily unavailable.")
			}
			if err := m.deliverPending(ctx, token, 8); err != nil && ctx.Err() == nil {
				m.recordError("Telegram delivery is degraded.")
			}
		}
	}
}

func (m *Manager) refreshWebhookState(ctx context.Context, token string) error {
	webhook, err := m.api.GetWebhookInfo(ctx, token)
	if err != nil {
		return err
	}
	conflict := strings.TrimSpace(webhook.URL) != ""
	current := m.store.snapshot()
	conflictMessage := "A Telegram webhook is configured; remove it explicitly before long polling."
	if current.WebhookConflict == conflict &&
		(conflict || current.LastError != conflictMessage) {
		return nil
	}
	return m.store.mutate(func(state *durableState) error {
		state.WebhookConflict = conflict
		if conflict {
			state.LastError = conflictMessage
		} else if state.LastError == conflictMessage {
			state.LastError = ""
		}
		return nil
	})
}

func (m *Manager) deliverPending(ctx context.Context, token string, limit int) error {
	for delivered := 0; delivered < limit; delivered++ {
		if !m.hasDeliverableOutbox() {
			return nil
		}
		if err := m.deliverOne(ctx, token); err != nil {
			return err
		}
		if !m.hasDeliverableOutbox() {
			return nil
		}
		// Telegram's official FAQ advises no more than one message per second
		// in a single chat. Edits share the same conservative serialization.
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func (m *Manager) hasDeliverableOutbox() bool {
	state := m.store.snapshot()
	for _, row := range state.Outbox {
		if row.State == "pending" &&
			(row.AttemptAt.IsZero() || !m.now().Before(row.AttemptAt)) {
			return true
		}
	}
	return false
}

func (m *Manager) jitteredBackoff() time.Duration {
	var sample [1]byte
	if _, err := rand.Read(sample[:]); err != nil {
		return m.backoff
	}
	// 75%-125% jitter avoids synchronized reconnects without reducing an
	// explicit Telegram retry_after interval.
	percent := 75 + int(sample[0])%51
	return time.Duration(int64(m.backoff) * int64(percent) / 100)
}

func (m *Manager) wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-m.wake:
		return true
	case <-timer.C:
		return true
	}
}

func (m *Manager) recordError(message string) {
	_ = m.store.mutate(func(state *durableState) error {
		state.LastError = message
		return nil
	})
}

func (m *Manager) handleUpdate(ctx context.Context, token string, update Update) error {
	state := m.store.snapshot()
	key := fmt.Sprintf("%d", update.UpdateID)
	if update.UpdateID < state.NextOffset || state.Processed[key].Disposition != "" {
		return nil
	}
	disposition := "ignored"
	message := update.Message
	if message != nil && message.From != nil && !message.From.IsBot && message.SenderChat == nil && message.Chat.Type == "private" {
		if state.OwnerID == 0 {
			disposition = m.tryBind(*message, state)
		} else if message.From.ID == state.OwnerID && message.Chat.ID == state.ChatID {
			disposition = m.handleOwnerMessage(ctx, *message, update.UpdateID)
		}
	}
	now := m.now().UTC()
	return m.store.mutate(func(current *durableState) error {
		current.Processed[key] = updateRecord{Disposition: disposition, HandledAt: now}
		if update.UpdateID >= current.NextOffset {
			current.NextOffset = update.UpdateID + 1
		}
		trimProcessedUpdates(current)
		current.LastReceiveAt = &now
		current.LastError = ""
		return nil
	})
}

func (m *Manager) tryBind(message Message, state durableState) string {
	command, argument := parseCommand(message.Text)
	if command != "/start" || argument == "" || state.ChallengeSHA256 == "" || !m.now().Before(state.ChallengeExpiresAt) {
		return "binding_rejected"
	}
	digest := sha256.Sum256([]byte(argument))
	if !strings.EqualFold(hex.EncodeToString(digest[:]), state.ChallengeSHA256) {
		return "binding_rejected"
	}
	err := m.store.mutate(func(current *durableState) error {
		if current.OwnerID != 0 || current.ChallengeSHA256 != state.ChallengeSHA256 || !m.now().Before(current.ChallengeExpiresAt) {
			return fmt.Errorf("binding challenge no longer valid")
		}
		current.OwnerID, current.ChatID = message.From.ID, message.Chat.ID
		current.OwnerHint = ownerHint(*message.From)
		current.DeliveryStartedAt = m.now().UTC()
		current.ChallengeSHA256 = ""
		current.ChallengeExpiresAt = time.Time{}
		enqueue(current, outboxRecord{ID: "binding:connected", Kind: "send", Text: "Zen Brain connected. Use /help for commands.", CreatedAt: m.now().UTC()})
		return nil
	})
	if err != nil {
		return "binding_rejected"
	}
	return "bound"
}

func (m *Manager) handleOwnerMessage(ctx context.Context, message Message, updateID int64) string {
	command, _ := parseCommand(message.Text)
	switch command {
	case "/help", "/start":
		m.enqueueText(fmt.Sprintf("command:%d", updateID), "Send text to Zen Brain. /status shows the connection. /new starts a fresh Brain chat.", message.MessageID)
		return "command"
	case "/status":
		m.enqueueText(fmt.Sprintf("command:%d", updateID), "Zen Brain is connected to this private chat.", message.MessageID)
		return "command"
	case "/new":
		if m.brain == nil {
			return "not_submitted"
		}
		if _, err := m.brain.NewChat(); err != nil {
			m.enqueueText(fmt.Sprintf("command:%d", updateID), "Zen could not start a fresh Brain chat.", message.MessageID)
			return "not_submitted"
		}
		m.enqueueText(fmt.Sprintf("command:%d", updateID), "Started a fresh Brain chat.", message.MessageID)
		return "command"
	}
	if message.hasUnsupportedMedia() {
		m.enqueueText(fmt.Sprintf("unsupported:%d", updateID), "This connection currently supports text and captions only. Send the content as text.", message.MessageID)
		return "unsupported_media"
	}
	body := strings.TrimSpace(message.Text)
	if body == "" {
		body = strings.TrimSpace(message.Caption)
	}
	if body == "" {
		return "ignored"
	}
	if reply := replyContext(message.ReplyToMessage); reply != "" {
		body = "Replying to: " + reply + "\n\n" + body
	}
	if m.brain == nil {
		return "not_submitted"
	}
	receipt := fmt.Sprintf("telegram:update:%d:%d", m.store.snapshot().BotID, updateID)
	result, err := m.brain.SubmitExternalUserInput(receipt, body)
	if err != nil && result == brain.ExternalInputPending {
		return "pending"
	}
	switch result {
	case brain.ExternalInputAccepted:
		m.enqueueText(fmt.Sprintf("ack:%d", updateID), "Accepted by Zen Brain.", message.MessageID)
		return "accepted"
	case brain.ExternalInputUncertain:
		m.enqueueText(fmt.Sprintf("ack:%d", updateID), "Zen could not prove whether Brain received this message. It was not replayed.", message.MessageID)
		return "uncertain"
	case brain.ExternalInputPending:
		return "pending"
	default:
		m.enqueueText(fmt.Sprintf("ack:%d", updateID), "Zen did not submit this message. Send it again when Brain is available.", message.MessageID)
		return "not_submitted"
	}
}

func (m *Manager) enqueueText(id, text string, reply int64) {
	_ = m.store.mutate(func(state *durableState) error {
		for index, chunk := range chunkText(text, maxMessageText) {
			enqueue(state, outboxRecord{ID: fmt.Sprintf("%s:%d", id, index), Kind: "send", Text: chunk, ReplyMessageID: reply, CreatedAt: m.now().UTC()})
		}
		return nil
	})
}

func enqueue(state *durableState, record outboxRecord) bool {
	for _, existing := range state.Outbox {
		if existing.ID == record.ID {
			return true
		}
	}
	compactOutbox(state)
	if len(state.Outbox) >= maxOutboxRows {
		state.LastError = "Telegram delivery queue is full."
		return false
	}
	record.State = "pending"
	state.Outbox = append(state.Outbox, record)
	return true
}

func compactOutbox(state *durableState) {
	if len(state.Outbox) < maxOutboxRows {
		return
	}
	compacted := state.Outbox[:0]
	for _, row := range state.Outbox {
		if row.State != "sent" && row.State != "failed" {
			compacted = append(compacted, row)
		}
	}
	state.Outbox = compacted
}

func trimProcessedUpdates(state *durableState) {
	minimum := state.NextOffset - maxProcessedUpdate
	if minimum <= 0 {
		return
	}
	for key := range state.Processed {
		var updateID int64
		if _, err := fmt.Sscan(key, &updateID); err != nil || updateID < minimum {
			delete(state.Processed, key)
		}
	}
}

func (m *Manager) projectTimeline() error {
	if m.brain == nil {
		return nil
	}
	threadID, err := m.brain.ChatThreadID()
	if err != nil || threadID == "" {
		return err
	}
	items, err := m.brain.ThreadTimeline(threadID, 0)
	if err != nil {
		return err
	}
	return m.store.mutate(func(state *durableState) error {
		for _, item := range items {
			if !state.DeliveryStartedAt.IsZero() && !item.CreatedAt.After(state.DeliveryStartedAt) {
				continue
			}
			switch {
			case item.Kind == "assistant_message" || (item.Role == "assistant" && item.Kind == ""):
				for index, chunk := range chunkText(item.Body, maxMessageText) {
					id := fmt.Sprintf("assistant:%s:%d", item.ID, index)
					checkpoint := "outbox:" + id
					digest := digestText(chunk)
					if state.Projection[checkpoint] == digest {
						continue
					}
					if enqueue(state, outboxRecord{ID: id, Kind: "send", CanonicalID: item.ID, Text: chunk, CreatedAt: m.now().UTC()}) {
						state.Projection[checkpoint] = digest
					}
				}
			case item.Kind == "work_card" && item.WorkID != "":
				text := workCardText(item)
				digest := digestText(text)
				if state.Projection["work:"+item.WorkID] == digest {
					continue
				}
				if coalescePendingWork(state, item, text, digest, m.now().UTC()) {
					continue
				}
				messageID := state.WorkMessages[item.WorkID]
				kind := "send"
				if messageID != 0 {
					kind = "edit"
				}
				enqueue(state, outboxRecord{ID: "work:" + item.WorkID + ":" + digest, Kind: kind, CanonicalID: item.ID, WorkID: item.WorkID, MessageID: messageID, Text: text, CreatedAt: m.now().UTC()})
			}
		}
		return nil
	})
}

// coalescePendingWork keeps one unsent logical row per Work. An indeterminate
// dispatch blocks later automatic sends because no local state can prove
// whether Telegram already created the Work message.
func coalescePendingWork(state *durableState, item brain.TimelineItem, text, digest string, now time.Time) bool {
	for index := range state.Outbox {
		row := &state.Outbox[index]
		if row.WorkID != item.WorkID {
			continue
		}
		switch row.State {
		case "pending":
			row.ID = "work:" + item.WorkID + ":" + digest
			row.CanonicalID = item.ID
			row.Text = text
			row.AttemptAt = time.Time{}
			return true
		case "dispatching", "ambiguous":
			return true
		}
	}
	return false
}

func (m *Manager) deliverOne(ctx context.Context, token string) error {
	state := m.store.snapshot()
	index := -1
	for i, row := range state.Outbox {
		if row.State == "pending" && (row.AttemptAt.IsZero() || !m.now().Before(row.AttemptAt)) {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	row := state.Outbox[index]
	if err := m.store.mutate(func(current *durableState) error {
		for i := range current.Outbox {
			if current.Outbox[i].ID == row.ID && current.Outbox[i].State == "pending" {
				current.Outbox[i].State = "dispatching"
				return nil
			}
		}
		return fmt.Errorf("outbox row unavailable")
	}); err != nil {
		return err
	}

	var sent Message
	var err error
	if row.Kind == "edit" {
		sent, err = m.api.EditMessage(ctx, token, EditRequest{ChatID: state.ChatID, MessageID: row.MessageID, Text: row.Text})
	} else {
		sent, err = m.api.SendMessage(ctx, token, SendRequest{ChatID: state.ChatID, Text: row.Text, ReplyToMessageID: row.ReplyMessageID})
	}
	if err != nil {
		if retryable(err) {
			delay := retryDelay(err)
			if delay <= 0 {
				delay = m.backoff
			}
			return m.store.mutate(func(current *durableState) error {
				for i := range current.Outbox {
					if current.Outbox[i].ID == row.ID {
						current.Outbox[i].State = "pending"
						current.Outbox[i].AttemptAt = m.now().Add(delay)
					}
				}
				return nil
			})
		}
		// A Bot API 4xx is a definite failure; a transport/decoding error may
		// have committed remotely and therefore becomes no-replay ambiguous.
		terminal := "ambiguous"
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			terminal = "failed"
		}
		_ = m.store.mutate(func(current *durableState) error {
			for i := range current.Outbox {
				if current.Outbox[i].ID == row.ID {
					current.Outbox[i].State = terminal
				}
			}
			return nil
		})
		return err
	}
	now := m.now().UTC()
	return m.store.mutate(func(current *durableState) error {
		for i := range current.Outbox {
			if current.Outbox[i].ID != row.ID {
				continue
			}
			current.Outbox[i].State = "sent"
			current.Outbox[i].MessageID = sent.MessageID
			if row.WorkID != "" {
				current.WorkMessages[row.WorkID] = sent.MessageID
				current.Projection["work:"+row.WorkID] = digestText(row.Text)
			}
		}
		current.LastSendAt = &now
		current.LastError = ""
		return nil
	})
}

func parseCommand(value string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return "", ""
	}
	command := strings.ToLower(strings.SplitN(fields[0], "@", 2)[0])
	argument := ""
	if len(fields) > 1 {
		argument = fields[1]
	}
	return command, argument
}

func replyContext(message *Message) string {
	if message == nil {
		return ""
	}
	value := strings.TrimSpace(message.Text)
	if value == "" {
		value = strings.TrimSpace(message.Caption)
	}
	runes := []rune(value)
	if len(runes) > 480 {
		value = string(runes[:479]) + "..."
	}
	return value
}

func displayName(user User) string {
	return strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " "))
}

func ownerHint(user User) string {
	if username := strings.TrimSpace(user.Username); username != "" {
		return "@" + username
	}
	name := displayName(user)
	if name == "" {
		return "Verified owner"
	}
	runes := []rune(name)
	if len(runes) > 48 {
		return string(runes[:48])
	}
	return name
}

func digestText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

func workCardText(item brain.TimelineItem) string {
	parts := []string{strings.TrimSpace(item.Title)}
	if parts[0] == "" {
		parts[0] = "Work update"
	}
	for _, value := range []string{item.Status, item.Phase, item.Summary, item.NextAction, item.WaitFor} {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	text := strings.Join(parts, "\n")
	chunks := chunkText(text, maxMessageText)
	if len(chunks) == 0 {
		return "Work update"
	}
	return chunks[0]
}

func chunkText(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var chunks []string
	for utf8.RuneCountInString(text) > limit {
		runes := []rune(text)
		split := limit
		for i := limit; i > limit/2; i-- {
			if runes[i-1] == '\n' || runes[i-1] == ' ' {
				split = i
				break
			}
		}
		chunks = append(chunks, strings.TrimSpace(string(runes[:split])))
		text = strings.TrimSpace(string(runes[split:]))
	}
	if text != "" {
		chunks = append(chunks, text)
	}
	return chunks
}
