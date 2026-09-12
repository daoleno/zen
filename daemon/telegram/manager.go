package telegram

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/attachment"
	"github.com/daoleno/zen/daemon/brain"
)

const (
	challengeTTL       = 10 * time.Minute
	maxMessageText     = 4096
	maxOutboxRows      = 512
	maxProcessedUpdate = 512
	maxTopicMappings   = 64
	maxTopicOps        = 128
	maxTopicNameRunes  = 128
	// topicCreateBackoff caps the retry delay for a definite createForumTopic
	// rejection. Definite rejection proves no Topic was created, so retrying is
	// safe; the cap keeps a permanently rejected create from busy-looping.
	topicCreateBackoff = 5 * time.Minute
)

type brainOwner interface {
	SubmitExternalUserInput(receipt, body string) (brain.ExternalInputDisposition, error)
	SubmitExternalUserInputInThread(receipt, body, expectedThread string) (brain.ExternalInputDisposition, error)
	ChatThreadID() (string, error)
	ThreadTimeline(threadID string, limit int) ([]brain.TimelineItem, error)
	NewChat() (brain.Snapshot, error)
	CurrentHostForegroundTurn() (*brain.HostForegroundTurn, error)
	DelegatedSessions() ([]brain.WorkerRef, error)
	WorkForSession(sessionID string) (brain.Work, bool, error)
	SubmitExternalSessionInput(sessionID, receipt, body string) (brain.ExternalInputDisposition, error)
	SessionProjection(sessionID string) (brain.SessionProjection, error)
}

type Options struct {
	Attachments    *attachment.Store
	MediaTimeout   time.Duration
	API            API
	Now            func() time.Time
	PollTimeout    int
	Backoff        time.Duration
	TypingInterval time.Duration
	TypingDeadline time.Duration
}

