package config

import (
	"net/netip"
	"slices"
	"testing"
)

func TestHTTPSOff(t *testing.T) {
	t.Setenv("TUNPLOY_DATA_DIR", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPSListen != ":443" || cfg.HTTPListen != ":80" {
		t.Fatalf("defaults = %q %q", cfg.HTTPSListen, cfg.HTTPListen)
	}

	t.Setenv("TUNPLOY_HTTPS", "false")
	if cfg, err = Load(); err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPSListen != "" || cfg.HTTPListen != "" {
		t.Fatalf("off left %q %q", cfg.HTTPSListen, cfg.HTTPListen)
	}
}

func TestTrustedProxies(t *testing.T) {
	t.Setenv("TUNPLOY_DATA_DIR", t.TempDir())
	t.Setenv("TUNPLOY_TRUSTED_PROXIES", "172.16.0.0/12, 10.1.2.3 ::1,192.168.1.7/24")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("10.1.2.3/32"),
		netip.MustParsePrefix("::1/128"),
		netip.MustParsePrefix("192.168.1.0/24"),
	}
	if !slices.Equal(cfg.TrustedProxies, want) {
		t.Fatalf("got %v, want %v", cfg.TrustedProxies, want)
	}

	t.Setenv("TUNPLOY_TRUSTED_PROXIES", "10.0.0.0/8, proxy.local")
	if _, err := Load(); err == nil {
		t.Fatal("a hostname must be rejected")
	}
}
