package tlscert

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"golang.org/x/crypto/acme"
)

func TestValidDomain(t *testing.T) {
	for domain, want := range map[string]bool{
		"panel.example.com":     true,
		"xn--bcher-kva.de":      true,
		"a-b.c.io":              true,
		"localhost":             false,
		"203.0.113.9":           false,
		"::1":                   false,
		"panel.example.com:443": false,
		"-bad.example.com":      false,
		"under_score.io":        false,
		"Panel.Example.com":     false,
		"":                      false,
	} {
		if got := ValidDomain(domain); got != want {
			t.Errorf("ValidDomain(%q) = %v, want %v", domain, got, want)
		}
	}
}

func TestConfigureStates(t *testing.T) {
	m := New(t.TempDir(), "", ":443")
	if st := m.Status(); !st.Enabled || st.State != StateOff {
		t.Fatalf("new manager: %+v", st)
	}
	if _, err := m.Obtain(context.Background()); !errors.Is(err, ErrNoDomain) {
		t.Fatalf("obtain without a domain: %v", err)
	}

	m.Configure("panel.example.com", "me@example.com")
	if st := m.Status(); st.State != StatePending || st.Domain != "panel.example.com" {
		t.Fatalf("configured: %+v", st)
	}
	m.Configure("", "")
	if st := m.Status(); st.State != StateOff || st.Domain != "" {
		t.Fatalf("removed: %+v", st)
	}

	m.Disable("could not listen on :443")
	m.Configure("panel.example.com", "")
	if st := m.Status(); st.Enabled || st.Error == "" {
		t.Fatalf("disabled must stick with its reason: %+v", st)
	}
}

func TestGetCertificateGuards(t *testing.T) {
	m := New(t.TempDir(), "", ":443")
	hello := &tls.ClientHelloInfo{ServerName: "panel.example.com"}
	if _, err := m.getCertificate(hello); !errors.Is(err, ErrNoDomain) {
		t.Fatalf("no domain: %v", err)
	}

	m.Configure("panel.example.com", "")
	if _, err := m.getCertificate(&tls.ClientHelloInfo{ServerName: "other.example.com"}); !errors.Is(err, errWrongHost) {
		t.Fatalf("other host: %v", err)
	}

	// A recent failure must answer handshakes without calling the CA again.
	acm := m.acm
	m.record(acm, nil, errors.New("dns problem"))
	start := time.Now()
	if _, err := m.getCertificate(hello); err == nil || err.Error() != errBackingOff {
		t.Fatalf("want the backoff error, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("backoff must not contact Let's Encrypt")
	}

	// Results from a manager that has since been replaced are dropped.
	m.Configure("new.example.com", "")
	m.record(acm, nil, errors.New("stale"))
	if st := m.Status(); st.State != StatePending {
		t.Fatalf("stale result overwrote the status: %+v", st)
	}

	if !slices.Contains(m.TLSConfig().NextProtos, acme.ALPNProto) {
		t.Fatal("TLS-ALPN challenges need the acme protocol advertised")
	}
}

func TestObtainIsRateLimited(t *testing.T) {
	m := New(t.TempDir(), "http://127.0.0.1:1/directory", ":443")
	m.Configure("panel.example.com", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	st, err := m.Obtain(ctx)
	if err != nil || st.State != StateFailed || st.Error == "" {
		t.Fatalf("unreachable CA: %+v, %v", st, err)
	}
	if _, err := m.Obtain(ctx); !errors.Is(err, ErrRetrySoon) {
		t.Fatalf("second attempt right away: %v", err)
	}
}

func TestHTTPHandler(t *testing.T) {
	m := New(t.TempDir(), "", ":8443")

	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/servers", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("no domain: want 404, got %d", rec.Code)
	}

	m.Configure("panel.example.com", "")
	rec = httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/servers?x=1", nil))
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "https://panel.example.com:8443/servers?x=1" {
		t.Fatalf("redirect: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}