type Manager struct {
	attachments    *attachment.Store
	mediaTimeout   time.Duration
	mediaMu        sync.Mutex
	mediaActive    *mediaTransfer
	store          *store
	api            API
	brain          brainOwner
	now            func() time.Time
	pollTimeout    int
	backoff        time.Duration
	wake           chan struct{}
	outboundMu     sync.Mutex
	typingMu       sync.Mutex
	typingCancel   context.CancelFunc
	typingInterval time.Duration
	typingDeadline time.Duration
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
		pollTimeout = 2
	}
	backoff := options.Backoff
	if backoff <= 0 {
		backoff = time.Second
	}
	typingInterval := options.TypingInterval
	if typingInterval <= 0 {
		typingInterval = 4 * time.Second
	}
	typingDeadline := options.TypingDeadline
	if typingDeadline <= 0 {
		typingDeadline = 10 * time.Minute
	}
	manager := &Manager{store: state, api: api, brain: owner, now: now, pollTimeout: pollTimeout, backoff: backoff,
		wake: make(chan struct{}, 1), typingInterval: typingInterval, typingDeadline: typingDeadline}
	manager.attachments = options.Attachments
	manager.mediaTimeout = options.MediaTimeout
	if manager.mediaTimeout <= 0 {
		manager.mediaTimeout = mediaDownloadTimeout
	}
	if err := manager.initializeDeliveryBoundary(); err != nil {
		return nil, err
	}
	stateSnapshot := state.snapshot()
	for _, row := range stateSnapshot.Outbox {
		if row.State == "failed" && row.TopicKey != "" && row.CreatedAt.After(stateSnapshot.DeliveryStartedAt) && invalidTelegramLink(row.Entities) {
			if err := state.mutate(func(current *durableState) error {
				delete(current.TopicProjection, row.TopicKey)
				delete(current.Projection, row.TopicKey)
				return nil
			}); err != nil {
				return nil, err
			}
		}
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
	m.outboundMu.Lock()
	defer m.outboundMu.Unlock()
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
	m.cancelMedia()
	stateErr := m.store.mutate(func(state *durableState) error {
		if state.BotID != 0 && state.BotID != bot.ID {
			resetRichInteractionBinding(state)
			state.OwnerID, state.ChatID, state.OwnerHint = 0, 0, ""
			state.DeliveryStartedAt = time.Time{}
			state.NextOffset = 0
			state.Processed = map[string]updateRecord{}
			state.Outbox = nil
			state.Projection = map[string]string{}
			state.WorkMessages = map[string]int64{}
			state.Topics = nil
			state.TopicOps = nil
			state.TopicProjection = map[string]string{}
			state.TopicMessages = map[string]int64{}
			state.FallbackSessionID = ""
			state.FallbackStartedAt = time.Time{}
			state.CallbackRoutes = map[string]string{}
			state.CallbackIDs = map[string]int64{}
			state.ReplySessions = map[int64]string{}
			state.SessionChoices = nil
			state.RetryAt = time.Time{}
			state.ChallengeSHA256 = ""
			state.ChallengeExpiresAt = time.Time{}
			state.BrainTopicID, state.BrainTopics = 0, nil
			state.BrainReplyTopicID = 0
		}
		if !state.Enabled {
			resetDeliveryBoundary(state, m.now().UTC())
		}
		state.Enabled = true
		state.BotID = bot.ID
		state.BotName = displayName(bot)
		state.BotUsername = strings.TrimSpace(bot.Username)
		state.TopicsAvailable = bot.Topics
		state.UsersCreateTopics = bot.UserTopics
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
	m.outboundMu.Lock()
	defer m.outboundMu.Unlock()
	m.cancelMedia()
	err := m.store.mutate(func(state *durableState) error {
		state.Enabled = false
		state.LastError = ""
		return nil
	})
	if err == nil {
		m.stopTyping()
	}
	m.signal()
	return err
}

func (m *Manager) Enable() error {
	m.outboundMu.Lock()
	defer m.outboundMu.Unlock()
	token, err := m.store.readToken()
	if err != nil || strings.TrimSpace(token) == "" {
		return fmt.Errorf("Telegram bot must be configured first")
	}
	err = m.store.mutate(func(state *durableState) error {
		if state.BotID == 0 {
			return fmt.Errorf("Telegram bot must be configured first")
		}
		if !state.Enabled {
			resetDeliveryBoundary(state, m.now().UTC())
		}
		state.Enabled = true
		state.LastError = ""
		return nil
	})
	m.signal()
	return err
}

func (m *Manager) RevokeOwner() error {
	m.outboundMu.Lock()
	defer m.outboundMu.Unlock()
	m.cancelMedia()
	err := m.store.mutate(func(state *durableState) error {
		state.OwnerID, state.ChatID, state.OwnerHint = 0, 0, ""
		resetRichInteractionBinding(state)
		state.DeliveryStartedAt = time.Time{}
		state.ChallengeSHA256 = ""
		state.ChallengeExpiresAt = time.Time{}
		state.Outbox = nil
		state.Projection = map[string]string{}
		state.WorkMessages = map[string]int64{}
		state.Topics = nil
		state.TopicOps = nil
		state.TopicProjection = map[string]string{}
		state.TopicMessages = map[string]int64{}
		state.FallbackSessionID = ""
		state.FallbackStartedAt = time.Time{}
		state.CallbackRoutes = map[string]string{}
		state.CallbackIDs = map[string]int64{}
		state.ReplySessions = map[int64]string{}
		state.SessionChoices = nil
		state.RetryAt = time.Time{}
		state.BrainTopicID, state.BrainTopics = 0, nil
		state.BrainReplyTopicID = 0
		return nil
	})
	if err == nil {
		m.stopTyping()
	}
	m.signal()
	return err
}

func (m *Manager) Remove() error {
	m.outboundMu.Lock()
	defer m.outboundMu.Unlock()
	m.cancelMedia()
	m.stopTyping()
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
		UsersCreateTopics: state.UsersCreateTopics, BrainTopicID: state.BrainTopicID,
		OwnerHint: state.OwnerHint, TopicsAvailable: state.TopicsAvailable, TopicNotice: state.TopicNotice,
		RecipientID: state.FallbackSessionID, LastReceiveAt: state.LastReceiveAt,
		LastSendAt: state.LastSendAt, LastError: state.LastError, WebhookConflict: state.WebhookConflict,
		BindingPending: state.Enabled && state.OwnerID == 0 && state.ChallengeSHA256 != "" && m.now().Before(state.ChallengeExpiresAt)}
	if m.brain != nil {
		status.BrainThreadID, _ = m.brain.ChatThreadID()
	}
	if !state.TopicsAvailable && state.FallbackSessionID != "" {
		status.RecipientLabel = state.FallbackSessionID + " (unavailable)"
		if m.brain != nil {
			if projection, err := m.brain.SessionProjection(state.FallbackSessionID); err == nil && projection.Present {
				status.RecipientLabel = sessionStatusLabel(projection)
			}
		}
	} else {
		status.RecipientID = ""
	}
	for _, row := range state.Outbox {
		if row.State == "ambiguous" {
			status.AmbiguousDelivery++
		}
	}
	status.TopicMappings = len(state.Topics)
	for _, op := range state.TopicOps {
		switch op.State {
		case "ambiguous":
			status.TopicAmbiguousOps++
		case "failed":
			status.TopicFailedOps++
		}
	}
	for _, row := range state.Outbox {
		if row.TopicKey != "" && row.State == "failed" {
			status.TopicFailedMessages++
		}
	}
	switch {
	case !state.Enabled:
		status.State = StateDisabled
	case state.WebhookConflict || state.LastError != "" || status.AmbiguousDelivery > 0 ||
		status.TopicAmbiguousOps > 0 || status.TopicFailedOps > 0 || status.TopicFailedMessages > 0:
		status.State = StateDegraded
	case state.OwnerID == 0 || state.ChatID == 0:
		status.State = StateSetupPending
	default:
		status.State = StateConnected
	}
	return status
}

func (m *Manager) Run(ctx context.Context) error {
	defer m.stopMedia()
	var refreshedAt time.Time
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		state := m.store.snapshot()
		if !state.Enabled || state.WebhookConflict {
			m.cancelMedia()
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
		if m.now().Sub(refreshedAt) >= time.Minute {
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
			if err := m.refreshTopicCapability(ctx, token); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				m.recordError("Telegram topic capability is temporarily unavailable.")
				if !m.wait(ctx, m.jitteredBackoff()) {
					return ctx.Err()
				}
				continue
			}
			refreshedAt = m.now()
		}
		state = m.store.snapshot()
		if state.WebhookConflict {
			continue
		}
		if state.Enabled && state.OwnerID != 0 {
			_ = m.ensureBrainTopic()
		}
		updates, err := m.api.GetUpdates(ctx, token, state.NextOffset, m.pollTimeout, []string{"message", "callback_query", "message_reaction"})
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
		current := m.store.snapshot()
		if !current.Enabled || current.BotID != state.BotID {
			continue
		}
		for _, update := range updates {
			if !m.store.snapshot().Enabled {
				break
			}
			if err := m.handleUpdate(ctx, token, update); err != nil {
				m.recordError("Telegram update could not be processed.")
				break
			}
		}
		state = m.store.snapshot()
		if state.Enabled && state.OwnerID != 0 {
			if err := m.advanceMedia(ctx, token); err != nil {
				m.recordError("Telegram attachment processing is temporarily unavailable.")
			}
			if err := m.ensureBrainEntry(); err != nil {
				m.recordError("Brain navigation is temporarily unavailable.")
			}
			if err := m.projectTimeline(); err != nil {
				m.recordError("Brain timeline projection is temporarily unavailable.")
			}
			if err := m.projectFallbackSession(); err != nil {
				m.recordError("Selected Session projection is temporarily unavailable.")
			}
			if err := m.projectSessionTopics(ctx, token); err != nil {
				m.recordError("Session topic projection is temporarily unavailable.")
			}
			if err := m.deliverPending(ctx, token, 8); err != nil && ctx.Err() == nil {
				m.recordError("Telegram delivery is degraded.")
			}
			if err := m.deliverTopicOps(ctx, token, 8); err != nil && ctx.Err() == nil {
				m.recordError("Telegram topic operations are degraded.")
			}
		}
	}
}

