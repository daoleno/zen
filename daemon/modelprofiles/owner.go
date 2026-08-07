package modelprofiles

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Typed control / wire error codes for App and CLI consumers.
const (
	CodeProfilesUnavailable        = "model_profiles_unavailable"
	CodeProfileNotFound            = "model_profile_not_found"
	CodeProfileConflict            = "model_profile_conflict"
	CodeProfileInvalid             = "model_profile_invalid"
	CodeProfileInUse               = "model_profile_in_use"
	CodeProfileUnsupported         = "model_profile_unsupported"
	CodeCredentialNotReady         = "model_profile_credential_not_ready"
	CodeCredentialStoreUnavailable = "credential_store_unavailable"
	CodeCredentialStoreFailed      = "credential_store_failed"
	CodeSecureTransportRequired    = "secure_transport_required"
	CodeBindingNotFound            = "route_binding_not_found"
	CodeBindingConflict            = "route_binding_conflict"
	CodeBindingBusy                = "route_binding_busy"
	CodeBindingIncompatible        = "route_binding_incompatible"
	CodeBindingNotRouted           = "route_binding_not_routed"
	CodeContractUnverified         = "model_profile_contract_unverified"
	CodeRouteListenerFailed        = "route_listener_failed"
	CodeRouteSnapshotInvalid       = "route_snapshot_invalid"
	CodeLaunchBypass               = "model_profile_bypass"
)

// OwnerConfig configures production Owner paths and seams.
type OwnerConfig struct {
	ProfilesPath  string
	RoutesPath    string
	ListenerPath  string
	DiscoveryPath string // secret-free TTL/LKG model id cache
	Lookup        func(string) (string, bool)
	Credentials   CredentialStore // OS keyring (or test fake); optional
	Verifier      ProfileContractVerifier
	ListenNetwork string // default "tcp"
	// PreferAddr overrides ListenerFile for tests when the listener is started
	// (live-route restore or first managed launch).
	PreferAddr string
	// RoutesPersistHook is a test seam installed on the route-state file before
	// StartOwner sweeps provisionals / starts the listener.
	RoutesPersistHook func(phase string) error
	// ListenerPersistHook is a test seam installed on the listener file before
	// StartOwner converges inert metadata or restores a sticky listener.
	ListenerPersistHook func(phase string) error
}

// Owner is the production lifecycle owner for catalog + RouteTable + loopback Router.
// All RouteTable mutations that touch durable state run under Owner.mu as
// mutate+persist transactions with snapshot rollback on save failure.
//
// The loopback listener is started only when live routes are restored or on the
// first managed launch. Cold installs with an empty catalog and no routes stay
// inert: no listen socket and no listener-state file write.
type Owner struct {
	mu            sync.Mutex
	store         *Store
	table         *RouteTable
	router        *Router
	routes        *RouteStateFile
	listener      *ListenerFile
	ln            net.Listener
	srv           *http.Server
	addr          string
	listenNetwork string
	preferAddr    string
	lookup        func(string) (string, bool)
	creds         CredentialStore
	verifier      ProfileContractVerifier
	started       bool
	closed        bool
	// idleListenerOwned is true when this Owner started the loopback listener
	// from an inert (no live routes) state and may tear it down on failed first
	// launch / last Abort when no routes remain.
	idleListenerOwned    bool
	listenerBackupRaw    []byte
	listenerBackupHad    bool
	listenerBackupSet    bool
	discovery            *modelDiscoveryCache
	discoveryPath        string
	discoveryLoadWarning error
}

// SessionLaunchPlan is the secret-free result of resolving a profile for create.
type SessionLaunchPlan struct {
	Applied       bool
	Bypass        bool
	Command       string
	Env           map[string]string
	ProvisionalID string
	State         SessionRouteState
	Wire          WireBinding
	Launch        ResolvedLaunch
	Persist       PersistResult
}

// WireSessionSnapshot is the control-plane Session Provider selection projection.
// Generation, activation history, and degradation facts are not ordinary public.
type WireSessionSnapshot struct {
	Launched *WireBinding `json:"launched,omitempty"`
	Current  *WireBinding `json:"current,omitempty"`
	Ready    bool         `json:"credential_ready"`
}

// CatalogProjection is an atomic catalog + views snapshot for control-plane
// list/mutation replies (same Store read lock / Owner transaction boundary).
type CatalogProjection struct {
	Catalog Catalog
	Views   []ProfileView
}

