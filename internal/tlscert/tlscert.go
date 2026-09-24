// Package tlscert serves the panel over HTTPS with Let's Encrypt certificates for a domain set at runtime.
package tlscert

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

const (
	StateOff     = "off"
	StatePending = "pending"
	StateReady   = "ready"
	StateFailed  = "failed"

	// Let's Encrypt allows five failed validations per domain an hour; browsers
	// knocking on 443 must not use them up.
	failureBackoff = 10 * time.Minute
	retryGap       = 15 * time.Second
)

var (
	ErrNoDomain   = errors.New("no panel domain is set")
	ErrRetrySoon  = errors.New("a certificate was requested moments ago; wait a few seconds")
	errWrongHost  = errors.New("not this panel's domain")
	errBackingOff = "last attempt failed; retrying after a pause to stay within Let's Encrypt's limits"
)

type Status struct {
	// Enabled is false when the panel doesn't listen for HTTPS at all.
	Enabled   bool       `json:"enabled"`
	Domain    string     `json:"domain"`
	Email     string     `json:"email"`
	State     string     `json:"state"`
	Expires   *time.Time `json:"expires,omitempty"`
	Error     string     `json:"error,omitempty"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
}

type Manager struct {
	cacheDir  string
	directory string
	httpsPort string

	mu       sync.Mutex
	acm      *autocert.Manager
	status   Status
	failedAt time.Time
	lastTry  time.Time
}

// An empty directory means Let's Encrypt's production CA.
func New(cacheDir, directory, httpsAddr string) *Manager {
	_, port, _ := net.SplitHostPort(httpsAddr)
	return &Manager{
		cacheDir:  cacheDir,
		directory: directory,
		httpsPort: port,
		status:    Status{Enabled: true, State: StateOff},
	}
}

func (m *Manager) Disable(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.Enabled = false
	m.status.Error = reason
}

// Configure swaps in a fresh autocert manager, since its fields can't change once in use.
func (m *Manager) Configure(domain, email string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.status.Domain, m.status.Email = domain, email
	m.status.Expires, m.status.CheckedAt = nil, nil
	m.failedAt, m.lastTry = time.Time{}, time.Time{}
	if m.status.Enabled {
		m.status.Error = ""
	}
	if domain == "" {
		m.acm = nil
		m.status.State = StateOff
		return
	}

	m.acm = &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(m.cacheDir),
		HostPolicy: autocert.HostWhitelist(domain),
		Email:      email,
	}
	if m.directory != "" {
		m.acm.Client = &acme.Client{DirectoryURL: m.directory}
	}
	m.status.State = StatePending
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// Obtain asks for the certificate now, ignoring the failure backoff. If ctx
// ends first, issuance carries on and Status reports the outcome later.
func (m *Manager) Obtain(ctx context.Context) (Status, error) {
	m.mu.Lock()
	acm, domain := m.acm, m.status.Domain
	switch {
	case acm == nil:
		m.mu.Unlock()
		return Status{}, ErrNoDomain
	case time.Since(m.lastTry) < retryGap:
		m.mu.Unlock()
		return Status{}, ErrRetrySoon
	}
	m.lastTry = time.Now()
	m.status.State = StatePending
	m.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		cert, err := acm.GetCertificate(&tls.ClientHelloInfo{ServerName: domain})
		m.record(acm, cert, err)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	return m.Status(), nil
}

func (m *Manager) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		NextProtos:     []string{"h2", "http/1.1", acme.ALPNProto},
		GetCertificate: m.getCertificate,
	}
}

func (m *Manager) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.Lock()
	acm, domain := m.acm, m.status.Domain
	backingOff := m.status.State == StateFailed && time.Since(m.failedAt) < failureBackoff
	m.mu.Unlock()

	if acm == nil {
		return nil, ErrNoDomain
	}
	// Let's Encrypt's own TLS-ALPN check must always get through.
	if slices.Contains(hello.SupportedProtos, acme.ALPNProto) {
		return acm.GetCertificate(hello)
	}
	if !strings.EqualFold(strings.TrimSuffix(hello.ServerName, "."), domain) {
		return nil, errWrongHost
	}
	if backingOff {
		return nil, errors.New(errBackingOff)
	}
	cert, err := acm.GetCertificate(hello)
	m.record(acm, cert, err)
	return cert, err
}

func (m *Manager) record(acm *autocert.Manager, cert *tls.Certificate, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.acm != acm {
		return
	}
	now := time.Now()
	m.status.CheckedAt = &now
	if err != nil {
		m.status.State = StateFailed
		m.status.Error = err.Error()
		m.failedAt = now
		return
	}
	m.status.State = StateReady
	m.status.Error = ""
	if cert != nil && cert.Leaf != nil {
		exp := cert.Leaf.NotAfter
		m.status.Expires = &exp
	}
}

// HTTPHandler answers Let's Encrypt's HTTP check and sends everything else to HTTPS.
func (m *Manager) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		acm, domain := m.acm, m.status.Domain
		m.mu.Unlock()
		if acm == nil {
			http.Error(w, "Tunploy: no panel domain is set. Add one under Settings.", http.StatusNotFound)
			return
		}
		origin := "https://" + domain
		if m.httpsPort != "" && m.httpsPort != "443" {
			origin += ":" + m.httpsPort
		}
		acm.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, origin+r.URL.RequestURI(), http.StatusMovedPermanently)
		})).ServeHTTP(w, r)
	})
}

// ValidDomain accepts DNS names only: Let's Encrypt doesn't issue for IPs or bare hosts.
func ValidDomain(s string) bool {
	if _, err := netip.ParseAddr(s); err == nil || !strings.Contains(s, ".") || len(s) > 253 {
		return false
	}
	for label := range strings.SplitSeq(s, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
