package modelprofiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		conn.CredentialHint = o.providerCredentialHint(view.Profile)
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

// providerCredentialHint returns the masked hint for the connection's active
// stored secret, or "" when the secret lives outside Zen's private store (env
// fallback) or no secret is stored. The hint is generated daemon-side from the
// active credential ref; secrets never appear on the wire, in logs, or in
// telemetry.
func (o *Owner) providerCredentialHint(profile Profile) string {
	if o == nil || o.creds == nil || !o.creds.Available() {
		return ""
	}
	ref := activeCredentialRef(profile)
	if ref == "" {
		return ""
	}
	val, ok, err := o.creds.Get(ref)
	if err != nil || !ok || strings.TrimSpace(val) == "" {
		return ""
	}
	return credentialHintFor(val)
}

func connectionCredentialReady(profile Profile, store CredentialStore, lookup func(string) (string, bool)) bool {
	if isAccountConnection(profile) {
		return providerCredentialReady(profile, store, lookup)
	}
	if providerCredentialReady(profile, store, lookup) {
		return true
	}
	return AuthReady(profile.AuthMode, profile.CredentialEnv, lookup)
}

func (o *Owner) modelsForConnection(profile Profile, forceDiscover bool) ([]ProviderModelEntry, error) {
	if o == nil {
		return nil, nil
	}
	if !forceDiscover {
		o.mu.Lock()
		cache := o.discovery
		o.mu.Unlock()
		if cache != nil {
			if e, ok := cache.get(profile.ID); ok {
				return o.projectConnectionModels(profile, e), nil
			}
		}
		return o.projectConnectionModels(profile, discoveryEntry{}), nil
	}
	entries, err := o.DiscoverProviderModels(profile.ID, true)
	if err != nil && len(entries) == 0 {
		return o.projectConnectionModels(profile, discoveryEntry{}), nil
	}
	return entries, nil
}

func (o *Owner) projectConnectionModels(profile Profile, discovered discoveryEntry) []ProviderModelEntry {
	executorID := normalizeID(profile.ExecutorID)
	if isAccountConnection(profile) {
		executorID = executorFromClient(profile.Client)
	}
	localIDs, localMetadata, _ := loadInstalledCodexModelCatalog()
	ids := discovered.IDs
	metadata := cloneModelMetadataMap(discovered.Metadata)
	source := ModelSourceDiscovered
	if discovered.Err != "" || len(ids) == 0 {
		if executorID == ExecutorCodex {
			ids = localIDs
			metadata = localMetadata
			source = ModelSourceCodexCache
		} else {
			ids = discovered.LastGood
			metadata = cloneModelMetadataMap(discovered.LastGoodMeta)
			source = ModelSourceLKG
		}
	} else if executorID == ExecutorCodex {
		// Legacy cache entries predate the metadata field and deserialize with a
		// nil Metadata map. Never write into a nil map.
		if metadata == nil {
			metadata = map[string]modelPresentationMetadata{}
		}
		for _, id := range ids {
			metadata[id] = mergeModelPresentationMetadata(metadata[id], localMetadata[id])
			// The daemon-pinned catalog is the final metadata authority for
			// managed Codex: the installed CLI cache is volatile (rewritten by
			// every codex run) and must never decide whether a model identity
			// has daemon-known display/effort metadata.
			metadata[id] = mergeModelPresentationMetadata(metadata[id], pinnedCodexPresentationMetadata()[id])
		}
	}
	required := o.connectionRequiredModels(profile)
	return projectCatalogModelEntries(ids, metadata, source, discovered.Disabled, required, localMetadata, executorID == ExecutorCodex)
}

