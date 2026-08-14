package modelprofiles

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

// RouteTable is the process-local atomic owner of per-Session RouteBindings.
type RouteTable struct {
	mu        sync.Mutex
	bySession map[string]SessionRouteState
	byRoute   map[string]string // routeID -> sessionID
	lookup    func(string) (string, bool)
	creds     CredentialStore
	newRoute  func() (string, error)
	verifier  ProfileContractVerifier
	// inFlight tracks per-route request leases until Release/Complete.
	inFlight map[string]map[string]routeFlight // routeID -> token -> meta
}

type routeFlight struct {
	sessionID     string
	historyDomain string
	generation    int64
}

// NewRouteTable constructs an empty in-memory route binding owner.
func NewRouteTable() *RouteTable {
	return &RouteTable{
		bySession: map[string]SessionRouteState{},
		byRoute:   map[string]string{},
		lookup:    lookupEnv,
		newRoute:  newOpaqueRouteID,
		inFlight:  map[string]map[string]routeFlight{},
	}
}

// SetLookup overrides credential probing (deterministic tests).
func (t *RouteTable) SetLookup(lookup func(string) (string, bool)) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if lookup == nil {
		t.lookup = lookupEnv
		return
	}
	t.lookup = lookup
}

// SetCredentials installs the private store used for launch/bind readiness.
// Secret values are never copied onto bindings or launch env.
func (t *RouteTable) SetCredentials(store CredentialStore) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.creds = store
}

func (t *RouteTable) requireAuthLocked(profile Profile) error {
	return requireAuthReady(profile, t.creds, t.lookup)
}

func (t *RouteTable) credentialReadyLocked(profile Profile) bool {
	return connectionAuthReady(profile, t.creds, t.lookup)
}

// SetContractVerifier installs the daemon-owned verifier used by Restore.
func (t *RouteTable) SetContractVerifier(v ProfileContractVerifier) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.verifier = v
}

