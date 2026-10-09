package server

import (
	"net/http"
	"net/netip"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/instance"
)

type settingsView struct {
	PublicHost string       `json:"public_host"`
	DefaultDNS []netip.Addr `json:"default_dns"`
	// Applies while PublicHost is empty.
	PublicHostEnv string `json:"public_host_env"`
}

func (s *Server) writeSettings(w http.ResponseWriter, cur instance.Settings) error {
	return httpx.JSON(w, http.StatusOK, settingsView{
		PublicHost:    cur.PublicHost,
		DefaultDNS:    cur.DefaultDNS,
		PublicHostEnv: s.cfg.PublicHost,
	})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) error {
	cur, err := s.instances.Settings(r.Context())
	if err != nil {
		return err
	}
	return s.writeSettings(w, cur)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) error {
	var req instance.SettingsChange
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	cur, err := s.instances.UpdateSettings(r.Context(), req)
	if err != nil {
		return err
	}
	return s.writeSettings(w, cur)
}
