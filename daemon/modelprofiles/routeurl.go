package modelprofiles

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

const (
	RoutePathPrefix = "/r/"
	RouteAPIPrefix  = "/v1"
)

// LoopbackCodexBaseURL builds http://127.0.0.1:<port>/r/<routeID>/v1 for Codex
// openai_base_url (CLI appends /responses under /v1).
func LoopbackCodexBaseURL(listenAddr, routeID string) (string, error) {
	return loopbackURL(listenAddr, routeID, true)
}

// LoopbackClaudeRootURL builds http://127.0.0.1:<port>/r/<routeID> for
// ANTHROPIC_BASE_URL (CLI requests /v1/messages, optionally ?beta=true).
func LoopbackClaudeRootURL(listenAddr, routeID string) (string, error) {
	return loopbackURL(listenAddr, routeID, false)
}

// LoopbackRouteBaseURL is an alias for Codex base (tests / legacy callers).
func LoopbackRouteBaseURL(listenAddr, routeID string) (string, error) {
	return LoopbackCodexBaseURL(listenAddr, routeID)
}

func loopbackURL(listenAddr, routeID string, withV1 bool) (string, error) {
	routeID = strings.TrimSpace(routeID)
	if routeID == "" || strings.ContainsAny(routeID, "/?#") {
		return "", fmt.Errorf("%w: route id is required and must be path-safe", ErrInvalid)
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return "", fmt.Errorf("%w: listen address: %v", ErrInvalid, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if !isLoopbackHost(host) {
		return "", fmt.Errorf("%w: listen host must be loopback", ErrInvalid)
	}
	if port == "" {
		return "", fmt.Errorf("%w: listen port is required", ErrInvalid)
	}
	path := RoutePathPrefix + routeID
	if withV1 {
		path += RouteAPIPrefix
	}
	u := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: path}
	if err := ValidateLoopbackRouteURL(u.String()); err != nil {
		return "", err
	}
	return u.String(), nil
}

// ParsedRouteRequest is the opaque-route admission parse of an inbound path.
type ParsedRouteRequest struct {
	RouteID  string
	APIPath  string // e.g. /v1/responses or /v1/messages
	Endpoint RouteEndpoint
}

// RouteEndpoint is a proven CLI path/method pair the router admits.
type RouteEndpoint string

const (
	EndpointResponses            RouteEndpoint = "responses"
	EndpointAnthropicMessages    RouteEndpoint = "anthropic_messages"
	EndpointAnthropicCountTokens RouteEndpoint = "anthropic_count_tokens"
	// EndpointModels is the standard GET /v1/models catalog surface Codex's
	// native /model switch reads. Served locally from the synced discovery
	// cache — never forwarded upstream.
	EndpointModels RouteEndpoint = "models"
)

// ParseRouteRequestPath extracts the opaque RouteID and API path from an
// inbound request path shaped like /r/{routeID}/v1/...
func ParseRouteRequestPath(rawPath string) (ParsedRouteRequest, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return ParsedRouteRequest{}, fmt.Errorf("%w: empty path", ErrRoutePathMismatch)
	}
	// Query is not part of Path in net/http; still strip defensively.
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if !strings.HasPrefix(path, RoutePathPrefix) {
		return ParsedRouteRequest{}, fmt.Errorf("%w: missing route prefix", ErrRoutePathMismatch)
	}
	rest := strings.TrimPrefix(path, RoutePathPrefix)
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 {
		return ParsedRouteRequest{}, fmt.Errorf("%w: missing route id", ErrRoutePathMismatch)
	}
	routeID := rest[:slash]
	apiPath := rest[slash:]
	if routeID == "" || strings.Contains(routeID, "/") {
		return ParsedRouteRequest{}, fmt.Errorf("%w: invalid route id", ErrRoutePathMismatch)
	}
	endpoint, err := matchRouteEndpoint(apiPath)
	if err != nil {
		return ParsedRouteRequest{}, err
	}
	return ParsedRouteRequest{RouteID: routeID, APIPath: apiPath, Endpoint: endpoint}, nil
}

func matchRouteEndpoint(apiPath string) (RouteEndpoint, error) {
	switch strings.TrimSuffix(apiPath, "/") {
	case "/v1/responses":
		return EndpointResponses, nil
	case "/v1/messages":
		return EndpointAnthropicMessages, nil
	case "/v1/messages/count_tokens":
		return EndpointAnthropicCountTokens, nil
	case "/v1/models":
		return EndpointModels, nil
	default:
		return "", fmt.Errorf("%w: unsupported path %s", ErrRoutePathMismatch, apiPath)
	}
}

// EndpointForRouteProtocol returns admitted endpoints for a route protocol.
// GET /v1/models is the local catalog surface and is admitted for every
// protocol; all other endpoints are POST-only request paths.
func EndpointAllowedForProtocol(routeProtocol string, endpoint RouteEndpoint) bool {
	switch normalizeID(routeProtocol) {
	case RouteProtocolResponses:
		return endpoint == EndpointResponses || endpoint == EndpointModels
	case RouteProtocolAnthropicMessages:
		return endpoint == EndpointAnthropicMessages || endpoint == EndpointAnthropicCountTokens || endpoint == EndpointModels
	default:
		return false
	}
}

// UpstreamRequestURL joins an upstream base URL with the API path under /v1/...
// RawQuery from the inbound request must be attached by the caller.
func UpstreamRequestURL(upstreamBase, apiPath string) (string, error) {
	if err := ValidateUpstreamBaseURL(upstreamBase); err != nil {
		return "", fmt.Errorf("%w: %v", ErrUpstreamInvalid, err)
	}
	apiPath = strings.TrimSpace(apiPath)
	if !strings.HasPrefix(apiPath, "/v1/") {
		return "", fmt.Errorf("%w: api path must start with /v1/", ErrRoutePathMismatch)
	}
	base, err := url.Parse(normalizeSpace(upstreamBase))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUpstreamInvalid, err)
	}
	base.Path = strings.TrimSuffix(base.Path, "/")
	suffix := strings.TrimPrefix(apiPath, "/v1")
	if strings.HasSuffix(base.Path, "/v1") {
		base.Path = base.Path + suffix
	} else {
		base.Path = base.Path + apiPath
	}
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}
