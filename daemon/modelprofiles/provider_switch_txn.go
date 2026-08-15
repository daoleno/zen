package modelprofiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const providerSwitchJournalFileName = "provider-switch-txn.json"

const (
	providerSwitchJournalSchemaVersion  = 1
	providerSwitchJournalStatePrepared  = "prepared"
	providerSwitchJournalStateCommitted = "committed"
	providerSwitchJournalStateCleared   = "cleared"
)

type providerSwitchSnapshot struct {
	Revision      int64               `json:"revision"`
	Defaults      map[string]string   `json:"defaults"`
	DefaultModels map[string]string   `json:"default_models"`
	Routes        []SessionRouteState `json:"routes"`
}

type providerSwitchSnapshotDisk struct {
	Revision      int64             `json:"revision"`
	Defaults      map[string]string `json:"defaults"`
	DefaultModels map[string]string `json:"default_models"`
	Routes        json.RawMessage   `json:"routes"`
}

func (s providerSwitchSnapshot) MarshalJSON() ([]byte, error) {
	routes, err := EncodeDurableSnapshot(s.Routes)
	if err != nil {
		return nil, err
	}
	disk := providerSwitchSnapshotDisk{
		Revision:      s.Revision,
		Defaults:      cloneDefaults(s.Defaults),
		DefaultModels: cloneDefaults(s.DefaultModels),
		Routes:        json.RawMessage(routes),
	}
	return json.Marshal(disk)
}

func (s *providerSwitchSnapshot) UnmarshalJSON(raw []byte) error {
	var disk providerSwitchSnapshotDisk
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&disk); err != nil {
		return err
	}
	routes, err := DecodeDurableSnapshot(disk.Routes)
	if err != nil {
		return err
	}
	s.Revision = disk.Revision
	s.Defaults = cloneDefaults(disk.Defaults)
	s.DefaultModels = cloneDefaults(disk.DefaultModels)
	s.Routes = cloneSessionStates(routes)
	return nil
}

type providerSwitchJournal struct {
	SchemaVersion int                    `json:"schema_version"`
	State         string                 `json:"state"`
	Client        string                 `json:"client"`
	Before        providerSwitchSnapshot `json:"before"`
	After         providerSwitchSnapshot `json:"after"`
}

func (j providerSwitchJournal) validate() error {
	if j.SchemaVersion != providerSwitchJournalSchemaVersion {
		return fmt.Errorf("%w: unsupported provider switch journal schema %d", ErrInvalid, j.SchemaVersion)
	}
	switch j.State {
	case providerSwitchJournalStatePrepared, providerSwitchJournalStateCommitted, providerSwitchJournalStateCleared:
	default:
		return fmt.Errorf("%w: invalid provider switch journal state %q", ErrInvalid, j.State)
	}
	if strings.TrimSpace(j.Client) == "" {
		return fmt.Errorf("%w: provider switch journal client is required", ErrInvalid)
	}
	if j.Before.Revision < 0 || j.After.Revision < 0 {
		return fmt.Errorf("%w: provider switch journal revision is required", ErrInvalid)
	}
	if j.After.Revision != j.Before.Revision+1 {
		return fmt.Errorf("%w: provider switch journal revision transition is invalid", ErrInvalid)
	}
	return nil
}

func (o *Owner) providerSwitchJournalPath() string {
	if o == nil || o.routes == nil {
		return ""
	}
	routePath := strings.TrimSpace(o.routes.Path())
	if routePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(routePath), providerSwitchJournalFileName)
}

func (o *Owner) writeProviderSwitchJournal(journal providerSwitchJournal) error {
	if o == nil {
		return fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	path := o.providerSwitchJournalPath()
	if path == "" {
		return fmt.Errorf("%w: provider switch journal path is required", ErrInvalid)
	}
	raw, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(path, raw, 0o600)
}

func readProviderSwitchJournal(path string) (providerSwitchJournal, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return providerSwitchJournal{}, false, nil
		}
		return providerSwitchJournal{}, false, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var journal providerSwitchJournal
	if err := dec.Decode(&journal); err != nil {
		return providerSwitchJournal{}, true, fmt.Errorf("%w: decode provider switch journal: %v", ErrInvalid, err)
	}
	if err := journal.validate(); err != nil {
		return providerSwitchJournal{}, true, err
	}
	return journal, true, nil
}

