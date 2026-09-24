package server

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
)

type settingsJSON struct {
	PublicHost    string   `json:"public_host"`
	DefaultDNS    []string `json:"default_dns"`
	PublicHostEnv string   `json:"public_host_env"`
}

func TestSettingsDefaults(t *testing.T) {
	p := newPanel(t)

	var got settingsJSON
	p.want(p.do("GET", "/api/settings", nil), http.StatusOK, &got)
	if got.PublicHost != "" || got.PublicHostEnv != "vpn.example.com" ||
		!slices.Equal(got.DefaultDNS, []string{"1.1.1.1", "1.0.0.1"}) {
		t.Fatalf("settings = %+v", got)
	}
}

func TestSettingsShapeNewServers(t *testing.T) {
	p := newPanel(t)
	before := p.createInstance(map[string]any{"name": "Before"})

	var got settingsJSON
	p.want(p.do("PATCH", "/api/settings", map[string]any{
		"public_host": " panel.example.org ",
		"default_dns": []string{"9.9.9.9"},
	}), http.StatusOK, &got)
	if got.PublicHost != "panel.example.org" || !slices.Equal(got.DefaultDNS, []string{"9.9.9.9"}) {
		t.Fatalf("saved settings = %+v", got)
	}

	after := p.createInstance(map[string]any{"name": "After"})
	if after.Endpoint != "panel.example.org" || !slices.Equal(after.DNS, []string{"9.9.9.9"}) {
		t.Fatalf("new server ignored settings: %+v", after)
	}

	var unchanged instanceJSON
	p.want(p.do("GET", fmt.Sprintf("/api/instances/%d", before.ID), nil), http.StatusOK, &unchanged)
	if unchanged.Endpoint != "vpn.example.com" || len(unchanged.DNS) != 2 {
		t.Fatalf("existing server must keep its own values: %+v", unchanged)
	}
}

func TestSettingsEmptyValues(t *testing.T) {
	p := newPanel(t)

	p.want(p.do("PATCH", "/api/settings", map[string]any{"public_host": "a.example.org"}), http.StatusOK, nil)
	var got settingsJSON
	p.want(p.do("PATCH", "/api/settings", map[string]any{
		"public_host": "",
		"default_dns": []string{},
	}), http.StatusOK, &got)
	if got.PublicHost != "" || len(got.DefaultDNS) != 0 {
		t.Fatalf("settings = %+v", got)
	}

	// An empty public host falls back to TUNPLOY_PUBLIC_HOST; empty DNS
	// means clients keep their own resolver.
	in := p.createInstance(map[string]any{"name": "Home"})
	if in.Endpoint != "vpn.example.com" || len(in.DNS) != 0 {
		t.Fatalf("instance = %+v", in)
	}
}

func TestSettingsValidation(t *testing.T) {
	p := newPanel(t)

	e := p.wantError(p.do("PATCH", "/api/settings", map[string]any{
		"public_host": "vpn.example.com:51820",
		"default_dns": []string{"not-an-ip"},
	}), http.StatusUnprocessableEntity, "validation_failed")
	if e.Error.Fields["public_host"] == "" || e.Error.Fields["default_dns"] == "" {
		t.Fatalf("fields = %+v", e.Error.Fields)
	}

	var got settingsJSON
	p.want(p.do("GET", "/api/settings", nil), http.StatusOK, &got)
	if got.PublicHost != "" {
		t.Fatalf("a rejected update must save nothing: %+v", got)
	}
}
