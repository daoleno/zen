package modelprofiles

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// Store is the process-local owner of ~/.zen/model-profiles.toml.
type Store struct {
	mu            sync.RWMutex
	path          string
	revision      int64
	profiles      map[string]Profile
	defaults      map[string]string
	defaultModels map[string]string // client -> model_id (Settings default; not connection row)
	lookup        func(string) (string, bool)
	// dirSync is an optional fault seam invoked after a successful rename.
	// nil uses the real parent-directory Sync. After rename, the write is
	// committed; a dirSync error returns ErrPersistDirSync with memory aligned.
	dirSync func(dir string) error
	// hook is a test failpoint/serialization seam for catalog persistence.
	hook func(phase string) error
}

type fileDocument struct {
	Revision      int64             `toml:"revision"`
	Profiles      []Profile         `toml:"profiles"`
	Defaults      map[string]string `toml:"defaults"`
	DefaultModels map[string]string `toml:"default_models,omitempty"`
}

// NewStore loads path or returns an empty revision-0 catalog when missing.
// Tests must pass an owned temp path; production uses DefaultModelProfilesPath.
func NewStore(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("%w: model profiles path is required", ErrInvalid)
	}
	s := &Store{
		path:          path,
		profiles:      map[string]Profile{},
		defaults:      map[string]string{},
		defaultModels: map[string]string{},
		lookup:        lookupEnv,
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Path returns the durable file path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// SetDirSync installs a test seam for parent-directory sync after rename.
// A failure returns ErrPersistDirSync with memory already aligned to the new catalog.
func (s *Store) SetDirSync(fn func(dir string) error) {
	if s == nil {
		return
	}
	s.dirSync = fn
}

// SetPersistHook installs a test failpoint/serialization seam. Phases:
// before_write, after_write, before_rename, after_rename, before_dirsync, after_dirsync.
func (s *Store) SetPersistHook(hook func(phase string) error) {
	if s == nil {
		return
	}
	s.hook = hook
}

// Revision returns the current catalog revision.
func (s *Store) Revision() int64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision
}

// Catalog returns a sorted secret-free snapshot of profiles and defaults.
func (s *Store) Catalog() Catalog {
	if s == nil {
		return Catalog{Profiles: nil, Defaults: map[string]string{}}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.catalogLocked()
}

// Views returns profiles with credential readiness metadata (never values).
func (s *Store) Views() []ProfileView {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.viewsLocked()
}

// Projection returns catalog and views under one Store read lock.
func (s *Store) Projection() (Catalog, []ProfileView) {
	if s == nil {
		return Catalog{Defaults: map[string]string{}}, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.catalogLocked(), s.viewsLocked()
}

func (s *Store) viewsLocked() []ProfileView {
	out := make([]ProfileView, 0, len(s.profiles))
	for _, id := range sortedProfileIDs(s.profiles) {
		profile := s.profiles[id]
		out = append(out, ProfileView{
			Profile:         profile,
			CredentialReady: AuthReady(profile.AuthMode, profile.CredentialEnv, s.lookup),
		})
	}
	return out
}

// Get returns one profile by id.
func (s *Store) Get(id string) (Profile, error) {
	if s == nil {
		return Profile{}, ErrNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, ok := s.profiles[normalizeID(id)]
	if !ok {
		return Profile{}, fmt.Errorf("%w: %s", ErrNotFound, normalizeID(id))
	}
	return profile, nil
}

// DefaultProfileID returns the default profile for an executor, if any.
func (s *Store) DefaultProfileID(executorID string) string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.defaults[normalizeID(executorID)])
}

// ResolveProfile returns an explicit profile or the executor default.
// Account-scoped connections are compiled into an ephemeral client Profile.
// Empty profileID with no default yields ErrNotFound.
func (s *Store) ResolveProfile(executorID, profileID string) (Profile, error) {
	if s == nil {
		return Profile{}, ErrNotFound
	}
	s.mu.RLock()
	executorID = normalizeID(executorID)
	profileID = normalizeID(profileID)
	if profileID == "" {
		profileID = strings.TrimSpace(s.defaults[executorID])
		if profileID == "" {
			profileID = strings.TrimSpace(s.defaults[clientFromExecutor(executorID)])
		}
	}
	if profileID == "" {
		s.mu.RUnlock()
		return Profile{}, fmt.Errorf("%w: no profile selected for executor %s", ErrNotFound, executorID)
	}
	profile, ok := s.profiles[profileID]
	modelOverride := strings.TrimSpace(s.defaultModels[clientFromExecutor(executorID)])
	s.mu.RUnlock()
	if !ok {
		return Profile{}, fmt.Errorf("%w: %s", ErrNotFound, profileID)
	}
	if isAccountConnection(profile) {
		return CompileConnectionTarget(profile, executorID, modelOverride)
	}
	if executorID != "" && profile.ExecutorID != executorID {
		return Profile{}, fmt.Errorf("%w: profile %s belongs to executor %s, not %s", ErrInvalid, profileID, profile.ExecutorID, executorID)
	}
	if modelOverride != "" {
		profile.Model = modelOverride
	}
	return profile, nil
}

// DefaultModelID returns the Settings default model override for a client.
func (s *Store) DefaultModelID(client string) string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.defaultModels[clientFromExecutor(client)])
}

