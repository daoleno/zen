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
		return projectModelEntries(trusted, manual, nil, nil, false), nil
	}
	if !forceDiscover {
		o.mu.Lock()
		cache := o.discovery
		o.mu.Unlock()
		if cache != nil {
			if e, ok := cache.get(profile.ID); ok {
				return projectModelEntries(trusted, manual, e.IDs, e.LastGood, e.Err == "" && len(e.IDs) > 0), nil
			}
		}
		return projectModelEntries(trusted, manual, nil, nil, false), nil
	}
	entries, err := o.DiscoverProviderModels(profile.ID, true)
	if err != nil && len(entries) == 0 {
		return projectModelEntries(trusted, manual, nil, nil, false), nil
	}
	return entries, nil
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

// UpsertProviderConnection creates/updates a connection via public input.
func (o *Owner) UpsertProviderConnection(in ProviderConnectionInput, revision int64, create bool) (ProviderCatalogProjection, error) {
	profile, err := CompileProviderConnection(in)
	if err != nil {
		return ProviderCatalogProjection{}, err
	}
	if _, err := o.UpsertProfile(profile, revision, create); err != nil {
		empty, _ := o.ProjectProviders()
		return empty, err
	}
	return o.ProjectProviders()
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

// SetProviderDefault sets the future-launch default connection + model for a
// client in one catalog mutation and one atomic durable write. The exact
// (connection, client, model) target is compiled/validated before persistence.
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
			if spec, ok := lookupPreset(inferPresetID(raw)); ok {
				modelID = spec.DefaultModel[executorFromClient(client)]
			}
		}
		if modelID == "" {
			entries, _ := o.modelsForConnection(raw, false)
			for _, entry := range entries {
				if entry.Available {
					modelID = entry.ID
					break
				}
			}
		}
		if _, err := CompileConnectionTarget(raw, client, modelID); err != nil {
			return ProviderCatalogProjection{}, err
		}
	}
	if _, err := o.store.SetClientDefault(client, connectionID, modelID, revision); err != nil {
		empty, _ := o.ProjectProviders()
		return empty, err
	}
	return o.ProjectProviders()
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