// refreshTopicCapability re-reads getMe's has_topics_enabled so a BotFather
// topic-mode change becomes actionable without leaving private talk state
// unrepresented. Existing mappings stay when capability is lost: delivery to
// an existing Topic does not depend on the flag; only new Topic creation does.
func (m *Manager) refreshTopicCapability(ctx context.Context, token string) error {
	bot, err := m.api.GetMe(ctx, token)
	if err != nil {
		return err
	}
	available := bot.Topics
	current := m.store.snapshot()
	message := ""
	if !available {
		message = "Threaded mode is unavailable; use /sessions in the private chat to choose a Session."
	}
	if current.TopicsAvailable == available && current.UsersCreateTopics == bot.UserTopics && current.TopicNotice == message {
		return nil
	}
	return m.store.mutate(func(state *durableState) error {
		state.TopicsAvailable = available
		state.UsersCreateTopics = bot.UserTopics
		if state.LastError == "Telegram topic mode is disabled; enable Threaded mode in @BotFather to create Session topics." {
			state.LastError = ""
		}
		if !available {
			state.TopicNotice = message
		} else {
			state.TopicNotice = ""
			removePendingFallbackRows(state, state.FallbackSessionID)
			state.FallbackSessionID = ""
			state.FallbackStartedAt = time.Time{}
			state.CallbackRoutes = map[string]string{}
		}
		return nil
	})
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
	if !state.Enabled || m.now().Before(state.RetryAt) {
		return false
	}
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
	disposition := "ignored"
	if update.CallbackQuery != nil {
		if callbackID := strings.TrimSpace(update.CallbackQuery.ID); callbackID != "" {
			defer func() {
				text := ""
				if isFeedback(update.CallbackQuery.Data) {
					text = "Feedback unavailable for this message."
					if disposition == "reaction_recorded" {
						text = "Feedback saved for this message."
					}
					if disposition == "reaction_recorded" && update.CallbackQuery.Data == "feedback:clear" {
						text = "Feedback cleared for this message."
					}
				}
				m.outboundMu.Lock()
				_ = m.api.AnswerCallbackQuery(ctx, token, callbackID, text)
				m.outboundMu.Unlock()
			}()
		}
	}
	if update.UpdateID < state.NextOffset || state.Processed[key].Disposition != "" {
		return nil
	}
	message := update.Message
	if update.CallbackQuery != nil {
		if _, duplicate := state.CallbackIDs[update.CallbackQuery.ID]; duplicate {
			disposition = "callback_duplicate"
		} else {
			disposition = m.handleCallback(ctx, token, *update.CallbackQuery, update.UpdateID)
		}
	} else if update.MessageReaction != nil {
		var err error
		disposition, err = m.handleReaction(*update.MessageReaction, update.UpdateID)
		if err != nil {
			return err
		}
	} else if message != nil && message.From != nil && !message.From.IsBot && message.SenderChat == nil && message.Chat.Type == "private" {
		if state.OwnerID == 0 {
			if isGeneralThread(message.MessageThreadID) || state.UsersCreateTopics {
				disposition = m.tryBind(*message, state)
			} else {
				// Binding is a General-topic contract; a topic-scoped /start can
				// never outrank the exact General route and fails closed.
				disposition = "binding_rejected"
			}
		} else if message.From.ID == state.OwnerID && message.Chat.ID == state.ChatID {
			if message.hasMedia() {
				var err error
				disposition, err = m.stageMedia(*message, update.UpdateID)
				if err != nil {
					return err
				}
			} else {
				_, sessionTopic := topicMappingByThread(state, message.MessageThreadID)
				if sessionTopic {
					disposition = m.handleSessionTopicMessage(ctx, token, *message, update.UpdateID)
				} else if isGeneralThread(message.MessageThreadID) {
					disposition = m.handleOwnerMessage(ctx, token, *message, update.UpdateID)
				} else if slices.Contains(state.BrainTopics, message.MessageThreadID) || (state.UsersCreateTopics && message.IsTopicMessage && message.ForumTopicCreated != nil) {
					if err := m.bindBrainTopic(message.MessageThreadID); err != nil {
						return err
					}
					disposition = m.handleOwnerMessage(ctx, token, *message, update.UpdateID)
				} else {
					disposition = m.handleSessionTopicMessage(ctx, token, *message, update.UpdateID)
				}
			}
		}
	}
	now := m.now().UTC()
	return m.store.mutate(func(current *durableState) error {
		record := updateRecord{Disposition: disposition, HandledAt: now}
		if message != nil {
			record.MessageThreadID = message.MessageThreadID
			if mapping, found := topicMappingByThread(*current, message.MessageThreadID); found {
				record.SessionID = mapping.SessionID
			}
			if record.SessionID == "" && strings.HasPrefix(disposition, "session_") {
				record.SessionID = current.FallbackSessionID
				if message.ReplyToMessage != nil && current.ReplySessions[message.ReplyToMessage.MessageID] != "" {
					record.SessionID = current.ReplySessions[message.ReplyToMessage.MessageID]
				}
			}
			if record.SessionID == "" && m.brain != nil && (disposition == "accepted" || disposition == "pending" || disposition == "uncertain") {
				record.BrainThreadID, _ = m.brain.ChatThreadID()
			}
		}
		current.Processed[key] = record
		if message != nil && (disposition == "accepted" || disposition == "session_accepted") {
			recordMessageSource(current, message.MessageID, messageSource{SessionID: record.SessionID, BrainThreadID: record.BrainThreadID, MessageThreadID: message.MessageThreadID})
		}
		if update.CallbackQuery != nil && strings.TrimSpace(update.CallbackQuery.ID) != "" {
			current.CallbackIDs[update.CallbackQuery.ID] = update.UpdateID
		}
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
		if !isGeneralThread(message.MessageThreadID) {
			current.BrainTopicID = message.MessageThreadID
			current.BrainTopics = append(current.BrainTopics, message.MessageThreadID)
		}
		enqueue(current, outboxRecord{ID: "binding:connected", Kind: "send", MessageThreadID: message.MessageThreadID, Text: "Brain connected.", ReplyMarkup: navigationKeyboard(*current, message.MessageThreadID), CreatedAt: m.now().UTC()})
		return nil
	})
	if err != nil {
		return "binding_rejected"
	}
	return "bound"
}

