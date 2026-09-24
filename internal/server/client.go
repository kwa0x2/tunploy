package server

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

type client struct {
	ip    string
	https bool
}

type clientKey struct{}

// Forwarded headers count only from a trusted proxy; anyone else could forge them.
func (s *Server) identifyClient(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := resolveClient(r, s.cfg.TrustedProxies)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientKey{}, c)))
	})
}

func resolveClient(r *http.Request, trusted []netip.Prefix) client {
	c := client{ip: remoteHost(r), https: r.TLS != nil}
	peer, err := netip.ParseAddr(c.ip)
	if err != nil || !isTrusted(peer, trusted) {
		return c
	}

	// Walk from the nearest hop back, stopping at the first address we don't run.
	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	ip := peer.Unmap()
	for _, hop := range slices.Backward(hops) {
		a, ok := parseHop(strings.TrimSpace(hop))
		if !ok {
			break
		}
		ip = a
		if !isTrusted(ip, trusted) {
			break
		}
	}
	c.ip = ip.String()

	proto, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ",")
	c.https = c.https || strings.EqualFold(strings.TrimSpace(proto), "https")
	return c
}

func parseHop(s string) (netip.Addr, bool) {
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap(), true
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap(), true
	}
	return netip.Addr{}, false
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	a = a.Unmap()
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func clientOf(r *http.Request) client {
	if c, ok := r.Context().Value(clientKey{}).(client); ok {
		return c
	}
	return client{ip: remoteHost(r), https: r.TLS != nil}
}

func clientIP(r *http.Request) string { return clientOf(r).ip }

func (s *Server) secureCookies(r *http.Request) bool {
	return s.cfg.SecureCookies || clientOf(r).https
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		// Only when we terminate TLS ourselves; behind a proxy that's the proxy's call.
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=15552000")
		}
		next.ServeHTTP(w, r)
	})
}
