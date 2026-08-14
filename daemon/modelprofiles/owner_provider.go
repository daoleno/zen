package modelprofiles

import (
	"errors"
	"fmt"
	"strings"
)

// ProjectProviders returns the Provider-first Settings projection.
func (o *Owner) ProjectProviders() (ProviderCatalogProjection, error) {
	if o == nil || !o.started {
		return ProviderCatalogProjection{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	proj := o.ProjectCatalog()
	out := ProviderCatalogProjection{
		Revision:    proj.Catalog.Revision,
		Connections: make([]ProviderConnection, 0, len(proj.Views)),
		Defaults:    map[string]ProviderDefault{},
		Presets:     ListProviderPresets(),
		Models:      map[string][]ProviderModelEntry{},
	}
	for _, view := range proj.Views {
		conn := providerConnectionFromProfile(view.Profile, o.connectionReady(view.Profile))
		out.Connections = append(out.Connections, conn)
		entries, _ := o.modelsForConnection(view.Profile, false)
		out.Models[conn.ID] = entries
	}
	for key, profileID := range proj.Catalog.Defaults {
		profileID = normalizeID(profileID)
		client := clientFromExecutor(key)
		if _, exists := out.Defaults[client]; exists && normalizeID(key) == executorFromClient(client) {
			// Prefer client-keyed entries when both exist.
			continue
		}
		modelID := o.defaultModelLocked(client)
		if modelID == "" {
			for _, view := range proj.Views {
				if view.ID != profileID {
					continue
				}
				if !isAccountConnection(view.Profile) {
					modelID = view.Model
				}
				break
			}
		}
		out.Defaults[client] = ProviderDefault{
			ConnectionID: profileID,
			ModelID:      modelID,
		}
	}
	return out, nil
}

func (o *Owner) defaultModelLocked(client string) string {
	if o == nil || o.store == nil {
		return ""
	}
	return o.store.DefaultModelID(client)
}

func (o *Owner) connectionReady(profile Profile) bool {
	if o == nil {
		return connectionCredentialReady(profile, nil, nil)
	}
	return connectionCredentialReady(profile, o.creds, o.lookup)
}

func connectionCredentialReady(profile Profile, store CredentialStore, lookup func(string) (string, bool)) bool {
	if isAccountConnection(profile) {
		return providerCredentialReady(profile.ID, profile.CredentialEnv, store, lookup)
	}
	if providerCredentialReady(profile.ID, profile.CredentialEnv, store, lookup) {
		return true
	}
	return AuthReady(profile.AuthMode, profile.CredentialEnv, lookup)
}

func (o *Owner) modelsForConnection(profile Profile, forceDiscover bool) ([]ProviderModelEntry, error) {
	presetID := inferPresetID(profile)
	trusted := presetTrustedModels(presetID)
	manual := ""
	if accountLooksAdvanced(profile, presetID) || normalizeID(presetID) == ProviderPresetCustom {
		manual = normalizeSpace(profile.Model)
	}
	if o == nil {
		return projectModelEntries(trusted, manual, nil, nil, nil, false), nil
	}
	if !forceDiscover {
		o.mu.Lock()
		cache := o.discovery
		o.mu.Unlock()
		if cache != nil {
			if e, ok := cache.get(profile.ID); ok {
				return projectModelEntries(trusted, manual, e.IDs, e.LastGood, e.Disabled, e.Err == "" && len(e.IDs) > 0), nil
			}
		}
		return projectModelEntries(trusted, manual, nil, nil, nil, false), nil
	}
	entries, err := o.DiscoverProviderModels(profile.ID, true)
	if err != nil && len(entries) == 0 {
		return projectModelEntries(trusted, manual, nil, nil, nil, false), nil
	}
	return entries, nil
}

// supportedModelEntriesLocked projects the synced support allowlist of a
// connection (available entries only, in catalog order). Caller must hold
// o.mu when reading through an Owner; nil-safe for the zero Owner. Returns nil
// when no synced catalog exists yet (never syncs, never reaches upstream).
func (o *Owner) supportedModelEntriesLocked(profile Profile) []ProviderModelEntry {
	entries, synced := o.syncedModelCatalogLocked(profile)
	if !synced {
		return nil
	}
	out := make([]ProviderModelEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Available {
			out = append(out, entry)
		}
	}
	return out
}

// syncedModelCatalogLocked projects the full synced catalog of a connection
// (with per-model availability) and reports whether a discovery cache entry
// exists. Never syncs and never reaches upstream.
func (o *Owner) syncedModelCatalogLocked(profile Profile) ([]ProviderModelEntry, bool) {
	presetID := inferPresetID(profile)
	trusted := presetTrustedModels(presetID)
	manual := ""
	if accountLooksAdvanced(profile, presetID) || normalizeID(presetID) == ProviderPresetCustom {
		manual = normalizeSpace(profile.Model)
	}
	if o == nil || o.discovery == nil {
		return nil, false
	}
	e, ok := o.discovery.get(profile.ID)
	if !ok || len(e.IDs) == 0 {
		return nil, false
	}
	return projectModelEntries(trusted, manual, e.IDs, e.LastGood, e.Disabled, e.Err == ""), true
}

// resolveSupportedLaunchModelLocked applies the deterministic launch-model rule
// for Provider account connections: the client-selected model is used while it
// stays in the synced support allowlist; an unsupported or missing selection
// falls back to the first supported model (catalog order); a synced allowlist
// with every model disabled fails closed. Legacy executor-scoped profiles own
// an explicit model and are untouched. Returns (profile, false) when the
// caller must fail closed.
func (o *Owner) resolveSupportedLaunchModelLocked(profile Profile) (Profile, bool) {
	// The ephemeral launch target loses account scope during compile; the
	// durable connection record decides whether the allowlist applies.
	durable, err := o.store.Get(profile.ID)
	if err != nil || !isAccountConnection(durable) {
		return profile, true
	}
	entries, synced := o.syncedModelCatalogLocked(durable)
	candidate := ""
	if !profile.ModelPlaceholder {
		candidate = normalizeSpace(profile.Model)
	}
	firstSupported := ""
	supportedSet := map[string]struct{}{}
	for _, entry := range entries {
		if !entry.Available {
			continue
		}
		supportedSet[entry.ID] = struct{}{}
		if firstSupported == "" {
			firstSupported = entry.ID
		}
	}

	if candidate != "" {
		if _, ok := supportedSet[candidate]; ok || !synced {
			// The client's explicit selection is still supported, or no synced
			// allowlist exists to contradict it: use it unchanged.
			return profile, true
		}
		// The selected model is no longer supported: deterministic visible
		// fallback to the first supported model.
		if firstSupported != "" {
			profile.Model = firstSupported
			profile.ModelPlaceholder = false
			return profile, true
		}
		// A synced allowlist exists but every model is disabled: fail closed
		// rather than route a model the client turned off.
		return profile, false
	}

	// No client selection: deterministic fallback from the allowlist.
	if firstSupported != "" {
		profile.Model = firstSupported
		profile.ModelPlaceholder = false
		return profile, true
	}
	return profile, false
}

func providerConnectionFromProfile(profile Profile, ready bool) ProviderConnection {
	presetID := inferPresetID(profile)
	advanced := normalizeID(presetID) == ProviderPresetCustom || accountLooksAdvanced(profile, presetID)
	clients := []string{}
	if isAccountConnection(profile) {
		clients = []string{clientFromExecutor(profile.Client)}
	} else {
		clients = []string{clientFromExecutor(profile.ExecutorID)}
	}
	conn := ProviderConnection{
		ID:              profile.ID,
		Name:            profile.Name,
		PresetID:        presetID,
		Clients:         clients,
		CredentialReady: ready,
		Advanced:        advanced,
	}
	if advanced {
		conn.BaseURL = profile.BaseURL
		conn.ManualModelID = normalizeSpace(profile.Model)
	}
	return conn
}

// UpsertProviderConnection creates/updates a connection via public input and
// optionally writes its API key in the same Owner.mu transaction. An empty
// apiKey preserves the existing stored secret; a non-empty value replaces it
// as part of the edit. Validation happens before any write, and a credential
// write failure rolls the catalog back, so a failed edit never leaves a
// partial name/URL/key state behind.
func (o *Owner) UpsertProviderConnection(in ProviderConnectionInput, apiKey string, revision int64, create bool) (ProviderCatalogProjection, error) {
	if o == nil || !o.started || o.store == nil {
		return ProviderCatalogProjection{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	// Update requires the stable internal id so a rename can never drift into
	// a new connection. Fail before any write.
	if !create && normalizeID(in.ID) == "" {
		return ProviderCatalogProjection{}, fmt.Errorf("%w: connection_id is required for update", ErrInvalid)
	}
	profile, err := CompileProviderConnection(in)
	if err != nil {
		return ProviderCatalogProjection{}, err
	}
	apiKey = strings.TrimSpace(apiKey)

	o.mu.Lock()
	applyErr := func() error {
		var previous Profile
		var err error
		if !create {
			previous, err = o.store.Get(profile.ID)
			if err != nil {
				return err
			}
		}
		// Fail before any write when the edit includes a key the store cannot take.
		if apiKey != "" && (o.creds == nil || !o.creds.Available()) {
			return ErrCredentialStoreUnavailable
		}

		if create {
			_, err = o.store.Create(profile, revision)
		} else {
			_, err = o.store.Update(profile, revision)
		}
		if err != nil && !errors.Is(err, ErrPersistDirSync) {
			return err
		}
		if apiKey != "" {
			if serr := o.creds.Set(CredentialRefFor(profile.ID), apiKey); serr != nil {
				// Roll the catalog back so the edit applied nothing: Delete the
				// fresh connection or restore the previous row. A rollback
				// persist failure leaves the connection changed but key-less
				// (recoverable via Edit; no orphaned secret exists).
				if create {
					if _, rerr := o.store.Delete(profile.ID, o.store.Revision()); rerr != nil && !errors.Is(rerr, ErrPersistDirSync) {
						return errors.Join(serr, rerr)
					}
				} else {
					if _, rerr := o.store.Update(previous, o.store.Revision()); rerr != nil && !errors.Is(rerr, ErrPersistDirSync) {
						return errors.Join(serr, rerr)
					}
				}
				return serr
			}
		}
		// ErrPersistDirSync means the rename committed: applied-with-warning.
		return err
	}()
	o.mu.Unlock()
	if applyErr != nil {
		// ErrPersistDirSync means the write committed: return the live catalog
		// so the client sees the applied state with the durability warning.
		proj, perr := o.ProjectProviders()
		if perr != nil {
			return ProviderCatalogProjection{}, errors.Join(applyErr, perr)
		}
		return proj, applyErr
	}
	proj, perr := o.ProjectProviders()
	if perr != nil {
		return ProviderCatalogProjection{}, perr
	}
	return proj, nil
}

// DeleteProviderConnection removes a connection after non-orphaning credential
// cleanup. Under Owner.mu: preflight revision/existence/defaults/in-use (no
// mutation) → delete private credential entry → commit catalog delete. Credential-delete
// failure leaves catalog/revision/defaults/key unchanged (not-applied). Catalog
// persistence failure after successful key deletion leaves the connection
// present but credential-not-ready (or env-ready). Dir-sync warnings remain
// applied-with-warning.
func (o *Owner) DeleteProviderConnection(id string, revision int64) (ProviderCatalogProjection, error) {
	if o == nil || !o.started || o.store == nil {
		return ProviderCatalogProjection{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	id = normalizeID(id)

	// Hold Owner.mu for the entire preflight → credential → catalog commit so
	// PrepareLaunch/Activate cannot bind the connection mid-delete. Project
	// only after unlock (ProjectCatalog also takes Owner.mu).
	o.mu.Lock()
	var delErr error
	if err := o.store.PreflightDelete(id, revision); err != nil {
		delErr = err
	} else if users := o.table.SessionsUsingProfile(id); len(users) > 0 {
		delErr = fmt.Errorf("%w: profile %s is bound to %d session(s)", ErrProfileInUse, id, len(users))
	} else {
		if o.creds != nil {
			if err := o.creds.Delete(CredentialRefFor(id)); err != nil {
				delErr = err
			}
		}
		if delErr == nil {
			_, delErr = o.store.Delete(id, revision)
		}
	}
	o.mu.Unlock()

	proj, _ := o.ProjectProviders()
	if delErr != nil && !errors.Is(delErr, ErrPersistDirSync) {
		// Credential-delete failure: catalog/revision/defaults/key unchanged.
		// Catalog persist failure after key delete: connection retained,
		// credential-not-ready (or env-ready) — recoverable, no orphan.
		return proj, delErr
	}
	return proj, delErr
}

// CodexRoutedDefault reports whether the effective Codex connection is a
// routed Provider/API-key connection rather than the direct official
// ChatGPT/Codex login. Official Codex subscription usage is meaningful only
// for the direct login, so Stats suppression keys off this authoritative fact.
func (o *Owner) CodexRoutedDefault() bool {
	if o == nil || o.store == nil {
		return false
	}
	connID, _ := o.store.ClientDefault(ClientCodex)
	return normalizeID(connID) != ""
}

// SetProviderDefault sets the future-launch default connection for a client in
// one catalog mutation and one atomic durable write. The gateway never owns a
// model: modelID is the client's explicit selection (chosen from the synced
// support allowlist) and is never fabricated from a preset or catalog. An empty
// modelID preserves the existing client-selected model when the same connection
// stays default, and clears it when the connection changes.
func (o *Owner) SetProviderDefault(clientOrExecutor, connectionID, modelID string, revision int64) (ProviderCatalogProjection, error) {
	client := clientFromExecutor(clientOrExecutor)
	connectionID = normalizeID(connectionID)
	modelID = normalizeSpace(modelID)
	if connectionID != "" {
		raw, err := o.GetProfile(connectionID)
		if err != nil {
			return ProviderCatalogProjection{}, err
		}
		if modelID == "" {
			// Keep the client's recorded selection when the same connection
			// remains the default; a new default connection starts without a
			// fabricated model until the client chooses one.
			currentConn, currentModel := o.store.ClientDefault(client)
			if normalizeID(currentConn) == connectionID {
				modelID = normalizeSpace(currentModel)
			}
		}
		if modelID != "" {
			target, err := CompileConnectionTarget(raw, client, modelID)
			if err != nil {
				return ProviderCatalogProjection{}, err
			}
			// Fail closed: never persist a client default whose model is only a
			// compile probe placeholder (connection with no explicit model). The
			// launch path resolves a deterministic supported model instead.
			if target.ModelPlaceholder {
				return ProviderCatalogProjection{}, ErrUpstreamModelRequired
			}
		}
	}
	if _, err := o.store.SetClientDefault(client, connectionID, modelID, revision); err != nil {
		empty, _ := o.ProjectProviders()
		return empty, err
	}
	return o.ProjectProviders()
}

// SetProviderModelSupport persists the client-side model support allowlist of
// one connection: every discovered model is supported unless the client
// explicitly disabled it. enabledIDs is the full set of models the client
// wants to expose; the durable representation is the complement (disabled ids)
// so a refresh never re-enables an explicitly disabled model while genuinely
// new discovered models default enabled. The catalog revision is untouched;
// durability comes from the discovery-cache file.
func (o *Owner) SetProviderModelSupport(connectionID string, enabledIDs []string) (ProviderCatalogProjection, PersistResult, error) {
	if o == nil || !o.started || o.store == nil {
		return ProviderCatalogProjection{}, PersistResult{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	connectionID = normalizeID(connectionID)
	profile, err := o.GetProfile(connectionID)
	if err != nil {
		return ProviderCatalogProjection{}, PersistResult{}, err
	}
	presetID := inferPresetID(profile)
	trusted := presetTrustedModels(presetID)
	manual := ""
	if accountLooksAdvanced(profile, presetID) || normalizeID(presetID) == ProviderPresetCustom {
		manual = normalizeSpace(profile.Model)
	}

	o.mu.Lock()
	if o.discovery == nil {
		o.discovery = newModelDiscoveryCache()
		if o.discoveryPath != "" {
			if lerr := o.discovery.load(o.discoveryPath); lerr != nil {
				o.discoveryLoadWarning = lerr
			}
		}
	}
	e, ok := o.discovery.get(connectionID)
	if !ok || len(e.IDs) == 0 {
		o.mu.Unlock()
		return ProviderCatalogProjection{}, PersistResult{}, fmt.Errorf("%w: sync models before choosing support", ErrDiscoveryCacheInvalid)
	}

	enabledSet := map[string]struct{}{}
	for _, id := range enabledIDs {
		id = normalizeSpace(id)
		if id != "" {
			enabledSet[id] = struct{}{}
		}
	}
	// The reference catalog is the full projected id set (trusted + discovered
	// + LKG + manual). Disabled = reference minus the client's enabled set.
	reference := projectModelEntries(trusted, manual, e.IDs, e.LastGood, nil, e.Err == "")
	disabled := make([]string, 0, len(reference))
	for _, entry := range reference {
		if _, on := enabledSet[entry.ID]; !on {
			disabled = append(disabled, entry.ID)
		}
	}
	o.discovery.setDisabled(connectionID, disabled)

	persist := PersistResult{Applied: true, Durable: true}
	if o.discoveryPath != "" {
		if serr := o.discovery.save(o.discoveryPath); serr != nil {
			persist = PersistResultFromError(serr)
			// Keep memory aligned with the intended allowlist even when the
			// rename-sync is uncertain (applied-with-warning semantics).
		}
	}
	o.mu.Unlock()
	proj, _ := o.ProjectProviders()
	return proj, persist, nil
}

// SetCredentialStore installs Zen's private credential store (or a test fake).
// Serialized with Set/Clear/Delete under Owner.mu so the store pointer cannot
// race a mid-flight credential mutation.
func (o *Owner) SetCredentialStore(store CredentialStore) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.creds = store
	if o.router != nil {
		o.router.creds = store
	}
	if o.table != nil {
		o.table.SetCredentials(store)
	}
}

// SetProviderCredential writes a secret to the credential store for a
// connection. Public replies expose readiness only — never the secret.
// Connection validation and credential persistence run under Owner.mu with
// DeleteProviderConnection / SetCredentialStore so a concurrent delete cannot
// leave an orphan key for a removed connection.
func (o *Owner) SetProviderCredential(connectionID, secret string) (ProviderCredentialResult, error) {
	if o == nil || !o.started || o.store == nil {
		return ProviderCredentialResult{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	connectionID = normalizeID(connectionID)
	secret = strings.TrimSpace(secret)
	if connectionID == "" || secret == "" {
		return ProviderCredentialResult{}, fmt.Errorf("%w: connection_id and credential are required", ErrInvalid)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	profile, err := o.store.Get(connectionID)
	if err != nil {
		return ProviderCredentialResult{}, err
	}
	creds := o.creds
	if creds == nil || !creds.Available() {
		return ProviderCredentialResult{}, ErrCredentialStoreUnavailable
	}
	if err := creds.Set(CredentialRefFor(connectionID), secret); err != nil {
		return ProviderCredentialResult{}, err
	}
	ready := connectionCredentialReady(profile, creds, o.lookup)
	return ProviderCredentialResult{
		ConnectionID:       connectionID,
		CredentialReady:    ready,
		PersistenceOutcome: "applied",
		PersistenceDurable: true,
	}, nil
}

// ClearProviderCredential removes the private secret for a connection.
// Serialized under Owner.mu with DeleteProviderConnection / SetCredentialStore.
func (o *Owner) ClearProviderCredential(connectionID string) (ProviderCredentialResult, error) {
	if o == nil || !o.started || o.store == nil {
		return ProviderCredentialResult{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	connectionID = normalizeID(connectionID)
	if connectionID == "" {
		return ProviderCredentialResult{}, fmt.Errorf("%w: connection_id is required", ErrInvalid)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	profile, err := o.store.Get(connectionID)
	if err != nil {
		return ProviderCredentialResult{}, err
	}
	creds := o.creds
	if creds == nil || !creds.Available() {
		return ProviderCredentialResult{}, ErrCredentialStoreUnavailable
	}
	if err := creds.Delete(CredentialRefFor(connectionID)); err != nil {
		return ProviderCredentialResult{}, err
	}
	ready := connectionCredentialReady(profile, creds, o.lookup)
	return ProviderCredentialResult{
		ConnectionID:       connectionID,
		CredentialReady:    ready,
		PersistenceOutcome: "applied",
		PersistenceDurable: true,
	}, nil
}

// SessionProviderSelection returns the Plus-menu projection for a Session.
func (o *Owner) SessionProviderSelection(sessionID string) (ProviderSessionSelection, bool) {
	snap, ok := o.SessionSnapshot(sessionID)
	if !ok || snap.Current == nil {
		return ProviderSessionSelection{}, false
	}
	c := snap.Current
	return ProviderSessionSelection{
		SessionID:       c.SessionID,
		Client:          c.Client,
		ConnectionID:    c.ConnectionID,
		ConnectionName:  c.ConnectionName,
		ProviderLabel:   c.ProviderLabel,
		ModelID:         c.ModelID,
		CredentialReady: c.CredentialReady,
		HotSwitchable:   c.HotSwitchable,
	}, true
}

// ActivateSessionProvider atomically activates Provider+model for the next
// admitted request on the existing routed Session (same RouteID). The model
// override is ephemeral: catalog connection, defaults, revision, and other
// Sessions are never mutated. Generation CAS is internal.
func (o *Owner) ActivateSessionProvider(sessionID, connectionID, modelID string) (SessionRouteState, WireSessionSnapshot, PersistResult, error) {
	if o == nil || !o.started {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	sessionID = strings.TrimSpace(sessionID)
	connectionID = normalizeID(connectionID)
	modelID = normalizeSpace(modelID)

	state, ok := o.table.Get(sessionID)
	if !ok {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, fmt.Errorf("%w: %s", ErrBindingNotFound, sessionID)
	}
	raw, err := o.GetProfile(connectionID)
	if err != nil {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, err
	}
	beforeRev := o.Catalog().Revision
	beforeModel := normalizeSpace(raw.Model)

	target, err := CompileConnectionTarget(raw, state.Binding.ExecutorID, modelID)
	if err != nil {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, err
	}
	next, snap, persist, err := o.activateCompiledProfile(sessionID, target, state.Generation)
	if !persist.Applied {
		return SessionRouteState{}, WireSessionSnapshot{}, persist, err
	}
	// Fail closed if Plus-menu activation mutated the catalog (regression guard).
	if o.Catalog().Revision != beforeRev {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, fmt.Errorf("%w: activate must not mutate provider catalog", ErrInvalid)
	}
	if got, gerr := o.GetProfile(connectionID); gerr == nil && normalizeSpace(got.Model) != beforeModel {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, fmt.Errorf("%w: activate must not mutate connection model", ErrInvalid)
	}
	return next, snap, persist, err
}