func (m *Manager) handleOwnerMessage(ctx context.Context, token string, message Message, updateID int64) string {
	reply := func(id, text string) {
		m.enqueueTopicText(id, text, message.MessageThreadID, message.MessageID)
	}
	command, _ := parseCommand(message.Text)
	switch command {
	case "/help", "/start":
		reply(fmt.Sprintf("command:%d", updateID), ownerHelpText)
		return "command"
	case "/status":
		reply(fmt.Sprintf("command:%d", updateID), m.ownerStatusText())
		return "command"
	case "/new":
		if m.brain == nil {
			return "not_submitted"
		}
		if _, err := m.brain.NewChat(); err != nil {
			reply(fmt.Sprintf("command:%d", updateID), "Zen could not start a fresh Brain chat.")
			return "not_submitted"
		}
		m.clearFallbackRecipient()
		reply(fmt.Sprintf("command:%d", updateID), "Started a fresh Brain chat.")
		return "command"
	case "/brain":
		m.returnToBrain(fmt.Sprintf("command:%d", updateID), message.MessageThreadID, message.MessageID)
		return "command"
	case "/sessions":
		m.enqueueSessionList(updateID, message.MessageID, message.MessageThreadID)
		return "command"
	case "/use", "/session":
		argument := strings.TrimSpace(parseCommandArgument(message.Text))
		if argument == "" {
			reply(fmt.Sprintf("command:%d", updateID), "Choose a Session with /sessions, then send /use <number>.")
			return "command"
		}
		if m.store.snapshot().TopicsAvailable {
			m.enqueueSessionList(updateID, message.MessageID, message.MessageThreadID)
		} else {
			m.selectFallbackRecipient(argument, updateID, message.MessageID)
		}
		return "command"
	}
	body := strings.TrimSpace(message.Text)
	if body == "" {
		body = strings.TrimSpace(message.Caption)
	}
	if body == "" {
		return "ignored"
	}
	if len(message.Entities) > 0 {
		caption := attachment.Caption{Text: message.Text, Entities: message.Entities}
		if err := validateCaption(caption); err != nil {
			reply(fmt.Sprintf("entities:%d", updateID), err.Error())
			return "invalid_entities"
		}
		body = attachment.Input(body, nil, []attachment.Caption{caption})
	}
	if reply := replyContext(message.ReplyToMessage); reply != "" {
		body = "Replying to: " + reply + "\n\n" + body
	}
	state := m.store.snapshot()
	if message.ReplyToMessage != nil && message.ReplyToMessage.From != nil && message.ReplyToMessage.From.ID == state.BotID {
		if sessionID := state.ReplySessions[message.ReplyToMessage.MessageID]; sessionID != "" {
			return m.handleFallbackSessionMessage(ctx, token, sessionID, body, updateID, message.MessageID, message.MessageThreadID)
		}
	}
	if !state.TopicsAvailable && state.FallbackSessionID != "" {
		return m.handleFallbackSessionMessage(ctx, token, state.FallbackSessionID, body, updateID, message.MessageID, message.MessageThreadID)
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
		m.startTyping(ctx, token)
		return "accepted"
	case brain.ExternalInputUncertain:
		reply(fmt.Sprintf("ack:%d", updateID), "Zen could not prove whether Brain received this message. It was not replayed.")
		return "uncertain"
	case brain.ExternalInputPending:
		return "pending"
	default:
		reply(fmt.Sprintf("ack:%d", updateID), "Zen did not submit this message. Send it again when Brain is available.")
		return "not_submitted"
	}
}

const ownerHelpText = "Brain\n/sessions - Sessions\n/brain - Back to Brain\n/new - New Chat\n/status - Current recipient"

