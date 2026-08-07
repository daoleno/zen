package modelprofiles

// Fail-closed JSON for daemon-internal types. Accidental encoding/json use
// must not emit RouteID, RouteProtocol, UpstreamBaseURL, CredentialEnv, or raw
// history. Safe Wire* DTOs are unchanged and marshal normally.

func (RouteBinding) MarshalJSON() ([]byte, error) {
	return nil, ErrInternalNotWire
}

func (*RouteBinding) UnmarshalJSON([]byte) error {
	return ErrInternalNotWire
}

func (RouteActivationEvent) MarshalJSON() ([]byte, error) {
	return nil, ErrInternalNotWire
}

func (*RouteActivationEvent) UnmarshalJSON([]byte) error {
	return ErrInternalNotWire
}

func (SessionRouteState) MarshalJSON() ([]byte, error) {
	return nil, ErrInternalNotWire
}

func (*SessionRouteState) UnmarshalJSON([]byte) error {
	return ErrInternalNotWire
}

func (ResolvedLaunch) MarshalJSON() ([]byte, error) {
	return nil, ErrInternalNotWire
}

func (*ResolvedLaunch) UnmarshalJSON([]byte) error {
	return ErrInternalNotWire
}