func (o *Owner) recoverProviderSwitchJournal() error {
	if o == nil || o.store == nil || o.table == nil || o.routes == nil {
		return nil
	}
	path := o.providerSwitchJournalPath()
	if path == "" {
		return nil
	}
	journal, ok, err := readProviderSwitchJournal(path)
	if err != nil || !ok {
		return err
	}
	if journal.State == providerSwitchJournalStateCleared {
		// A process may observe a renamed tombstone before that directory entry
		// is crash-durable. Re-write it through the atomic writer before allowing
		// any later mutation; a crash can then resurrect only a harmless cleared
		// record, never the previous prepared/committed journal.
		if err := o.writeProviderSwitchJournal(journal); err != nil {
			return err
		}
		_ = os.Remove(path)
		return nil
	}
	snapshot := journal.Before
	if journal.State == providerSwitchJournalStateCommitted {
		snapshot = journal.After
	}
	persistErr := o.persistProviderSwitchSnapshot(snapshot)
	if persistErr != nil {
		return persistErr
	}
	o.applyProviderSwitchSnapshot(snapshot)
	return o.clearProviderSwitchJournal(journal)
}

// ensureProviderSwitchJournalClearedLocked resolves any prior switch journal
// before a later mutation can publish. This prevents a stale committed or
// prepared record from replaying over newer catalog/route state after a crash.
// Caller holds Owner.mu.
func (o *Owner) ensureProviderSwitchJournalClearedLocked() error {
	return o.recoverProviderSwitchJournal()
}

// clearProviderSwitchJournal durably replaces a replayable journal with a
// harmless tombstone before unlinking it. If unlink is lost in a crash, the
// surviving cleared record is ignored on restart and cannot clobber newer
// mutations.
func (o *Owner) clearProviderSwitchJournal(journal providerSwitchJournal) error {
	journal.State = providerSwitchJournalStateCleared
	if err := o.writeProviderSwitchJournal(journal); err != nil {
		return err
	}
	_ = os.Remove(o.providerSwitchJournalPath())
	return nil
}