func parseCommandArgument(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func (m *Manager) ownerStatusText() string {
	state := m.store.snapshot()
	if !state.TopicsAvailable && state.FallbackSessionID != "" {
		if m.brain != nil {
			if projection, err := m.brain.SessionProjection(state.FallbackSessionID); err == nil && projection.Present {
				return sessionStatusText(projection)
			}
		}
		return "Recipient: Session " + state.FallbackSessionID + " (unavailable). Choose /brain or /sessions."
	}
	if !state.TopicsAvailable {
		return "Telegram is connected to Zen Brain. Threaded mode is unavailable; use /sessions to choose a Session."
	}
	return "Recipient: Brain. Session conversations are in their own topics."
}

func sessionStatusLabel(projection brain.SessionProjection) string {
	label := strings.TrimSpace(projection.Label)
	if label == "" {
		label = strings.TrimSpace(projection.SessionID)
	}
	if projection.Status != "" {
		label += " (" + projection.Status + ")"
	}
	return label
}

func sessionListText(sessions []brain.WorkerRef, topicMode bool) string {
	if len(sessions) == 0 {
		return "No delegated Sessions are available right now."
	}
	lines := []string{"Delegated Sessions:"}
	for index, session := range sessions {
		label := topicLabel(session)
		if label == "" {
			label = session.ID
		}
		status := strings.TrimSpace(session.Status)
		if status == "" {
			status = "available"
		}
		lines = append(lines, fmt.Sprintf("%d. %s [%s]", index+1, label, status))
	}
	if topicMode {
		lines = append(lines, "Choose a Session topic. /brain returns to Brain.")
	} else {
		lines = append(lines, "Open one with /use <number>. Return to Brain with /brain.")
	}
	return strings.Join(lines, "\n")
}

func (m *Manager) selectFallbackRecipient(argument string, updateID, replyID int64) {
	state := m.store.snapshot()
	if state.TopicsAvailable {
		m.enqueueSessionList(updateID, replyID, brainDestination(state))
		return
	}
	if m.brain == nil {
		m.enqueueText(fmt.Sprintf("command:%d", updateID), "Sessions are currently unavailable.", replyID)
		return
	}
	sessions, err := m.brain.DelegatedSessions()
	if err != nil {
		m.enqueueText(fmt.Sprintf("command:%d", updateID), "Sessions are temporarily unavailable. Try /sessions again shortly.", replyID)
		return
	}
	selected := ""
	if number, scanErr := strconv.Atoi(argument); scanErr == nil && number >= 1 && number <= len(state.SessionChoices) {
		for _, session := range sessions {
			if session.ID == state.SessionChoices[number-1] {
				selected = session.ID
				break
			}
		}
	} else {
		for _, session := range sessions {
			if session.ID == argument {
				selected = session.ID
				break
			}
		}
	}
	if selected == "" {
		m.enqueueText(fmt.Sprintf("command:%d", updateID), "That Session is not available. Run /sessions and choose an exact current entry.", replyID)
		return
	}
	m.setFallbackRecipient(selected, updateID, replyID, sessions)
}

func (m *Manager) setFallbackRecipient(selected string, updateID, replyID int64, sessions []brain.WorkerRef) {
	now := m.now().UTC()
	if err := m.store.mutate(func(current *durableState) error {
		removePendingFallbackRows(current, current.FallbackSessionID)
		current.FallbackSessionID = selected
		current.FallbackStartedAt = now
		return nil
	}); err != nil {
		m.enqueueText(fmt.Sprintf("command:%d", updateID), "The Session recipient could not be selected. Try again.", replyID)
		return
	}
	label := selected
	for _, session := range sessions {
		if session.ID == selected && strings.TrimSpace(session.Name) != "" {
			label = topicLabel(session)
			break
		}
	}
	m.enqueueText(fmt.Sprintf("command:%d", updateID), "Recipient set to Session: "+label+". Send text to continue it, or use /brain to return to Brain.", replyID)
}

func (m *Manager) handleCallback(ctx context.Context, token string, query CallbackQuery, updateID int64) string {
	if query.ID == "" || query.From == nil || query.From.IsBot || query.Message == nil || query.Message.Chat.Type != "private" {
		return "callback_rejected"
	}
	state := m.store.snapshot()
	if state.OwnerID == 0 || query.From.ID != state.OwnerID || query.Message.Chat.ID != state.ChatID {
		return "callback_rejected"
	}
	if isFeedback(query.Data) {
		var reactions []ReactionType
		switch query.Data {
		case "feedback:up":
			reactions = []ReactionType{{Type: "emoji", Emoji: "\U0001f44d"}}
		case "feedback:down":
			reactions = []ReactionType{{Type: "emoji", Emoji: "\U0001f44e"}}
		}
		disposition, err := m.recordFeedback(query.Message.MessageID, updateID, reactions, false)
		if err != nil {
			return "reaction_unavailable"
		}
		return disposition
	}
	reply := func(text string) {
		m.enqueueTopicText("callback:"+query.ID, text, query.Message.MessageThreadID, query.Message.MessageID)
	}
	switch query.Data {
	case "brain":
		m.returnToBrain("callback:"+query.ID, query.Message.MessageThreadID, query.Message.MessageID)
		return "callback_brain"
	case "sessions":
		m.enqueueSessionList(updateID, query.Message.MessageID, query.Message.MessageThreadID)
		return "callback_sessions"
	case "new":
		if _, sessionTopic := topicMappingByThread(state, query.Message.MessageThreadID); sessionTopic {
			reply(sessionNewResponseText)
			return "callback_rejected"
		}
		_ = m.store.mutate(func(current *durableState) error {
			enqueue(current, outboxRecord{ID: "callback:" + query.ID, Kind: "send", Text: "Start a new Brain conversation?", MessageThreadID: query.Message.MessageThreadID, ReplyMarkup: &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "New Chat", CallbackData: "new_confirm"}, {Text: "Cancel", CallbackData: "brain"}}}}, CreatedAt: m.now().UTC()})
			return nil
		})
		return "callback_confirm"
	case "new_confirm":
		if _, sessionTopic := topicMappingByThread(state, query.Message.MessageThreadID); sessionTopic {
			return "callback_rejected"
		}
		return m.handleOwnerMessage(ctx, token, Message{Text: "/new", MessageThreadID: query.Message.MessageThreadID}, updateID)
	}
	if state.TopicsAvailable && !strings.HasPrefix(query.Data, "session:") {
		return "callback_topic_mode"
	}
	const prefix = "session:"
	if !strings.HasPrefix(query.Data, prefix) {
		return "callback_unknown"
	}
	sessionID, ok := state.CallbackRoutes[strings.TrimPrefix(query.Data, prefix)]
	if !ok || strings.TrimSpace(sessionID) == "" || m.brain == nil {
		reply("That Session choice has expired. Run /sessions again.")
		return "callback_stale"
	}
	sessions, err := m.brain.DelegatedSessions()
	if err != nil {
		reply("Sessions are temporarily unavailable. Run /sessions again shortly.")
		return "callback_unavailable"
	}
	for _, session := range sessions {
		if session.ID == sessionID {
			if state.TopicsAvailable {
				for _, mapping := range state.Topics {
					if mapping.ChatID == state.ChatID && mapping.SessionID == sessionID && mapping.State != topicStateStale {
						projection, err := m.brain.SessionProjection(sessionID)
						if err != nil || !projection.Present {
							reply("That Session is unavailable. Run /sessions again.")
							return "callback_unavailable"
						}
						var buttons []InlineKeyboardButton
						if link := topicURL(state, mapping.MessageThreadID); link != "" {
							buttons = append(buttons, InlineKeyboardButton{Text: "Open Session", URL: link})
						}
						m.enqueueTopicText("callback:"+query.ID, sessionStatusText(projection), query.Message.MessageThreadID, query.Message.MessageID, buttons...)
						return "callback_topic_link"
					}
				}
				if err := m.ensureSessionTopic(session, m.now().UTC()); err != nil {
					reply("The Session topic is unavailable. Run /sessions again shortly.")
					return "callback_unavailable"
				}
				reply("The Session topic is not ready yet. Run /sessions again shortly.")
				return "callback_topic_pending"
			}
			m.setFallbackRecipient(sessionID, updateID, query.Message.MessageID, sessions)
			return "callback_selected"
		}
	}
	reply("That Session is no longer available. Run /sessions again.")
	return "callback_stale"
}

