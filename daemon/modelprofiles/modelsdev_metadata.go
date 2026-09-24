package modelprofiles

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	modelsDevTTL       = 24 * time.Hour
	modelsDevMaxBytes  = 16 << 20
	modelsDevTimeout   = 10 * time.Second
	modelsDevCacheFile = "models-dev-metadata.json"
)

var modelsDevURL = "https://models.dev/api.json"

type modelsDevCatalog struct {
	mu        sync.RWMutex
	UpdatedAt time.Time                                       `json:"updated_at"`
	Models    map[string]map[string]modelPresentationMetadata `json:"models"`
	path      string
	client    *http.Client
}

func newModelsDevCatalog(path string) *modelsDevCatalog {
	return &modelsDevCatalog{path: path, client: &http.Client{Timeout: modelsDevTimeout}, Models: map[string]map[string]modelPresentationMetadata{}}
}

func (c *modelsDevCatalog) load() error {
	if c == nil || c.path == "" {
		return nil
	}
	if info, statErr := os.Stat(c.path); statErr == nil && info.Size() > modelsDevMaxBytes {
		return fmt.Errorf("models.dev metadata cache exceeds size limit")
	}
	raw, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(raw) > modelsDevMaxBytes {
		return fmt.Errorf("models.dev metadata cache exceeds size limit")
	}
	var disk modelsDevCatalog
	if err := json.Unmarshal(raw, &disk); err != nil {
		return err
	}
	if disk.Models == nil {
		return fmt.Errorf("models.dev metadata cache has no models")
	}
	c.mu.Lock()
	c.UpdatedAt, c.Models = disk.UpdatedAt, disk.Models
	c.mu.Unlock()
	return nil
}

func (c *modelsDevCatalog) stale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.UpdatedAt.IsZero() || time.Since(c.UpdatedAt) >= modelsDevTTL
}

func (c *modelsDevCatalog) lookup(provider, id string) (modelPresentationMetadata, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	items := c.Models[strings.ToLower(strings.TrimSpace(provider))]
	item, ok := items[strings.TrimSpace(id)]
	return item, c.UpdatedAt, ok
}

func (c *modelsDevCatalog) refresh(ctx context.Context) error {
	if c == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, modelsDevTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsDevURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("models.dev metadata status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, modelsDevMaxBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > modelsDevMaxBytes {
		return fmt.Errorf("models.dev metadata exceeds size limit")
	}
	parsed, err := parseModelsDevMetadata(raw)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	disk := modelsDevCatalog{UpdatedAt: now, Models: parsed}
	data, err := json.Marshal(disk)
	if err != nil {
		return err
	}
	if c.path != "" {
		if err := atomicWriteModelsDev(c.path, data); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.UpdatedAt, c.Models = now, parsed
	c.mu.Unlock()
	return nil
}

// RefreshModelsDevMetadata performs an explicit bounded refresh. It only
// updates presentation metadata; Provider discovery remains authoritative.
func (o *Owner) RefreshModelsDevMetadata(ctx context.Context) error {
	if o == nil || o.modelsDev == nil {
		return fmt.Errorf("models.dev metadata unavailable")
	}
	return o.modelsDev.refresh(ctx)
}

func atomicWriteModelsDev(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".models-dev-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func parseModelsDevMetadata(raw []byte) (map[string]map[string]modelPresentationMetadata, error) {
	var providers map[string]struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(raw, &providers); err != nil {
		return nil, err
	}
	out := map[string]map[string]modelPresentationMetadata{}
	for provider, value := range providers {
		for id, rawModel := range value.Models {
			var model map[string]json.RawMessage
			if json.Unmarshal(rawModel, &model) != nil {
				continue
			}
			meta := modelPresentationMetadata{DisplayName: jsonString(model["name"]), ReleaseDate: jsonString(model["release_date"])}
			meta.ContextWindow = jsonInt(model["context"])
			if meta.ContextWindow == 0 {
				meta.ContextWindow = jsonInt(model["context_window"])
			}
			if meta.ContextWindow == 0 {
				var limit map[string]json.RawMessage
				if json.Unmarshal(model["limit"], &limit) == nil {
					meta.ContextWindow = jsonInt(limit["context"])
				}
			}
			meta.InputPricePerMillion = jsonFloat(model["input"])
			meta.OutputPricePerMillion = jsonFloat(model["output"])
			if cost, ok := model["cost"]; ok {
				var c map[string]json.RawMessage
				_ = json.Unmarshal(cost, &c)
				if meta.InputPricePerMillion == nil {
					meta.InputPricePerMillion = jsonFloat(c["input"])
				}
				if meta.OutputPricePerMillion == nil {
					meta.OutputPricePerMillion = jsonFloat(c["output"])
				}
			}
			if v, ok := model["temperature"]; ok {
				var b bool
				if json.Unmarshal(v, &b) == nil {
					meta.TemperatureSupported = &b
				}
			}
			meta.Modalities = jsonStrings(model["modalities"])
			for _, effort := range jsonStrings(model["reasoning_efforts"]) {
				meta.SupportedReasoningLevels = append(meta.SupportedReasoningLevels, CodexReasoningEffortPreset{Effort: effort})
			}
			meta = normalizeModelPresentationMetadata(meta)
			if out[strings.ToLower(provider)] == nil {
				out[strings.ToLower(provider)] = map[string]modelPresentationMetadata{}
			}
			out[strings.ToLower(provider)][id] = meta
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("models.dev metadata has no providers")
	}
	return out, nil
}

func jsonString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return strings.TrimSpace(s)
}
func jsonFloat(raw json.RawMessage) *float64 {
	var n float64
	if len(raw) == 0 || json.Unmarshal(raw, &n) != nil || n < 0 {
		return nil
	}
	return &n
}
func jsonInt(raw json.RawMessage) int64 {
	var n int64
	_ = json.Unmarshal(raw, &n)
	if n < 0 {
		return 0
	}
	return n
}
func jsonStrings(raw json.RawMessage) []string {
	var a []string
	if json.Unmarshal(raw, &a) == nil {
		return a
	}
	var o map[string][]string
	if json.Unmarshal(raw, &o) == nil {
		for _, values := range o {
			a = append(a, values...)
		}
	}
	return a
}
