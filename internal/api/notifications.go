package api

import (
	"context"
	"net/http"
	"net/mail"
	"slices"
	"strconv"
	"strings"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/notify"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/tlscert"
	"github.com/kwa0x2/tunploy/internal/wg"
)

const maxRecipients = 10

type notificationSettings struct {
	Enabled     bool          `json:"enabled"`
	Host        string        `json:"host"`
	Port        int           `json:"port"`
	Security    string        `json:"security"`
	Username    string        `json:"username"`
	PasswordSet bool          `json:"password_set"`
	From        string        `json:"from"`
	To          []string      `json:"to"`
	Events      []string      `json:"events"`
	Status      notify.Status `json:"status"`
}

type notificationRequest struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"`
	Username string `json:"username"`
	// nil keeps the saved password, so the form never has to show it.
	Password *string  `json:"password"`
	From     string   `json:"from"`
	To       []string `json:"to"`
	Events   []string `json:"events"`
}

func notificationView(cur notify.Settings) notificationSettings {
	return notificationSettings{
		Enabled:     cur.Enabled,
		Host:        cur.Host,
		Port:        cur.Port,
		Security:    cur.Security,
		Username:    cur.Username,
		PasswordSet: cur.Password != "",
		From:        cur.From,
		To:          nonNil(cur.To),
		Events:      nonNil(cur.Groups),
	}
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func (s *Server) notificationSettings(ctx context.Context) (notify.Settings, error) {
	stored, err := s.store.Settings(ctx)
	if err != nil {
		return notify.Settings{}, err
	}
	return notify.LoadSettings(stored), nil
}

func (s *Server) handleGetNotifications(w http.ResponseWriter, r *http.Request) error {
	cur, err := s.notificationSettings(r.Context())
	if err != nil {
		return err
	}
	v := notificationView(cur)
	v.Status = s.notifier.Status()
	return httpx.JSON(w, http.StatusOK, v)
}

func (s *Server) handleSetNotifications(w http.ResponseWriter, r *http.Request) error {
	var req notificationRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	cur, err := s.notificationSettings(r.Context())
	if err != nil {
		return err
	}
	cfg, fields := req.config(cur, req.Enabled)
	if len(fields) > 0 {
		return httpx.Invalid(fields)
	}
	cfg.PanelURL = s.panelURL(r)
	next := notify.Settings{Enabled: req.Enabled, Config: cfg}
	if err := s.store.SaveSettings(r.Context(), next.Values()); err != nil {
		return err
	}
	s.notifier.Configure(next.Active())

	detail := "off"
	if req.Enabled {
		detail = "to " + strings.Join(cfg.To, ", ")
	}
	s.events.Record(r.Context(), store.Event{Kind: "settings.notifications_changed", Detail: detail})
	return s.handleGetNotifications(w, r)
}

// Tests the form as it is, before it is saved.
func (s *Server) handleTestNotifications(w http.ResponseWriter, r *http.Request) error {
	var req notificationRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	cur, err := s.notificationSettings(r.Context())
	if err != nil {
		return err
	}
	cfg, fields := req.config(cur, true)
	if len(fields) > 0 {
		return httpx.Invalid(fields)
	}
	cfg.PanelURL = s.panelURL(r)
	if err := s.notifier.Test(r.Context(), cfg); err != nil {
		return httpx.Errorf(http.StatusBadGateway, "email_failed", "the test email could not be sent: %v", err)
	}
	return httpx.NoContent(w)
}

// complete demands everything needed to send, not just well-formed values.
func (req notificationRequest) config(cur notify.Settings, complete bool) (notify.Config, map[string]string) {
	fields := map[string]string{}
	cfg := notify.Config{
		SMTP: notify.SMTP{
			Host:     strings.TrimSpace(req.Host),
			Port:     req.Port,
			Security: req.Security,
			Username: strings.TrimSpace(req.Username),
			Password: cur.Password,
			From:     strings.TrimSpace(req.From),
		},
		Groups: []string{},
		To:     []string{},
	}
	if req.Password != nil {
		cfg.Password = *req.Password
	}

	switch {
	case cfg.Host == "" && complete:
		fields["host"] = "SMTP server is required"
	case cfg.Host != "" && !wg.ValidHost(cfg.Host):
		fields["host"] = "enter a hostname such as smtp.example.com, without a port"
	}
	if cfg.Port == 0 {
		cfg.Port = notify.DefaultPort
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		fields["port"] = "port must be between 1 and 65535"
	}
	if cfg.Security == "" {
		cfg.Security = notify.SecurityStartTLS
	}
	if !slices.Contains([]string{notify.SecurityStartTLS, notify.SecurityTLS, notify.SecurityNone}, cfg.Security) {
		fields["security"] = "security must be starttls, tls or none"
	}
	if cfg.Security == notify.SecurityNone && cfg.Username != "" {
		fields["security"] = "a password is never sent unencrypted; choose STARTTLS or TLS"
	}

	switch {
	case cfg.From == "" && complete:
		fields["from"] = "sender address is required"
	case cfg.From != "":
		if _, err := mail.ParseAddress(cfg.From); err != nil {
			fields["from"] = "sender must be an address such as tunploy@example.com"
		}
	}

	for _, raw := range req.To {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		addr, err := mail.ParseAddress(raw)
		if err != nil {
			fields["to"] = raw + " is not an email address"
			break
		}
		if !slices.ContainsFunc(cfg.To, func(to string) bool { return strings.EqualFold(to, addr.Address) }) {
			cfg.To = append(cfg.To, addr.Address)
		}
	}
	switch {
	case fields["to"] != "":
	case len(cfg.To) == 0 && complete:
		fields["to"] = "add at least one address to send to"
	case len(cfg.To) > maxRecipients:
		fields["to"] = "at most 10 addresses"
	}

	for _, g := range req.Events {
		if !notify.ValidGroup(g) {
			fields["events"] = "unknown event group " + strconv.Quote(g)
			break
		}
		if !slices.Contains(cfg.Groups, g) {
			cfg.Groups = append(cfg.Groups, g)
		}
	}
	return cfg, fields
}

// Links point at the panel domain once it has a certificate, else at the
// address the admin used to save these settings.
func (s *Server) panelURL(r *http.Request) string {
	if st := s.https.Status(); st.State == tlscert.StateReady && st.Domain != "" {
		return "https://" + st.Domain
	}
	scheme := "http"
	if clientOf(r).https {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
