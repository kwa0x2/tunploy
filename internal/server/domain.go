package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/tlscert"
)

const (
	settingPanelDomain = "panel_domain"
	settingACMEEmail   = "acme_email"

	obtainWait = 60 * time.Second
)

type HTTPS interface {
	Status() tlscert.Status
	Configure(domain, email string)
	Obtain(ctx context.Context) (tlscert.Status, error)
}

type domainRequest struct {
	Domain string `json:"domain"`
	Email  string `json:"email"`
}

// RestoreDomain re-applies the saved panel domain after a restart and fetches its certificate.
func (s *Server) RestoreDomain(ctx context.Context) error {
	stored, err := s.store.Settings(ctx)
	if err != nil {
		return err
	}
	s.applyDomain(ctx, stored)
	return nil
}

func (s *Server) applyDomain(ctx context.Context, stored map[string]string) {
	if !s.https.Status().Enabled {
		return
	}
	domain := stored[settingPanelDomain]
	s.https.Configure(domain, stored[settingACMEEmail])
	if domain == "" {
		return
	}
	go func() {
		st, err := s.https.Obtain(ctx)
		if err == nil && st.State == tlscert.StateFailed {
			err = errors.New(st.Error)
		}
		if err != nil {
			slog.Error("could not get an HTTPS certificate; check that the domain's DNS points to this server and TCP 80 and 443 are open",
				"domain", domain, "error", err)
			return
		}
		slog.Info("https certificate ready", "domain", domain)
	}()
}

func (s *Server) handleGetDomain(w http.ResponseWriter, r *http.Request) error {
	return httpx.JSON(w, http.StatusOK, s.https.Status())
}

// Waits for the certificate, so the form can say at once whether it worked.
func (s *Server) handleSetDomain(w http.ResponseWriter, r *http.Request) error {
	var req domainRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(req.Domain)), ".")
	email := strings.TrimSpace(req.Email)

	fields := map[string]string{}
	if domain != "" && !tlscert.ValidDomain(domain) {
		fields["domain"] = "enter a domain name such as panel.example.com, without http:// or a port"
	}
	if email != "" {
		if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
			fields["email"] = "email is not a valid address"
		}
	}
	if len(fields) > 0 {
		return httpx.Invalid(fields)
	}
	if domain != "" && !s.https.Status().Enabled {
		return httpx.Conflict("this panel does not listen for HTTPS; %s", s.https.Status().Error)
	}

	// Checked first: Let's Encrypt would fail too, and failures count against its limits.
	if domain != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		addrs, err := s.lookupHost(ctx, domain)
		cancel()
		if err != nil || len(addrs) == 0 {
			return httpx.Invalid(map[string]string{
				"domain": "this domain does not resolve yet; add an A record pointing to this server and try again in a few minutes",
			})
		}
	}

	if err := s.store.SaveSettings(r.Context(), map[string]string{
		settingPanelDomain: domain,
		settingACMEEmail:   email,
	}); err != nil {
		return err
	}
	s.https.Configure(domain, email)
	s.record(r.Context(), store.Event{Kind: "settings.domain_changed", IP: clientIP(r), Detail: domain})

	if domain == "" {
		return httpx.JSON(w, http.StatusOK, s.https.Status())
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), obtainWait)
	defer cancel()
	st, err := s.https.Obtain(ctx)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, st)
}

func (s *Server) handleRetryDomain(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), obtainWait)
	defer cancel()
	st, err := s.https.Obtain(ctx)
	switch {
	case errors.Is(err, tlscert.ErrNoDomain):
		return httpx.Conflict("no panel domain is set")
	case errors.Is(err, tlscert.ErrRetrySoon):
		return httpx.Errorf(http.StatusTooManyRequests, "retry_soon", "%s", err.Error())
	case err != nil:
		return err
	}
	return httpx.JSON(w, http.StatusOK, st)
}

type httpsInfo struct {
	URL string `json:"url,omitempty"`
}

// Public, so the login page can point people at the encrypted address.
func (s *Server) handleHTTPSInfo(w http.ResponseWriter, r *http.Request) error {
	var info httpsInfo
	if st := s.https.Status(); st.State == tlscert.StateReady {
		info.URL = "https://" + st.Domain + s.httpsPortSuffix()
	}
	return httpx.JSON(w, http.StatusOK, info)
}

func (s *Server) httpsPortSuffix() string {
	i := strings.LastIndex(s.cfg.HTTPSListen, ":")
	if i < 0 || s.cfg.HTTPSListen[i+1:] == "443" {
		return ""
	}
	return s.cfg.HTTPSListen[i:]
}
