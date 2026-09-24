package server

import (
	"context"
	"net/http"
	"net/netip"
	"strings"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/wg"
)

const (
	settingPublicHost = "public_host"
	settingDefaultDNS = "default_dns"

	maxDNSServers = 8
)

// panelSettings are the defaults new servers start from. Existing servers
// keep their own copy, so changing these never touches a live tunnel.
type panelSettings struct {
	PublicHost string       `json:"public_host"`
	DefaultDNS []netip.Addr `json:"default_dns"`
}

type settingsResponse struct {
	panelSettings
	// PublicHostEnv is TUNPLOY_PUBLIC_HOST, which applies while PublicHost
	// is empty.
	PublicHostEnv string `json:"public_host_env"`
}

type settingsRequest struct {
	PublicHost *string   `json:"public_host"`
	DefaultDNS *[]string `json:"default_dns"`
}

func (s *Server) settings(ctx context.Context) (panelSettings, error) {
	stored, err := s.store.Settings(ctx)
	if err != nil {
		return panelSettings{}, err
	}

	out := panelSettings{
		PublicHost: stored[settingPublicHost],
		DefaultDNS: wg.DefaultDNS,
	}
	if raw, ok := stored[settingDefaultDNS]; ok {
		out.DefaultDNS, err = parseEach(splitList(raw), netip.ParseAddr)
		if err != nil {
			return panelSettings{}, err
		}
	}
	return out, nil
}

// publicHost is the endpoint new servers get: the panel setting, or the
// environment when the admin has not set one.
func (p panelSettings) publicHost(env string) string {
	if p.PublicHost != "" {
		return p.PublicHost
	}
	return env
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) error {
	cur, err := s.settings(r.Context())
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, settingsResponse{panelSettings: cur, PublicHostEnv: s.cfg.PublicHost})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) error {
	var req settingsRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}

	fields := map[string]string{}
	values := map[string]string{}

	if req.PublicHost != nil {
		host := strings.TrimSpace(*req.PublicHost)
		if host != "" && !wg.ValidHost(host) {
			fields["public_host"] = "public host must be a hostname or IP address, without a port"
		}
		values[settingPublicHost] = host
	}
	if req.DefaultDNS != nil {
		addrs, err := parseEach(*req.DefaultDNS, netip.ParseAddr)
		switch {
		case err != nil:
			fields["default_dns"] = "dns must be a list of IP addresses"
		case len(addrs) > maxDNSServers:
			fields["default_dns"] = "at most 8 dns servers are allowed"
		}
		values[settingDefaultDNS] = joinAddrs(addrs)
	}

	if len(fields) > 0 {
		return httpx.Invalid(fields)
	}
	if err := s.store.SaveSettings(r.Context(), values); err != nil {
		return err
	}
	return s.handleGetSettings(w, r)
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
