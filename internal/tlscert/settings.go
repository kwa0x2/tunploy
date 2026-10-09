package tlscert

import (
	"context"
	"errors"
	"log/slog"
)

const (
	settingDomain = "panel_domain"
	settingEmail  = "acme_email"
)

// Settings stores the panel domain and the address Let's Encrypt writes to.
func Settings(domain, email string) map[string]string {
	return map[string]string{settingDomain: domain, settingEmail: email}
}

// Load applies the saved panel domain, as after a restart or a restore, and
// fetches its certificate in the background.
func (m *Manager) Load(stored map[string]string) {
	if !m.Status().Enabled {
		return
	}
	domain := stored[settingDomain]
	m.Configure(domain, stored[settingEmail])
	if domain == "" {
		return
	}
	go func() {
		st, err := m.Obtain(context.Background())
		if err == nil && st.State == StateFailed {
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