// StartOwner loads catalog + durable routes. Missing profile/route files are OK
// (empty). Malformed route snapshots fail closed.
//
// Listener semantics:
//   - Live (committed) routes: bind the persisted (or PreferAddr) port or fail
//     closed, then serve the Router and refresh listener-state.
//   - No live routes: remain inert — no loopback listener and no listener-state
//     write. Stale/malformed listener metadata is ignored, not rewritten.
//     Catalog CRUD works; the listener starts atomically on first managed launch.
//
// pending:* provisional bindings are cleanup records, never live Sessions. On
// start they are quarantined and removed (and listener metadata cleared when no
// live routes remain) before hasRoutes / sticky listen. Failure to persist that
// sweep fails StartOwner closed.
func StartOwner(cfg OwnerConfig) (*Owner, error) {
	profilesPath := strings.TrimSpace(cfg.ProfilesPath)
	routesPath := strings.TrimSpace(cfg.RoutesPath)
	listenerPath := strings.TrimSpace(cfg.ListenerPath)
	if profilesPath == "" || routesPath == "" || listenerPath == "" {
		return nil, fmt.Errorf("%w: profiles, routes, and listener paths are required", ErrInvalid)
	}
	lookup := cfg.Lookup
	if lookup == nil {
		lookup = lookupEnv
	}
	verifier := cfg.Verifier
	if verifier == nil {
		verifier = BuiltinEnvelopeVerifier{}
	}

	store, err := NewStore(profilesPath)
	if err != nil {
		return nil, err
	}
	store.SetLookup(lookup)
	if err := reauthorizeCatalogProfiles(store, verifier); err != nil {
		return nil, err
	}

	table := NewRouteTable()
	table.SetLookup(lookup)
	table.SetContractVerifier(verifier)

	routes, err := NewRouteStateFile(routesPath)
	if err != nil {
		return nil, err
	}
	if cfg.RoutesPersistHook != nil {
		routes.SetPersistHook(cfg.RoutesPersistHook)
	}
	if err := routes.Load(table); err != nil {
		return nil, err
	}

	listenerFile, err := NewListenerFile(listenerPath)
	if err != nil {
		return nil, err
	}
	if cfg.ListenerPersistHook != nil {
		listenerFile.SetPersistHook(cfg.ListenerPersistHook)
	}
	network := strings.TrimSpace(cfg.ListenNetwork)
	if network == "" {
		network = "tcp"
	}

	o := &Owner{
		store:         store,
		table:         table,
		router:        NewRouter(table, WithRouterLookup(lookup), WithRouterCredentials(cfg.Credentials)),
		routes:        routes,
		listener:      listenerFile,
		listenNetwork: network,
		preferAddr:    strings.TrimSpace(cfg.PreferAddr),
		lookup:        lookup,
		creds:         cfg.Credentials,
		verifier:      verifier,
		started:       true,
		discovery:     newModelDiscoveryCache(),
		discoveryPath: strings.TrimSpace(cfg.DiscoveryPath),
	}
	if o.discoveryPath != "" {
		if err := o.discovery.load(o.discoveryPath); err != nil {
			o.discoveryLoadWarning = fmt.Errorf("%w: %v", ErrDiscoveryCacheInvalid, err)
		}
	}
	o.mu.Lock()
	sweepErr := o.sweepProvisionalRoutesLocked()
	hasRoutes := o.table.Len() > 0
	var listenErr error
	if sweepErr == nil {
		if hasRoutes {
			_, listenErr = o.ensureListenerLocked(true)
		} else {
			// Zero live routes after load/sweep: always converge Zen-owned
			// listener metadata to inert, including retries after a prior
			// sweep that already removed pending:* but failed RemoveDurable.
			listenErr = o.convergeInertListenerMetadataLocked()
		}
	}
	o.mu.Unlock()
	if sweepErr != nil {
		_ = o.Close()
		return nil, sweepErr
	}
	if listenErr != nil && !listenerMetadataApplied(listenErr) {
		_ = o.Close()
		return nil, listenErr
	}
	return o, nil
}

const provisionalSessionPrefix = "pending:"

func isProvisionalSessionID(sessionID string) bool {
	return strings.HasPrefix(strings.TrimSpace(sessionID), provisionalSessionPrefix)
}