func (o *Owner) persistProviderSwitchSnapshot(snapshot providerSwitchSnapshot) error {
	if o == nil || o.store == nil || o.table == nil || o.routes == nil {
		return fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	o.store.mu.RLock()
	profiles := cloneProfiles(o.store.profiles)
	o.store.mu.RUnlock()
	storeErr := o.store.persistLocked(snapshot.Revision, profiles, cloneDefaults(snapshot.Defaults), cloneDefaults(snapshot.DefaultModels))
	routeErr := o.routes.SaveStates(snapshot.Routes)
	if storeErr != nil && !errors.Is(storeErr, ErrPersistDirSync) {
		return storeErr
	}
	if routeErr != nil && !errors.Is(routeErr, ErrPersistDirSync) {
		return routeErr
	}
	return errors.Join(storeErr, routeErr)
}

func (o *Owner) applyProviderSwitchSnapshot(snapshot providerSwitchSnapshot) {
	if o == nil || o.store == nil || o.table == nil {
		return
	}
	o.table.mu.Lock()
	defer o.table.mu.Unlock()
	o.applyProviderSwitchSnapshotLocked(snapshot)
}

func (o *Owner) applyProviderSwitchSnapshotLocked(snapshot providerSwitchSnapshot) {
	o.store.mu.Lock()
	o.store.defaults = cloneDefaults(snapshot.Defaults)
	o.store.defaultModels = cloneDefaults(snapshot.DefaultModels)
	o.store.revision = snapshot.Revision
	o.store.mu.Unlock()
	routes := cloneSessionStates(snapshot.Routes)
	for i := range routes {
		routes[i].Binding.CredentialReady = o.table.credentialReadyLocked(profileFromBinding(routes[i].Binding))
	}
	o.table.replaceSnapshotLocked(routes)
}

func (o *Owner) switchProviderLocked(clientOrExecutor, connectionID string, revision int64) (PersistResult, error) {
	if o == nil || !o.started || o.store == nil || o.table == nil || o.routes == nil {
		return PersistResult{}, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	if err := o.ensureProviderSwitchJournalClearedLocked(); err != nil {
		return PersistResult{}, fmt.Errorf("%w: resolve prior provider switch: %v", ErrInvalid, err)
	}
	client := clientFromExecutor(clientOrExecutor)
	connectionID = normalizeID(connectionID)
	if client == "" || connectionID == "" {
		return PersistResult{}, fmt.Errorf("%w: client and connection are required", ErrInvalid)
	}

	raw, err := o.GetProfile(connectionID)
	if err != nil {
		return PersistResult{}, err
	}

	currentRevision := o.store.Revision()
	if revision != currentRevision {
		return PersistResult{}, fmt.Errorf("%w: expected revision %d, have %d", ErrConflict, revision, currentRevision)
	}

	currentConnectionID, currentModelID := o.store.ClientDefault(client)
	currentConnectionID = normalizeID(currentConnectionID)
	currentModelID = normalizeSpace(currentModelID)
	if currentConnectionID == "" || currentModelID == "" {
		return PersistResult{}, fmt.Errorf("%w: default runtime requires connection and model", ErrUpstreamModelRequired)
	}

	defaults := map[string]string{}
	defaultModels := map[string]string{}
	o.store.mu.RLock()
	for key, value := range o.store.defaults {
		defaults[key] = value
	}
	for key, value := range o.store.defaultModels {
		defaultModels[key] = value
	}
	o.store.mu.RUnlock()

	// This is the route-admission linearization boundary. BeginRouteFlight and
	// EndRouteFlight wait while the complete before/after transaction is
	// validated, journaled, persisted, and published. Already-admitted requests
	// retain their copied binding and finish against the old upstream; their
	// history completion applies after the new binding is visible.
	o.table.mu.Lock()
	defer o.table.mu.Unlock()
	states := o.table.snapshotLocked()

	before := providerSwitchSnapshotFromState(currentRevision, defaults, defaultModels, states)
	nextDefaults, nextModels := switchDefaultMaps(defaults, defaultModels, client, connectionID, currentModelID)
	nextRevision := currentRevision + 1

	defaultTarget, err := CompileConnectionTarget(raw, executorFromClient(client), currentModelID, "")
	if err != nil {
		return PersistResult{}, err
	}
	if _, err := o.admitProviderSwitchTargetLocked(raw, defaultTarget); err != nil {
		return PersistResult{}, err
	}

	nextStates := make([]SessionRouteState, 0, len(states))
	for _, state := range states {
		executorID := normalizeID(state.Binding.ExecutorID)
		if executorID == "" {
			if state.Binding.RouteID != "" || state.Binding.RouteProtocol != "" {
				return PersistResult{}, fmt.Errorf("%w: routed session %s has no executor", ErrInvalid, state.Binding.SessionID)
			}
			nextStates = append(nextStates, state)
			continue
		}
		if executorID != executorFromClient(client) {
			nextStates = append(nextStates, state)
			continue
		}
		if state.Binding.RouteID == "" || state.Binding.RouteProtocol == "" {
			nextStates = append(nextStates, state)
			continue
		}
		if !ProfileHotSwitchable(state.Binding.Protocol) {
			return PersistResult{}, fmt.Errorf("%w: session %s cannot switch providers", ErrBindingNotRouted, state.Binding.SessionID)
		}
		target, err := CompileConnectionTarget(raw, executorID, state.Binding.ClientModel, state.Binding.ReasoningEffort)
		if err != nil {
			return PersistResult{}, err
		}
		admitted, err := o.admitProviderSwitchTargetLocked(raw, target)
		if err != nil {
			return PersistResult{}, err
		}
		next, err := o.table.activateStateLocked(state, target, nextRevision, state.Generation, admitted)
		if err != nil {
			return PersistResult{}, err
		}
		nextStates = append(nextStates, next)
	}

	after := providerSwitchSnapshotFromState(nextRevision, nextDefaults, nextModels, nextStates)
	prepared := providerSwitchJournal{
		SchemaVersion: providerSwitchJournalSchemaVersion,
		State:         providerSwitchJournalStatePrepared,
		Client:        client,
		Before:        before,
		After:         after,
	}
	if err := o.writeProviderSwitchJournal(prepared); err != nil {
		if errors.Is(err, ErrPersistDirSync) {
			return PersistResult{}, fmt.Errorf("%w: provider switch prepare journal durability is uncertain: %v", ErrInvalid, err)
		}
		return PersistResult{}, err
	}
	afterPersistErr := o.persistProviderSwitchSnapshot(after)
	if afterPersistErr != nil && !errors.Is(afterPersistErr, ErrPersistDirSync) {
		rollbackErr := o.persistProviderSwitchSnapshot(before)
		if rollbackErr != nil && !errors.Is(rollbackErr, ErrPersistDirSync) {
			return PersistResult{}, errors.Join(
				fmt.Errorf("%w: provider switch persist failed: %v", ErrInvalid, afterPersistErr),
				fmt.Errorf("%w: provider switch rollback failed: %v", ErrInvalid, rollbackErr),
			)
		}
		if rollbackErr == nil {
			if clearErr := o.clearProviderSwitchJournal(prepared); clearErr != nil {
				rollbackErr = fmt.Errorf("rollback journal cleanup is pending: %v", clearErr)
			}
		}
		return PersistResult{}, providerSwitchRollbackError("provider switch persist failed", afterPersistErr, rollbackErr)
	}
	committed := prepared
	committed.State = providerSwitchJournalStateCommitted
	if err := o.writeProviderSwitchJournal(committed); err != nil {
		if errors.Is(err, ErrPersistDirSync) {
			o.applyProviderSwitchSnapshotLocked(after)
			return PersistResult{Applied: true, Durable: false}, err
		}
		rollbackErr := o.persistProviderSwitchSnapshot(before)
		if rollbackErr != nil && !errors.Is(rollbackErr, ErrPersistDirSync) {
			return PersistResult{}, errors.Join(
				fmt.Errorf("%w: provider switch commit journal failed: %v", ErrInvalid, err),
				fmt.Errorf("%w: provider switch rollback failed: %v", ErrInvalid, rollbackErr),
			)
		}
		if rollbackErr == nil {
			if clearErr := o.clearProviderSwitchJournal(prepared); clearErr != nil {
				rollbackErr = fmt.Errorf("rollback journal cleanup is pending: %v", clearErr)
			}
		}
		return PersistResult{}, providerSwitchRollbackError("provider switch commit journal failed", err, rollbackErr)
	}
	o.applyProviderSwitchSnapshotLocked(after)
	if afterPersistErr != nil {
		if err := o.persistProviderSwitchSnapshot(after); err != nil {
			return PersistResult{Applied: true, Durable: false}, fmt.Errorf("%w: provider switch snapshot durability remains uncertain: %v", ErrPersistDirSync, err)
		}
	}
	if err := o.clearProviderSwitchJournal(committed); err != nil {
		return PersistResult{Applied: true, Durable: false}, fmt.Errorf("%w: provider switch journal cleanup pending: %v", ErrPersistDirSync, err)
	}
	return PersistResult{Applied: true, Durable: true}, nil
}

func providerSwitchRollbackError(operation string, operationErr, rollbackErr error) error {
	if rollbackErr == nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalid, operation, operationErr)
	}
	// A rollback rename with uncertain directory sync is still not an applied
	// Provider switch. Keep ErrPersistDirSync out of the public error chain so
	// transports cannot misclassify the failed transaction as applied; the
	// prepared journal remains the startup authority for retrying all-before.
	return fmt.Errorf("%w: %s: %v; rollback durability is uncertain: %v", ErrInvalid, operation, operationErr, rollbackErr)
}

