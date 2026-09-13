package server

import (
	"net"
	"net/http"
)

// desktopIngress is the truthful deployment path of one desktop request.
// Trusted ingress comes only from operator-configured deployment facts and the
// actual peer address; forwarded headers, Host and client JSON are ignored.
type desktopIngress struct {
	TLS     bool
	Trusted bool
}

// SetDesktopTrustedNetwork records the operator's explicit trusted-deployment
// configuration (--lan or a private/tailnet-bound origin).
func (s *Server) SetDesktopTrustedNetwork(enabled bool) {
	s.desktopTrustedNetwork = enabled
}

// desktopTrustedPeer reports whether the peer address is inside the private
// ranges served by the documented trusted deployments: loopback (a local
// connector such as cloudflared), RFC1918, Tailscale CGNAT and IPv6 ULA.
func desktopTrustedPeer(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 10:
			return true
		case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
			return true
		case v4[0] == 192 && v4[1] == 168:
			return true
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127:
			return true
		}
		return false
	}
	return ip[0]&0xfe == 0xfc
}

// desktopIngressOf derives the request ingress without consulting headers.
func (s *Server) desktopIngressOf(r *http.Request) desktopIngress {
	ingress := desktopIngress{TLS: actualRequestTLS(r)}
	if ingress.TLS {
		// A real TLS request is trusted by definition; the transport protects it.
		ingress.Trusted = true
		return ingress
	}
	ingress.Trusted = s.desktopTrustedNetwork && desktopTrustedPeer(r.RemoteAddr)
	return ingress
}
