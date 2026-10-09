package notify

import (
	"strconv"
	"strings"
)

const (
	settingEnabled      = "notify_enabled"
	settingTo           = "notify_to"
	settingGroups       = "notify_groups"
	settingPanelURL     = "notify_panel_url"
	settingSMTPHost     = "smtp_host"
	settingSMTPPort     = "smtp_port"
	settingSMTPSecurity = "smtp_security"
	settingSMTPUsername = "smtp_username"
	// Stored as is, like the WireGuard keys: whoever reads the database owns the VPN anyway.
	settingSMTPPassword = "smtp_password"
	settingSMTPFrom     = "smtp_from"

	DefaultPort = 587
)

// Settings is the email setup as the settings table keeps it.
type Settings struct {
	Enabled bool
	Config
}

func LoadSettings(stored map[string]string) Settings {
	port, _ := strconv.Atoi(stored[settingSMTPPort])
	if port == 0 {
		port = DefaultPort
	}
	security := stored[settingSMTPSecurity]
	if security == "" {
		security = SecurityStartTLS
	}
	groups := DefaultGroups
	if raw, ok := stored[settingGroups]; ok {
		groups = splitList(raw)
	}
	return Settings{
		Enabled: stored[settingEnabled] == "true",
		Config: Config{
			SMTP: SMTP{
				Host:     stored[settingSMTPHost],
				Port:     port,
				Security: security,
				Username: stored[settingSMTPUsername],
				Password: stored[settingSMTPPassword],
				From:     stored[settingSMTPFrom],
			},
			To:       splitList(stored[settingTo]),
			Groups:   groups,
			PanelURL: stored[settingPanelURL],
		},
	}
}

func (s Settings) Values() map[string]string {
	return map[string]string{
		settingEnabled:      strconv.FormatBool(s.Enabled),
		settingSMTPHost:     s.Host,
		settingSMTPPort:     strconv.Itoa(s.Port),
		settingSMTPSecurity: s.Security,
		settingSMTPUsername: s.Username,
		settingSMTPPassword: s.Password,
		settingSMTPFrom:     s.From,
		settingTo:           strings.Join(s.To, ","),
		settingGroups:       strings.Join(s.Groups, ","),
		settingPanelURL:     s.PanelURL,
	}
}

// Active is what to send with; nil while email is off or not set up.
func (s Settings) Active() *Config {
	if !s.Enabled || s.Host == "" || len(s.To) == 0 {
		return nil
	}
	cfg := s.Config
	return &cfg
}

// Load configures the notifier from the saved settings.
func (n *Notifier) Load(stored map[string]string) {
	n.Configure(LoadSettings(stored).Active())
}

func splitList(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ',' })
}
