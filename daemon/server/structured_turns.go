package server

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/work"
	"github.com/google/uuid"
)

const maxRememberedStructuredTurnIDs = 512
const maxRememberedStructuredProviderFacts = 2048
const structuredProviderStartBackdateTolerance = 2 * time.Second

var errStructuredLifecycleSyncing = errors.New("structured turn lifecycle is still syncing; retry after the current turn refreshes")

// structuredTurnRegistry bridges an accepted mobile submission to the
// provider lifecycle that appears on a later transcript poll. Its key is the
// visible conversation scope (Brain thread when supplied, otherwise Work
// agent), so reconnects and Brain host replacement cannot reset the public
// turn identity or clock.
type structuredTurnRegistry struct {
	mu      sync.Mutex
	byScope map[string]*structuredTurnScope
	now     func() time.Time
	epoch   string
}

type structuredTurnScope struct {
	current                  *trackedStructuredTurn
	queued                   []*trackedStructuredTurn
	revision                 int64
	lastConversationIdentity string
	agentID                  string
	executorRemoved          bool

	lastProvider        *work.CodexConversationTurn
	terminalProviderIDs map[string]struct{}
	providerStartedAt   map[string]string
	providerSettledAt   map[string]string
	seenProviderFacts   map[string]struct{}
	seenProviderOrder   []string
	acceptedIDs         map[string]struct{}
	acceptedOrder       []string
}

type trackedStructuredTurn struct {
	turn                 work.CodexConversationTurn
	accepted             bool
	providerIDs          map[string]struct{}
	baselineProvider     string
	acceptedAt           time.Time
	control              bool
	baselineConversation string
}

type structuredInputAcceptance struct {
	TurnID    string
	Queued    bool
	Duplicate bool
	Revision  int64
	Epoch     string
}

func newStructuredTurnRegistry() *structuredTurnRegistry {
	return &structuredTurnRegistry{
		byScope: make(map[string]*structuredTurnScope),
		now:     time.Now,
		epoch:   uuid.NewString(),
	}
}

func structuredTurnRegistryKey(conversationScopeKey, agentID string) string {
	if scope := strings.TrimSpace(conversationScopeKey); scope != "" {
		return "scope:" + scope
	}
	if agent := strings.TrimSpace(agentID); agent != "" {
		return "agent:" + agent
	}
	return ""
}

func (r *structuredTurnRegistry) acceptInput(
	key string,
	turnID string,
	startedAt string,
	queuedHint bool,
	dispatch func() error,
) (structuredInputAcceptance, error) {
	return r.acceptInputWithOptions(
		key,
		"",
		turnID,
		startedAt,
		queuedHint,
		false,
		"",
		dispatch,
	)
}