// SetDefaultModel sets or clears the Settings default model for a client without
// mutating the connection row. Prefer SetClientDefault for Provider+model pairs.
func (s *Store) SetDefaultModel(client, modelID string, expectedRevision int64) (Catalog, error) {
	if s == nil {
		return Catalog{}, fmt.Errorf("model profile store is not configured")
	}
	client = clientFromExecutor(client)
	modelID = normalizeSpace(modelID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision != s.revision {
		return Catalog{}, fmt.Errorf("%w: expected revision %d, have %d", ErrConflict, expectedRevision, s.revision)
	}
	nextDefaults := cloneDefaults(s.defaults)
	nextModels := cloneDefaults(s.defaultModels)
	if modelID == "" {
		delete(nextModels, client)
	} else {
		nextModels[client] = modelID
	}
	nextRev := s.revision + 1
	err := s.persistLocked(nextRev, cloneProfiles(s.profiles), nextDefaults, nextModels)
	return s.applyPersist(err, nextRev, cloneProfiles(s.profiles), nextDefaults, nextModels)
}

// SetClientDefault atomically sets or clears the future-launch default
// connection and model for one client in a single revisioned durable write.
// Empty connectionID clears both fields. Persistence failure leaves memory and
// revision unchanged.
func (s *Store) SetClientDefault(client, connectionID, modelID string, expectedRevision int64) (Catalog, error) {
	if s == nil {
		return Catalog{}, fmt.Errorf("model profile store is not configured")
	}
	client = clientFromExecutor(client)
	executorID := executorFromClient(client)
	connectionID = normalizeID(connectionID)
	modelID = normalizeSpace(modelID)
	if client == "" || executorID == "" {
		return Catalog{}, fmt.Errorf("%w: client is required", ErrInvalid)
	}
	if !SupportsExecutor(executorID) {
		return Catalog{}, fmt.Errorf("%w: %s", ErrUnsupportedExecutor, executorID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision != s.revision {
		return Catalog{}, fmt.Errorf("%w: expected revision %d, have %d", ErrConflict, expectedRevision, s.revision)
	}
	nextDefaults := cloneDefaults(s.defaults)
	nextModels := cloneDefaults(s.defaultModels)
	if connectionID == "" {
		delete(nextDefaults, executorID)
		delete(nextDefaults, client)
		delete(nextModels, client)
	} else {
		profile, ok := s.profiles[connectionID]
		if !ok {
			return Catalog{}, fmt.Errorf("%w: %s", ErrNotFound, connectionID)
		}
		if isAccountConnection(profile) {
			spec, ok := lookupPreset(inferPresetID(profile))
			if !ok || !presetSupportsClient(spec, executorID) {
				return Catalog{}, fmt.Errorf("%w: connection %s does not support client %s", ErrInvalid, connectionID, client)
			}
		} else if profile.ExecutorID != executorID {
			return Catalog{}, fmt.Errorf("%w: profile %s belongs to executor %s, not %s", ErrInvalid, connectionID, profile.ExecutorID, executorID)
		}
		if modelID != "" {
			if err := ValidateModelID(modelID); err != nil {
				return Catalog{}, fmt.Errorf("%w: model: %v", ErrInvalid, err)
			}
			if isAccountConnection(profile) && !accountLooksAdvanced(profile, inferPresetID(profile)) {
				if err := requireTrustedOrFail(inferPresetID(profile), modelID); err != nil {
					return Catalog{}, err
				}
			}
		}
		nextDefaults[client] = connectionID
		nextDefaults[executorID] = connectionID
		if modelID == "" {
			delete(nextModels, client)
		} else {
			nextModels[client] = modelID
		}
	}
	nextRev := s.revision + 1
	nextProfiles := cloneProfiles(s.profiles)
	err := s.persistLocked(nextRev, nextProfiles, nextDefaults, nextModels)
	return s.applyPersist(err, nextRev, nextProfiles, nextDefaults, nextModels)
}

// Create inserts a profile. expectedRevision must match the current revision.
func (s *Store) Create(profile Profile, expectedRevision int64) (Catalog, error) {
	if s == nil {
		return Catalog{}, fmt.Errorf("model profile store is not configured")
	}
	profile = normalizeProfile(profile)
	if err := ValidateProfile(profile); err != nil {
		return Catalog{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision != s.revision {
		return Catalog{}, fmt.Errorf("%w: expected revision %d, have %d", ErrConflict, expectedRevision, s.revision)
	}
	if _, exists := s.profiles[profile.ID]; exists {
		return Catalog{}, fmt.Errorf("%w: %s", ErrDuplicateID, profile.ID)
	}
	next := cloneProfiles(s.profiles)
	next[profile.ID] = profile
	nextRev := s.revision + 1
	err := s.persistLocked(nextRev, next, cloneDefaults(s.defaults), cloneDefaults(s.defaultModels))
	return s.applyPersist(err, nextRev, next, cloneDefaults(s.defaults), cloneDefaults(s.defaultModels))
}

// Update replaces an existing profile. expectedRevision must match.
func (s *Store) Update(profile Profile, expectedRevision int64) (Catalog, error) {
	if s == nil {
		return Catalog{}, fmt.Errorf("model profile store is not configured")
	}
	profile = normalizeProfile(profile)
	if err := ValidateProfile(profile); err != nil {
		return Catalog{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision != s.revision {
		return Catalog{}, fmt.Errorf("%w: expected revision %d, have %d", ErrConflict, expectedRevision, s.revision)
	}
	existing, ok := s.profiles[profile.ID]
	if !ok {
		return Catalog{}, fmt.Errorf("%w: %s", ErrNotFound, profile.ID)
	}
	if normalizeID(existing.Scope) != normalizeID(profile.Scope) {
		return Catalog{}, fmt.Errorf("%w: connection scope is immutable for profile %s", ErrInvalid, profile.ID)
	}
	if existing.ExecutorID != profile.ExecutorID {
		return Catalog{}, fmt.Errorf("%w: executor_id is immutable for profile %s", ErrInvalid, profile.ID)
	}
	next := cloneProfiles(s.profiles)
	next[profile.ID] = profile
	nextRev := s.revision + 1
	err := s.persistLocked(nextRev, next, cloneDefaults(s.defaults), cloneDefaults(s.defaultModels))
	return s.applyPersist(err, nextRev, next, cloneDefaults(s.defaults), cloneDefaults(s.defaultModels))
}

// Delete removes a profile. Fails if the profile is currently a default.
func (s *Store) Delete(id string, expectedRevision int64) (Catalog, error) {
	if s == nil {
		return Catalog{}, fmt.Errorf("model profile store is not configured")
	}
	id = normalizeID(id)
	if id == "" {
		return Catalog{}, fmt.Errorf("%w: profile id is required", ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.preflightDeleteLocked(id, expectedRevision); err != nil {
		return Catalog{}, err
	}
	next := cloneProfiles(s.profiles)
	delete(next, id)
	nextRev := s.revision + 1
	err := s.persistLocked(nextRev, next, cloneDefaults(s.defaults), cloneDefaults(s.defaultModels))
	return s.applyPersist(err, nextRev, next, cloneDefaults(s.defaults), cloneDefaults(s.defaultModels))
}

// preflightDeleteLocked checks revision/existence/defaults without mutation.
// Caller must hold s.mu.
func (s *Store) preflightDeleteLocked(id string, expectedRevision int64) error {
	if expectedRevision != s.revision {
		return fmt.Errorf("%w: expected revision %d, have %d", ErrConflict, expectedRevision, s.revision)
	}
	if _, ok := s.profiles[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	for executorID, defaultID := range s.defaults {
		if defaultID == id {
			return fmt.Errorf("%w: profile %s is the default for executor %s; clear or replace the default first", ErrConflict, id, executorID)
		}
	}
	return nil
}

// PreflightDelete reports whether Delete would succeed without mutating state.
func (s *Store) PreflightDelete(id string, expectedRevision int64) error {
	if s == nil {
		return fmt.Errorf("model profile store is not configured")
	}
	id = normalizeID(id)
	if id == "" {
		return fmt.Errorf("%w: profile id is required", ErrInvalid)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.preflightDeleteLocked(id, expectedRevision)
}

// SetDefault sets or clears the default profile for an executor/client.
// Empty profileID clears the default. Account connections may be defaults for
// any client the preset supports.
func (s *Store) SetDefault(executorID, profileID string, expectedRevision int64) (Catalog, error) {
	if s == nil {
		return Catalog{}, fmt.Errorf("model profile store is not configured")
	}
	executorID = normalizeID(executorID)
	client := clientFromExecutor(executorID)
	executorID = executorFromClient(client)
	profileID = normalizeID(profileID)
	if executorID == "" {
		return Catalog{}, fmt.Errorf("%w: executor id is required", ErrInvalid)
	}
	if !SupportsExecutor(executorID) {
		return Catalog{}, fmt.Errorf("%w: %s", ErrUnsupportedExecutor, executorID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision != s.revision {
		return Catalog{}, fmt.Errorf("%w: expected revision %d, have %d", ErrConflict, expectedRevision, s.revision)
	}
	nextDefaults := cloneDefaults(s.defaults)
	nextModels := cloneDefaults(s.defaultModels)
	if profileID == "" {
		delete(nextDefaults, executorID)
		delete(nextDefaults, client)
		delete(nextModels, client)
	} else {
		profile, ok := s.profiles[profileID]
		if !ok {
			return Catalog{}, fmt.Errorf("%w: %s", ErrNotFound, profileID)
		}
		if isAccountConnection(profile) {
			spec, ok := lookupPreset(inferPresetID(profile))
			if !ok || !presetSupportsClient(spec, executorID) {
				return Catalog{}, fmt.Errorf("%w: connection %s does not support client %s", ErrInvalid, profileID, client)
			}
		} else if profile.ExecutorID != executorID {
			return Catalog{}, fmt.Errorf("%w: profile %s belongs to executor %s, not %s", ErrInvalid, profileID, profile.ExecutorID, executorID)
		}
		nextDefaults[client] = profileID
		nextDefaults[executorID] = profileID
	}
	nextRev := s.revision + 1
	nextProfiles := cloneProfiles(s.profiles)
	err := s.persistLocked(nextRev, nextProfiles, nextDefaults, nextModels)
	return s.applyPersist(err, nextRev, nextProfiles, nextDefaults, nextModels)
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var doc fileDocument
	if _, err := toml.Decode(string(raw), &doc); err != nil {
		return fmt.Errorf("decode model profiles: %w", err)
	}
	if doc.Revision < 0 {
		return fmt.Errorf("%w: revision must be >= 0", ErrInvalid)
	}
	profiles := map[string]Profile{}
	for _, profile := range doc.Profiles {
		profile = normalizeProfile(profile)
		if err := ValidateProfile(profile); err != nil {
			return fmt.Errorf("profile %q: %w", profile.ID, err)
		}
		if _, exists := profiles[profile.ID]; exists {
			return fmt.Errorf("%w: %s", ErrDuplicateID, profile.ID)
		}
		profiles[profile.ID] = profile
	}
	defaults := map[string]string{}
	for executorID, profileID := range doc.Defaults {
		executorID = normalizeID(executorID)
		profileID = normalizeID(profileID)
		if executorID == "" || profileID == "" {
			continue
		}
		client := clientFromExecutor(executorID)
		ex := executorFromClient(client)
		if !SupportsExecutor(ex) {
			return fmt.Errorf("%w: default executor %s", ErrUnsupportedExecutor, executorID)
		}
		profile, ok := profiles[profileID]
		if !ok {
			return fmt.Errorf("%w: default profile %s for %s", ErrNotFound, profileID, executorID)
		}
		if isAccountConnection(profile) {
			spec, ok := lookupPreset(inferPresetID(profile))
			if !ok || !presetSupportsClient(spec, ex) {
				return fmt.Errorf("%w: default connection %s does not support client %s", ErrInvalid, profileID, client)
			}
		} else if profile.ExecutorID != ex {
			return fmt.Errorf("%w: default profile %s belongs to %s, not %s", ErrInvalid, profileID, profile.ExecutorID, ex)
		}
		defaults[client] = profileID
		defaults[ex] = profileID
	}
	defaultModels := map[string]string{}
	for client, modelID := range doc.DefaultModels {
		client = clientFromExecutor(client)
		modelID = normalizeSpace(modelID)
		if client == "" || modelID == "" {
			continue
		}
		defaultModels[client] = modelID
	}
	s.revision = doc.Revision
	s.profiles = profiles
	s.defaults = defaults
	s.defaultModels = defaultModels
	return nil
}

func (s *Store) persistLocked(revision int64, profiles map[string]Profile, defaults, defaultModels map[string]string) error {
	doc := fileDocument{
		Revision:      revision,
		Profiles:      make([]Profile, 0, len(profiles)),
		Defaults:      map[string]string{},
		DefaultModels: map[string]string{},
	}
	for _, id := range sortedProfileIDs(profiles) {
		doc.Profiles = append(doc.Profiles, profiles[id])
	}
	for _, executorID := range sortedDefaultKeys(defaults) {
		doc.Defaults[executorID] = defaults[executorID]
	}
	for _, client := range sortedDefaultKeys(defaultModels) {
		doc.DefaultModels[client] = defaultModels[client]
	}
	var encoded strings.Builder
	if err := toml.NewEncoder(&encoded).Encode(doc); err != nil {
		return err
	}
	return s.atomicWriteFile(s.path, []byte(encoded.String()), 0o600)
}

// applyPersist aligns memory with a committed rename. Pre-rename failures leave
// memory unchanged. ErrPersistDirSync means rename committed: memory is updated
// to match disk and the uncertain-durability error is returned for the caller.
func (s *Store) applyPersist(err error, revision int64, profiles map[string]Profile, defaults, defaultModels map[string]string) (Catalog, error) {
	if err != nil && !errors.Is(err, ErrPersistDirSync) {
		return Catalog{}, err
	}
	s.profiles = profiles
	s.defaults = defaults
	s.defaultModels = defaultModels
	s.revision = revision
	return s.catalogLocked(), err
}

func (s *Store) catalogLocked() Catalog {
	profiles := make([]Profile, 0, len(s.profiles))
	for _, id := range sortedProfileIDs(s.profiles) {
		profiles = append(profiles, s.profiles[id])
	}
	return Catalog{
		Revision: s.revision,
		Profiles: profiles,
		Defaults: cloneDefaults(s.defaults),
	}
}

func sortedProfileIDs(profiles map[string]Profile) []string {
	ids := make([]string, 0, len(profiles))
	for id := range profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedDefaultKeys(defaults map[string]string) []string {
	keys := make([]string, 0, len(defaults))
	for key := range defaults {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneProfiles(in map[string]Profile) map[string]Profile {
	out := make(map[string]Profile, len(in))
	for id, profile := range in {
		out[id] = profile
	}
	return out
}

func cloneDefaults(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func lookupEnv(name string) (string, bool) {
	return os.LookupEnv(name)
}

// SetLookup overrides credential readiness probing for Views (tests).
func (s *Store) SetLookup(lookup func(string) (string, bool)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if lookup == nil {
		s.lookup = lookupEnv
		return
	}
	s.lookup = lookup
}

func (s *Store) atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if s != nil && s.hook != nil {
		if err := s.hook("before_write"); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, ".model-profiles-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if s != nil && s.hook != nil {
		if err := s.hook("after_write"); err != nil {
			return err
		}
	}
	if s != nil && s.hook != nil {
		if err := s.hook("before_rename"); err != nil {
			return err
		}
	}
	// Rename is the commit point: on success the new bytes are the durable file
	// content. Parent-directory sync is best-effort durability of the dir entry.
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	if s != nil && s.hook != nil {
		if err := s.hook("after_rename"); err != nil {
			return fmt.Errorf("%w: %v", ErrPersistDirSync, err)
		}
	}
	if s != nil && s.hook != nil {
		if err := s.hook("before_dirsync"); err != nil {
			return fmt.Errorf("%w: %v", ErrPersistDirSync, err)
		}
	}
	if err := s.syncParentDir(dir); err != nil {
		return fmt.Errorf("%w: %v", ErrPersistDirSync, err)
	}
	if s != nil && s.hook != nil {
		if err := s.hook("after_dirsync"); err != nil {
			return fmt.Errorf("%w: %v", ErrPersistDirSync, err)
		}
	}
	return nil
}

func (s *Store) syncParentDir(dir string) error {
	if s != nil && s.dirSync != nil {
		return s.dirSync(dir)
	}
	dirFile, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer dirFile.Close()
	return dirFile.Sync()
}