func (m *Manager) enqueueSessionList(updateID, replyID, threadID int64) {
	if m.brain == nil {
		m.enqueueTopicText(fmt.Sprintf("command:%d", updateID), "Sessions are currently unavailable.", threadID, replyID)
		return
	}
	sessions, err := m.brain.DelegatedSessions()
	if err != nil {
		m.enqueueTopicText(fmt.Sprintf("command:%d", updateID), "Sessions are currently unavailable.", threadID, replyID)
		return
	}
	if len(sessions) > maxCallbackRoutes {
		sessions = sessions[:maxCallbackRoutes]
	}
	connection := m.store.snapshot()
	text := sessionListText(sessions, connection.TopicsAvailable)
	routes := make(map[string]string, len(sessions))
	rows := make([][]InlineKeyboardButton, 0, len(sessions))
	for _, session := range sessions {
		if len(rows) >= maxCallbackRoutes {
			break
		}
		key := digestText(session.ID)
		routes[key] = session.ID
		button := InlineKeyboardButton{Text: topicLabel(session), CallbackData: "session:" + key}
		for _, mapping := range connection.Topics {
			if mapping.ChatID == connection.ChatID && mapping.SessionID == session.ID && mapping.State != topicStateStale {
				if link := topicURL(connection, mapping.MessageThreadID); link != "" {
					button.URL, button.CallbackData = link, ""
				}
				break
			}
		}
		rows = append(rows, []InlineKeyboardButton{button})
	}
	_ = m.store.mutate(func(state *durableState) error {
		state.CallbackRoutes = routes
		state.SessionChoices = nil
		for _, session := range sessions {
			state.SessionChoices = append(state.SessionChoices, session.ID)
		}
		chunks := chunkRichText(richText{Text: text}, maxMessageText)
		for i, chunk := range chunks {
			var keyboard *InlineKeyboardMarkup
			if i == len(chunks)-1 {
				keyboard = &InlineKeyboardMarkup{InlineKeyboard: append(rows, navigationKeyboard(*state, threadID).InlineKeyboard...)}
			}
			enqueue(state, outboxRecord{ID: fmt.Sprintf("command:%d:%d", updateID, i), Kind: "send", Text: chunk.Text,
				MessageThreadID: threadID, ReplyMessageID: replyID, ReplyMarkup: keyboard, CreatedAt: m.now().UTC()})
		}
		return nil
	})
}

func (m *Manager) clearFallbackRecipient() bool {
	state := m.store.snapshot()
	if state.FallbackSessionID == "" {
		return false
	}
	_ = m.store.mutate(func(current *durableState) error {
		removePendingFallbackRows(current, current.FallbackSessionID)
		current.FallbackSessionID = ""
		current.FallbackStartedAt = time.Time{}
		return nil
	})
	return true
}

func removePendingFallbackRows(state *durableState, sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	prefix := "private:msg:" + sessionID + ":"
	rows := state.Outbox[:0]
	for _, row := range state.Outbox {
		if row.State == "pending" && strings.HasPrefix(row.TopicKey, prefix) {
			continue
		}
		rows = append(rows, row)
	}
	state.Outbox = rows
}

