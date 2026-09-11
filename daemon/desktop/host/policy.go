// Package host owns unattended authorization, broker lifecycle and installation.
// Display/codec libraries run only in the separately UID-dropped native agent.
package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop"
)

type Mode string

const (
	Attended   Mode = "attended"
	Unattended Mode = "unattended"
)

type Surface string

const (
	Unavailable Surface = "unavailable"
	Desktop     Surface = "desktop"
	Locked      Surface = "locked"
	Greeter     Surface = "greeter"
)

// Session is supplied by the privileged session observer and verified agent,
// never by a network request. Ready requires capture AND lock-state evidence;
// logind's advisory LockedHint alone cannot establish it.
type Session struct {
	BootID            string
	ID                string
	Seat              string
	UID               uint32
	Surface           Surface
	Backend           string
	DisplayGeneration uint64
	Active            bool
	Ready             bool
	Control           bool
}

// HostConfig binds OS setup to the canonical owner. Device grants stay in the
// existing auth.Manager; there is no second trust file or unattended switch.
type HostConfig struct {
	Version   int    `json:"version"`
	HostID    string `json:"hostId"`
	OwnerUID  uint32 `json:"ownerUid"`
	Seat      string `json:"seat"`
	OwnerUnit string `json:"ownerUnit,omitempty"`
}

