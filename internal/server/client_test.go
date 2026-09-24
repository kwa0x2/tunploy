package server

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/kwa0x2/tunploy/internal/store"
)

func TestResolveClient(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("10.0.0.5/32")}

	for name, tc := range map[string]struct {
		remote, xff, proto string
		wantIP             string
		wantHTTPS          bool
	}{
		"direct":                  {"203.0.113.9:5000", "", "", "203.0.113.9", false},
		"untrusted peer forges":   {"203.0.113.9:5000", "1.2.3.4", "https", "203.0.113.9", false},
		"trusted proxy":           {"172.17.0.1:5000", "198.51.100.7", "https", "198.51.100.7", true},
		"spoofed left hop":        {"172.17.0.1:5000", "1.2.3.4, 198.51.100.7", "", "198.51.100.7", false},
		"chain of trusted":        {"172.17.0.1:5000", "198.51.100.7, 10.0.0.5", "", "198.51.100.7", false},
		"hop with port":           {"172.17.0.1:5000", "198.51.100.7:4711", "", "198.51.100.7", false},
		"garbage stops the walk":  {"172.17.0.1:5000", "198.51.100.7, nonsense", "", "172.17.0.1", false},
		"trusted without headers": {"172.17.0.1:5000", "", "", "172.17.0.1", false},
		"ipv6 client":             {"172.17.0.1:5000", "2001:db8::1", "", "2001:db8::1", false},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remote
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			c := resolveClient(r, trusted)
			if c.ip != tc.wantIP || c.https != tc.wantHTTPS {
				t.Fatalf("got %+v, want ip %s https %v", c, tc.wantIP, tc.wantHTTPS)
			}
		})
	}
}

func TestLoginRecordsForwardedIP(t *testing.T) {
	s := newTestServer(t)
	s.cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}
	createAdmin(t, s)

	req := httptest.NewRequest("POST", "/api/auth/login",
		strings.NewReader(`{"email":"admin@example.com","password":"hunter2hunter2"}`))
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}

	if !sessionCookieFrom(t, rec).Secure {
		t.Error("a login forwarded as https must get a Secure cookie")
	}
	events, err := s.store.Events(context.Background(), store.EventFilter{Limit: 10, Category: store.CategoryAuth})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[0].Kind != "auth.login" || events[0].IP != "198.51.100.7" {
		t.Fatalf("want the login recorded with the client's IP, got %+v", events)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "GET", "/api/health", nil)
	if rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %v", rec.Header())
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS must not be sent over plain HTTP")
	}

	req := httptest.NewRequest("GET", "/api/health", nil)
	req.TLS = &tls.ConnectionState{}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Error("HSTS must be sent over TLS")
	}
}
