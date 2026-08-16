package modelprofiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Takeover projection markers. Everything between the two markers is
// Zen-owned and is replaced/removed atomically; unrelated user config bytes
// (comments, formatting, other keys, projects, trust) are preserved exactly.
const (
	takeoverMarkerOpen  = "# >>> zen-gateway: managed by Zen. Do not edit. >>>"
	takeoverMarkerClose = "# <<< zen-gateway <<<"
)

// TakeoverState is the durable takeover record (never secrets).
type TakeoverState struct {
	Enabled               bool   `json:"enabled"`
	ConfigPath            string `json:"config_path"`
	BackupPath            string `json:"backup_path"`
	ListenAddr            string `json:"listen_addr"`
	ProviderName          string `json:"provider_name"`
	OriginalProviderValue string `json:"original_provider_value,omitempty"`
	EnabledAt             string `json:"enabled_at,omitempty"`
}

// TakeoverStatus is the truthful control-plane view of takeover state.
type TakeoverStatus struct {
	State              string `json:"state"` // active | inactive | drifted | broken
	Detail             string `json:"detail,omitempty"`
	ConfigPath         string `json:"config_path,omitempty"`
	BackupPath         string `json:"backup_path,omitempty"`
	ListenAddr         string `json:"listen_addr,omitempty"`
	ProviderName       string `json:"provider_name,omitempty"`
	UpstreamProfileID  string `json:"upstream_profile_id,omitempty"`
	GatewayListening   bool   `json:"gateway_listening"`
	Enabled            bool   `json:"enabled"`
	RestoreAvailable   bool   `json:"restore_available"`
}

const (
	TakeoverStateActive   = "active"
	TakeoverStateInactive = "inactive"
	TakeoverStateDrifted  = "drifted"
	TakeoverStateBroken   = "broken"
)

// Takeover manages the machine-level Codex gateway projection into the CLI's
// native config (~/.codex/config.toml by default, CODEX_HOME-aware). It never
// touches the user's model, effort, or unrelated config; it preserves the
// exact pre-takeover bytes in a durable backup and only ever writes the
// marked Zen-owned projection.
type Takeover struct {
	statePath string
	backupDir string
	configPath string
	gateway   *Gateway
}

// NewTakeover constructs the takeover manager. configPath is the Codex config
// file (honor CODEX_HOME); stateDir is the daemon-owned gateway state dir.
func NewTakeover(configPath, stateDir string, gateway *Gateway) *Takeover {
	return &Takeover{
		configPath: strings.TrimSpace(configPath),
		statePath:  filepath.Join(strings.TrimSpace(stateDir), "state.json"),
		backupDir:  filepath.Join(strings.TrimSpace(stateDir), "backups"),
		gateway:    gateway,
	}
}

// ConfigPath returns the managed Codex config path.
func (t *Takeover) ConfigPath() string {
	if t == nil {
		return ""
	}
	return t.configPath
}

// StatePath returns the durable takeover state path.
func (t *Takeover) StatePath() string {
	if t == nil {
		return ""
	}
	return t.statePath
}

// LoadState reads the durable takeover state; missing state means inactive.
func (t *Takeover) LoadState() (TakeoverState, error) {
	if t == nil || t.statePath == "" {
		return TakeoverState{}, nil
	}
	raw, err := os.ReadFile(t.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return TakeoverState{}, nil
		}
		return TakeoverState{}, err
	}
	var state TakeoverState
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&state); err != nil {
		return TakeoverState{}, fmt.Errorf("%w: takeover state: %v", ErrRouteSnapshotInvalid, err)
	}
	return state, nil
}

func (t *Takeover) persistState(state TakeoverState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(t.statePath, raw, 0o600)
}