var ownerUnitName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}\.service$`)

func (e HostConfig) Validate() error {
	if e.Version != 1 || e.HostID == "" || len(e.HostID) > 256 || e.OwnerUID == 0 || e.Seat != "seat0" || (e.OwnerUnit != "" && (!ownerUnitName.MatchString(e.OwnerUnit) || e.OwnerUnit == "zen-desktop-host.service")) {
		return errors.New("invalid_host_config")
	}
	return nil
}

// ReadHostConfig parses bounded configuration data. Live authority must use
// LoadRootConfig to verify root ownership, parent directories and mode 0600.
func ReadHostConfig(r io.Reader) (HostConfig, error) {
	var e HostConfig
	data, err := io.ReadAll(io.LimitReader(r, 32769))
	if err != nil || len(data) > 32768 {
		return e, errors.New("invalid_host_config")
	}
	// Reject unknown fields, including any attempted OS password configuration.
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&e) != nil || d.Decode(new(any)) != io.EOF {
		return HostConfig{}, errors.New("invalid_host_config")
	}
	return e, e.Validate()
}

// Channel facts must come from fresh existing device-signature verification and
// actual inner TLS, not JSON, URL schemes, loopback addresses or proxy headers.
type Request struct {
	HostID       string
	DeviceID     string
	Fingerprint  [32]byte
	ConnectionID string
	Generation   uint64
	Mode         Mode
	Control      bool
	TLS          bool
	LANApproved  bool
}

// Consent is created by the host-visible prompt for exactly this Request.
type Consent struct {
	Request Request
	used    bool
}

// Agent is already bound to a verified Session. Write/Close must have bounded
// deadlines; Close releases held input, destroys devices and stops capture.
// Implementations must not log input, frames, Xauthority or arbitrary errors.
type Agent interface {
	Write(desktop.Command) error
	Close() error
}

// Lease is process-local, opaque and non-serializable. Never accept one from IPC.
type Lease struct {
	serial uint64
	owner  *Gate
}

type activeLease struct {
	lease   Lease
	request Request
	agent   Agent
}

type Gate struct {
	mu         sync.Mutex
	config     HostConfig
	trusted    func(string, [32]byte) bool
	scoped     func(string, [32]byte) bool
	session    Session
	generation uint64
	serial     uint64
	active     *activeLease
	denied     bool
	faulted    bool
	window     time.Time
	attempts   int
}

func NewGate(e HostConfig, trusted, scoped func(string, [32]byte) bool) (*Gate, error) {
	if err := e.Validate(); err != nil || trusted == nil || scoped == nil {
		return nil, errors.New("invalid_host_config")
	}
	return &Gate{config: e, trusted: trusted, scoped: scoped}, nil
}

// NewManagedGate binds to the existing canonical owner, not a copied device
// database. The broker additionally authenticates the owner UID, MainPID and proof.
func NewManagedGate(config HostConfig, manager *auth.Manager) (*Gate, func(), error) {
	if manager == nil || config.HostID != manager.DaemonID() {
		return nil, nil, errors.New("wrong_host_owner")
	}
	g, err := NewGate(config, func(id string, key [32]byte) bool { trusted, _ := manager.DesktopTrust(id, key); return trusted }, func(id string, key [32]byte) bool { _, scoped := manager.DesktopTrust(id, key); return scoped })
	if err != nil {
		return nil, nil, err
	}
	unsubscribe := manager.SubscribeRevocations(g.Revoke)
	return g, func() { g.Deny(true); unsubscribe() }, nil
}

func (g *Gate) retire() {
	if g.active != nil {
		if g.active.agent.Close() != nil {
			g.faulted = true
		}
		g.active = nil
	}
}

// Observe retires the old agent BEFORE publishing a new generation. Lock,
// logout, VT/UID/display changes and reboot all invalidate old input and media.
func (g *Gate) Observe(s Session) uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	if s != g.session {
		g.retire()
		g.session = s
		g.generation++
	}
	return g.generation
}

func (g *Gate) authorized(r Request, consent *Consent) bool {
	s := g.session
	if g.denied || g.faulted || r.HostID != g.config.HostID || r.ConnectionID == "" || len(r.ConnectionID) > 256 || r.Generation != g.generation || r.Fingerprint == [32]byte{} || !g.trusted(r.DeviceID, r.Fingerprint) || s.BootID == "" || s.ID == "" || !s.Active || !s.Ready || s.Seat != g.config.Seat || (r.Control && !s.Control) {
		return false
	}
	switch s.Backend {
	case "x11":
	case "wayland-portal":
		if s.Surface != Desktop {
			return false
		}
	default:
		return false
	}
	if r.Mode == Attended {
		return s.Surface == Desktop && s.UID == g.config.OwnerUID && (r.TLS || r.LANApproved) && consent != nil && !consent.used && consent.Request == r
	}
	if r.Mode != Unattended || !r.TLS || !g.scoped(r.DeviceID, r.Fingerprint) {
		return false
	}
	switch s.Surface {
	case Desktop, Locked:
		return s.UID == g.config.OwnerUID
	case Greeter:
		return true
	}
	return false
}

// Open transfers agent ownership only on success. Authentication occurs before
// this bounded, host-wide admission limiter (10 attempts/minute, no key map).
func (g *Gate) Open(r Request, consent *Consent, agent Agent, now time.Time) (Lease, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.Mode == "" {
		r.Mode = Unattended
	}
	if g.window.IsZero() || !now.Before(g.window.Add(time.Minute)) {
		g.window, g.attempts = now, 0
	}
	if g.attempts >= 10 {
		return Lease{}, errors.New("rate_limited")
	}
	g.attempts++
	if agent == nil || g.active != nil || !g.authorized(r, consent) {
		return Lease{}, errors.New("access_denied")
	}
	g.serial++
	lease := Lease{serial: g.serial, owner: g}
	g.active = &activeLease{lease: lease, request: r, agent: agent}
	if r.Mode == Attended {
		consent.used = true
	}
	return lease, nil
}

// Input never exposes the event in returned errors. Revocation, session updates
// and writes serialize; after retirement returns no old-agent write is active.
func (g *Gate) Input(lease Lease, event desktop.Command) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	a := g.active
	if a == nil || a.lease != lease {
		return errors.New("stale_lease")
	}
	if !g.trusted(a.request.DeviceID, a.request.Fingerprint) || (a.request.Mode == Unattended && !g.scoped(a.request.DeviceID, a.request.Fingerprint)) {
		g.retire()
		return errors.New("access_denied")
	}
	if err := event.ValidateInput(a.request.Control); err != nil {
		return errors.New("invalid_input")
	}
	if err := a.agent.Write(event); err != nil {
		g.retire()
		return errors.New("agent_failed")
	}
	return nil
}

// Forward serializes bounded media writes with revocation and session retirement.
// A socket writer must set a deadline before writing and must not reenter Gate.
// Frames already handed to the network cannot be recalled; native clients must
// clear decoder/surface state when that connection ends, before reconnecting.
func (g *Gate) Forward(lease Lease, kind byte, data []byte, write func(byte, []byte) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	a := g.active
	if a == nil || a.lease != lease {
		return errors.New("stale_lease")
	}
	if !g.trusted(a.request.DeviceID, a.request.Fingerprint) || (a.request.Mode == Unattended && !g.scoped(a.request.DeviceID, a.request.Fingerprint)) {
		g.retire()
		return errors.New("access_denied")
	}
	if write == nil || (kind != 1 && kind != 2) || len(data) == 0 || len(data) > desktop.MaxFrame-1 || (kind == 1 && len(data) > 8192) {
		g.retire()
		return errors.New("invalid_agent_packet")
	}
	if write(kind, data) != nil {
		g.retire()
		return errors.New("media_failed")
	}
	return nil
}

func (g *Gate) Disconnect(lease Lease) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active != nil && g.active.lease == lease {
		g.retire()
	}
}

// Revoke retires any connection for this device. Register it with the existing
// auth.Manager revocation owner; it must not maintain an independent trust list.
func (g *Gate) Revoke(deviceID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active != nil && g.active.request.DeviceID == deviceID {
		g.retire()
	}
}

func (g *Gate) Deny(denied bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.denied = denied
	if denied {
		g.retire()
	}
}