// BindLaunch creates the initial RouteBinding for a Session from a profile.
func (t *RouteTable) BindLaunch(sessionID string, profile Profile, catalogRevision int64, auth ContractAuth) (SessionRouteState, error) {
	if t == nil {
		return SessionRouteState{}, fmt.Errorf("route table is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionRouteState{}, ErrBindingSessionRequired
	}
	admitted, err := AuthorizeProfileContract(profile, auth)
	if err != nil {
		return SessionRouteState{}, err
	}
	profile = normalizeProfile(profile)

	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.bySession[sessionID]; exists {
		return SessionRouteState{}, fmt.Errorf("%w: session %s already has a route binding", ErrBindingConflict, sessionID)
	}
	if err := t.requireAuthLocked(profile); err != nil {
		return SessionRouteState{}, err
	}

	ready := t.credentialReadyLocked(profile)
	draft, err := BindingDraftFromProfile(profile, catalogRevision, ActivationLaunch, ready, admitted)
	if err != nil {
		return SessionRouteState{}, err
	}
	draft.SessionID = sessionID
	draft.Generation = 1
	if draft.RouteProtocol != "" {
		routeID, err := t.newRoute()
		if err != nil {
			return SessionRouteState{}, err
		}
		if _, taken := t.byRoute[routeID]; taken {
			return SessionRouteState{}, fmt.Errorf("%w: route id collision", ErrBindingConflict)
		}
		draft.RouteID = routeID
		t.byRoute[routeID] = sessionID
	}
	state := SessionRouteState{
		Binding:    draft,
		Launched:   draft,
		Generation: 1,
		History: []RouteActivationEvent{{
			Generation: 1,
			Activation: ActivationLaunch,
			To:         draft,
		}},
	}
	t.bySession[sessionID] = cloneSessionState(state)
	return cloneSessionState(state), nil
}

// Activate atomically updates upstream fields on an existing routed Session.
// Client model + client envelope are immutable. History domain may change only
// while HistoryState is empty; once may_contain_opaque, domain must match exactly.
func (t *RouteTable) Activate(sessionID string, profile Profile, catalogRevision, expectedGeneration int64, auth ContractAuth) (SessionRouteState, error) {
	if t == nil {
		return SessionRouteState{}, fmt.Errorf("route table is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionRouteState{}, ErrBindingSessionRequired
	}
	admitted, err := AuthorizeProfileContract(profile, auth)
	if err != nil {
		return SessionRouteState{}, err
	}
	profile = normalizeProfile(profile)

	t.mu.Lock()
	defer t.mu.Unlock()
	current, ok := t.bySession[sessionID]
	if !ok {
		return SessionRouteState{}, fmt.Errorf("%w: %s", ErrBindingNotFound, sessionID)
	}
	if expectedGeneration != current.Generation {
		return SessionRouteState{}, fmt.Errorf("%w: expected generation %d, have %d", ErrBindingConflict, expectedGeneration, current.Generation)
	}
	if current.Binding.RouteID == "" || current.Binding.RouteProtocol == "" {
		return SessionRouteState{}, fmt.Errorf("%w: session %s", ErrBindingNotRouted, sessionID)
	}
	if normalizeID(profile.ExecutorID) != normalizeID(current.Binding.ExecutorID) {
		return SessionRouteState{}, fmt.Errorf("%w: current %s next %s", ErrBindingExecutorMismatch, current.Binding.ExecutorID, profile.ExecutorID)
	}
	if err := t.requireAuthLocked(profile); err != nil {
		return SessionRouteState{}, err
	}

	ready := t.credentialReadyLocked(profile)
	draft, err := BindingDraftFromProfile(profile, catalogRevision, ActivationActiveSession, ready, admitted)
	if err != nil {
		return SessionRouteState{}, err
	}
	if draft.RouteProtocol == "" {
		return SessionRouteState{}, fmt.Errorf("%w: session %s", ErrBindingNotRouted, sessionID)
	}
	if err := contractsCompatible(current.Binding, draft); err != nil {
		return SessionRouteState{}, err
	}
	// The binding swap is atomic and is never blocked by in-flight requests: a
	// request already admitted on this route keeps its immutable snapshot from
	// BeginRouteFlight and may finish against the old upstream, while every
	// later request admits under the new binding. A successful 2xx from the
	// old snapshot still marks the Session's history opaque (the CLI retains
	// that state), so cross-domain history guards keep applying to subsequent
	// switches.

	draft.SessionID = sessionID
	draft.Generation = current.Generation + 1
	draft.RouteID = current.Binding.RouteID
	draft.RouteProtocol = current.Binding.RouteProtocol
	draft.ClientModel = current.Binding.ClientModel
	draft.ClientModelProvenance = current.Binding.ClientModelProvenance
	draft.ClientEnvelope = current.Binding.ClientEnvelope
	draft.Protocol = current.Binding.Protocol
	draft.HistoryState = current.Binding.HistoryState
	// Sticky portability once enabled — CLIs may resent old opaque blocks forever.
	draft.HistoryPortability = current.Binding.HistoryPortability
	historyDegradation := ""
	opaqueCrossDomain := normalizeID(current.Binding.HistoryState) == HistoryStateMayContainOpaque &&
		current.Binding.HistoryDomain != draft.HistoryDomain
	if opaqueCrossDomain {
		// contractsCompatible already verified the portable same-protocol boundary.
		draft.HistoryPortability = HistoryPortabilityStripOpaque
		historyDegradation = HistoryDegradationStripOpaque
	} else if normalizeID(current.Binding.HistoryState) == HistoryStateMayContainOpaque {
		// Same-domain activate: keep the existing opaque domain owner.
		draft.HistoryDomain = current.Binding.HistoryDomain
	}

	event := RouteActivationEvent{
		Generation:         draft.Generation,
		Activation:         ActivationActiveSession,
		HistoryDegradation: historyDegradation,
		From:               current.Binding,
		To:                 draft,
	}
	next := SessionRouteState{
		Binding:    draft,
		Launched:   current.Launched,
		Generation: draft.Generation,
		History:    trimHistory(append(append([]RouteActivationEvent{}, current.History...), event)),
	}
	t.bySession[sessionID] = cloneSessionState(next)
	return cloneSessionState(next), nil
}

// BeginRouteFlight atomically snapshots the binding and registers an in-flight lease.
// Callers must Release(false) on local/network failure, or Complete(true) after a
// successful 2xx upstream response (before forwarding to the client).
func (t *RouteTable) BeginRouteFlight(routeID string) (RouteBinding, string, error) {
	if t == nil {
		return RouteBinding{}, "", fmt.Errorf("route table is not configured")
	}
	routeID = strings.TrimSpace(routeID)
	t.mu.Lock()
	defer t.mu.Unlock()
	sessionID, ok := t.byRoute[routeID]
	if !ok {
		return RouteBinding{}, "", ErrRouteNotFound
	}
	state, ok := t.bySession[sessionID]
	if !ok {
		return RouteBinding{}, "", ErrRouteNotFound
	}
	token, err := newFlightToken()
	if err != nil {
		return RouteBinding{}, "", err
	}
	if t.inFlight[routeID] == nil {
		t.inFlight[routeID] = map[string]routeFlight{}
	}
	t.inFlight[routeID][token] = routeFlight{
		sessionID:     sessionID,
		historyDomain: state.Binding.HistoryDomain,
		generation:    state.Generation,
	}
	binding := state.Binding
	binding.CredentialReady = t.credentialReadyLocked(profileFromBinding(binding))
	return binding, token, nil
}

// EndRouteFlight releases a lease. markOpaque=true atomically sets
// HistoryStateMayContainOpaque after a successful 2xx upstream response.
func (t *RouteTable) EndRouteFlight(routeID, token string, markOpaque bool) error {
	if t == nil {
		return fmt.Errorf("route table is not configured")
	}
	routeID = strings.TrimSpace(routeID)
	token = strings.TrimSpace(token)
	t.mu.Lock()
	defer t.mu.Unlock()
	flights := t.inFlight[routeID]
	if flights == nil {
		return nil
	}
	meta, ok := flights[token]
	if !ok {
		return nil
	}
	delete(flights, token)
	if len(flights) == 0 {
		delete(t.inFlight, routeID)
	}
	if !markOpaque {
		return nil
	}
	state, ok := t.bySession[meta.sessionID]
	if !ok {
		return ErrRouteNotFound
	}
	if normalizeID(state.Binding.HistoryState) == HistoryStateMayContainOpaque {
		return nil
	}
	state.Binding.HistoryState = HistoryStateMayContainOpaque
	t.bySession[meta.sessionID] = cloneSessionState(state)
	return nil
}

// InFlightCount returns the number of in-flight leases for a route (tests).
func (t *RouteTable) InFlightCount(routeID string) int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.inFlight[strings.TrimSpace(routeID)])
}

