package modelprofiles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gatewayOwnerFile groups the Owner integration of the machine-level Codex
// gateway and its config takeover. The gateway is independent of Session
// route bindings: any Codex process whose native config points at the stable
// loopback endpoint is routed through the currently selected Provider.

// SetGatewayBypass installs the takeover-readiness callback consulted by
// PrepareLaunchModel: when it reports true, new managed Codex launches use the
// plain base command and rely on the machine-level config projection (the
// canonical gateway route) instead of per-Session loopback injection.
func (o *Owner) SetGatewayBypass(bypass func() bool) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.gatewayBypass = bypass
}

// Gateway returns the machine-level gateway runtime (nil when not configured).
func (o *Owner) Gateway() *Gateway {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.gateway
}

// Takeover returns the Codex config takeover manager (nil when not configured).
func (o *Owner) Takeover() *Takeover {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.takeover
}

// startGateway builds, binds, and restores the machine-level gateway from
// durable state. A bind failure is non-fatal for the daemon: takeover reports
// broken truthfully and per-Session routing continues to work. A corrupt
// gateway state file is fatal (fail closed, same as the route listener).
func (o *Owner) startGateway(cfg OwnerConfig) error {
	addr := strings.TrimSpace(cfg.GatewayAddr)
	if addr == "" {
		return nil
	}
	stateDir := strings.TrimSpace(cfg.GatewayStateDir)
	gateway := NewGateway(addr, cfg.Credentials)
	if stateDir != "" {
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			return fmt.Errorf("%w: create gateway state dir: %v", ErrInvalid, err)
		}
		gateway.SetGatewayStatePath(filepath.Join(stateDir, "gateway.json"))
	}
	o.gateway = gateway
	if stateDir != "" {
		o.takeover = NewTakeover(cfg.CodexConfigPath, stateDir, gateway)
	}
	if err := gateway.Listen(); err != nil {
		// Non-fatal: takeover status reports broken; honest connection
		// failures reach every routed Codex process.
		return nil
	}
	// Restore the same listener address / upstream profile across restarts.
	listenAddr, profileID, err := LoadGatewayState(gatewayStateFilePath(stateDir))
	if err != nil {
		return err
	}
	if up, ok := o.resolveGatewayUpstream(profileID); ok {
		gateway.SetUpstream(up)
	} else {
		gateway.ClearUpstream()
	}
	if o.takeover != nil {
		state, err := o.takeover.LoadState()
		if err != nil {
			return err
		}
		if state.Enabled {
			// Repair the live projection to the same address before the
			// takeover can claim active (daemon restart path).
			repairAddr := strings.TrimSpace(state.ListenAddr)
			if repairAddr == "" {
				repairAddr = strings.TrimSpace(listenAddr)
			}
			if repairAddr == "" {
				repairAddr = addr
			}
			if _, repairErr := o.takeover.Repair(repairAddr); repairErr != nil {
				return repairErr
			}
		}
	}
	return nil
}

func gatewayStateFilePath(stateDir string) string {
	if strings.TrimSpace(stateDir) == "" {
		return ""
	}
	return filepath.Join(strings.TrimSpace(stateDir), "gateway.json")
}

// EnableCodexGateway activates the machine-level takeover and points the
// gateway at the currently selected Codex Provider connection.
func (o *Owner) EnableCodexGateway(listenAddr string) (TakeoverStatus, error) {
	if o == nil || o.takeover == nil {
		return TakeoverStatus{}, fmt.Errorf("%w: codex gateway is not configured", ErrInvalid)
	}
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		listenAddr = DefaultGatewayListenAddr
	}
	status, err := o.takeover.Enable(listenAddr)
	if err != nil {
		return status, err
	}
	o.refreshGatewayUpstream()
	return o.GatewayStatus(), nil
}

// DisableCodexGateway removes the Zen-owned projection and restores the
// pre-takeover config; the gateway listener stays up (harmless, and
// re-enable is instant).
func (o *Owner) DisableCodexGateway() (TakeoverStatus, error) {
	if o == nil || o.takeover == nil {
		return TakeoverStatus{}, fmt.Errorf("%w: codex gateway is not configured", ErrInvalid)
	}
	status, err := o.takeover.Disable()
	if err != nil {
		return status, err
	}
	return o.GatewayStatus(), nil
}

// RestoreCodexGatewayBackup rolls the exact pre-takeover config backup back.
func (o *Owner) RestoreCodexGatewayBackup() (TakeoverStatus, error) {
	if o == nil || o.takeover == nil {
		return TakeoverStatus{}, fmt.Errorf("%w: codex gateway is not configured", ErrInvalid)
	}
	status, err := o.takeover.RestoreBackup()
	if err != nil {
		return status, err
	}
	return o.GatewayStatus(), nil
}

// GatewayStatus returns the truthful takeover + gateway status.
func (o *Owner) GatewayStatus() TakeoverStatus {
	if o == nil || o.takeover == nil {
		return TakeoverStatus{State: TakeoverStateInactive}
	}
	return o.takeover.Status()
}

// refreshGatewayUpstream points the gateway at the currently selected Codex
// Provider connection when takeover is active. Called after Provider switches
// and default changes.
func (o *Owner) refreshGatewayUpstream() {
	if o == nil || o.gateway == nil || o.takeover == nil {
		return
	}
	state, err := o.takeover.LoadState()
	if err != nil || !state.Enabled {
		return
	}
	profileID := ""
	if o.store != nil {
		profileID = strings.TrimSpace(o.store.DefaultProfileID(ExecutorCodex))
	}
	if up, ok := o.resolveGatewayUpstream(profileID); ok {
		o.gateway.SetUpstream(up)
		return
	}
	o.gateway.ClearUpstream()
}

// resolveGatewayUpstream derives the machine-level gateway upstream for a
// Codex connection id through the same per-client compile the launch/router
// path uses. Durable account connections store auth_mode=none (the raw form:
// per-client auth semantics are compiled at use time); compiling the target
// yields the codex bearer_env credential contract so the gateway injects the
// same credentials as the per-Session router. Secret-free; ok=false clears the
// upstream (no default, unknown id, or un-compilable connection).
func (o *Owner) resolveGatewayUpstream(profileID string) (GatewayUpstream, bool) {
	if o == nil || o.store == nil {
		return GatewayUpstream{}, false
	}
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return GatewayUpstream{}, false
	}
	profile, err := o.store.ResolveProfileWithModel(ExecutorCodex, profileID, "")
	if err != nil {
		return GatewayUpstream{}, false
	}
	return GatewayUpstreamFromProfile(profile), true
}

// DefaultCodexConfigPath resolves the CLI's native Codex config path,
// honoring CODEX_HOME.
func DefaultCodexConfigPath() string {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return filepath.Join(home, "config.toml")
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(userHome, ".codex", "config.toml")
}
