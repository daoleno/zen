package modelprofiles

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// NewSafeHTTPClient builds an upstream client that never uses ambient HTTP
// proxy environment variables, never follows redirects, and dials through an
// SSRF-aware resolver. Loopback dials are allowed only when the request host is
// an explicit literal 127.0.0.1, ::1, or localhost — never when a remote name
// resolves to loopback/private/link-local.
func NewSafeHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	transport := &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) {
			return nil, nil
		},
		DialContext:           safeDialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   0,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return ErrUpstreamRedirect
		},
	}
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%w: dial address: %v", ErrUpstreamSSRF, err)
	}
	ips, err := resolveSafeHost(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, ip := range ips {
		d := net.Dialer{Timeout: 10 * time.Second}
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%w: no dialable address", ErrUpstreamSSRF)
	}
	return nil, lastErr
}

// hostExplicitlyAllowsLoopback is true only for literal loopback hosts in the
// Profile/request URL (127.0.0.1, ::1, localhost). Remote DNS names never qualify.
func hostExplicitlyAllowsLoopback(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return ip.IsLoopback()
}

type ipLookupFunc func(ctx context.Context, host string) ([]netip.Addr, error)

func defaultIPLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func resolveSafeHost(ctx context.Context, host string) ([]netip.Addr, error) {
	return resolveSafeHostLookup(ctx, host, defaultIPLookup)
}

// resolveSafeHostLookup is the pure SSRF gate: original host decides allowLoopback;
// lookup results are filtered with that decision (deterministic under fake lookup).
func resolveSafeHostLookup(ctx context.Context, host string, lookup ipLookupFunc) ([]netip.Addr, error) {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return nil, fmt.Errorf("%w: empty host", ErrUpstreamSSRF)
	}
	allowLoopback := hostExplicitlyAllowsLoopback(host)
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if !isDialableUpstreamIP(ip, allowLoopback) {
			return nil, fmt.Errorf("%w: address %s blocked", ErrUpstreamSSRF, ip)
		}
		return []netip.Addr{ip}, nil
	}
	if lookup == nil {
		lookup = defaultIPLookup
	}
	addrs, err := lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("%w: dns: %v", ErrUpstreamSSRF, err)
	}
	return filterResolvedAddresses(host, addrs, allowLoopback)
}

// filterResolvedAddresses rejects DNS answers that land on loopback/private/
// link-local/metadata unless the original host explicitly allowed loopback.
func filterResolvedAddresses(host string, addrs []netip.Addr, allowLoopback bool) ([]netip.Addr, error) {
	_ = host
	out := make([]netip.Addr, 0, len(addrs))
	for _, ip := range addrs {
		ip = ip.Unmap()
		if !isDialableUpstreamIP(ip, allowLoopback) {
			return nil, fmt.Errorf("%w: resolved address %s blocked", ErrUpstreamSSRF, ip)
		}
		out = append(out, ip)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no addresses", ErrUpstreamSSRF)
	}
	return out, nil
}

func isDialableUpstreamIP(ip netip.Addr, allowLoopback bool) bool {
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if ip.IsLoopback() {
		return allowLoopback
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if isMetadataIP(ip) {
		return false
	}
	return true
}

func isMetadataIP(ip netip.Addr) bool {
	if ip.Is4() {
		v4 := ip.As4()
		if v4[0] == 169 && v4[1] == 254 {
			return true
		}
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
	}
	return false
}

// isNativePassthroughHost reports whether host is an immutable official origin.
func isNativePassthroughHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "api.openai.com", "api.anthropic.com":
		return true
	default:
		return false
	}
}