func (m *Manager) handleFallbackSessionMessage(ctx context.Context, token, sessionID, body string, updateID, replyID, threadID int64) string {
	projection, err := m.brain.SessionProjection(sessionID)
	if err != nil || !projection.Present {
		m.enqueueTopicText(fmt.Sprintf("fallback:%d", updateID), "That Session is unavailable. Choose /brain or /sessions; this message was not forwarded.", threadID, replyID)
		return "fallback_stale"
	}
	receipt := fmt.Sprintf("telegram:update:%d:%d", m.store.snapshot().BotID, updateID)
	result, submitErr := m.brain.SubmitExternalSessionInput(sessionID, receipt, body)
	if submitErr != nil && result == brain.ExternalInputPending {
		return "session_pending"
	}
	switch result {
	case brain.ExternalInputAccepted:
		m.startSessionTyping(ctx, token, sessionID, threadID)
		return "session_accepted"
	case brain.ExternalInputUncertain:
		m.enqueueTopicText(fmt.Sprintf("ack:%d", updateID), "Zen could not prove whether the selected Session received this message. It was not replayed.", threadID, replyID)
		return "session_uncertain"
	case brain.ExternalInputPending:
		return "session_pending"
	default:
		m.enqueueTopicText(fmt.Sprintf("ack:%d", updateID), "Zen did not submit this message to the selected Session. Send it again when the Session is available.", threadID, replyID)
		return "session_not_submitted"
	}
}

func (m *Manager) projectFallbackSession() error {
	state := m.store.snapshot()
	if state.TopicsAvailable || state.FallbackSessionID == "" || m.brain == nil {
		return nil
	}
	projection, err := m.brain.SessionProjection(state.FallbackSessionID)
	if err != nil {
		return err
	}
	if !projection.Present {
		return nil
	}
	return m.projectSessionOutput(topicMapping{SessionID: projection.SessionID, Label: projection.Label, MessageThreadID: 0, CreatedAt: state.FallbackStartedAt}, projection, m.now().UTC())
}

func (m *Manager) startTyping(parent context.Context, token string) {
	if m.brain == nil {
		return
	}
	turn, err := m.brain.CurrentHostForegroundTurn()
	if err != nil || turn == nil {
		return
	}
	state := m.store.snapshot()
	if !state.Enabled || state.OwnerID == 0 || state.ChatID == 0 {
		return
	}
	m.stopTyping()
	ctx, cancel := context.WithCancel(parent)
	m.typingMu.Lock()
	m.typingCancel = cancel
	m.typingMu.Unlock()
	go m.runTyping(ctx, token, state.ChatID, *turn)
}

