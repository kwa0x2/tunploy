// Package config loads Tunploy's runtime configuration from the environment.
package config

import (
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen        string
	DataDir       string
	DockerHost    string
	PublicHost    string
	SessionTTL    time.Duration
	SecureCookies bool
	GeoIP         bool
	UpdateCheck   bool
	LogLevel      slog.Level

	// HTTPSListen is empty when HTTPS is off; the domain itself is set in the panel.
	HTTPSListen    string
	HTTPListen     string
	ACMEDirectory  string
	TrustedProxies []netip.Prefix
}

func (c Config) DBPath() string { return filepath.Join(c.DataDir, "tunploy.db") }

func Load() (Config, error) {
	cfg := Config{
		Listen:        env("TUNPLOY_LISTEN", ":3000"),
		DataDir:       env("TUNPLOY_DATA_DIR", "/var/lib/tunploy"),
		DockerHost:    env("TUNPLOY_DOCKER_HOST", ""),
		PublicHost:    env("TUNPLOY_PUBLIC_HOST", ""),
		SessionTTL:    7 * 24 * time.Hour,
		SecureCookies: false,
		GeoIP:         true,
		UpdateCheck:   true,
		HTTPSListen:   env("TUNPLOY_HTTPS_LISTEN", ":443"),
		HTTPListen:    env("TUNPLOY_HTTP_LISTEN", ":80"),
		ACMEDirectory: env("TUNPLOY_ACME_DIRECTORY", ""),
	}

	if raw := env("TUNPLOY_SESSION_TTL", ""); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TUNPLOY_SESSION_TTL: %w", err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("TUNPLOY_SESSION_TTL must be positive, got %s", raw)
		}
		cfg.SessionTTL = d
	}

	if raw := env("TUNPLOY_SECURE_COOKIES", ""); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TUNPLOY_SECURE_COOKIES: %w", err)
		}
		cfg.SecureCookies = b
	}

	if raw := env("TUNPLOY_GEOIP", ""); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TUNPLOY_GEOIP: %w", err)
		}
		cfg.GeoIP = b
	}

	if raw := env("TUNPLOY_UPDATE_CHECK", ""); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TUNPLOY_UPDATE_CHECK: %w", err)
		}
		cfg.UpdateCheck = b
	}

	if raw := env("TUNPLOY_HTTPS", ""); raw != "" {
		on, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TUNPLOY_HTTPS: %w", err)
		}
		if !on {
			cfg.HTTPSListen, cfg.HTTPListen = "", ""
		}
	}

	if raw := env("TUNPLOY_TRUSTED_PROXIES", ""); raw != "" {
		prefixes, err := parsePrefixes(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TUNPLOY_TRUSTED_PROXIES: %w", err)
		}
		cfg.TrustedProxies = prefixes
	}

	lvl, err := parseLevel(env("TUNPLOY_LOG_LEVEL", "info"))
	if err != nil {
		return Config{}, err
	}
	cfg.LogLevel = lvl

	abs, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve TUNPLOY_DATA_DIR: %w", err)
	}
	cfg.DataDir = abs

	return cfg, nil
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// Accepts CIDRs and bare addresses, comma or space separated.
func parsePrefixes(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' }) {
		if p, err := netip.ParsePrefix(f); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(f)
		if err != nil {
			return nil, fmt.Errorf("%q is neither an IP address nor a CIDR", f)
		}
		out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return out, nil
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("TUNPLOY_LOG_LEVEL: unknown level %q", s)
	}
}
