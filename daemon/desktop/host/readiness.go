package host

// Readiness is a non-authoritative host probe. Device trust and TLS are decided
// separately. Broker is lock/login after reboot. CurrentSession is the logged-in
// display of this zen process. A user-mode daemon cannot control pre-login.
type Readiness struct {
	Status         string `json:"status"`
	Broker         bool   `json:"broker"`
	CurrentSession bool   `json:"current_session"`
	Surface        string `json:"surface"`
	Session        string `json:"session"`
}

const (
	ReadinessReady         = "ready"
	ReadinessSession       = "session"
	ReadinessSetupRequired = "setup_required"
	ReadinessUnsupported   = "unsupported"
)
