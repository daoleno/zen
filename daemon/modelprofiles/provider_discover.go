package modelprofiles

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// modelDiscoveryCache stores last-known-good /v1/models id lists per connection.
type modelDiscoveryCache struct {
	mu      sync.Mutex
	entries map[string]discoveryEntry
	now     func() time.Time
	client  *http.Client
	ttl     time.Duration
	// saveMu serializes the entire snapshot→write→rename→dirsync pipeline so
	// an older writer cannot rename after a newer writer.
	saveMu sync.Mutex
	// saveSeq is monotonic claim order under saveMu (test/observability).
	saveSeq atomic.Uint64
	// saveHook is a test barrier seam: after_claim, before_write, after_rename.
	saveHook func(phase string)
}

type discoveryEntry struct {
	IDs       []string  `json:"ids,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
	LastGood  []string  `json:"last_good,omitempty"`
	// Disabled is the durable client-side support allowlist: model ids the
	// client explicitly turned off. Everything discovered is supported unless
	// listed here, so a catalog refresh never silently re-enables an
	// explicitly disabled model while genuinely new models default enabled.
	Disabled []string `json:"disabled,omitempty"`
	Err      string   `json:"err,omitempty"`
	Seq      uint64   `json:"seq,omitempty"`
}

type durableDiscoveryFile struct {
	SchemaVersion int                       `json:"schema_version"`
	Entries       map[string]discoveryEntry `json:"entries"`
}

const discoverySchemaVersion = 1

func newModelDiscoveryCache() *modelDiscoveryCache {
	return &modelDiscoveryCache{
		entries: map[string]discoveryEntry{},
		now:     time.Now,
		client:  NewSafeHTTPClient(15 * time.Second),
		ttl:     DiscoveryTTL,
	}
}

func (c *modelDiscoveryCache) load(path string) error {
	if c == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%w: read: %v", ErrDiscoveryCacheInvalid, err)
	}
	var doc durableDiscoveryFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrDiscoveryCacheInvalid, err)
	}
	if doc.SchemaVersion != discoverySchemaVersion {
		return fmt.Errorf("%w: unsupported schema_version %d", ErrDiscoveryCacheInvalid, doc.SchemaVersion)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]discoveryEntry{}
	var maxSeq uint64
	for k, e := range doc.Entries {
		c.entries[normalizeID(k)] = e
		if e.Seq > maxSeq {
			maxSeq = e.Seq
		}
	}
	c.saveSeq.Store(maxSeq)
	return nil
}

func (c *modelDiscoveryCache) setSaveHook(hook func(phase string)) {
	if c == nil {
		return
	}
	c.saveMu.Lock()
	c.saveHook = hook
	c.saveMu.Unlock()
}

func (c *modelDiscoveryCache) save(path string) error {
	if c == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	// Entire durable ownership is ordered under saveMu: claim → snapshot →
	// encode → temp write → rename → directory sync. Stale rename is impossible.
	c.saveMu.Lock()
	defer c.saveMu.Unlock()

	seq := c.saveSeq.Add(1)
	if c.saveHook != nil {
		c.saveHook("after_claim")
	}

	c.mu.Lock()
	doc := durableDiscoveryFile{
		SchemaVersion: discoverySchemaVersion,
		Entries:       map[string]discoveryEntry{},
	}
	for k, e := range c.entries {
		e.Seq = seq
		doc.Entries[k] = e
	}
	c.mu.Unlock()

	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: encode: %v", ErrDiscoveryPersistFailed, err)
	}
	if c.saveHook != nil {
		c.saveHook("before_write")
	}
	if err := atomicWriteDiscoveryFile(path, raw); err != nil {
		return err
	}
	if c.saveHook != nil {
		c.saveHook("after_rename")
	}
	return nil
}

func atomicWriteDiscoveryFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("%w: mkdir: %v", ErrDiscoveryPersistFailed, err)
	}
	tmp, err := os.CreateTemp(dir, ".provider-discovery-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: temp: %v", ErrDiscoveryPersistFailed, err)
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
		return fmt.Errorf("%w: write: %v", ErrDiscoveryPersistFailed, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%w: chmod: %v", ErrDiscoveryPersistFailed, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%w: sync: %v", ErrDiscoveryPersistFailed, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%w: close: %v", ErrDiscoveryPersistFailed, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("%w: rename: %v", ErrDiscoveryPersistFailed, err)
	}
	cleanup = false
	if err := syncParentDir(dir); err != nil {
		return fmt.Errorf("%w: %w: %v", ErrDiscoveryPersistFailed, ErrPersistDirSync, err)
	}
	return nil
}

func syncParentDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (c *modelDiscoveryCache) get(key string) (discoveryEntry, bool) {
	if c == nil {
		return discoveryEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	return e, ok
}

func (c *modelDiscoveryCache) put(key string, ids []string, fetchErr error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	prev := c.entries[key]
	next := discoveryEntry{FetchedAt: c.now(), Seq: c.saveSeq.Load()}
	// The explicit support allowlist survives refresh: disabled ids are never
	// re-enabled by a rediscovery, and new ids are not disabled.
	next.Disabled = append([]string{}, prev.Disabled...)
	if fetchErr != nil {
		next.Err = fetchErr.Error()
		next.LastGood = append([]string{}, prev.LastGood...)
		if len(next.LastGood) == 0 {
			next.LastGood = append([]string{}, prev.IDs...)
		}
		next.IDs = append([]string{}, next.LastGood...)
	} else {
		next.IDs = append([]string{}, ids...)
		next.LastGood = append([]string{}, ids...)
	}
	c.entries[key] = next
}

// setDisabled replaces the durable support allowlist (disabled ids) of one
// connection without treating the write as a rediscovery: catalog ids,
// timestamps, and last-good state are preserved.
func (c *modelDiscoveryCache) setDisabled(key string, disabled []string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return false
	}
	e.Disabled = append([]string{}, disabled...)
	c.entries[key] = e
	return true
}

func (c *modelDiscoveryCache) fresh(key string) bool {
	e, ok := c.get(key)
	if !ok || e.FetchedAt.IsZero() {
		return false
	}
	ttl := c.ttl
	if ttl <= 0 {
		ttl = DiscoveryTTL
	}
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	return now().Sub(e.FetchedAt) < ttl
}

// DiscoverProviderModelsResult is live discovery plus honest LKG persistence.
type DiscoverProviderModelsResult struct {
	Entries            []ProviderModelEntry
	PersistenceDurable bool
	PersistenceWarning string
}

// DiscoverProviderModels performs SSRF-safe live /v1/models (or /models) lookup
// and returns availability entries intersected with the trusted/bundled set.
// On refresh failure, last-known-good ids are retained. Live availability may
// be returned with a persistence warning; durable LKG is never claimed on write failure.
func (o *Owner) DiscoverProviderModels(connectionID string, force bool) ([]ProviderModelEntry, error) {
	res, err := o.DiscoverProviderModelsDetailed(connectionID, force)
	return res.Entries, err
}

// DiscoverProviderModelsDetailed returns entries plus LKG persistence honesty.
func (o *Owner) DiscoverProviderModelsDetailed(connectionID string, force bool) (DiscoverProviderModelsResult, error) {
	out := DiscoverProviderModelsResult{}
	if o == nil || !o.started {
		return out, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	connectionID = normalizeID(connectionID)
	profile, err := o.GetProfile(connectionID)
	if err != nil {
		return out, err
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
	cache := o.discovery
	loadWarn := o.discoveryLoadWarning
	o.mu.Unlock()
	if loadWarn != nil {
		out.PersistenceWarning = loadWarn.Error()
	}

	key := connectionID
	if !force && cache.fresh(key) {
		e, _ := cache.get(key)
		out.Entries = projectModelEntries(trusted, manual, e.IDs, e.LastGood, e.Disabled, e.Err == "")
		out.PersistenceDurable = len(e.LastGood) > 0 && out.PersistenceWarning == ""
		return out, nil
	}

	probe := profile
	if isAccountConnection(profile) {
		client := executorFromClient(profile.Client)
		compiled, cErr := CompileConnectionTarget(profile, client, "")
		if cErr != nil {
			out.Entries = projectModelEntries(trusted, manual, nil, nil, nil, false)
			return out, cErr
		}
		probe = compiled
	}

	ids, discoverErr := fetchUpstreamModelIDs(context.Background(), cache.client, probe, o.creds, o.lookup)
	cache.put(key, ids, discoverErr)
	if o.discoveryPath != "" {
		if serr := cache.save(o.discoveryPath); serr != nil {
			out.PersistenceDurable = false
			out.PersistenceWarning = serr.Error()
		} else {
			out.PersistenceDurable = true
			out.PersistenceWarning = ""
		}
	}
	e, _ := cache.get(key)
	out.Entries = projectModelEntries(trusted, manual, e.IDs, e.LastGood, e.Disabled, discoverErr == nil)
	if !out.PersistenceDurable {
		// Never claim LKG source durability when write failed — force bundled
		// labels for entries that would otherwise say lkg from this refresh.
		for i := range out.Entries {
			if out.Entries[i].Source == ModelSourceLKG && out.PersistenceWarning != "" && discoverErr == nil {
				// Live discovery succeeded but LKG persist failed: keep discovered
				// availability, do not advertise durable lkg.
				if out.Entries[i].Available {
					out.Entries[i].Source = ModelSourceDiscovered
				} else {
					out.Entries[i].Source = ModelSourceBundled
				}
			}
		}
	}
	if discoverErr != nil && len(e.LastGood) == 0 && len(trusted) == 0 {
		return out, discoverErr
	}
	return out, nil
}

func projectModelEntries(trusted []string, manual string, discovered, lkg, disabled []string, liveOK bool) []ProviderModelEntry {
	available := map[string]struct{}{}
	for _, id := range discovered {
		id = normalizeSpace(id)
		if id == "" {
			continue
		}
		available[id] = struct{}{}
	}
	lkgSet := map[string]struct{}{}
	for _, id := range lkg {
		id = normalizeSpace(id)
		if id != "" {
			lkgSet[id] = struct{}{}
		}
	}
	disabledSet := map[string]struct{}{}
	for _, id := range disabled {
		id = normalizeSpace(id)
		if id != "" {
			disabledSet[id] = struct{}{}
		}
	}

	seen := map[string]struct{}{}
	out := make([]ProviderModelEntry, 0, len(trusted)+1)
	add := func(id, source string, avail bool) {
		id = normalizeSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		// The client support allowlist is authoritative: an explicitly
		// disabled model is never exposed as supported, even when the gateway
		// still lists it.
		if _, off := disabledSet[id]; off {
			avail = false
		}
		out = append(out, ProviderModelEntry{ID: id, Available: avail, Source: source})
	}
	for _, id := range trusted {
		_, live := available[id]
		_, wasLKG := lkgSet[id]
		switch {
		case liveOK && live:
			add(id, ModelSourceDiscovered, true)
		case wasLKG:
			add(id, ModelSourceLKG, true)
		default:
			add(id, ModelSourceBundled, liveOK && live)
		}
	}
	// Custom endpoints do not have a daemon-curated allowlist. Their live model
	// catalog is the source of truth, so project every discovered/LKG id instead
	// of silently returning an empty list.
	if len(trusted) == 0 {
		for _, id := range discovered {
			add(id, ModelSourceDiscovered, true)
		}
		for _, id := range lkg {
			if _, live := available[id]; !live {
				add(id, ModelSourceLKG, true)
			}
		}
	}
	if manual != "" {
		if _, ok := seen[manual]; !ok {
			_, live := available[manual]
			src := ModelSourceManual
			if liveOK && live {
				src = ModelSourceDiscovered
			} else if _, ok := lkgSet[manual]; ok {
				src = ModelSourceLKG
			}
			add(manual, src, live || !liveOK)
		}
	}
	return out
}

func fetchUpstreamModelIDs(ctx context.Context, client *http.Client, profile Profile, store CredentialStore, lookup func(string) (string, bool)) ([]string, error) {
	if client == nil {
		client = NewSafeHTTPClient(15 * time.Second)
	}
	base := strings.TrimRight(normalizeSpace(profile.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("%w: base_url required for discovery", ErrInvalid)
	}
	if err := ValidateUpstreamBaseURL(base); err != nil {
		return nil, err
	}
	candidates := modelDiscoveryURLs(base, profile.Protocol)
	var lastErr error
	for _, endpoint := range candidates {
		ids, err := getModelsOnce(ctx, client, endpoint, profile, store, lookup)
		if err == nil {
			return ids, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%w: discovery failed", ErrUpstreamInvalid)
	}
	return nil, lastErr
}

func modelDiscoveryURLs(base, protocol string) []string {
	base = strings.TrimRight(base, "/")
	switch normalizeID(protocol) {
	case ProtocolAnthropicMessages:
		return []string{base + "/v1/models", base + "/models"}
	default:
		if strings.HasSuffix(base, "/v1") {
			return []string{base + "/models"}
		}
		return []string{base + "/v1/models", base + "/models"}
	}
}

func getModelsOnce(ctx context.Context, client *http.Client, endpoint string, profile Profile, store CredentialStore, lookup func(string) (string, bool)) ([]string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	if err := applyDiscoveryAuth(req, profile, store, lookup); err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: discovery status %d", ErrUpstreamInvalid, resp.StatusCode)
	}
	return parseModelsResponse(body)
}

func applyDiscoveryAuth(req *http.Request, profile Profile, store CredentialStore, lookup func(string) (string, bool)) error {
	switch normalizeID(profile.AuthMode) {
	case AuthModeNone, AuthModeNativePassthrough:
		return nil
	case AuthModeBearerEnv:
		val, err := resolveProviderSecret(CredentialRefFor(profile.ID), profile.CredentialEnv, store, lookup)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(val))
		return nil
	case AuthModeXAPIKeyEnv:
		val, err := resolveProviderSecret(CredentialRefFor(profile.ID), profile.CredentialEnv, store, lookup)
		if err != nil {
			return err
		}
		req.Header.Set("x-api-key", strings.TrimSpace(val))
		req.Header.Set("anthropic-version", "2023-06-01")
		return nil
	default:
		return fmt.Errorf("%w: auth mode %q", ErrInvalid, profile.AuthMode)
	}
}

func parseModelsResponse(body []byte) ([]string, error) {
	var obj struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("%w: models json: %v", ErrRequestBodyMalformed, err)
	}
	out := make([]string, 0, len(obj.Data)+len(obj.Models))
	seen := map[string]struct{}{}
	add := func(id string) {
		id = normalizeSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, m := range obj.Data {
		add(m.ID)
	}
	for _, m := range obj.Models {
		add(m.ID)
	}
	return out, nil
}