// sweepProvisionalRoutesLocked removes durable pending:* cleanup records before
// live-route listener decisions. Caller must hold o.mu.
func (o *Owner) sweepProvisionalRoutesLocked() error {
	if o == nil || o.table == nil {
		return nil
	}
	pending := make([]string, 0)
	for _, state := range o.table.Snapshot() {
		if isProvisionalSessionID(state.Binding.SessionID) {
			pending = append(pending, state.Binding.SessionID)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	before := o.table.Snapshot()
	for _, id := range pending {
		if err := o.table.Release(id); err != nil && !errors.Is(err, ErrBindingNotFound) {
			o.table.ReplaceSnapshot(before)
			return fmt.Errorf("%w: release provisional %s: %v", ErrLaunchCleanupIncomplete, id, err)
		}
	}
	after := o.table.Snapshot()
	if err := o.persistStatesLocked(after); err != nil {
		if errors.Is(err, ErrPersistDirSync) {
			// Rename committed: keep swept memory aligned with disk.
		} else {
			o.table.ReplaceSnapshot(before)
			return fmt.Errorf("%w: persist provisional sweep: %w", ErrLaunchCleanupIncomplete, err)
		}
	}
	// Listener metadata is converged by StartOwner after sweep when the table
	// is empty so a failed RemoveDurable can retry on the next startup even
	// when no pending:* entries remain.
	return nil
}

// convergeInertListenerMetadataLocked removes stale Zen-owned listener metadata
// when there are zero live routes. Fail closed on not-applied removal; allow
// applied+ErrPersistDirSync conservatively. Caller must hold o.mu.
func (o *Owner) convergeInertListenerMetadataLocked() error {
	if o == nil || o.listener == nil {
		return nil
	}
	if o.table != nil && o.table.Len() > 0 {
		return nil
	}
	if o.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = o.srv.Shutdown(ctx)
		cancel()
	}
	if o.ln != nil {
		_ = o.ln.Close()
	}
	o.ln = nil
	o.srv = nil
	o.addr = ""
	if err := o.listener.RemoveDurable(); err != nil && !listenerMetadataApplied(err) {
		return fmt.Errorf("%w: converge inert listener metadata: %w", ErrLaunchCleanupIncomplete, err)
	} else if errors.Is(err, ErrPersistDirSync) {
		return err
	}
	return nil
}

// ensureListenerLocked binds and serves the loopback Router if needed.
// sticky=true requires the persisted (or PreferAddr) listen address — used when
// restoring live routes. sticky=false ignores stale listener metadata and may
// fall back to an ephemeral port (first managed launch / PreferAddr conflict).
// Returns PersistResult for listener-state durability (ErrPersistDirSync =>
// Applied+!Durable). Caller must hold o.mu.
func (o *Owner) ensureListenerLocked(sticky bool) (PersistResult, error) {
	if o == nil {
		return PersistResult{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	if o.closed {
		return PersistResult{}, fmt.Errorf("%w: owner closed", ErrInvalid)
	}
	if o.ln != nil && o.srv != nil && strings.TrimSpace(o.addr) != "" {
		return PersistResult{Applied: true, Durable: true}, nil
	}

	fromIdle := true
	if !sticky {
		if err := o.captureListenerBackupLocked(); err != nil {
			return PersistResult{}, err
		}
	}

	prefer := strings.TrimSpace(o.preferAddr)
	if prefer == "" && sticky {
		loaded, err := o.listener.Load()
		if err != nil {
			return PersistResult{}, err
		}
		if strings.TrimSpace(loaded) == "" {
			return PersistResult{}, fmt.Errorf("%w: live routes require persisted listen addr", ErrListenerFailed)
		}
		prefer = loaded
	}

	ln, addr, err := bindLoopbackListener(o.listenNetwork, prefer, sticky)
	if err != nil {
		return PersistResult{}, err
	}
	saveErr := o.listener.Save(addr)
	if saveErr != nil && !errors.Is(saveErr, ErrPersistDirSync) {
		_ = ln.Close()
		return PersistResult{Applied: false, Durable: false}, saveErr
	}

	if o.router == nil {
		o.router = NewRouter(o.table, WithRouterLookup(o.lookup))
	}
	srv := &http.Server{
		Handler:           o.router.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	o.ln = ln
	o.srv = srv
	o.addr = addr
	if fromIdle && !sticky {
		o.idleListenerOwned = true
	}
	go func() {
		_ = srv.Serve(ln)
	}()
	if saveErr != nil && errors.Is(saveErr, ErrPersistDirSync) {
		return PersistResult{Applied: true, Durable: false}, saveErr
	}
	return PersistResult{Applied: true, Durable: true}, nil
}

func (o *Owner) captureListenerBackupLocked() error {
	if o == nil || o.listener == nil {
		return fmt.Errorf("%w: listener file is not configured", ErrInvalid)
	}
	if o.listenerBackupSet {
		return nil
	}
	raw, err := o.listener.readBytes()
	if err != nil {
		if os.IsNotExist(err) {
			o.listenerBackupHad = false
			o.listenerBackupRaw = nil
			o.listenerBackupSet = true
			return nil
		}
		// Present-but-unreadable (or other IO): fail closed before Save.
		return fmt.Errorf("%w: read listener metadata: %v", ErrInvalid, err)
	}
	o.listenerBackupHad = true
	o.listenerBackupRaw = append([]byte{}, raw...)
	o.listenerBackupSet = true
	return nil
}

// releaseIdleListenerLocked stops a listener this Owner started from inert when
// no live routes remain, restoring prior listener metadata bytes (or removing
// a file this attempt created). Never deletes unrelated live-route listener
// state (idleListenerOwned=false).
//
// On pre-rename / not-applied restore failure, retains idleListenerOwned and the
// exact original backup so cleanup is retryable. Clears ownership + backup only
// after restore/remove is applied (including ErrPersistDirSync).
func (o *Owner) releaseIdleListenerLocked() error {
	if o == nil || !o.idleListenerOwned {
		return nil
	}
	if o.table != nil && o.table.Len() > 0 {
		return nil
	}
	var first error
	if o.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := o.srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			first = err
		}
		cancel()
	}
	if o.ln != nil {
		_ = o.ln.Close()
	}
	o.ln = nil
	o.srv = nil
	o.addr = ""

	restoreErr := o.restoreListenerBackupLocked()
	if restoreErr != nil {
		first = joinErrors(first, restoreErr)
	}
	if listenerMetadataApplied(restoreErr) {
		o.idleListenerOwned = false
		o.listenerBackupSet = false
		o.listenerBackupHad = false
		o.listenerBackupRaw = nil
	}
	return first
}

// restoreListenerBackupLocked restores or removes listener metadata using the
// captured backup. Does not clear backup ownership; caller clears only after
// the write is applied.
func (o *Owner) restoreListenerBackupLocked() error {
	if o == nil || o.listener == nil {
		return nil
	}
	if !o.listenerBackupSet {
		return nil
	}
	if !o.listenerBackupHad {
		return o.listener.RemoveDurable()
	}
	return o.listener.RestoreBytes(o.listenerBackupRaw)
}

func listenerMetadataApplied(err error) bool {
	return err == nil || errors.Is(err, ErrPersistDirSync)
}

func bindLoopbackListener(network, prefer string, sticky bool) (net.Listener, string, error) {
	if prefer != "" {
		ln, err := net.Listen(network, prefer)
		if err == nil {
			return ln, ln.Addr().String(), nil
		}
		if sticky {
			return nil, "", fmt.Errorf("%w: required listen addr %s: %v", ErrListenerFailed, prefer, err)
		}
	}
	ln, err := net.Listen(network, "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrListenerFailed, err)
	}
	return ln, ln.Addr().String(), nil
}

// Close stops the loopback listener and HTTP server.
func (o *Owner) Close() error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil
	}
	o.closed = true
	var first error
	if o.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := o.srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			first = err
		}
	}
	if o.ln != nil {
		_ = o.ln.Close()
	}
	o.started = false
	return first
}