func (r *structuredTurnRegistry) acceptInputWithOptions(
	key string,
	agentID string,
	turnID string,
	startedAt string,
	queuedHint bool,
	control bool,
	baselineConversation string,
	dispatch func() error,
) (structuredInputAcceptance, error) {
	turnID = strings.TrimSpace(turnID)
	epoch := ""
	if r != nil {
		epoch = r.epoch
	}
	if r == nil || key == "" || turnID == "" {
		if err := dispatch(); err != nil {
			return structuredInputAcceptance{}, err
		}
		return structuredInputAcceptance{TurnID: turnID, Queued: queuedHint, Epoch: epoch}, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	scope := r.scopeLocked(key)
	agentID = strings.TrimSpace(agentID)
	if agentID == "" && strings.HasPrefix(key, "agent:") {
		agentID = strings.TrimPrefix(key, "agent:")
	}
	if queued, ok := scope.acceptedTurnLocked(turnID); ok {
		return structuredInputAcceptance{TurnID: turnID, Queued: queued, Duplicate: true, Revision: scope.revision, Epoch: r.epoch}, nil
	}
	if queuedHint && scope.current == nil {
		// The hint is an assertion about lifecycle the App previously observed,
		// never a source of daemon state. A queue without an authoritative current
		// turn cannot be correlated safely after reconnect/restart, so fail before
		// dispatch and let the Composer restore the submission for retry.
		return structuredInputAcceptance{}, errStructuredLifecycleSyncing
	}
	if err := dispatch(); err != nil {
		return structuredInputAcceptance{}, err
	}
	scope.executorRemoved = false
	if scope.agentID == "" {
		// Associate the initial executor only after its dispatch succeeds. A
		// failed send must not consume a later Brain host-replacement boundary.
		scope.observeAgentLocked(agentID)
	}

	acceptedAt := r.now().UTC()
	startedAt = normalizeStructuredTurnTime(startedAt, acceptedAt)
	tracked := &trackedStructuredTurn{
		turn: work.CodexConversationTurn{
			ID:        turnID,
			Status:    work.CodexConversationTurnRunning,
			StartedAt: startedAt,
		},
		accepted:         true,
		providerIDs:      make(map[string]struct{}),
		baselineProvider: structuredProviderTurnFingerprint(scope.lastProvider),
		acceptedAt:       acceptedAt,
		control:          control,
		baselineConversation: firstNonEmptyStructuredIdentity(
			baselineConversation,
			scope.lastConversationIdentity,
		),
	}
	queued := false
	if scope.current == nil || (isStructuredTurnTerminal(scope.current.turn.Status) && len(scope.queued) == 0) {
		scope.current = tracked
	} else {
		tracked.turn.Status = work.CodexConversationTurnQueued
		scope.queued = append(scope.queued, tracked)
		queued = true
	}
	scope.rememberAcceptedIDLocked(turnID)
	scope.revision++
	return structuredInputAcceptance{TurnID: turnID, Queued: queued, Revision: scope.revision, Epoch: r.epoch}, nil
}

func (r *structuredTurnRegistry) interrupt(key, turnID string, settledAt time.Time) bool {
	interrupted, _ := r.interruptWithDispatch(key, turnID, settledAt, func() error { return nil })
	return interrupted
}

func (r *structuredTurnRegistry) interruptWithDispatch(
	key string,
	turnID string,
	settledAt time.Time,
	dispatch func() error,
) (bool, error) {
	if r == nil || key == "" {
		if err := dispatch(); err != nil {
			return false, err
		}
		return true, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	scope := r.byScope[key]
	if scope == nil || scope.current == nil || scope.current.turn.Status != work.CodexConversationTurnRunning {
		return false, nil
	}
	turnID = strings.TrimSpace(turnID)
	if turnID != "" && turnID != scope.current.turn.ID {
		return false, nil
	}
	// Validate and dispatch while holding the lifecycle transition lock. A
	// queued turn cannot be promoted between targeting Stop and the executor
	// accepting it, so a stale Stop can never interrupt the next turn.
	if err := dispatch(); err != nil {
		return false, err
	}
	scope.current.turn.Status = work.CodexConversationTurnInterrupted
	scope.current.turn.SettledAt = settledAt.UTC().Format(time.RFC3339Nano)
	scope.revision++
	return true, nil
}

func (r *structuredTurnRegistry) cancel(key, turnID string, settledAt time.Time) bool {
	return r.settleAccepted(key, turnID, work.CodexConversationTurnCancelled, settledAt)
}

func (r *structuredTurnRegistry) cancelAll(key string, settledAt time.Time) bool {
	if r == nil || key == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	scope := r.byScope[key]
	if scope == nil || (scope.current == nil && len(scope.queued) == 0) {
		return false
	}
	changed := scope.cancelAllLocked(settledAt)
	if changed {
		scope.revision++
	}
	return changed
}

func (r *structuredTurnRegistry) cancelAllForAgent(agentID string, settledAt time.Time) int {
	if r == nil || strings.TrimSpace(agentID) == "" {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	agentID = strings.TrimSpace(agentID)
	cancelled := 0
	for _, scope := range r.byScope {
		if scope.agentID != agentID || !scope.cancelAllLocked(settledAt) {
			continue
		}
		scope.revision++
		cancelled++
	}
	return cancelled
}

// failWorkAgent settles only the ordinary Work lifecycle keyed directly by an
// executor session. Brain scopes deliberately use a different key so a host
// process disappearing during Brain replacement cannot settle the durable
// Brain turn.
func (r *structuredTurnRegistry) failWorkAgent(agentID string, settledAt time.Time) bool {
	if r == nil || strings.TrimSpace(agentID) == "" {
		return false
	}
	key := structuredTurnRegistryKey("", agentID)
	r.mu.Lock()
	defer r.mu.Unlock()
	scope := r.scopeLocked(key)
	wasRemoved := scope.executorRemoved
	scope.executorRemoved = true
	wireChanged := len(scope.queued) > 0
	scope.queued = nil
	if scope.current != nil && !isStructuredTurnTerminal(scope.current.turn.Status) {
		scope.current.turn.Status = work.CodexConversationTurnFailed
		scope.current.turn.SettledAt = settledAt.UTC().Format(time.RFC3339Nano)
		wireChanged = true
	}
	if wireChanged {
		scope.revision++
	}
	return !wasRemoved || wireChanged
}

func (r *structuredTurnRegistry) markWorkAgentPresent(agentID string) {
	if r == nil || strings.TrimSpace(agentID) == "" {
		return
	}
	key := structuredTurnRegistryKey("", agentID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if scope := r.byScope[key]; scope != nil {
		scope.executorRemoved = false
	}
}

func (s *structuredTurnScope) cancelAllLocked(settledAt time.Time) bool {
	if s == nil || (s.current == nil && len(s.queued) == 0) {
		return false
	}
	changed := len(s.queued) > 0
	s.queued = nil
	if s.current != nil && !isStructuredTurnTerminal(s.current.turn.Status) {
		s.current.turn.Status = work.CodexConversationTurnCancelled
		s.current.turn.SettledAt = settledAt.UTC().Format(time.RFC3339Nano)
		changed = true
	}
	return changed
}

func (r *structuredTurnRegistry) settleAccepted(key, turnID, status string, settledAt time.Time) bool {
	if r == nil || key == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	scope := r.byScope[key]
	if scope == nil || scope.current == nil || scope.current.turn.Status != work.CodexConversationTurnRunning {
		return false
	}
	turnID = strings.TrimSpace(turnID)
	if turnID != "" && turnID != scope.current.turn.ID {
		return false
	}
	scope.current.turn.Status = status
	scope.current.turn.SettledAt = settledAt.UTC().Format(time.RFC3339Nano)
	scope.revision++
	return true
}

func (r *structuredTurnRegistry) project(
	key string,
	provider *work.CodexConversationTurn,
) (*work.CodexConversationTurn, []work.CodexConversationTurn) {
	return r.projectProviderHistory(key, nil, provider)
}

func (r *structuredTurnRegistry) projectProviderHistory(
	key string,
	providerHistory []work.CodexConversationTurn,
	provider *work.CodexConversationTurn,
) (*work.CodexConversationTurn, []work.CodexConversationTurn) {
	turn, queued, _, _ := r.projectProviderHistoryVersioned(key, providerHistory, provider)
	return turn, queued
}

func (r *structuredTurnRegistry) projectProviderHistoryVersioned(
	key string,
	providerHistory []work.CodexConversationTurn,
	provider *work.CodexConversationTurn,
) (*work.CodexConversationTurn, []work.CodexConversationTurn, string, int64) {
	return r.projectProviderHistoryWithIdentity(
		key,
		"",
		providerHistory,
		provider,
	)
}

func (r *structuredTurnRegistry) projectProviderHistoryWithIdentity(
	key string,
	conversationIdentity string,
	providerHistory []work.CodexConversationTurn,
	provider *work.CodexConversationTurn,
) (*work.CodexConversationTurn, []work.CodexConversationTurn, string, int64) {
	return r.projectProviderHistoryWithContext(
		key,
		"",
		conversationIdentity,
		providerHistory,
		provider,
	)
}

func (r *structuredTurnRegistry) projectProviderHistoryWithContext(
	key string,
	agentID string,
	conversationIdentity string,
	providerHistory []work.CodexConversationTurn,
	provider *work.CodexConversationTurn,
) (*work.CodexConversationTurn, []work.CodexConversationTurn, string, int64) {
	if r == nil || key == "" {
		return cloneStructuredTurn(provider), nil, "", 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	scope := r.scopeLocked(key)
	before := scope.lifecycleFingerprintLocked()
	hostChanged := scope.observeAgentLocked(agentID)
	if hostChanged {
		scope.rebaseControlTurnsLocked(conversationIdentity)
	}
	directHistoryOnly := hostChanged && scope.current != nil &&
		scope.current.turn.Status == work.CodexConversationTurnRunning &&
		!scope.current.accepted
	if directHistoryOnly {
		// A replacement host may expose unrelated history from its own transcript.
		// For a provider-hydrated public turn, consume only facts already bound to
		// that turn; these can authoritatively settle A and promote queued B without
		// allowing old host history to replace the durable public identity.
		for index := range providerHistory {
			if scope.current == nil ||
				scope.current.turn.Status != work.CodexConversationTurnRunning ||
				scope.current.accepted ||
				scope.currentProviderMatchesLocked(&providerHistory[index]) {
				scope.reconcileProviderLocked(&providerHistory[index], r.now())
			} else {
				// The same provider history is emitted on every poll. Remember a
				// skipped replacement-host fact as observed so it cannot replace the
				// durable public turn after hostChanged becomes false next poll.
				scope.rememberProviderWithoutLifecycleLocked(&providerHistory[index], r.now())
			}
		}
	} else {
		for index := range providerHistory {
			scope.reconcileProviderLocked(&providerHistory[index], r.now())
		}
	}
	if hostChanged && scope.current != nil &&
		scope.current.turn.Status == work.CodexConversationTurnRunning {
		// Host replacement alone is not a turn transition. After consuming any
		// direct terminal/promotion above, bind the replacement provider's current
		// native ID to whichever durable public turn is now active.
		scope.bindCurrentProviderLocked(provider)
	}
	scope.reconcileProviderLocked(provider, r.now())
	scope.reconcileConversationIdentityLocked(conversationIdentity, r.now())
	if scope.lifecycleFingerprintLocked() != before {
		scope.revision++
	}
	turn, queued := scope.snapshotLocked()
	return turn, queued, r.epoch, scope.revision
}

func (s *structuredTurnScope) currentProviderMatchesLocked(provider *work.CodexConversationTurn) bool {
	if s == nil || s.current == nil || provider == nil {
		return false
	}
	providerID := strings.TrimSpace(provider.ID)
	if providerID == "" {
		return false
	}
	if s.current.turn.ID == providerID {
		return true
	}
	_, matches := s.current.providerIDs[providerID]
	return matches
}

func (s *structuredTurnScope) bindCurrentProviderLocked(provider *work.CodexConversationTurn) {
	if s == nil || s.current == nil || provider == nil ||
		s.current.turn.Status != work.CodexConversationTurnRunning {
		return
	}
	providerID := strings.TrimSpace(provider.ID)
	if providerID == "" {
		return
	}
	if s.current.providerIDs == nil {
		s.current.providerIDs = make(map[string]struct{})
	}
	s.current.providerIDs[providerID] = struct{}{}
}

func (s *structuredTurnScope) observeAgentLocked(agentID string) bool {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return false
	}
	changed := s.agentID != "" && s.agentID != agentID
	s.agentID = agentID
	return changed
}

func (s *structuredTurnScope) rebaseControlTurnsLocked(identity string) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return
	}
	if s.current != nil && s.current.control {
		s.current.baselineConversation = identity
	}
	for _, queued := range s.queued {
		if queued.control {
			queued.baselineConversation = identity
		}
	}
}

func (s *structuredTurnScope) reconcileConversationIdentityLocked(identity string, now time.Time) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return
	}
	// Queue promotion during this reconciliation must see the identity that
	// caused the transition. In particular, a second queued /new starts from
	// the first /new's new session, not from their shared acceptance baseline.
	s.lastConversationIdentity = identity
	if s.current != nil &&
		s.current.accepted &&
		s.current.control &&
		s.current.turn.Status == work.CodexConversationTurnRunning {
		if structuredConversationIdentityReplaced(
			s.current.baselineConversation,
			identity,
		) {
			s.current.turn.Status = work.CodexConversationTurnCompleted
			s.current.turn.SettledAt = now.UTC().Format(time.RFC3339Nano)
			if len(s.queued) > 0 {
				s.advanceQueueLocked(structuredProviderTurnFingerprint(s.lastProvider))
			}
		} else if structuredConversationIdentityKind(s.current.baselineConversation) !=
			structuredConversationIdentityKind(identity) {
			// Path -> session metadata enrichment does not prove replacement.
			// Rebase so a later same-kind provider thread change can settle it.
			s.current.baselineConversation = identity
		}
	}
}

