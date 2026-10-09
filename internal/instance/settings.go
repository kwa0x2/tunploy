package instance

import (
	"context"
	"net/netip"
	"strings"

	"github.com/kwa0x2/tunploy/internal/apperr"
	"github.com/kwa0x2/tunploy/internal/store"
	"github.com/kwa0x2/tunploy/internal/wg"
)

const (
	settingPublicHost = "public_host"
	settingDefaultDNS = "default_dns"

	maxDNSServers = 8
)

// Settings are what a new server starts from. Existing servers keep their
// own copy, so a change never touches a live tunnel.
type Settings struct {
	// Empty falls back to the publicHost given to New.
	PublicHost string
	DefaultDNS []netip.Addr
}

func (s *Service) Settings(ctx context.Context) (Settings, error) {
	stored, err := s.store.Settings(ctx)
	if err != nil {
		return Settings{}, err
	}
	out := Settings{PublicHost: stored[settingPublicHost], DefaultDNS: wg.DefaultDNS}
	if raw, ok := stored[settingDefaultDNS]; ok {
		out.DefaultDNS, err = parseEach(splitList(raw), netip.ParseAddr)
		if err != nil {
			return Settings{}, err
		}
	}
	return out, nil
}

// PublicHost is where clients reach the panel's own machine.
func (s *Service) PublicHost(ctx context.Context) (string, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return "", err
	}
	if settings.PublicHost != "" {
		return settings.PublicHost, nil
	}
	return s.publicHost, nil
}

// SettingsChange leaves a nil field as it is.
type SettingsChange struct {
	PublicHost *string   `json:"public_host"`
	DefaultDNS *[]string `json:"default_dns"`
}

func (s *Service) UpdateSettings(ctx context.Context, c SettingsChange) (Settings, error) {
	fields := map[string]string{}
	values := map[string]string{}

	if c.PublicHost != nil {
		host := strings.TrimSpace(*c.PublicHost)
		if host != "" && !wg.ValidHost(host) {
			fields["public_host"] = "public host must be a hostname or IP address, without a port"
		}
		values[settingPublicHost] = host
	}
	if c.DefaultDNS != nil {
		addrs, err := parseEach(*c.DefaultDNS, netip.ParseAddr)
		switch {
		case err != nil:
			fields["default_dns"] = "dns must be a list of IP addresses"
		case len(addrs) > maxDNSServers:
			fields["default_dns"] = "at most 8 dns servers are allowed"
		}
		values[settingDefaultDNS] = joinAddrs(addrs)
	}

	if err := apperr.Fields(fields); err != nil {
		return Settings{}, err
	}
	if err := s.store.SaveSettings(ctx, values); err != nil {
		return Settings{}, err
	}
	s.events.Record(ctx, store.Event{Kind: "settings.updated"})
	return s.Settings(ctx)
}

func splitList(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ',' })
}

func joinAddrs(addrs []netip.Addr) string {
	parts := make([]string, len(addrs))
	for i, a := range addrs {
		parts[i] = a.String()
	}
	return strings.Join(parts, ",")
}