// ListenAddr returns the bound loopback host:port.
func (o *Owner) ListenAddr() string {
	if o == nil {
		return ""
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.addr
}

// Store returns the catalog owner (tests / advanced callers).
func (o *Owner) Store() *Store {
	if o == nil {
		return nil
	}
	return o.store
}

// Table returns the RouteTable (tests).
func (o *Owner) Table() *RouteTable {
	if o == nil {
		return nil
	}
	return o.table
}

// RoutesFile returns the durable route-state owner (tests/failpoints).
func (o *Owner) RoutesFile() *RouteStateFile {
	if o == nil {
		return nil
	}
	return o.routes
}

// Catalog returns the secret-free catalog snapshot.
func (o *Owner) Catalog() Catalog {
	if o == nil || o.store == nil {
		return Catalog{Defaults: map[string]string{}}
	}
	return o.store.Catalog()
}

// Views returns profiles with credential readiness.
func (o *Owner) Views() []ProfileView {
	if o == nil || o.store == nil {
		return nil
	}
	return o.store.Views()
}

// ProjectCatalog captures catalog + views under Owner.mu and one Store read lock
// so list/CRUD replies cannot mix revisions under concurrent mutation.
func (o *Owner) ProjectCatalog() CatalogProjection {
	if o == nil || o.store == nil {
		return CatalogProjection{Catalog: Catalog{Defaults: map[string]string{}}}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	catalog, views := o.store.Projection()
	return CatalogProjection{Catalog: catalog, Views: views}
}

// GetProfile returns one profile.
func (o *Owner) GetProfile(id string) (Profile, error) {
	if o == nil || o.store == nil {
		return Profile{}, ErrNotFound
	}
	return o.store.Get(id)
}

// UpsertProfile creates or updates a profile under CAS revision after full
// AuthorizeProfileContract admission. Serialized under Owner.mu against
// PrepareLaunch/ActivateSession profile resolution. Returns an atomic
// catalog+views projection from the same transaction.
func (o *Owner) UpsertProfile(profile Profile, expectedRevision int64, create bool) (CatalogProjection, error) {
	if o == nil || o.store == nil {
		return CatalogProjection{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	profile = normalizeProfile(profile)
	if err := ValidateProfile(profile); err != nil {
		return CatalogProjection{}, err
	}
	if !isAccountConnection(profile) {
		if _, err := AuthorizeProfileContract(profile, ContractAuth{Verifier: o.verifier}); err != nil {
			return CatalogProjection{}, err
		}
	}
	var err error
	if create {
		_, err = o.store.Create(profile, expectedRevision)
	} else {
		_, err = o.store.Update(profile, expectedRevision)
	}
	if err != nil && !errors.Is(err, ErrPersistDirSync) {
		return CatalogProjection{}, err
	}
	catalog, views := o.store.Projection()
	return CatalogProjection{Catalog: catalog, Views: views}, err
}

// DeleteProfile removes a profile. Rejects defaults and in-use Session bindings.
// The in-use check and Store.Delete run under Owner.mu so PrepareLaunch /
// ActivateSession / Commit / Transfer / Release cannot bind a deleted Profile.
func (o *Owner) DeleteProfile(id string, expectedRevision int64) (CatalogProjection, error) {
	if o == nil || o.store == nil {
		return CatalogProjection{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if users := o.table.SessionsUsingProfile(id); len(users) > 0 {
		return CatalogProjection{}, fmt.Errorf("%w: profile %s is bound to %d session(s)", ErrProfileInUse, normalizeID(id), len(users))
	}
	_, err := o.store.Delete(id, expectedRevision)
	if err != nil && !errors.Is(err, ErrPersistDirSync) {
		return CatalogProjection{}, err
	}
	catalog, views := o.store.Projection()
	return CatalogProjection{Catalog: catalog, Views: views}, err
}

// SetDefault sets or clears the default profile for an executor under Owner.mu
// so PrepareLaunch cannot observe a torn catalog revision.
func (o *Owner) SetDefault(executorID, profileID string, expectedRevision int64) (CatalogProjection, error) {
	if o == nil || o.store == nil {
		return CatalogProjection{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	_, err := o.store.SetDefault(executorID, profileID, expectedRevision)
	if err != nil && !errors.Is(err, ErrPersistDirSync) {
		return CatalogProjection{}, err
	}
	catalog, views := o.store.Projection()
	return CatalogProjection{Catalog: catalog, Views: views}, err
}

// reauthorizeCatalogProfiles fail-closes StartOwner when durable TOML profiles
// do not pass the configured verifier via AuthorizeProfileContract.
func reauthorizeCatalogProfiles(store *Store, verifier ProfileContractVerifier) error {
	if store == nil {
		return nil
	}
	for _, profile := range store.Catalog().Profiles {
		if isAccountConnection(profile) {
			// Account connections are authorized when compiled per client.
			if err := validateAccountConnection(profile); err != nil {
				return fmt.Errorf("%w: catalog connection %q: %v", ErrContractUnverified, profile.ID, err)
			}
			continue
		}
		if _, err := AuthorizeProfileContract(profile, ContractAuth{Verifier: verifier}); err != nil {
			return fmt.Errorf("%w: catalog profile %q: %v", ErrContractUnverified, profile.ID, err)
		}
	}
	return nil
}

// PersistResult mirrors auth.PersistenceResult / control PersistenceOutcome:
// Applied means the durable named file reflects the mutation; Durable means
// directory sync confirmed. Applied && !Durable is success-with-warning.
type PersistResult struct {
	Applied bool
	Durable bool
}

// WirePersistFields projects PersistResult onto control PersistenceOutcome fields.
func WirePersistFields(p PersistResult) (outcome string, durable *bool) {
	if !p.Applied {
		return "", nil
	}
	d := p.Durable
	return string(controlPersistenceApplied), &d
}

// PersistResultFromError maps Store/Route persist errors onto PersistResult.
// nil => applied+durable; ErrPersistDirSync => applied+!durable; else not-applied.
func PersistResultFromError(err error) PersistResult {
	switch {
	case err == nil:
		return PersistResult{Applied: true, Durable: true}
	case errors.Is(err, ErrPersistDirSync):
		return PersistResult{Applied: true, Durable: false}
	default:
		return PersistResult{Applied: false, Durable: false}
	}
}

// CombinePersistResults merges independent applied mutations. If either side is
// applied with uncertain durability, the result is applied+!durable.
func CombinePersistResults(a, b PersistResult) PersistResult {
	if !a.Applied {
		return a
	}
	if !b.Applied {
		return b
	}
	return PersistResult{Applied: true, Durable: a.Durable && b.Durable}
}

// combinePersistResults is the internal alias used by Owner.
func combinePersistResults(a, b PersistResult) PersistResult {
	return CombinePersistResults(a, b)
}

// Keep wire strings aligned with control.PersistenceApplied without importing
// control into this package (avoid cycles with cmd/zen).
const controlPersistenceApplied = "applied"

// mutateAndPersistLocked runs mut under Owner.mu, then persists a clone of the
// new table. Pre-rename persist failures roll memory back (not applied).
// Rename-committed durability uncertainty keeps memory at `after` and returns
// Applied=true, Durable=false with ErrPersistDirSync.
func (o *Owner) mutateAndPersistLocked(mut func() error) (PersistResult, error) {
	before := o.table.Snapshot()
	if err := mut(); err != nil {
		o.table.ReplaceSnapshot(before)
		return PersistResult{}, err
	}
	after := o.table.Snapshot()
	if err := o.persistStatesLocked(after); err != nil {
		if errors.Is(err, ErrPersistDirSync) {
			return PersistResult{Applied: true, Durable: false}, err
		}
		o.table.ReplaceSnapshot(before)
		return PersistResult{Applied: false, Durable: false}, err
	}
	return PersistResult{Applied: true, Durable: true}, nil
}

func (o *Owner) persistStatesLocked(states []SessionRouteState) error {
	if o == nil || o.routes == nil {
		return nil
	}
	return o.routes.SaveStates(states)
}

// PrepareLaunch resolves profile override/default, binds a provisional route, and
// compiles a secret-free command/env plan. Raw/custom/unsupported commands bypass.
func (o *Owner) PrepareLaunch(executorID, profileID, baseCommand string) (SessionLaunchPlan, error) {
	if o == nil || !o.started {
		return SessionLaunchPlan{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	o.mu.Lock()
	defer o.mu.Unlock()

	baseCommand = strings.TrimSpace(baseCommand)
	executorID = normalizeID(executorID)
	if executorID == "" {
		executorID = inferExecutorFromCommand(baseCommand)
	}
	if shouldBypassProfiles(executorID, profileID, baseCommand) {
		return SessionLaunchPlan{Bypass: true, Command: baseCommand}, nil
	}
	if !SupportsExecutor(executorID) {
		if strings.TrimSpace(profileID) != "" {
			return SessionLaunchPlan{}, fmt.Errorf("%w: %s", ErrUnsupportedExecutor, executorID)
		}
		return SessionLaunchPlan{Bypass: true, Command: baseCommand}, nil
	}

	profile, err := o.store.ResolveProfile(executorID, profileID)
	if err != nil {
		if errors.Is(err, ErrNotFound) && strings.TrimSpace(profileID) == "" {
			return SessionLaunchPlan{Bypass: true, Command: baseCommand}, nil
		}
		return SessionLaunchPlan{}, err
	}

	auth := ContractAuth{Verifier: o.verifier}
	provisional := "pending:" + uuid.NewString()
	var state SessionRouteState
	var launch ResolvedLaunch
	listenPersist := PersistResult{Applied: true, Durable: true}
	startedListener := false
	if routeProto, needsRoute := RouteProtocolFor(profile.Protocol); needsRoute && routeProto != "" {
		wasListening := o.ln != nil && strings.TrimSpace(o.addr) != ""
		var listenErr error
		listenPersist, listenErr = o.ensureListenerLocked(false)
		if listenErr != nil && !errors.Is(listenErr, ErrPersistDirSync) {
			return SessionLaunchPlan{}, listenErr
		}
		startedListener = !wasListening && o.ln != nil
	}
	persist, err := o.mutateAndPersistLocked(func() error {
		bound, bindErr := o.table.BindLaunch(provisional, profile, o.store.Revision(), auth)
		if bindErr != nil {
			return bindErr
		}
		state = bound
		loopbackURL := ""
		if state.Binding.RouteProtocol != "" {
			var urlErr error
			switch normalizeID(profile.ExecutorID) {
			case ExecutorCodex:
				loopbackURL, urlErr = LoopbackCodexBaseURL(o.addr, state.Binding.RouteID)
			case ExecutorClaude:
				loopbackURL, urlErr = LoopbackClaudeRootURL(o.addr, state.Binding.RouteID)
			default:
				urlErr = fmt.Errorf("%w: %s", ErrUnsupportedExecutor, profile.ExecutorID)
			}
			if urlErr != nil {
				return urlErr
			}
		}
		compiled, compileErr := Compile(baseCommand, profile, CompileOptions{
			LoopbackRouteURL: loopbackURL,
			CatalogRevision:  o.store.Revision(),
			Lookup:           o.lookup,
			Verifier:         o.verifier,
		})
		if compileErr != nil {
			return compileErr
		}
		launch = compiled
		return nil
	})
	if !persist.Applied {
		if startedListener {
			if releaseErr := o.releaseIdleListenerLocked(); releaseErr != nil {
				err = joinErrors(err, fmt.Errorf("listener cleanup: %w", releaseErr))
				if !listenerMetadataApplied(releaseErr) {
					err = joinErrors(err, ErrLaunchCleanupIncomplete)
				}
			}
		}
		return SessionLaunchPlan{}, err
	}
	persist = combinePersistResults(listenPersist, persist)
	if !persist.Durable && err == nil && !listenPersist.Durable {
		err = ErrPersistDirSync
	}
	wire := state.Binding.ToWire()
	return SessionLaunchPlan{
		Applied:       true,
		Command:       launch.Command,
		Env:           launch.Env,
		ProvisionalID: provisional,
		State:         state,
		Wire:          wire,
		Launch:        launch,
		Persist:       persist,
	}, err
}

// CommitLaunch rebinds a provisional launch to the real Zen Session id.
// When Persist.Applied is true the binding is live even if Durable is false.
// The returned WireSessionSnapshot is built under the same Owner.mu transaction.
func (o *Owner) CommitLaunch(provisionalID, sessionID string) (SessionRouteState, WireSessionSnapshot, PersistResult, error) {
	if o == nil || !o.started {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	persist, err := o.mutateAndPersistLocked(func() error {
		return o.table.RebindSession(provisionalID, sessionID)
	})
	if !persist.Applied {
		return SessionRouteState{}, WireSessionSnapshot{}, persist, err
	}
	state, ok := o.table.Get(sessionID)
	if !ok {
		return SessionRouteState{}, WireSessionSnapshot{}, persist, fmt.Errorf("%w: %s", ErrBindingNotFound, sessionID)
	}
	return state, wireSessionSnapshotFromState(state), persist, err
}

// AbortLaunch releases a provisional binding after create failure.
func (o *Owner) AbortLaunch(provisionalID string) (PersistResult, error) {
	if o == nil || provisionalID == "" {
		return PersistResult{Applied: true, Durable: true}, nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.started {
		return PersistResult{Applied: true, Durable: true}, nil
	}
	persist, err := o.mutateAndPersistLocked(func() error {
		if err := o.table.Release(provisionalID); err != nil && !errors.Is(err, ErrBindingNotFound) {
			return err
		}
		return nil
	})
	if persist.Applied {
		if releaseErr := o.releaseIdleListenerLocked(); releaseErr != nil {
			cleanup := PersistResultFromError(releaseErr)
			if cleanup.Applied && !cleanup.Durable {
				persist = combinePersistResults(persist, cleanup)
			} else if !cleanup.Applied {
				persist = PersistResult{Applied: persist.Applied, Durable: false}
			} else {
				persist = combinePersistResults(persist, cleanup)
			}
			err = joinErrors(err, fmt.Errorf("listener cleanup: %w", releaseErr))
			if !listenerMetadataApplied(releaseErr) {
				err = joinErrors(err, ErrLaunchCleanupIncomplete)
			}
		}
	} else if err != nil {
		err = joinErrors(err, ErrLaunchCleanupIncomplete)
	}
	return persist, err
}

// ActivateSession atomically activates a profile on an existing routed Session.
// WireSessionSnapshot is projected from the mutated state under the same Owner.mu
// transaction; callers must not unlock then re-read for the mutation reply.
func (o *Owner) ActivateSession(sessionID, profileID string, expectedGeneration int64) (SessionRouteState, WireSessionSnapshot, PersistResult, error) {
	if o == nil || !o.started {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	current, ok := o.table.Get(sessionID)
	if !ok {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, fmt.Errorf("%w: %s", ErrBindingNotFound, sessionID)
	}
	profile, err := o.store.ResolveProfile(current.Binding.ExecutorID, profileID)
	if err != nil {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, err
	}
	return o.activateCompiledProfileLocked(sessionID, profile, expectedGeneration)
}

// activateCompiledProfile activates an already-compiled Profile without catalog mutation.
func (o *Owner) activateCompiledProfile(sessionID string, profile Profile, expectedGeneration int64) (SessionRouteState, WireSessionSnapshot, PersistResult, error) {
	if o == nil || !o.started {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.activateCompiledProfileLocked(sessionID, profile, expectedGeneration)
}

func (o *Owner) activateCompiledProfileLocked(sessionID string, profile Profile, expectedGeneration int64) (SessionRouteState, WireSessionSnapshot, PersistResult, error) {
	var state SessionRouteState
	persist, err := o.mutateAndPersistLocked(func() error {
		next, actErr := o.table.Activate(sessionID, profile, o.store.Revision(), expectedGeneration, ContractAuth{Verifier: o.verifier})
		if actErr != nil {
			return actErr
		}
		state = next
		return nil
	})
	if !persist.Applied {
		return SessionRouteState{}, WireSessionSnapshot{}, persist, err
	}
	return state, wireSessionSnapshotFromState(state), persist, err
}

// SessionSnapshot returns the App-safe launched/current binding view.
func (o *Owner) SessionSnapshot(sessionID string) (WireSessionSnapshot, bool) {
	if o == nil {
		return WireSessionSnapshot{}, false
	}
	state, ok := o.table.Get(sessionID)
	if !ok {
		return WireSessionSnapshot{}, false
	}
	return wireSessionSnapshotFromState(state), true
}

// SessionRouteCapabilities is the secret-free authorization fact for App
// Model Profile presentation and active-Session actions. Derived only from the
// route table — never from agent command/name heuristics. Does not expose
// gateway URL, credential env/value, route id, or history.
type SessionRouteCapabilities struct {
	Managed      bool // Session has a managed Model Profile binding
	ActiveSwitch bool // current binding admits ActivateSession (hot switch)
}

// SessionRouteCapabilities reports whether sessionID currently has a managed
// Model Profile binding and whether that binding supports active switching.
// Provisional pending:* IDs are never App-visible managed Sessions.
func (o *Owner) SessionRouteCapabilities(sessionID string) SessionRouteCapabilities {
	if o == nil || o.table == nil {
		return SessionRouteCapabilities{}
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || isProvisionalSessionID(sessionID) {
		return SessionRouteCapabilities{}
	}
	state, ok := o.table.Get(sessionID)
	if !ok {
		return SessionRouteCapabilities{}
	}
	activeSwitch := strings.TrimSpace(state.Binding.RouteID) != "" &&
		normalizeID(state.Binding.RouteProtocol) != "" &&
		ProfileHotSwitchable(state.Binding.Protocol)
	return SessionRouteCapabilities{Managed: true, ActiveSwitch: activeSwitch}
}

func wireSessionSnapshotFromState(state SessionRouteState) WireSessionSnapshot {
	launched := state.Launched.ToWire()
	current := state.Binding.ToWire()
	return WireSessionSnapshot{
		Launched: &launched,
		Current:  &current,
		Ready:    current.CredentialReady,
	}
}

// TransferSession remaps route ownership when a Zen Session id changes.
func (o *Owner) TransferSession(fromID, toID string) (PersistResult, error) {
	if o == nil || !o.started {
		return PersistResult{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.mutateAndPersistLocked(func() error {
		return o.table.RebindSession(fromID, toID)
	})
}

// ResumeLaunch recompiles a secret-free command/env from the immutable
// RouteBinding snapshot only — never from a later-edited catalog profile.
func (o *Owner) ResumeLaunch(sessionID, baseCommand string) (command string, env map[string]string, found bool, err error) {
	if o == nil || !o.started {
		return "", nil, false, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	state, ok := o.table.Get(sessionID)
	if !ok {
		return "", nil, false, nil
	}
	profile := profileFromBinding(state.Binding)
	if err := ValidateProfile(profile); err != nil {
		return "", nil, false, fmt.Errorf("%w: binding insufficient for resume: %v", ErrInvalid, err)
	}
	baseCommand = strings.TrimSpace(baseCommand)
	if baseCommand == "" {
		baseCommand = state.Binding.ExecutorID
	}
	o.mu.Lock()
	if state.Binding.RouteID != "" && strings.TrimSpace(o.addr) == "" {
		if _, listenErr := o.ensureListenerLocked(true); listenErr != nil && !errors.Is(listenErr, ErrPersistDirSync) {
			o.mu.Unlock()
			return "", nil, false, listenErr
		}
	}
	addr := o.addr
	o.mu.Unlock()
	loopbackURL := ""
	if state.Binding.RouteID != "" {
		switch normalizeID(state.Binding.ExecutorID) {
		case ExecutorCodex:
			loopbackURL, err = LoopbackCodexBaseURL(addr, state.Binding.RouteID)
		case ExecutorClaude:
			loopbackURL, err = LoopbackClaudeRootURL(addr, state.Binding.RouteID)
		default:
			return "", nil, false, fmt.Errorf("%w: %s", ErrUnsupportedExecutor, state.Binding.ExecutorID)
		}
		if err != nil {
			return "", nil, false, err
		}
	}
	launch, err := Compile(baseCommand, profile, CompileOptions{
		LoopbackRouteURL:        loopbackURL,
		CatalogRevision:         state.Binding.CatalogRevision,
		Lookup:                  o.lookup,
		VerifiedProfileContract: contractFromBinding(state.Binding),
	})
	if err != nil {
		return "", nil, false, err
	}
	return launch.Command, launch.Env, true, nil
}

// ResumeEnv returns secret-free loopback env for an existing binding.
func (o *Owner) ResumeEnv(sessionID string) (map[string]string, bool, error) {
	_, env, found, err := o.ResumeLaunch(sessionID, "")
	return env, found, err
}

// ReleaseSession drops route ownership on Session teardown.
func (o *Owner) ReleaseSession(sessionID string) (PersistResult, error) {
	if o == nil || !o.started {
		return PersistResult{Applied: true, Durable: true}, nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	persist, err := o.mutateAndPersistLocked(func() error {
		if err := o.table.Release(sessionID); err != nil && !errors.Is(err, ErrBindingNotFound) {
			return err
		}
		return nil
	})
	if persist.Applied {
		if releaseErr := o.releaseIdleListenerLocked(); releaseErr != nil {
			cleanup := PersistResultFromError(releaseErr)
			persist = PersistResult{Applied: true, Durable: persist.Durable && cleanup.Durable}
			if !cleanup.Applied {
				persist.Durable = false
			}
			err = joinErrors(err, fmt.Errorf("listener cleanup: %w", releaseErr))
			if !listenerMetadataApplied(releaseErr) {
				err = joinErrors(err, ErrLaunchCleanupIncomplete)
			}
		}
	}
	return persist, err
}

// SessionTeardownResult is the coherent kill+release outcome for a Session.
type SessionTeardownResult struct {
	Persist PersistResult
	Err     error
}

// TeardownSession kills a Session then releases its Model Profile route only
// when KillSession returns nil (window gone and any delegated resource cleanup
// completed). A non-nil kill error — including resource-release failure after a
// successful window kill, or ambiguous probe failures — preserves the route and
// is surfaced. probe is consulted only to annotate kill failures; it never
// authorizes release. Applied but non-durable release/listener cleanup is
// surfaced (not silent success).
func TeardownSession(
	sessionID string,
	kill func(string) error,
	probe func(string) (SessionLiveness, error),
	release func(string) (PersistResult, error),
) SessionTeardownResult {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionTeardownResult{Persist: PersistResult{Applied: true, Durable: true}}
	}
	var killErr error
	if kill != nil {
		killErr = kill(sessionID)
	}
	if killErr != nil {
		err := killErr
		if probe != nil {
			live, probeErr := probe(sessionID)
			switch {
			case probeErr != nil:
				err = joinErrors(err, ErrSessionLivenessUnknown)
				err = joinErrors(err, probeErr)
			case live == SessionLivenessUnknown:
				err = joinErrors(err, ErrSessionLivenessUnknown)
			case live == SessionLivenessPresent:
				err = joinErrors(err, ErrSessionStillLive)
				// Absent: still do not release — kill/resource cleanup incomplete.
			}
		}
		return SessionTeardownResult{
			Persist: PersistResult{Applied: false, Durable: false},
			Err:     err,
		}
	}
	if release == nil {
		return SessionTeardownResult{Persist: PersistResult{Applied: true, Durable: true}}
	}
	persist, releaseErr := release(sessionID)
	var err error
	if releaseErr != nil {
		err = joinErrors(err, releaseErr)
	}
	if !persist.Applied {
		err = joinErrors(err, fmt.Errorf("%w: release session %s", ErrLaunchCleanupIncomplete, sessionID))
		return SessionTeardownResult{Persist: persist, Err: err}
	}
	if !persist.Durable {
		err = joinErrors(err, ErrPersistDirSync)
	}
	if err != nil {
		return SessionTeardownResult{Persist: persist, Err: err}
	}
	return SessionTeardownResult{Persist: persist}
}

// TeardownSession releases via Owner.ReleaseSession after the kill rule.
func (o *Owner) TeardownSession(sessionID string, kill func(string) error, probe func(string) (SessionLiveness, error)) SessionTeardownResult {
	if o == nil {
		return TeardownSession(sessionID, kill, probe, nil)
	}
	return TeardownSession(sessionID, kill, probe, o.ReleaseSession)
}

func joinErrors(base, extra error) error {
	return errors.Join(base, extra)
}

// LaunchCleanupResult is the typed compensation outcome after a failed
// watcher create or not-applied CommitLaunch.
type LaunchCleanupResult struct {
	Persist PersistResult
	Err     error
}

// LaunchRouteOwner is the Abort/Release surface used by failed-launch compensation.
// *Owner implements it; callers may pass any route lifecycle that owns those ops.
type LaunchRouteOwner interface {
	AbortLaunch(provisionalID string) (PersistResult, error)
	ReleaseSession(sessionID string) (PersistResult, error)
}

// CleanupFailedLaunch compensates a failed create/commit/attach path.
// It kills the Session (or requires KillSession to report idempotent missing)
// before AbortLaunch/ReleaseSession. A non-missing kill error, delegated
// resource cleanup failure, or ambiguous liveness probe preserves the exact
// provisional and/or committed route and surfaces retryable cleanup state.
func CleanupFailedLaunch(
	owner LaunchRouteOwner,
	provisionalID, agentID string,
	kill func(string) error,
	probe func(string) (SessionLiveness, error),
) LaunchCleanupResult {
	persist := PersistResult{Applied: true, Durable: true}
	var err error
	provisionalID = strings.TrimSpace(provisionalID)
	agentID = strings.TrimSpace(agentID)

	if agentID != "" && kill != nil {
		if killErr := kill(agentID); killErr != nil {
			err = joinErrors(killErr, fmt.Errorf("%w: kill session %s", ErrLaunchCleanupIncomplete, agentID))
			if probe != nil {
				live, probeErr := probe(agentID)
				switch {
				case probeErr != nil:
					err = joinErrors(err, ErrSessionLivenessUnknown)
					err = joinErrors(err, probeErr)
				case live == SessionLivenessUnknown:
					err = joinErrors(err, ErrSessionLivenessUnknown)
				case live == SessionLivenessPresent:
					err = joinErrors(err, ErrSessionStillLive)
				}
			}
			return LaunchCleanupResult{
				Persist: PersistResult{Applied: false, Durable: false},
				Err:     err,
			}
		}
	}

	if owner == nil {
		return LaunchCleanupResult{Persist: persist, Err: err}
	}
	if provisionalID != "" {
		abortPersist, abortErr := owner.AbortLaunch(provisionalID)
		if abortErr != nil {
			err = joinErrors(err, abortErr)
		}
		if !abortPersist.Applied {
			persist = PersistResult{Applied: false, Durable: false}
			// AbortLaunch already joins ErrLaunchCleanupIncomplete when it
			// returns a non-nil error; only add it when Applied=false with nil err.
			if abortErr == nil && !errors.Is(err, ErrLaunchCleanupIncomplete) {
				err = joinErrors(err, fmt.Errorf("%w: abort provisional %s", ErrLaunchCleanupIncomplete, provisionalID))
			}
		} else {
			persist = combinePersistResults(persist, abortPersist)
		}
	}
	if agentID != "" {
		releasePersist, releaseErr := owner.ReleaseSession(agentID)
		if releaseErr != nil {
			err = joinErrors(err, releaseErr)
		}
		if !releasePersist.Applied {
			persist = PersistResult{Applied: false, Durable: false}
			if releaseErr == nil && !errors.Is(err, ErrLaunchCleanupIncomplete) {
				err = joinErrors(err, fmt.Errorf("%w: release session %s", ErrLaunchCleanupIncomplete, agentID))
			}
		} else {
			persist = combinePersistResults(persist, releasePersist)
		}
	}
	return LaunchCleanupResult{Persist: persist, Err: err}
}

// ControlErrorCode maps typed package errors to stable wire codes.
func ControlErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNotFound):
		return CodeProfileNotFound
	case errors.Is(err, ErrConflict):
		return CodeProfileConflict
	case errors.Is(err, ErrProfileInUse):
		return CodeProfileInUse
	case errors.Is(err, ErrDuplicateID):
		return CodeProfileConflict
	case errors.Is(err, ErrInvalid):
		return CodeProfileInvalid
	case errors.Is(err, ErrUnsupportedExecutor), errors.Is(err, ErrUnsupportedProtocol):
		return CodeProfileUnsupported
	case errors.Is(err, ErrCredentialNotReady):
		return CodeCredentialNotReady
	case errors.Is(err, ErrCredentialStoreUnavailable):
		return CodeCredentialStoreUnavailable
	case errors.Is(err, ErrCredentialStoreFailed):
		return CodeCredentialStoreFailed
	case errors.Is(err, ErrSecureTransportRequired):
		return CodeSecureTransportRequired
	case errors.Is(err, ErrBindingNotFound):
		return CodeBindingNotFound
	case errors.Is(err, ErrBindingConflict):
		return CodeBindingConflict
	case errors.Is(err, ErrBindingBusy):
		return CodeBindingBusy
	case errors.Is(err, ErrBindingNotRouted):
		return CodeBindingNotRouted
	case errors.Is(err, ErrBindingProtocolChange),
		errors.Is(err, ErrBindingContractChange),
		errors.Is(err, ErrBindingHistoryDomain),
		errors.Is(err, ErrBindingHistoryState),
		errors.Is(err, ErrBindingExecutorMismatch),
		errors.Is(err, ErrEnvelopeIncompatible):
		return CodeBindingIncompatible
	case errors.Is(err, ErrContractUnverified):
		return CodeContractUnverified
	case errors.Is(err, ErrRouteSnapshotInvalid):
		return CodeRouteSnapshotInvalid
	case errors.Is(err, ErrListenerFailed), errors.Is(err, ErrLaunchCleanupIncomplete):
		return CodeRouteListenerFailed
	case errors.Is(err, ErrSessionStillLive), errors.Is(err, ErrSessionLivenessUnknown):
		return "close_failed"
	case errors.Is(err, ErrPersistDirSync):
		return CodeRouteSnapshotInvalid
	default:
		return CodeProfilesUnavailable
	}
}

func shouldBypassProfiles(executorID, profileID, baseCommand string) bool {
	if strings.TrimSpace(profileID) != "" {
		return false
	}
	cmd := strings.TrimSpace(baseCommand)
	if cmd == "" {
		return true
	}
	if looksLikeRawOrCustomCommand(cmd) {
		return true
	}
	if executorID == "" || !SupportsExecutor(executorID) {
		return true
	}
	return false
}

func looksLikeRawOrCustomCommand(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	if strings.ContainsAny(lower, ";|&") || strings.Contains(lower, "&&") || strings.Contains(lower, "||") {
		return true
	}
	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return true
	}
	bin := fields[0]
	if i := strings.LastIndex(bin, "/"); i >= 0 {
		bin = bin[i+1:]
	}
	switch bin {
	case ExecutorCodex, ExecutorClaude:
		return false
	case "env":
		for _, f := range fields[1:] {
			if !strings.Contains(f, "=") {
				name := f
				if j := strings.LastIndex(name, "/"); j >= 0 {
					name = name[j+1:]
				}
				return name != ExecutorCodex && name != ExecutorClaude
			}
		}
		return true
	default:
		return true
	}
}

func inferExecutorFromCommand(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	for _, f := range fields {
		if strings.Contains(f, "=") {
			continue
		}
		name := f
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		switch normalizeID(name) {
		case ExecutorCodex:
			return ExecutorCodex
		case ExecutorClaude:
			return ExecutorClaude
		default:
			if normalizeID(name) == "env" {
				continue
			}
			return ""
		}
	}
	return ""
}

func contractFromBinding(b RouteBinding) VerifiedProfileContract {
	return VerifiedProfileContract{
		Provenance:       b.ClientModelProvenance,
		ClientModelID:    b.ClientModel,
		UpstreamModelID:  b.UpstreamModel,
		ExecutorID:       b.ExecutorID,
		Protocol:         b.Protocol,
		RouteProtocol:    b.RouteProtocol,
		ProviderID:       b.ProviderID,
		ClientEnvelope:   b.ClientEnvelope,
		UpstreamEnvelope: b.UpstreamEnvelope,
		HistoryDomain:    b.HistoryDomain,
	}
}