// Projection returns the exact Zen-owned config block for the gateway.
func (t *Takeover) Projection(listenAddr string) string {
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		listenAddr = DefaultGatewayListenAddr
	}
	var b strings.Builder
	b.WriteString(takeoverMarkerOpen + "\n")
	b.WriteString("[model_providers." + GatewayProviderName + "]\n")
	b.WriteString("name = \"" + GatewayProviderName + "\"\n")
	b.WriteString("base_url = \"http://" + listenAddr + "/v1\"\n")
	b.WriteString("wire_api = \"responses\"\n")
	b.WriteString("requires_openai_auth = false\n")
	b.WriteString(takeoverMarkerClose + "\n")
	return b.String()
}

// projectedProviderLine returns the top-level model_provider assignment the
// takeover installs.
func projectedProviderLine() string {
	return "model_provider = \"" + GatewayProviderName + "\""
}

// Enable activates the machine-level takeover: exact backup, then an atomic
// surgical projection that preserves every unrelated byte of the user's
// config. Idempotent: re-enabling an already-matching projection is a no-op.
// Missing config is created; malformed config fails safe.
func (t *Takeover) Enable(listenAddr string) (TakeoverStatus, error) {
	if t == nil {
		return TakeoverStatus{}, fmt.Errorf("%w: takeover not configured", ErrInvalid)
	}
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		listenAddr = DefaultGatewayListenAddr
	}
	projection := t.Projection(listenAddr)
	providerLine := projectedProviderLine()

	current, missing, err := readConfigBytes(t.configPath)
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	if !missing {
		if err := validateConfigTOML(current); err != nil {
			return TakeoverStatus{State: TakeoverStateDrifted, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
	}

	state, err := t.LoadState()
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	originalValue := strings.TrimSpace(state.OriginalProviderValue)
	if originalValue == "" && !missing {
		originalValue = topLevelProviderValue(current)
	}

	projected, conflict, err := projectConfig(current, projection, providerLine)
	if err != nil {
		return TakeoverStatus{State: TakeoverStateDrifted, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	if conflict != "" {
		return TakeoverStatus{State: TakeoverStateDrifted, Detail: conflict, ConfigPath: t.configPath}, fmt.Errorf("%w: %s", ErrInvalid, conflict)
	}
	if bytes.Equal(projected, current) {
		// Already projected: idempotent no-op.
		state.Enabled = true
		state.ConfigPath = t.configPath
		state.ListenAddr = listenAddr
		state.ProviderName = GatewayProviderName
		if err := t.persistState(state); err != nil {
			return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
		return t.Status(), nil
	}

	backupPath := ""
	if state.BackupPath != "" {
		if _, statErr := os.Stat(state.BackupPath); statErr == nil {
			backupPath = state.BackupPath
		}
	}
	if backupPath == "" {
		if err := os.MkdirAll(t.backupDir, 0o700); err != nil {
			return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
		backupPath = filepath.Join(t.backupDir, "backup-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".toml")
		if err := writeAtomicFile(backupPath, current, 0o600); err != nil {
			return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
	}

	if err := writeAtomicFile(t.configPath, projected, 0o600); err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	if err := validateConfigTOML(projected); err != nil {
		// Never leave a broken projection: roll the exact backup back.
		_ = writeAtomicFile(t.configPath, current, 0o600)
		return TakeoverStatus{State: TakeoverStateBroken, Detail: "projection produced invalid TOML: " + err.Error(), ConfigPath: t.configPath}, err
	}

	state = TakeoverState{
		Enabled:               true,
		ConfigPath:            t.configPath,
		BackupPath:            backupPath,
		ListenAddr:            listenAddr,
		ProviderName:          GatewayProviderName,
		OriginalProviderValue: originalValue,
		EnabledAt:             time.Now().UTC().Format(time.RFC3339),
	}
	if err := t.persistState(state); err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	return t.Status(), nil
}

// Disable removes only the Zen-owned projection: the marked block and the
// projected model_provider line (restoring the pre-takeover value when it was
// recorded). Unrelated user changes are preserved. A user-edited projected
// line is a conflict and is left in place, reported as drifted.
func (t *Takeover) Disable() (TakeoverStatus, error) {
	if t == nil {
		return TakeoverStatus{}, fmt.Errorf("%w: takeover not configured", ErrInvalid)
	}
	current, _, err := readConfigBytes(t.configPath)
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	state, err := t.LoadState()
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	providerLine := projectedProviderLine()
	conflicts := []string{}
	withoutBlock := removeProjectionBlock(string(current))
	restored, conflict := restoreProviderLine(withoutBlock, providerLine, strings.TrimSpace(state.OriginalProviderValue))
	if conflict != "" {
		conflicts = append(conflicts, conflict)
	}
	if bytes.Equal([]byte(restored), current) {
		// Nothing to remove; the projection was already gone.
		state.Enabled = false
		if err := t.persistState(state); err != nil {
			return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
		status := t.Status()
		if len(conflicts) > 0 {
			status.State = TakeoverStateDrifted
			status.Detail = strings.Join(conflicts, "; ")
		}
		return status, nil
	}
	if err := validateConfigTOML([]byte(restored)); err != nil {
		return TakeoverStatus{State: TakeoverStateDrifted, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	if err := writeAtomicFile(t.configPath, []byte(restored), 0o600); err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	state.Enabled = false
	if err := t.persistState(state); err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	status := t.Status()
	if len(conflicts) > 0 {
		status.State = TakeoverStateDrifted
		status.Detail = strings.Join(conflicts, "; ")
	}
	return status, nil
}

// Repair re-applies the Zen-owned projection over the current config while
// preserving unrelated user changes. It is used at daemon restart when the
// durable state claims takeover but the live config drifted (or the daemon
// crashed mid-write). Never creates a second backup.
func (t *Takeover) Repair(listenAddr string) (TakeoverStatus, error) {
	if t == nil {
		return TakeoverStatus{}, fmt.Errorf("%w: takeover not configured", ErrInvalid)
	}
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		listenAddr = DefaultGatewayListenAddr
	}
	current, missing, err := readConfigBytes(t.configPath)
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	if !missing {
		if err := validateConfigTOML(current); err != nil {
			return TakeoverStatus{State: TakeoverStateDrifted, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
	}
	projected, conflict, err := projectConfig(current, t.Projection(listenAddr), projectedProviderLine())
	if err != nil || conflict != "" {
		detail := err.Error()
		if conflict != "" {
			detail = conflict
		}
		return TakeoverStatus{State: TakeoverStateDrifted, Detail: detail, ConfigPath: t.configPath}, nil
	}
	if !bytes.Equal(projected, current) {
		if err := writeAtomicFile(t.configPath, projected, 0o600); err != nil {
			return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
	}
	state, err := t.LoadState()
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	state.Enabled = true
	state.ConfigPath = t.configPath
	state.ListenAddr = listenAddr
	state.ProviderName = GatewayProviderName
	if err := t.persistState(state); err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	return t.Status(), nil
}

// RestoreBackup rolls the exact pre-takeover backup over the current config.
// This is the recorded rollback procedure; it discards any changes made to the
// config while takeover was enabled.
func (t *Takeover) RestoreBackup() (TakeoverStatus, error) {
	if t == nil {
		return TakeoverStatus{}, fmt.Errorf("%w: takeover not configured", ErrInvalid)
	}
	state, err := t.LoadState()
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	if strings.TrimSpace(state.BackupPath) == "" {
		return TakeoverStatus{State: TakeoverStateDrifted, Detail: "no takeover backup exists", ConfigPath: t.configPath}, nil
	}
	backup, err := os.ReadFile(state.BackupPath)
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: "read backup: " + err.Error(), ConfigPath: t.configPath}, err
	}
	if len(backup) > 0 {
		if err := validateConfigTOML(backup); err != nil {
			return TakeoverStatus{State: TakeoverStateDrifted, Detail: "backup is not valid TOML: " + err.Error(), ConfigPath: t.configPath}, err
		}
	}
	if err := writeAtomicFile(t.configPath, backup, 0o600); err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	state.Enabled = false
	if err := t.persistState(state); err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	return t.Status(), nil
}

// Status computes the truthful takeover status.
func (t *Takeover) Status() TakeoverStatus {
	status := TakeoverStatus{
		State:            TakeoverStateInactive,
		GatewayListening: t != nil && t.gateway != nil && t.gateway.Listening(),
	}
	if t == nil {
		return status
	}
	status.ConfigPath = t.configPath
	state, err := t.LoadState()
	if err != nil {
		status.State = TakeoverStateBroken
		status.Detail = "state unreadable: " + err.Error()
		return status
	}
	status.Enabled = state.Enabled
	status.BackupPath = state.BackupPath
	status.ListenAddr = state.ListenAddr
	status.ProviderName = state.ProviderName
	if !state.Enabled {
		return status
	}
	status.RestoreAvailable = strings.TrimSpace(state.BackupPath) != ""
	if !status.GatewayListening {
		status.State = TakeoverStateBroken
		status.Detail = "gateway is not listening on " + status.ListenAddr
		return status
	}
	if up, ok := t.gateway.Upstream(); ok {
		status.UpstreamProfileID = up.ProfileID
	}
	current, missing, err := readConfigBytes(t.configPath)
	if err != nil {
		status.State = TakeoverStateBroken
		status.Detail = "config unreadable: " + err.Error()
		return status
	}
	if missing {
		status.State = TakeoverStateDrifted
		status.Detail = "managed config is missing"
		return status
	}
	if err := validateConfigTOML(current); err != nil {
		status.State = TakeoverStateDrifted
		status.Detail = "managed config is malformed: " + err.Error()
		return status
	}
	expected := t.Projection(state.ListenAddr)
	if !bytes.Contains(current, []byte(expected)) {
		status.State = TakeoverStateDrifted
		status.Detail = "Zen projection block is missing or edited"
		return status
	}
	if !hasExactProviderLine(current, projectedProviderLine()) {
		status.State = TakeoverStateDrifted
		status.Detail = "model_provider line was changed outside Zen"
		return status
	}
	status.State = TakeoverStateActive
	return status
}

// projectConfig returns current with the Zen projection block and the
// top-level model_provider line installed, preserving every other byte exactly
// (no blank lines are invented, so disable can restore byte-for-byte).
// conflict reports a fail-safe condition (multiple uncommented model_provider
// lines).
func projectConfig(current []byte, projection, providerLine string) ([]byte, string, error) {
	content := string(current)
	withoutBlock := removeProjectionBlock(content)
	lines := strings.Split(withoutBlock, "\n")
	// A trailing newline yields a final empty split element; the final join
	// re-emits exactly one trailing newline, so trailing empties are not part
	// of the content model (interior blank lines are preserved untouched).
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	providerIndex := -1
	uncommented := 0
	firstSection := -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") && !strings.HasPrefix(trimmed, "[[") {
			if firstSection < 0 {
				firstSection = index
			}
			continue
		}
		if strings.HasPrefix(trimmed, "model_provider") {
			key := trimmed
			if eq := strings.Index(key, "="); eq >= 0 {
				key = strings.TrimSpace(key[:eq])
			}
			if key != "model_provider" {
				continue
			}
			uncommented++
			if providerIndex < 0 {
				providerIndex = index
			}
		}
	}
	if uncommented > 1 {
		return nil, "multiple model_provider assignments exist; refusing to clobber", nil
	}
	// Canonical block position: directly before the first section, walking
	// back over the blank separator so the block lands right after the last
	// top-level key. Deterministic across repeated enables (idempotency).
	insertAt := firstSection
	for insertAt > 0 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}
	if insertAt < 0 {
		insertAt = len(lines)
	}
	blockLines := strings.Split(strings.TrimRight(projection, "\n"), "\n")
	var merged []string
	if uncommented == 1 {
		// Replace the existing top-level line in place; the block goes at the
		// canonical position.
		lines[providerIndex] = providerLine
		merged = make([]string, 0, len(lines)+len(blockLines))
		merged = append(merged, lines[:insertAt]...)
		merged = append(merged, blockLines...)
		merged = append(merged, lines[insertAt:]...)
	} else {
		// Insert the top-level provider line at the canonical position,
		// followed by the projection block.
		merged = make([]string, 0, len(lines)+len(blockLines)+1)
		merged = append(merged, lines[:insertAt]...)
		merged = append(merged, providerLine)
		merged = append(merged, blockLines...)
		merged = append(merged, lines[insertAt:]...)
	}
	// Exactly one trailing newline, preserving the original byte-for-byte on
	// disable (the split/join of a trailing newline yields a final empty
	// element, so a bare +"\n" would double it).
	return []byte(strings.TrimRight(strings.Join(merged, "\n"), "\n") + "\n"), "", nil
}

// removeProjectionBlock strips the marked Zen-owned block (by marker lines),
// preserving everything else byte-for-byte.
func removeProjectionBlock(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inside := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == takeoverMarkerOpen {
			inside = true
			continue
		}
		if inside {
			if trimmed == takeoverMarkerClose {
				inside = false
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// restoreProviderLine replaces the projected model_provider line with the
// original value (or removes it), unless the line was edited outside Zen.
func restoreProviderLine(content, projectedLine, originalValue string) (string, string) {
	lines := strings.Split(content, "\n")
	providerIndex := -1
	uncommented := 0
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == projectedLine {
			providerIndex = index
			uncommented++
			continue
		}
		if strings.HasPrefix(trimmed, "model_provider") {
			key := trimmed
			if eq := strings.Index(key, "="); eq >= 0 {
				key = strings.TrimSpace(key[:eq])
			}
			if key == "model_provider" {
				uncommented++
			}
		}
	}
	if uncommented > 1 {
		return content, "model_provider was changed outside Zen; refusing to clobber"
	}
	if providerIndex < 0 {
		return content, ""
	}
	if originalValue != "" && originalValue != GatewayProviderName {
		lines[providerIndex] = "model_provider = " + quoteTOMLValue(originalValue)
	} else {
		lines = append(lines[:providerIndex], lines[providerIndex+1:]...)
	}
	return strings.Join(lines, "\n"), ""
}

// hasExactProviderLine reports whether content contains exactly the projected
// uncommented model_provider line.
func hasExactProviderLine(content []byte, projectedLine string) bool {
	uncommented := 0
	exact := false
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "model_provider") {
			key := trimmed
			if eq := strings.Index(key, "="); eq >= 0 {
				key = strings.TrimSpace(key[:eq])
			}
			if key != "model_provider" {
				continue
			}
			uncommented++
			exact = exact || trimmed == projectedLine
		}
	}
	return uncommented == 1 && exact
}

// topLevelProviderValue returns the current uncommented top-level
// model_provider value ("" when none).
func topLevelProviderValue(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "model_provider") {
			continue
		}
		eq := strings.Index(trimmed, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eq])
		if key != "model_provider" {
			continue
		}
		return strings.Trim(strings.TrimSpace(trimmed[eq+1:]), `"'`)
	}
	return ""
}

func quoteTOMLValue(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

// readConfigBytes reads the managed config; missing reports a not-yet-created
// config (treated as empty for first enable).
func readConfigBytes(path string) ([]byte, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false, fmt.Errorf("%w: codex config path is required", ErrInvalid)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("%w: read codex config: %v", ErrInvalid, err)
	}
	return raw, false, nil
}

// validateConfigTOML fails safe on malformed config.
func validateConfigTOML(raw []byte) error {
	var doc map[string]any
	if _, err := toml.Decode(string(raw), &doc); err != nil {
		return fmt.Errorf("%w: codex config is not valid TOML: %v", ErrInvalid, err)
	}
	return nil
}
