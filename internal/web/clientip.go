package web

import (
	"net/http"
	"net/netip"
)

// peerAddr returns the TCP peer of r, with a v4-mapped IPv6 address turned
// into IPv4. It is the zero Addr if RemoteAddr does not parse.
func peerAddr(r *http.Request) netip.Addr {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		a, aerr := netip.ParseAddr(r.RemoteAddr)
		if aerr != nil {
			return netip.Addr{}
		}
		return a.Unmap()
	}
	return ap.Addr().Unmap()
}

// fromTrustedProxy reports whether the TCP peer of r is in
// TODO_WEB_TRUSTED_PROXIES.
func (s *Server) fromTrustedProxy(r *http.Request) bool {
	peer := peerAddr(r)
	if !peer.IsValid() {
		return false
	}
	for _, p := range s.d.Config.WebTrustedProxies {
		if p.Contains(peer) {
			return true
		}
	}
	return false
}

// clientIP is the rule of docs/WEB-DESIGN.md §9.3: CF-Connecting-IP when the
// peer is a trusted proxy and the header holds an IP; otherwise the TCP
// peer. It never reads X-Forwarded-For. The result serves only rate limits,
// logs and the sign-in notice.
func (s *Server) clientIP(r *http.Request) netip.Addr {
	if s.fromTrustedProxy(r) {
		if a, err := netip.ParseAddr(r.Header.Get("CF-Connecting-IP")); err == nil {
			return a.Unmap()
		}
	}
	return peerAddr(r)
}

// clientCountry is CF-IPCountry from a trusted proxy, for the sign-in
// notice (§3.3.1). It is "??" without a trusted peer, or when the header is
// not two ASCII letters.
func (s *Server) clientCountry(r *http.Request) string {
	if !s.fromTrustedProxy(r) {
		return "??"
	}
	c := r.Header.Get("CF-IPCountry")
	if len(c) != 2 {
		return "??"
	}
	for i := range 2 {
		if b := c[i]; (b < 'A' || b > 'Z') && (b < 'a' || b > 'z') {
			return "??"
		}
	}
	return c
}