func (m *Manager) runTyping(ctx context.Context, token string, chatID int64, expected brain.HostForegroundTurn) {
	deadline := time.NewTimer(m.typingDeadline)
	defer deadline.Stop()
	for {
		if !m.typingTurnActive(expected, chatID) {
			return
		}
		m.outboundMu.Lock()
		_ = m.api.SendChatAction(ctx, token, ChatActionRequest{ChatID: chatID, Action: "typing"})
		m.outboundMu.Unlock()

		timer := time.NewTimer(m.typingInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-deadline.C:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (m *Manager) typingTurnActive(expected brain.HostForegroundTurn, chatID int64) bool {
	state := m.store.snapshot()
	if !state.Enabled || state.OwnerID == 0 || state.ChatID != chatID {
		return false
	}
	active, err := m.brain.CurrentHostForegroundTurn()
	return err == nil && active != nil && *active == expected
}

func (m *Manager) stopTyping() {
	m.typingMu.Lock()
	cancel := m.typingCancel
	m.typingCancel = nil
	m.typingMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) enqueueText(id, text string, reply int64) {
	_ = m.store.mutate(func(state *durableState) error {
		for index, chunk := range chunkRichText(richText{Text: strings.TrimSpace(text)}, maxMessageText) {
			enqueue(state, outboxRecord{ID: fmt.Sprintf("%s:%d", id, index), Kind: "send", Text: chunk.Text, MessageThreadID: brainDestination(*state), ReplyMessageID: reply, ReplyMarkup: navigationKeyboard(*state, brainDestination(*state)), CreatedAt: m.now().UTC()})
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
	if record.PlainText == "" {
		record.PlainText = record.Text
	}
	if record.Variant == "" {
		record.Variant = plainVariant
	}
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
	for id, updateID := range state.CallbackIDs {
		if updateID < minimum {
			delete(state.CallbackIDs, id)
		}
	}
}

func (m *Manager) projectTimeline() error {
	if m.brain == nil {
		return nil
	}
	connection := m.store.snapshot()
	if connection.UsersCreateTopics && connection.BrainTopicID == 0 {
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
				rendered := renderMarkdown(item.Body)
				for index, chunk := range chunkRichText(rendered, maxMessageText) {
					id := fmt.Sprintf("assistant:%s:%d", item.ID, index)
					checkpoint := "outbox:" + id
					digest := digestRichText(chunk)
					if state.Projection[checkpoint] == digest {
						continue
					}
					candidate := sessionOutputCandidate{Key: checkpoint, CanonicalID: item.ID, Content: chunk, Digest: digest}
					if coalesceTopicRow(state, candidate, brainDestination(*state), m.now().UTC()) {
						state.Projection[checkpoint] = digest
						continue
					}
					kind, messageID := "send", state.TopicMessages[checkpoint]
					if messageID != 0 {
						kind = "edit"
						id += ":" + digest
					}
					if enqueue(state, outboxRecord{ID: id, Kind: kind, BrainThreadID: threadID, MessageID: messageID, TopicKey: checkpoint, CanonicalID: item.ID, Text: chunk.Text, MessageThreadID: brainDestination(*state),
						PlainText: chunk.Text, Entities: chunk.Entities, Variant: variantFor(chunk), CreatedAt: m.now().UTC()}) {
						state.Projection[checkpoint] = digest
					}
				}
			case item.Kind == "work_card" && item.WorkID != "":
				content := renderMarkdown(workCardText(item))
				chunks := chunkRichText(content, maxMessageText)
				if len(chunks) == 0 {
					continue
				}
				formatted := chunks[0]
				digest := digestRichText(formatted)
				if state.Projection["work:"+item.WorkID] == digest {
					continue
				}
				if coalescePendingWork(state, item, formatted, digest, m.now().UTC()) {
					continue
				}
				messageID := state.WorkMessages[item.WorkID]
				kind := "send"
				if messageID != 0 {
					kind = "edit"
				}
				enqueue(state, outboxRecord{ID: "work:" + item.WorkID + ":" + digest, Kind: kind, CanonicalID: item.ID,
					WorkID: item.WorkID, MessageID: messageID, MessageThreadID: brainDestination(*state), Text: formatted.Text, PlainText: formatted.Text,
					Entities: formatted.Entities, Variant: variantFor(formatted), CreatedAt: m.now().UTC()})
			}
		}
		return nil
	})
}

// coalescePendingWork keeps one unsent logical row per Work. An indeterminate
// dispatch blocks later automatic sends because no local state can prove
// whether Telegram already created the Work message.
func coalescePendingWork(state *durableState, item brain.TimelineItem, content richText, digest string, now time.Time) bool {
	for index := range state.Outbox {
		row := &state.Outbox[index]
		if row.WorkID != item.WorkID {
			continue
		}
		switch row.State {
		case "pending":
			row.ID = "work:" + item.WorkID + ":" + digest
			row.CanonicalID = item.ID
			row.Text = content.Text
			row.PlainText = content.Text
			row.Entities = content.Entities
			row.Variant = variantFor(content)
			return true
		case "dispatching", "ambiguous":
			return true
		}
	}
	return false
}

func (m *Manager) deliverOne(ctx context.Context, token string) error {
	m.outboundMu.Lock()
	defer m.outboundMu.Unlock()
	state := m.store.snapshot()
	if !state.Enabled || state.ChatID == 0 || m.now().Before(state.RetryAt) {
		return nil
	}
	currentToken, tokenErr := m.store.readToken()
	if tokenErr != nil || currentToken == "" {
		return fmt.Errorf("Telegram credential is unavailable")
	}
	token = currentToken
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
	entities := row.Entities
	if row.Variant != formattedVariant {
		entities = nil
	}
	if row.Kind == "pin" {
		if api, ok := m.api.(InteractionAPI); ok {
			err = api.PinChatMessage(ctx, token, state.ChatID, row.MessageID)
			sent.MessageID = row.MessageID
		} else {
			err = &APIError{Code: 400}
		}
	} else if row.Kind == "edit" {
		sent, err = m.api.EditMessage(ctx, token, EditRequest{ChatID: state.ChatID, MessageID: row.MessageID, Text: row.Text, Entities: entities})
	} else {
		if row.CanonicalID != "" && row.WorkID == "" {
			row.ReplyMarkup = feedbackKeyboard(state, row.MessageThreadID)
		}
		sent, err = m.api.SendMessage(ctx, token, SendRequest{ChatID: state.ChatID, MessageThreadID: row.MessageThreadID, Text: row.Text, Entities: entities, ReplyToMessageID: row.ReplyMessageID, ReplyMarkup: row.ReplyMarkup})
	}
	if err != nil {
		if row.Variant == formattedVariant && formattingRejected(err) {
			return m.store.mutate(func(current *durableState) error {
				for i := range current.Outbox {
					if current.Outbox[i].ID == row.ID && current.Outbox[i].State == "dispatching" {
						current.Outbox[i].State = "pending"
						current.Outbox[i].Variant = plainVariant
						current.Outbox[i].Text = current.Outbox[i].PlainText
						current.Outbox[i].AttemptAt = time.Time{}
					}
				}
				return nil
			})
		}
		if retryable(err) {
			delay := retryDelay(err)
			if delay <= 0 {
				delay = m.backoff
			}
			return m.store.mutate(func(current *durableState) error {
				current.RetryAt = m.now().Add(delay)
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
		if row.ID == "brain-entry:v1" {
			current.BrainEntryMessageID, current.BrainEntryState = sent.MessageID, "sent"
			if !enqueue(current, outboxRecord{ID: "brain-entry:pin:v1", Kind: "pin", MessageID: sent.MessageID, CreatedAt: now}) {
				return fmt.Errorf("Brain pin queue is full")
			}
		}
		if row.ID == "brain-entry:pin:v1" {
			current.BrainEntryState = "pinned"
		}
		recordMessageSource(current, sent.MessageID, messageSource{SessionID: row.SessionID, BrainThreadID: row.BrainThreadID, MessageThreadID: row.MessageThreadID})
		if row.SessionID != "" {
			current.ReplySessions[sent.MessageID] = row.SessionID
		}
		for len(current.ReplySessions) > maxOutboxRows {
			var oldest int64
			for id := range current.ReplySessions {
				if oldest == 0 || id < oldest {
					oldest = id
				}
			}
			delete(current.ReplySessions, oldest)
		}
		for i := range current.Outbox {
			if current.Outbox[i].ID != row.ID {
				continue
			}
			current.Outbox[i].State = "sent"
			current.Outbox[i].MessageID = sent.MessageID
			if row.WorkID != "" {
				current.WorkMessages[row.WorkID] = sent.MessageID
				current.Projection["work:"+row.WorkID] = digestRichText(richText{Text: row.PlainText, Entities: row.Entities})
			}
			if row.TopicKey != "" {
				current.TopicMessages[row.TopicKey] = sent.MessageID
				current.TopicProjection[row.TopicKey] = digestRichText(richText{Text: row.PlainText, Entities: row.Entities})
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

func digestRichText(content richText) string {
	encoded, _ := json.Marshal(content)
	return digestText(string(encoded))
}

func variantFor(content richText) string {
	if len(content.Entities) > 0 {
		return formattedVariant
	}
	return plainVariant
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
	if strings.TrimSpace(text) == "" {
		return "Work update"
	}
	return text
}