func (o *Owner) connectionRequiredModels(profile Profile) []string {
	seen := map[string]struct{}{}
	out := []string{}
	add := func(model string) {
		model = normalizeSpace(model)
		if err := ValidateModelID(model); err != nil {
			return
		}
		if _, ok := seen[model]; ok {
			return
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	if !profile.ModelPlaceholder {
		add(profile.Model)
	}
	if o != nil && o.store != nil {
		for _, client := range []string{ClientCodex, ClientClaude} {
			connectionID, modelID := o.store.ClientDefault(client)
			if normalizeID(connectionID) == normalizeID(profile.ID) {
				add(modelID)
			}
		}
	}
	if o != nil && o.table != nil {
		for _, state := range o.table.Snapshot() {
			if normalizeID(state.Binding.ProfileID) != normalizeID(profile.ID) {
				continue
			}
			add(state.Binding.ClientModel)
		}
	}
	return out
}

func projectCatalogModelEntries(ids []string, metadata map[string]modelPresentationMetadata, source string, disabled, required []string, fallbackMetadata map[string]modelPresentationMetadata, codex bool) []ProviderModelEntry {
	disabledSet := map[string]struct{}{}
	for _, id := range disabled {
		disabledSet[normalizeSpace(id)] = struct{}{}
	}
	seen := map[string]struct{}{}
	out := make([]ProviderModelEntry, 0, len(ids)+len(required))
	add := func(id, entrySource string, forceAvailable bool) {
		id = normalizeSpace(id)
		if err := ValidateModelID(id); err != nil {
			return
		}
		if _, ok := seen[id]; ok {
			if forceAvailable {
				for i := range out {
					if out[i].ID == id {
						out[i].Available = true
					}
				}
			}
			return
		}
		seen[id] = struct{}{}
		available := true
		if _, off := disabledSet[id]; off && !forceAvailable {
			available = false
		}
		entry := ProviderModelEntry{ID: id, Available: available, Source: entrySource}
		if codex {
			modelMetadata := mergeModelPresentationMetadata(metadata[id], fallbackMetadata[id])
			entry.DisplayName = modelMetadata.DisplayName
			entry.ReasoningEffortDefault = modelMetadata.DefaultReasoningLevel
			for _, preset := range modelMetadata.SupportedReasoningLevels {
				entry.ReasoningEfforts = append(entry.ReasoningEfforts, preset.Effort)
			}
			entry.Known = entry.DisplayName != "" || entry.ReasoningEffortDefault != "" || len(entry.ReasoningEfforts) > 0 || modelMetadata.ContextWindow > 0
		}
		out = append(out, entry)
	}
	for _, id := range ids {
		add(id, source, false)
	}
	for _, id := range required {
		add(id, ModelSourceManual, true)
	}
	return out
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
	if o == nil || o.discovery == nil {
		return nil, false
	}
	e, ok := o.discovery.get(profile.ID)
	if !ok || len(e.IDs) == 0 {
		return nil, false
	}
	return o.projectConnectionModels(profile, e), true
}

// CodexModelCatalogPath returns the stable per-connection Codex ModelsResponse
// catalog file path (empty when the owner has no routes path).
func (o *Owner) CodexModelCatalogPath(connectionID string) string {
	if o == nil || o.routes == nil {
		return ""
	}
	dir := filepath.Dir(o.routes.Path())
	return filepath.Join(dir, "codex-model-catalog", normalizeID(connectionID)+".json")
}

// writeCodexModelCatalogLocked writes the deterministic per-connection Codex
// model catalog (ModelsResponse shape): daemon-known metadata for the
// connection's available models (discovery availability ∩ known metadata).
// The launch model is always included when known so the CLI resolves the
// running identity even before discovery syncs. Caller must hold o.mu.
func (o *Owner) writeCodexModelCatalogLocked(profile Profile) error {
	if o == nil || o.routes == nil {
		return nil
	}
	path := o.CodexModelCatalogPath(profile.ID)
	if path == "" {
		return nil
	}
	known := map[string]ProviderModelEntry{}
	if !profile.ModelPlaceholder {
		known[normalizeSpace(profile.Model)] = ProviderModelEntry{ID: normalizeSpace(profile.Model), Available: true, Source: ModelSourceManual}
	}
	if durable, err := o.store.Get(profile.ID); err == nil && isAccountConnection(durable) {
		if entries, synced := o.syncedModelCatalogLocked(durable); synced {
			for _, entry := range entries {
				if !entry.Available {
					continue
				}
				known[normalizeSpace(entry.ID)] = entry
			}
		}
	}
	models := make([]ProviderModelEntry, 0, len(known))
	for _, entry := range known {
		models = append(models, entry)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	raw, err := json.MarshalIndent(CodexModelsResponseForEntries(models), "", "  ")
	if err != nil {
		return fmt.Errorf("%w: encode codex model catalog: %v", ErrInvalid, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("%w: codex model catalog dir: %v", ErrInvalid, err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("%w: write codex model catalog: %v", ErrInvalid, err)
	}
	return nil
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
// optionally rotates its API key as part of the same crash-recoverable edit.
// An empty apiKey preserves the existing stored secret; a non-empty value
// replaces it atomically with the whole Provider edit.
//
// Durability design (versioned credential reference):
//  1. VALIDATE first — any validation failure applies zero writes.
//  2. STAGE — when the edit carries a new key, write it privately under a
//     fresh staged ref (provider:<id>:<token>) that no catalog row references
//     yet. The old secret stays active and routing still resolves the old ref.
//  3. COMMIT — the single atomic catalog write flips Name/Base URL AND the
//     active credential ref together. Before it, router/launch observe the
//     complete old version; after it, the complete new version. A crash can
//     never expose a mixed Name/Base URL/API key state.
//  4. CLEANUP — delete the old ref once nothing references it (a crash here
//     leaves an inactive secret only; the deterministic StartOwner orphan
//     sweep removes every provider:* ref no catalog row or route binding
//     references). Secrets never enter the catalog, journal, logs, or
//     telemetry — the catalog stores only the opaque secret-free ref.
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
		if err := o.ensureProviderSwitchJournalClearedLocked(); err != nil {
			return fmt.Errorf("%w: resolve prior provider switch: %v", ErrInvalid, err)
		}
		var previous Profile
		if !create {
			previous, err = o.store.Get(profile.ID)
			if err != nil {
				return err
			}
			// Preserve the active credential slot across edits that do not
			// rotate the key; only a staged-ref commit may change it.
			profile.CredentialRef = previous.CredentialRef
		}

		stagedRef := ""
		if apiKey != "" {
			// Fail before any write when the edit includes a key the store
			// cannot take.
			if o.creds == nil || !o.creds.Available() {
				return ErrCredentialStoreUnavailable
			}
			stagedRef = newStagedCredentialRef(profile.ID)
			if stagedRef == "" {
				return fmt.Errorf("%w: cannot stage credential for empty connection id", ErrInvalid)
			}
			if err := o.runEditHook("before_stage"); err != nil {
				return err
			}
			if serr := o.creds.Set(stagedRef, apiKey); serr != nil {
				return serr
			}
			if err := o.runEditHook("after_stage"); err != nil {
				return err
			}
			profile.CredentialRef = stagedRef
		}
		if err := o.runEditHook("before_commit"); err != nil {
			return err
		}

		if create {
			_, err = o.store.Create(profile, revision)
		} else {
			_, err = o.store.Update(profile, revision)
		}
		if err != nil && !errors.Is(err, ErrPersistDirSync) {
			// Not applied: retract the private staged secret so no orphaned
			// secret outlives the failed edit (best-effort; the startup sweep
			// is the deterministic backstop).
			if stagedRef != "" {
				_ = o.creds.Delete(stagedRef)
			}
			return err
		}
		// The catalog commit is the single visibility flip; the edit is
		// applied (ErrPersistDirSync means applied-with-warning).
		if err := o.runEditHook("after_commit"); err != nil {
			return err
		}
		if stagedRef != "" {
			if err := o.runEditHook("before_cleanup"); err != nil {
				return err
			}
			oldRef := activeCredentialRef(previous)
			if oldRef != "" && oldRef != stagedRef && !o.bindingUsesCredentialRefLocked(oldRef) {
				// Best-effort: a leftover old secret is inactive (the row no
				// longer references it) and the startup sweep removes it
				// deterministically. Old secrets referenced by a live binding
				// are the complete old version and stay until the binding ends.
				_ = o.creds.Delete(oldRef)
			}
			if err := o.runEditHook("after_cleanup"); err != nil {
				return err
			}
		}
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

// SetEditHook installs a test failpoint seam for Provider edit transactions.
// Phases: before_stage, after_stage, before_commit, after_commit,
// before_cleanup, after_cleanup. Returning an error aborts the transaction at
// that point exactly as a process crash would: every durable write before the
// phase stays on disk and recovery is exercised by the next StartOwner.
func (o *Owner) SetEditHook(hook func(phase string) error) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.editHook = hook
}

func (o *Owner) runEditHook(phase string) error {
	if o == nil || o.editHook == nil {
		return nil
	}
	return o.editHook(phase)
}

// bindingUsesCredentialRefLocked reports whether any current, launched, or
// history binding resolves the given credential ref. Caller must hold o.mu.
func (o *Owner) bindingUsesCredentialRefLocked(ref string) bool {
	ref = normalizeSpace(ref)
	if ref == "" || o == nil || o.table == nil {
		return false
	}
	for _, state := range o.table.Snapshot() {
		if bindingUsesCredentialRef(state.Binding, ref) || bindingUsesCredentialRef(state.Launched, ref) {
			return true
		}
		for _, event := range state.History {
			if bindingUsesCredentialRef(event.To, ref) || bindingUsesCredentialRef(event.From, ref) {
				return true
			}
		}
	}
	return false
}

func bindingUsesCredentialRef(b RouteBinding, ref string) bool {
	return normalizeSpace(b.CredentialRef) != "" && normalizeSpace(b.CredentialRef) == normalizeSpace(ref)
}

// sweepOrphanProviderCredentialsLocked deterministically recovers credential
// state after a crash: every provider:* ref that no catalog row and no route
// binding references is deleted. This is the durable backstop for staged
// secrets whose edit crashed before the catalog commit and for old secrets
// whose cleanup did not run. Best-effort: a failure is recorded as a warning
// and retried on the next start; secrets are never logged. Caller must hold
// o.mu.
func (o *Owner) sweepOrphanProviderCredentialsLocked() {
	if o == nil || o.creds == nil || !o.creds.Available() || o.store == nil {
		return
	}
	o.credentialSweepWarning = nil
	refs, err := o.creds.Refs()
	if err != nil {
		o.credentialSweepWarning = fmt.Errorf("%w: list credential refs: %v", ErrCredentialStoreFailed, err)
		return
	}
	referenced := map[string]struct{}{}
	for _, profile := range o.store.Catalog().Profiles {
		referenced[activeCredentialRef(profile)] = struct{}{}
	}
	if o.table != nil {
		for _, state := range o.table.Snapshot() {
			mark := func(b RouteBinding) {
				if r := normalizeSpace(b.CredentialRef); r != "" {
					referenced[r] = struct{}{}
				}
			}
			mark(state.Binding)
			mark(state.Launched)
			for _, event := range state.History {
				mark(event.To)
				mark(event.From)
			}
		}
	}
	var firstErr error
	for _, ref := range refs {
		ref = normalizeSpace(ref)
		if !isProviderCredentialRef(ref) {
			continue
		}
		if _, keep := referenced[ref]; keep {
			continue
		}
		if derr := o.creds.Delete(ref); derr != nil && firstErr == nil {
			firstErr = derr
		}
	}
	if firstErr != nil {
		o.credentialSweepWarning = fmt.Errorf("%w: sweep orphan credential refs: %v", ErrCredentialStoreFailed, firstErr)
	}
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
	if err := o.ensureProviderSwitchJournalClearedLocked(); err != nil {
		delErr = fmt.Errorf("%w: resolve prior provider switch: %v", ErrInvalid, err)
	} else if err := o.store.PreflightDelete(id, revision); err != nil {
		delErr = err
	} else if users := o.table.SessionsUsingProfile(id); len(users) > 0 {
		delErr = fmt.Errorf("%w: profile %s is bound to %d session(s)", ErrProfileInUse, id, len(users))
	} else {
		if o.creds != nil {
			profile, gerr := o.store.Get(id)
			if gerr != nil {
				delErr = gerr
			} else {
				// Delete the active ref first: a failure there is not-applied
				// (catalog/revision/defaults/key all unchanged). Staged or
				// leftover refs for this connection are then removed
				// best-effort; the startup sweep is the deterministic backstop
				// for anything that remains (inactive, never routed).
				refs := []string{activeCredentialRef(profile)}
				if listed, lerr := o.creds.Refs(); lerr == nil {
					for _, extra := range providerCredentialRefsForConnection(id, listed) {
						if extra != refs[0] {
							refs = append(refs, extra)
						}
					}
				}
				for i, ref := range refs {
					if ref == "" {
						continue
					}
					if derr := o.creds.Delete(ref); derr != nil {
						if i == 0 {
							delErr = derr
						}
						break
					}
				}
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
// stays default. A different connection must provide its model atomically.
func (o *Owner) SetProviderDefault(clientOrExecutor, connectionID, modelID string, revision int64) (ProviderCatalogProjection, error) {
	if o == nil || !o.started || o.store == nil {
		return ProviderCatalogProjection{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	client := clientFromExecutor(clientOrExecutor)
	connectionID = normalizeID(connectionID)
	modelID = normalizeSpace(modelID)
	o.mu.Lock()
	applyErr := func() error {
		if err := o.ensureProviderSwitchJournalClearedLocked(); err != nil {
			return fmt.Errorf("%w: resolve prior provider switch: %v", ErrInvalid, err)
		}
		if connectionID == "" {
			_, err := o.store.SetClientDefault(client, connectionID, modelID, revision)
			return err
		}
		raw, err := o.store.Get(connectionID)
		if err != nil {
			return err
		}
		if modelID == "" {
			// Keep a complete existing seed only when the same connection remains
			// default. A different connection must provide its model atomically.
			currentConn, currentModel := o.store.ClientDefault(client)
			if normalizeID(currentConn) == connectionID {
				modelID = normalizeSpace(currentModel)
			}
			if modelID == "" {
				return fmt.Errorf("%w: default runtime requires connection and model", ErrUpstreamModelRequired)
			}
		}
		target, err := CompileConnectionTarget(raw, client, modelID, "")
		if err != nil {
			return err
		}
		// Fail closed: never persist a client default whose model is only a
		// compile probe placeholder (connection with no explicit model). The
		// launch path resolves a deterministic supported model instead.
		if target.ModelPlaceholder {
			return ErrUpstreamModelRequired
		}
		_, err = o.store.SetClientDefault(client, connectionID, modelID, revision)
		return err
	}()
	o.mu.Unlock()
	// The machine-level gateway follows the default Codex connection: every
	// Provider-selection mutation must retarget it atomically.
	o.refreshGatewayUpstream()
	projection, projectionErr := o.ProjectProviders()
	if projectionErr != nil {
		return ProviderCatalogProjection{}, projectionErr
	}
	return projection, applyErr
}

// SwitchProvider atomically updates the future-launch default Provider and
// retargets every currently running routed Session for the same client without
// changing each Session's selected model or effect.
func (o *Owner) SwitchProvider(clientOrExecutor, connectionID string, revision int64) (ProviderCatalogProjection, error) {
	if o == nil || !o.started || o.store == nil || o.table == nil || o.routes == nil {
		return ProviderCatalogProjection{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	o.mu.Lock()
	persist, err := o.switchProviderLocked(clientOrExecutor, connectionID, revision)
	o.mu.Unlock()
	if !persist.Applied {
		return ProviderCatalogProjection{}, err
	}
	o.refreshGatewayUpstream()
	projection, projectionErr := o.ProjectProviders()
	if projectionErr != nil {
		return ProviderCatalogProjection{}, projectionErr
	}
	return projection, err
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
	o.mu.Lock()
	if err := o.ensureProviderSwitchJournalClearedLocked(); err != nil {
		o.mu.Unlock()
		return ProviderCatalogProjection{}, PersistResult{}, fmt.Errorf("%w: resolve prior provider switch: %v", ErrInvalid, err)
	}
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
	for _, client := range []string{ClientCodex, ClientClaude} {
		defaultConnectionID, defaultModelID := o.store.ClientDefault(client)
		if normalizeID(defaultConnectionID) != connectionID {
			continue
		}
		if _, enabled := enabledSet[normalizeSpace(defaultModelID)]; !enabled {
			o.mu.Unlock()
			return ProviderCatalogProjection{}, PersistResult{}, fmt.Errorf(
				"%w: choose another complete default runtime before disabling model %q",
				ErrUpstreamModelRequired,
				defaultModelID,
			)
		}
	}
	// Discovery/cache entries are presentation choices only. Persist support
	// toggles without treating the set as model identity admission.
	reference := o.projectConnectionModels(profile, e)
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
	if err := o.ensureProviderSwitchJournalClearedLocked(); err != nil {
		return ProviderCredentialResult{}, fmt.Errorf("%w: resolve prior provider switch: %v", ErrInvalid, err)
	}

	profile, err := o.store.Get(connectionID)
	if err != nil {
		return ProviderCredentialResult{}, err
	}
	creds := o.creds
	if creds == nil || !creds.Available() {
		return ProviderCredentialResult{}, ErrCredentialStoreUnavailable
	}
	if err := creds.Set(activeCredentialRef(profile), secret); err != nil {
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
	if err := o.ensureProviderSwitchJournalClearedLocked(); err != nil {
		return ProviderCredentialResult{}, fmt.Errorf("%w: resolve prior provider switch: %v", ErrInvalid, err)
	}

	profile, err := o.store.Get(connectionID)
	if err != nil {
		return ProviderCredentialResult{}, err
	}
	creds := o.creds
	if creds == nil || !creds.Available() {
		return ProviderCredentialResult{}, ErrCredentialStoreUnavailable
	}
	if err := creds.Delete(activeCredentialRef(profile)); err != nil {
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

// ThreadRuntime returns the Plus-menu projection for a Session.
func (o *Owner) ThreadRuntime(sessionID string) (ThreadRuntimeSelection, bool) {
	snap, ok := o.SessionSnapshot(sessionID)
	if !ok || snap.Current == nil {
		return ThreadRuntimeSelection{}, false
	}
	c := snap.Current
	selection := ThreadRuntimeSelection{
		SessionID:              c.SessionID,
		Client:                 c.Client,
		ConnectionID:           c.ConnectionID,
		ConnectionName:         c.ConnectionName,
		ProviderLabel:          c.ProviderLabel,
		ModelID:                c.ModelID,
		ReasoningEffort:        c.ReasoningEffort,
		ReasoningEffortDefault: c.ReasoningEffortDefault,
		ReasoningEfforts:       append([]string(nil), c.ReasoningEfforts...),
		CredentialReady:        c.CredentialReady,
		HotSwitchable:          c.HotSwitchable,
	}
	if profile, err := o.GetProfile(c.ConnectionID); err == nil {
		if entries, _ := o.modelsForConnection(profile, false); len(entries) > 0 {
			for _, entry := range entries {
				if normalizeSpace(entry.ID) != normalizeSpace(c.ModelID) {
					continue
				}
				selection.ReasoningEffortDefault = entry.ReasoningEffortDefault
				selection.ReasoningEfforts = append([]string(nil), entry.ReasoningEfforts...)
				break
			}
		}
	}
	return selection, true
}

// SetThreadRuntime atomically activates Provider+model (+ optional Effect)
// for the next admitted request on the existing routed
// Session (same RouteID). The model/effort override is ephemeral: catalog
// connection, defaults, revision, and other Sessions are never mutated.
// Generation CAS is internal. The selected model must be admitted by the
// target connection's synced support allowlist; otherwise the activation fails
// inline and the old route is kept — the daemon never silently substitutes
// another model.
//
// Reasoning Effort semantics (never send an invalid effort upstream):
//   - effortOverride empty: PRESERVE the Session's current override when the
//     target client model's daemon-owned contract supports it, else clear it
//     (the model's documented default applies). Compatible model switches keep
//     the user's effort; incompatible switches fall back to the safe default.
//   - effortOverride set: must be in the daemon Codex vocabulary AND supported
//     by the target client model; otherwise the activation fails inline.
//
// prepareThreadRuntimeLocked resolves one exact route mutation target from a
// single Owner transaction. Caller holds Owner.mu.
func (o *Owner) prepareThreadRuntimeLocked(sessionID string, choice ThreadRuntimeChoice, preserveOmittedEffect bool) (preparedThreadRuntime, error) {
	if err := o.ensureProviderSwitchJournalClearedLocked(); err != nil {
		return preparedThreadRuntime{}, fmt.Errorf("%w: resolve prior provider switch: %v", ErrInvalid, err)
	}
	sessionID = strings.TrimSpace(sessionID)
	connectionID := normalizeID(choice.ConnectionID)
	modelID := normalizeSpace(choice.ModelID)
	effortOverride := normalizeID(choice.Effect)
	// The native wire value "none" is Codex's model-default effort; it is the
	// same state as Zen's empty override and never a selectable explicit value.
	if effortOverride == ReasoningEffortNone {
		effortOverride = ""
	}
	if choice.UseDefaultEffect && effortOverride != "" {
		return preparedThreadRuntime{}, fmt.Errorf("%w: effect and use_default_effect are mutually exclusive", ErrInvalid)
	}
	if connectionID == "" || modelID == "" {
		return preparedThreadRuntime{}, fmt.Errorf("%w: runtime connection_id and model_id are required", ErrInvalid)
	}

	state, ok := o.table.Get(sessionID)
	if !ok {
		return preparedThreadRuntime{}, fmt.Errorf("%w: %s", ErrBindingNotFound, sessionID)
	}
	raw, err := o.GetProfile(connectionID)
	if err != nil {
		return preparedThreadRuntime{}, err
	}

	target, err := CompileConnectionTarget(raw, state.Binding.ExecutorID, modelID, effortOverride)
	if err != nil {
		return preparedThreadRuntime{}, err
	}
	// Explicit effort: admit against the TARGET model's daemon-owned contract
	// (fail closed; an unsupported effort never reaches the route or the
	// upstream). Omitted effort: PRESERVE the current override when the target
	// model supports it (compatible model switch); otherwise clear it so the
	// target model's documented default applies (safe fallback — never an
	// invalid value).
	if effortOverride != "" {
		if normalizeID(target.ExecutorID) != ExecutorCodex || !codexEffortAdmitted(target.ClientModel, effortOverride) {
			return preparedThreadRuntime{}, fmt.Errorf("%w: client model %s does not support effect %q", ErrReasoningEffortUnsupported, target.ClientModel, effortOverride)
		}
	} else if preserveOmittedEffect {
		currentEffort := normalizeID(state.Binding.ReasoningEffort)
		if normalizeID(target.ExecutorID) == ExecutorCodex && codexEffortAdmitted(target.ClientModel, currentEffort) {
			// Compatible model switch: keep the user's effort.
			effortOverride = currentEffort
		}
		// Incompatible: fall through with an empty override (model default).
	}
	target.ReasoningEffort = effortOverride
	return preparedThreadRuntime{
		SessionID: sessionID,
		Target: ThreadRuntimeChoice{
			ConnectionID: connectionID,
			ModelID:      target.ClientModel,
			Effect:       effortOverride,
		},
		expectedGeneration: state.Generation,
		targetProfile:      target,
		Previous: ThreadRuntimeChoice{
			ConnectionID: state.Binding.ProfileID,
			ModelID:      state.Binding.ClientModel,
			Effect:       state.Binding.ReasoningEffort,
		},
	}, nil
}

type preparedThreadRuntime struct {
	SessionID          string
	Target             ThreadRuntimeChoice
	expectedGeneration int64
	targetProfile      Profile
	// Previous is the pre-mutation native thread identity (client model +
	// reasoning effort) needed to revert a native thread/settings/update when
	// the route commit fails.
	Previous ThreadRuntimeChoice
}

// ApplyTerminalModelSwitch applies only Codex's explicit reserved model-switch
// signal through the same route mutation and durable projection used by the
// Interface picker. A bare request-body mismatch never calls this operation.
func (o *Owner) ApplyTerminalModelSwitch(routeID, modelID, effort string, effortPresent bool) error {
	if o == nil || !o.started || o.table == nil {
		return fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.ensureProviderSwitchJournalClearedLocked(); err != nil {
		return fmt.Errorf("%w: resolve prior provider switch: %v", ErrInvalid, err)
	}
	binding, ok := o.table.GetByRouteID(routeID)
	if !ok {
		return fmt.Errorf("%w: %s", ErrBindingNotFound, routeID)
	}
	choice := ThreadRuntimeChoice{
		ConnectionID: binding.ProfileID,
		ModelID:      modelID,
	}
	if effortPresent {
		choice.Effect = effort
	}
	if normalizeSpace(binding.ClientModel) == normalizeSpace(modelID) &&
		((effortPresent && normalizeID(binding.ReasoningEffort) == normalizeID(effort)) ||
			(!effortPresent && normalizeID(binding.ReasoningEffort) == "")) {
		return nil
	}
	plan, err := o.prepareThreadRuntimeLocked(binding.SessionID, choice, false)
	if err != nil {
		return err
	}
	_, _, persist, err := o.commitThreadRuntimeLocked(plan)
	if !persist.Applied {
		return err
	}
	return nil
}

// commitThreadRuntimeLocked publishes a prepared mutation while holding the
// same Owner transaction that selected its Provider/model/effect target.
// Caller holds Owner.mu.
func (o *Owner) commitThreadRuntimeLocked(plan preparedThreadRuntime) (SessionRouteState, WireSessionSnapshot, PersistResult, error) {
	beforeRev := o.store.Revision()
	raw, err := o.store.Get(plan.Target.ConnectionID)
	if err != nil {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, err
	}
	beforeModel := normalizeSpace(raw.Model)
	next, snap, persist, err := o.activateCompiledProfileLocked(plan.SessionID, plan.targetProfile, plan.expectedGeneration)
	if !persist.Applied {
		return SessionRouteState{}, WireSessionSnapshot{}, persist, err
	}
	// Fail closed if Plus-menu activation mutated the catalog (regression guard).
	if o.store.Revision() != beforeRev {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, fmt.Errorf("%w: activate must not mutate provider catalog", ErrInvalid)
	}
	if got, gerr := o.store.Get(plan.Target.ConnectionID); gerr == nil && normalizeSpace(got.Model) != beforeModel {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, fmt.Errorf("%w: activate must not mutate connection model", ErrInvalid)
	}
	return next, snap, persist, err
}

// PreparedThreadRuntime is a validated, not-yet-committed runtime mutation.
// The native-first transaction in the server layer applies the native Codex
// thread/settings/update between PrepareThreadRuntime and CommitThreadRuntime;
// Commit re-validates the generation CAS so a concurrent mutation fails closed.
type PreparedThreadRuntime struct {
	plan preparedThreadRuntime
}

// Target is the exact resolved native identity (client model + effort) the
// mutation will apply.
func (p PreparedThreadRuntime) Target() ThreadRuntimeChoice {
	return p.plan.Target
}

// Previous is the pre-mutation native identity for rollback.
func (p PreparedThreadRuntime) Previous() ThreadRuntimeChoice {
	return p.plan.Previous
}

// PrepareThreadRuntime validates and resolves one runtime mutation without
// committing it. The Owner lock is not held across the returned handle, so
// callers may perform native (network) work between Prepare and Commit.
func (o *Owner) PrepareThreadRuntime(sessionID string, choice ThreadRuntimeChoice) (PreparedThreadRuntime, error) {
	if o == nil || !o.started {
		return PreparedThreadRuntime{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	plan, err := o.prepareThreadRuntimeLocked(sessionID, choice, !choice.UseDefaultEffect)
	if err != nil {
		return PreparedThreadRuntime{}, err
	}
	return PreparedThreadRuntime{plan: plan}, nil
}

// CommitThreadRuntime publishes a prepared mutation atomically (generation CAS
// inside the same Owner transaction). On failure the table is unchanged; a
// caller that already applied the native side must revert it.
func (o *Owner) CommitThreadRuntime(prepared PreparedThreadRuntime) (SessionRouteState, WireSessionSnapshot, PersistResult, error) {
	if o == nil || !o.started {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.commitThreadRuntimeLocked(prepared.plan)
}

// SetThreadRuntime validates and commits one runtime without process staging,
// process replacement, or resume input. It is the route-only path; live
// native synchronization is orchestrated by the server layer through
// PrepareThreadRuntime + CommitThreadRuntime.
func (o *Owner) SetThreadRuntime(sessionID string, choice ThreadRuntimeChoice) (SessionRouteState, WireSessionSnapshot, PersistResult, error) {
	prepared, err := o.PrepareThreadRuntime(sessionID, choice)
	if err != nil {
		return SessionRouteState{}, WireSessionSnapshot{}, PersistResult{}, err
	}
	return o.CommitThreadRuntime(prepared)
}