// admitProviderSwitchTargetLocked validates one exact preserved model/effect
// against the target connection. Caller holds Owner.mu and RouteTable.mu.
func (o *Owner) admitProviderSwitchTargetLocked(connection, target Profile) (VerifiedProfileContract, error) {
	if target.ModelPlaceholder || normalizeSpace(target.Model) == "" {
		return VerifiedProfileContract{}, ErrUpstreamModelRequired
	}
	if err := o.activationModelAdmittedLocked(connection, target.Model); err != nil {
		return VerifiedProfileContract{}, err
	}
	if effort := normalizeID(target.ReasoningEffort); effort != "" {
		if normalizeID(target.ExecutorID) != ExecutorCodex || !codexEffortSupported(target.ClientModel, effort) {
			return VerifiedProfileContract{}, fmt.Errorf("%w: client model %s does not support effect %q", ErrReasoningEffortUnsupported, target.ClientModel, effort)
		}
	}
	admitted, err := AuthorizeProfileContract(target, ContractAuth{Verifier: o.verifier})
	if err != nil {
		return VerifiedProfileContract{}, err
	}
	if err := o.table.requireAuthLocked(target); err != nil {
		return VerifiedProfileContract{}, err
	}
	return admitted, nil
}

func providerSwitchSnapshotFromState(revision int64, defaults, defaultModels map[string]string, routes []SessionRouteState) providerSwitchSnapshot {
	return providerSwitchSnapshot{
		Revision:      revision,
		Defaults:      cloneDefaults(defaults),
		DefaultModels: cloneDefaults(defaultModels),
		Routes:        cloneSessionStates(routes),
	}
}

func cloneSessionStates(states []SessionRouteState) []SessionRouteState {
	out := make([]SessionRouteState, 0, len(states))
	for _, state := range states {
		out = append(out, cloneSessionState(state))
	}
	return out
}

func switchDefaultMaps(defaults, defaultModels map[string]string, client, connectionID, modelID string) (map[string]string, map[string]string) {
	nextDefaults := cloneDefaults(defaults)
	nextModels := cloneDefaults(defaultModels)
	client = clientFromExecutor(client)
	executorID := executorFromClient(client)
	connectionID = normalizeID(connectionID)
	modelID = normalizeSpace(modelID)
	if connectionID == "" {
		delete(nextDefaults, client)
		delete(nextDefaults, executorID)
		delete(nextModels, client)
		return nextDefaults, nextModels
	}
	nextDefaults[client] = connectionID
	nextDefaults[executorID] = connectionID
	if modelID == "" {
		delete(nextModels, client)
	} else {
		nextModels[client] = modelID
	}
	return nextDefaults, nextModels
}