func newFlightToken() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "fl_" + hex.EncodeToString(buf[:]), nil
}

// MarkHistoryMayContainOpaque is retained for tests that force opaque state without a flight.
func (t *RouteTable) MarkHistoryMayContainOpaque(routeID string) error {
	if t == nil {
		return fmt.Errorf("route table is not configured")
	}
	routeID = strings.TrimSpace(routeID)
	t.mu.Lock()
	defer t.mu.Unlock()
	sessionID, ok := t.byRoute[routeID]
	if !ok {
		return ErrRouteNotFound
	}
	state, ok := t.bySession[sessionID]
	if !ok {
		return ErrRouteNotFound
	}
	if normalizeID(state.Binding.HistoryState) == HistoryStateMayContainOpaque {
		return nil
	}
	state.Binding.HistoryState = HistoryStateMayContainOpaque
	t.bySession[sessionID] = cloneSessionState(state)
	return nil
}

// Get returns a copy of the Session route state with CredentialReady rechecked.
func (t *RouteTable) Get(sessionID string) (SessionRouteState, bool) {
	if t == nil {
		return SessionRouteState{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state, ok := t.bySession[strings.TrimSpace(sessionID)]
	if !ok {
		return SessionRouteState{}, false
	}
	state = cloneSessionState(state)
	state.Binding.CredentialReady = t.credentialReadyLocked(profileFromBinding(state.Binding))
	return state, true
}

// GetByRouteID resolves a route id to its Session binding (router lookup only).
func (t *RouteTable) GetByRouteID(routeID string) (RouteBinding, bool) {
	if t == nil {
		return RouteBinding{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	sessionID, ok := t.byRoute[strings.TrimSpace(routeID)]
	if !ok {
		return RouteBinding{}, false
	}
	state, ok := t.bySession[sessionID]
	if !ok {
		return RouteBinding{}, false
	}
	binding := state.Binding
	binding.CredentialReady = t.credentialReadyLocked(profileFromBinding(binding))
	return binding, true
}

// RebindSession moves an existing binding from fromID to toID while preserving
// RouteID, generation, history, and upstream fields. Used when a Zen Session
// identity is remapped (e.g. missing-tmux native resume) without allocating a
// new opaque route or native conversation.
func (t *RouteTable) RebindSession(fromID, toID string) error {
	if t == nil {
		return fmt.Errorf("route table is not configured")
	}
	fromID = strings.TrimSpace(fromID)
	toID = strings.TrimSpace(toID)
	if fromID == "" || toID == "" {
		return ErrBindingSessionRequired
	}
	if fromID == toID {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state, ok := t.bySession[fromID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrBindingNotFound, fromID)
	}
	if _, exists := t.bySession[toID]; exists {
		return fmt.Errorf("%w: session %s already has a route binding", ErrBindingConflict, toID)
	}
	if n := len(t.inFlight[state.Binding.RouteID]); n > 0 {
		return fmt.Errorf("%w: %d in-flight", ErrBindingBusy, n)
	}
	state.Binding.SessionID = toID
	if state.Launched.ProfileID != "" || state.Launched.RouteID != "" || state.Launched.SessionID != "" {
		state.Launched.SessionID = toID
	}
	delete(t.bySession, fromID)
	t.bySession[toID] = cloneSessionState(state)
	if rid := state.Binding.RouteID; rid != "" {
		t.byRoute[rid] = toID
	}
	return nil
}

// SessionsUsingProfile returns Session IDs currently bound to profileID.
func (t *RouteTable) SessionsUsingProfile(profileID string) []string {
	if t == nil {
		return nil
	}
	profileID = normalizeID(profileID)
	if profileID == "" {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0)
	for sid, state := range t.bySession {
		if normalizeID(state.Binding.ProfileID) == profileID {
			out = append(out, sid)
		}
	}
	return out
}

// Release removes a Session binding and its route id index entry.
func (t *RouteTable) Release(sessionID string) error {
	if t == nil {
		return fmt.Errorf("route table is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ErrBindingSessionRequired
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state, ok := t.bySession[sessionID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrBindingNotFound, sessionID)
	}
	if state.Binding.RouteID != "" {
		delete(t.byRoute, state.Binding.RouteID)
		delete(t.inFlight, state.Binding.RouteID)
	}
	delete(t.bySession, sessionID)
	return nil
}

// Len returns the number of bound Sessions (tests/diagnostics).
func (t *RouteTable) Len() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.bySession)
}

func (t *RouteTable) credentialLookup(name string) (string, bool) {
	if t == nil {
		return lookupEnv(name)
	}
	t.mu.Lock()
	lookup := t.lookup
	t.mu.Unlock()
	if lookup == nil {
		return lookupEnv(name)
	}
	return lookup(name)
}

func (t *RouteTable) contractVerifier() ProfileContractVerifier {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.verifier
}

func cloneSessionState(state SessionRouteState) SessionRouteState {
	out := state
	if state.History != nil {
		out.History = append([]RouteActivationEvent{}, state.History...)
	}
	out.Binding.ClientEnvelope = cloneEnvelope(state.Binding.ClientEnvelope)
	out.Binding.UpstreamEnvelope = cloneEnvelope(state.Binding.UpstreamEnvelope)
	out.Launched.ClientEnvelope = cloneEnvelope(state.Launched.ClientEnvelope)
	out.Launched.UpstreamEnvelope = cloneEnvelope(state.Launched.UpstreamEnvelope)
	return out
}

func cloneEnvelope(e CapabilityEnvelope) CapabilityEnvelope {
	if e.Modalities != nil {
		e.Modalities = append([]string{}, e.Modalities...)
	}
	return e
}

func newOpaqueRouteID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "rt_" + hex.EncodeToString(buf[:]), nil
}
