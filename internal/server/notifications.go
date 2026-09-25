package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"slices"
	"strconv"
	"strings"

	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/notify"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/tlscert"
	"github.com/kwa0x2/tunploy/internal/wg"
)

const (
	settingNotifyEnabled  = "notify_enabled"
	settingNotifyTo       = "notify_to"
	settingNotifyGroups   = "notify_groups"
	settingNotifyPanelURL = "notify_panel_url"
	settingSMTPHost       = "smtp_host"
	settingSMTPPort       = "smtp_port"
	settingSMTPSecurity   = "smtp_security"
	settingSMTPUsername   = "smtp_username"
	// Stored as is, like the WireGuard keys: whoever reads the database owns the VPN anyway.
	settingSMTPPassword = "smtp_password"
	settingSMTPFrom     = "smtp_from"

	defaultSMTPPort = 587
	maxRecipients   = 10
)

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

// RunNotifications loads the saved email settings and sends until ctx ends.
func (s *Server) RunNotifications(ctx context.Context) {
	stored, err := s.store.Settings(ctx)
	if err != nil {
		slog.Error("load notification settings", "error", err)
	} else {
		s.notifier.Configure(notifyConfig(stored))
	}
	s.notifier.Run(ctx)
}

func notificationView(stored map[string]string) notificationSettings {
	port, _ := strconv.Atoi(stored[settingSMTPPort])
	if port == 0 {
		port = defaultSMTPPort
	}
	security := stored[settingSMTPSecurity]
	if security == "" {
		security = notify.SecurityStartTLS
	}
	groups := notify.DefaultGroups
	if raw, ok := stored[settingNotifyGroups]; ok {
		groups = splitList(raw)
	}
	return notificationSettings{
		Enabled:     stored[settingNotifyEnabled] == "true",
		Host:        stored[settingSMTPHost],
		Port:        port,
		Security:    security,
		Username:    stored[settingSMTPUsername],
		PasswordSet: stored[settingSMTPPassword] != "",
		From:        stored[settingSMTPFrom],
		To:          nonNil(splitList(stored[settingNotifyTo])),
		Events:      nonNil(groups),
	}
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// nil when email is off or not set up.
func notifyConfig(stored map[string]string) *notify.Config {
	v := notificationView(stored)
	if !v.Enabled || v.Host == "" || len(v.To) == 0 {
		return nil
	}
	return &notify.Config{
		SMTP: notify.SMTP{
			Host:     v.Host,
			Port:     v.Port,
			Security: v.Security,
			Username: v.Username,
			Password: stored[settingSMTPPassword],
			From:     v.From,
		},
		To:       v.To,
		Groups:   v.Events,
		PanelURL: stored[settingNotifyPanelURL],
	}
}

func (s *Server) handleGetNotifications(w http.ResponseWriter, r *http.Request) error {
	stored, err := s.store.Settings(r.Context())
	if err != nil {
		return err
	}
	v := notificationView(stored)
	v.Status = s.notifier.Status()
	return httpx.JSON(w, http.StatusOK, v)
}

func (s *Server) handleSetNotifications(w http.ResponseWriter, r *http.Request) error {
	var req notificationRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	stored, err := s.store.Settings(r.Context())
	if err != nil {
		return err
	}
	cfg, fields := req.config(stored, req.Enabled)
	if len(fields) > 0 {
		return httpx.Invalid(fields)
	}

	values := map[string]string{
		settingNotifyEnabled:  strconv.FormatBool(req.Enabled),
		settingSMTPHost:       cfg.Host,
		settingSMTPPort:       strconv.Itoa(cfg.Port),
		settingSMTPSecurity:   cfg.Security,
		settingSMTPUsername:   cfg.Username,
		settingSMTPPassword:   cfg.Password,
		settingSMTPFrom:       cfg.From,
		settingNotifyTo:       strings.Join(cfg.To, ","),
		settingNotifyGroups:   strings.Join(cfg.Groups, ","),
		settingNotifyPanelURL: s.panelURL(r),
	}
	if err := s.store.SaveSettings(r.Context(), values); err != nil {
		return err
	}
	for k, v := range values {
		stored[k] = v
	}
	s.notifier.Configure(notifyConfig(stored))

	detail := "off"
	if req.Enabled {
		detail = "to " + strings.Join(cfg.To, ", ")
	}
	s.record(r.Context(), store.Event{Kind: "settings.notifications_changed", Detail: detail})
	return s.handleGetNotifications(w, r)
}

// Tests the form as it is, before it is saved.
func (s *Server) handleTestNotifications(w http.ResponseWriter, r *http.Request) error {
	var req notificationRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	stored, err := s.store.Settings(r.Context())
	if err != nil {
		return err
	}
	cfg, fields := req.config(stored, true)
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
func (req notificationRequest) config(stored map[string]string, complete bool) (notify.Config, map[string]string) {
	fields := map[string]string{}
	cfg := notify.Config{
		SMTP: notify.SMTP{
			Host:     strings.TrimSpace(req.Host),
			Port:     req.Port,
			Security: req.Security,
			Username: strings.TrimSpace(req.Username),
			Password: stored[settingSMTPPassword],
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
		cfg.Port = defaultSMTPPort
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

func (s *Server) serverHealth(h deploy.ServerHealth) {
	ctx := context.Background()
	in, err := s.store.InstanceByID(ctx, h.InstanceID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("load instance for health event", "instance", h.InstanceID, "error", err)
		}
		return
	}
	kind := "server.recovered"
	if h.Down {
		kind = "server.down"
	}
	e := instanceEvent(kind, in)
	e.Detail = h.Reason
	s.record(ctx, e)
}