func firstNonEmptyStructuredIdentity(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func structuredConversationIdentityKind(identity string) string {
	kind, _, ok := strings.Cut(strings.TrimSpace(identity), ":")
	if !ok {
		return ""
	}
	return kind
}

func structuredConversationIdentityReplaced(previous, next string) bool {
	previous = strings.TrimSpace(previous)
	next = strings.TrimSpace(next)
	if previous == "" || next == "" || previous == next {
		return false
	}
	previousKind := structuredConversationIdentityKind(previous)
	return previousKind != "" && previousKind == structuredConversationIdentityKind(next)
}

func (r *structuredTurnRegistry) snapshot(key string) (*work.CodexConversationTurn, []work.CodexConversationTurn) {
	if r == nil || key == "" {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	scope := r.byScope[key]
	if scope == nil {
		return nil, nil
	}
	return scope.snapshotLocked()
}

func (r *structuredTurnRegistry) scopeLocked(key string) *structuredTurnScope {
	scope := r.byScope[key]
	if scope == nil {
		scope = &structuredTurnScope{
			terminalProviderIDs: make(map[string]struct{}),
			providerStartedAt:   make(map[string]string),
			providerSettledAt:   make(map[string]string),
			seenProviderFacts:   make(map[string]struct{}),
			acceptedIDs:         make(map[string]struct{}),
		}
		r.byScope[key] = scope
	}
	return scope
}

func (s *structuredTurnScope) reconcileProviderLocked(provider *work.CodexConversationTurn, now time.Time) {
	if s.executorRemoved {
		// A watcher removal is authoritative for ordinary Work. Subscription
		// fallback metadata may still reload the last transcript, but it cannot
		// reopen Working until watcher rediscovery or a newly accepted dispatch.
		return
	}
	provider, fact, providerStartAuthoritative := s.normalizeProviderFactLocked(provider, now)
	if provider == nil {
		return
	}
	if _, seen := s.seenProviderFacts[fact]; seen {
		s.lastProvider = cloneStructuredTurn(provider)
		return
	}
	s.rememberProviderFactLocked(fact)
	if provider.Status == work.CodexConversationTurnRunning {
		s.reconcileProviderRunningLocked(provider, providerStartAuthoritative)
	} else if isStructuredTurnTerminal(provider.Status) {
		s.reconcileProviderTerminalLocked(provider, now)
	}
	s.lastProvider = cloneStructuredTurn(provider)
}

func (s *structuredTurnScope) rememberProviderWithoutLifecycleLocked(
	provider *work.CodexConversationTurn,
	now time.Time,
) {
	if s == nil || s.executorRemoved {
		return
	}
	_, fact, _ := s.normalizeProviderFactLocked(provider, now)
	if fact != "" {
		s.rememberProviderFactLocked(fact)
	}
}

func (s *structuredTurnScope) normalizeProviderFactLocked(
	provider *work.CodexConversationTurn,
	now time.Time,
) (*work.CodexConversationTurn, string, bool) {
	if provider == nil || strings.TrimSpace(provider.ID) == "" || strings.TrimSpace(provider.Status) == "" {
		return nil, "", false
	}
	_, providerStartAuthoritative := parseStructuredTurnTime(provider.StartedAt)
	provider = cloneStructuredTurn(provider)
	provider.ID = strings.TrimSpace(provider.ID)
	provider.Status = strings.TrimSpace(provider.Status)
	if strings.TrimSpace(provider.StartedAt) == "" {
		provider.StartedAt = s.providerStartedAt[provider.ID]
		provider.StartedAt = normalizeStructuredTurnTime(provider.StartedAt, now)
	}
	s.providerStartedAt[provider.ID] = provider.StartedAt
	if isStructuredTurnTerminal(provider.Status) && strings.TrimSpace(provider.SettledAt) == "" {
		provider.SettledAt = s.providerSettledAt[provider.ID]
		provider.SettledAt = normalizeStructuredTurnTime(provider.SettledAt, now)
	}
	if isStructuredTurnTerminal(provider.Status) {
		s.providerSettledAt[provider.ID] = provider.SettledAt
	}
	return provider, structuredProviderTurnFingerprint(provider), providerStartAuthoritative
}

func (s *structuredTurnScope) reconcileProviderRunningLocked(
	provider *work.CodexConversationTurn,
	providerStartAuthoritative bool,
) {
	if _, settled := s.terminalProviderIDs[provider.ID]; settled {
		return
	}
	if s.current == nil {
		s.current = trackedProviderTurn(provider)
		return
	}

	if s.current.turn.Status == work.CodexConversationTurnRunning {
		if _, sameProvider := s.current.providerIDs[provider.ID]; sameProvider {
			return
		}
		if s.current.accepted {
			if structuredProviderTurnFingerprint(provider) == s.current.baselineProvider {
				return
			}
			// A provider observation that predates the accepted submission is a
			// delayed fact from an earlier executor turn. It cannot acquire the
			// public identity of the newly accepted turn merely because the
			// registry had not projected it before the submission arrived.
			if !providerCouldBelongToAcceptedTurn(provider, s.current.acceptedAt) {
				return
			}
			s.current.providerIDs[provider.ID] = struct{}{}
			return
		}
		if s.current.turn.ID == provider.ID {
			return
		}
		s.current = trackedProviderTurn(provider)
		return
	}

	if s.current.accepted && len(s.current.providerIDs) == 0 &&
		structuredProviderTurnFingerprint(provider) != s.current.baselineProvider &&
		providerCouldBelongToAcceptedTurn(provider, s.current.acceptedAt) &&
		providerStartedBeforePublicSettlement(provider, &s.current.turn) {
		// A stop can settle the public turn before the provider has exposed its
		// native identity. Only a native turn that demonstrably began before Stop
		// belongs to that terminal turn; a turn beginning at/after Stop is the
		// queued successor and must be promoted immediately.
		s.current.providerIDs[provider.ID] = struct{}{}
		return
	}
	if _, sameProvider := s.current.providerIDs[provider.ID]; sameProvider || s.current.turn.ID == provider.ID {
		return
	}
	if isStructuredTurnTerminal(s.current.turn.Status) &&
		s.current.accepted && !providerStartAuthoritative {
		// Missing/unparseable native start time is not proof of a later turn.
		// It must never reopen an accepted turn settled by Stop, kill, or failure.
		return
	}
	if len(s.queued) > 0 {
		s.advanceQueueLocked(structuredProviderTurnFingerprint(s.lastProvider))
		s.current.providerIDs[provider.ID] = struct{}{}
		return
	}
	s.current = trackedProviderTurn(provider)
}

func (s *structuredTurnScope) reconcileProviderTerminalLocked(provider *work.CodexConversationTurn, now time.Time) {
	s.terminalProviderIDs[provider.ID] = struct{}{}
	if s.current == nil {
		s.current = trackedProviderTurn(provider)
		if len(s.queued) > 0 {
			s.advanceQueueLocked(structuredProviderTurnFingerprint(provider))
		}
		return
	}
	directMatch := s.current.turn.ID == provider.ID
	if _, ok := s.current.providerIDs[provider.ID]; ok {
		directMatch = true
	}
	if isStructuredTurnTerminal(s.current.turn.Status) &&
		s.current.accepted && !directMatch &&
		providerStartedAtOrAfterPublicSettlement(provider, &s.current.turn) {
		if len(s.queued) > 0 {
			// Stop settled A, and distinct queued B both started and finished
			// between polls. Promote B before consuming its terminal fact. This
			// remains true when A exposed a native identity before Stop: direct
			// provider IDs above still reserve late A facts for A.
			s.advanceQueueLocked(structuredProviderTurnFingerprint(s.lastProvider))
		} else {
			// This is a later provider-only turn, not a late terminal for A.
			s.current = trackedProviderTurn(provider)
			return
		}
	}

	// Promotion above changed the current public turn. Re-evaluate identity
	// against the successor before applying this terminal fact.
	directMatch = s.current.turn.ID == provider.ID
	if _, ok := s.current.providerIDs[provider.ID]; ok {
		directMatch = true
	}
	matches := directMatch
	if s.current.accepted && !matches {
		providerFingerprint := structuredProviderTurnFingerprint(provider)
		matches = providerFingerprint != s.current.baselineProvider && providerCouldBelongToAcceptedTurn(provider, s.current.acceptedAt)
	}
	if !matches {
		if !s.current.accepted {
			s.current = trackedProviderTurn(provider)
		}
		return
	}
	if isStructuredTurnTerminal(s.current.turn.Status) &&
		s.current.accepted && !directMatch && len(s.current.providerIDs) > 0 && len(s.queued) == 0 {
		// The accepted turn already has an authoritative provider terminal.
		// A later distinct terminal is a newer provider-only turn that began and
		// ended between polls, not another settlement of the public turn.
		s.current = trackedProviderTurn(provider)
		return
	}

	s.current.providerIDs[provider.ID] = struct{}{}
	if s.current.turn.Status == work.CodexConversationTurnRunning {
		s.current.turn.Status = provider.Status
		s.current.turn.SettledAt = normalizeStructuredTurnTime(provider.SettledAt, now)
	}
	if len(s.queued) > 0 {
		s.advanceQueueLocked(structuredProviderTurnFingerprint(provider))
	}
}

func (s *structuredTurnScope) advanceQueueLocked(baselineProvider string) {
	if len(s.queued) == 0 {
		return
	}
	next := s.queued[0]
	s.queued = s.queued[1:]
	next.turn.Status = work.CodexConversationTurnRunning
	next.turn.SettledAt = ""
	next.baselineProvider = baselineProvider
	if next.control && strings.TrimSpace(s.lastConversationIdentity) != "" {
		next.baselineConversation = s.lastConversationIdentity
	}
	s.current = next
}

func (s *structuredTurnScope) snapshotLocked() (*work.CodexConversationTurn, []work.CodexConversationTurn) {
	var current *work.CodexConversationTurn
	if s.current != nil {
		current = cloneStructuredTurn(&s.current.turn)
	}
	queued := make([]work.CodexConversationTurn, 0, len(s.queued))
	for _, item := range s.queued {
		turn := item.turn
		turn.Status = work.CodexConversationTurnQueued
		turn.SettledAt = ""
		queued = append(queued, turn)
	}
	return current, queued
}

func (s *structuredTurnScope) lifecycleFingerprintLocked() string {
	if s == nil {
		return ""
	}
	var value strings.Builder
	appendTurn := func(turn *work.CodexConversationTurn) {
		if turn == nil {
			value.WriteString("<nil>")
			return
		}
		value.WriteString(turn.ID)
		value.WriteByte('\x00')
		value.WriteString(turn.Status)
		value.WriteByte('\x00')
		value.WriteString(turn.StartedAt)
		value.WriteByte('\x00')
		value.WriteString(turn.SettledAt)
		value.WriteByte('\x01')
	}
	if s.current == nil {
		appendTurn(nil)
	} else {
		appendTurn(&s.current.turn)
	}
	for _, queued := range s.queued {
		appendTurn(&queued.turn)
	}
	return value.String()
}

func (s *structuredTurnScope) acceptedTurnLocked(turnID string) (bool, bool) {
	if _, ok := s.acceptedIDs[turnID]; !ok {
		return false, false
	}
	for _, queued := range s.queued {
		if queued.turn.ID == turnID {
			return true, true
		}
	}
	return false, true
}

func (s *structuredTurnScope) rememberAcceptedIDLocked(turnID string) {
	if _, ok := s.acceptedIDs[turnID]; ok {
		return
	}
	s.acceptedIDs[turnID] = struct{}{}
	s.acceptedOrder = append(s.acceptedOrder, turnID)
	if len(s.acceptedOrder) <= maxRememberedStructuredTurnIDs {
		return
	}
	oldest := s.acceptedOrder[0]
	s.acceptedOrder = s.acceptedOrder[1:]
	delete(s.acceptedIDs, oldest)
}

func (s *structuredTurnScope) rememberProviderFactLocked(fact string) {
	if _, ok := s.seenProviderFacts[fact]; ok {
		return
	}
	s.seenProviderFacts[fact] = struct{}{}
	s.seenProviderOrder = append(s.seenProviderOrder, fact)
	if len(s.seenProviderOrder) <= maxRememberedStructuredProviderFacts {
		return
	}
	oldest := s.seenProviderOrder[0]
	s.seenProviderOrder = s.seenProviderOrder[1:]
	delete(s.seenProviderFacts, oldest)
}

func trackedProviderTurn(provider *work.CodexConversationTurn) *trackedStructuredTurn {
	return &trackedStructuredTurn{
		turn:        *cloneStructuredTurn(provider),
		providerIDs: map[string]struct{}{provider.ID: {}},
	}
}

func providerCouldBelongToAcceptedTurn(provider *work.CodexConversationTurn, acceptedAt time.Time) bool {
	providerStarted, providerOK := parseStructuredTurnTime(provider.StartedAt)
	if !providerOK || acceptedAt.IsZero() {
		return true
	}
	return !providerStarted.Before(acceptedAt.Add(-structuredProviderStartBackdateTolerance))
}

func providerStartedBeforePublicSettlement(
	provider *work.CodexConversationTurn,
	public *work.CodexConversationTurn,
) bool {
	if provider == nil || public == nil {
		return false
	}
	providerStarted, providerOK := parseStructuredTurnTime(provider.StartedAt)
	publicSettled, publicOK := parseStructuredTurnTime(public.SettledAt)
	return providerOK && publicOK && providerStarted.Before(publicSettled)
}

func providerStartedAtOrAfterPublicSettlement(
	provider *work.CodexConversationTurn,
	public *work.CodexConversationTurn,
) bool {
	if provider == nil || public == nil {
		return false
	}
	providerStarted, providerOK := parseStructuredTurnTime(provider.StartedAt)
	publicSettled, publicOK := parseStructuredTurnTime(public.SettledAt)
	return providerOK && publicOK && !providerStarted.Before(publicSettled)
}

func isStructuredTurnTerminal(status string) bool {
	switch strings.TrimSpace(status) {
	case work.CodexConversationTurnCompleted,
		work.CodexConversationTurnFailed,
		work.CodexConversationTurnInterrupted,
		work.CodexConversationTurnCancelled:
		return true
	default:
		return false
	}
}

func cloneStructuredTurn(turn *work.CodexConversationTurn) *work.CodexConversationTurn {
	if turn == nil {
		return nil
	}
	cloned := *turn
	return &cloned
}

func structuredProviderTurnFingerprint(turn *work.CodexConversationTurn) string {
	if turn == nil {
		return ""
	}
	return strings.Join([]string{turn.ID, turn.Status, turn.StartedAt, turn.SettledAt}, "\x00")
}

func normalizeStructuredTurnTime(value string, fallback time.Time) string {
	if parsed, ok := parseStructuredTurnTime(value); ok {
		return parsed.UTC().Format(time.RFC3339Nano)
	}
	return fallback.UTC().Format(time.RFC3339Nano)
}

func parseStructuredTurnTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed, err == nil
}
