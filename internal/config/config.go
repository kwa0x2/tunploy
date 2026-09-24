// Package config loads Tunploy's runtime configuration from the environment.
package config

import (
	"fmt"
	"log/slog"
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
	LogLevel      slog.Level
}

func (c Config) DBPath() string { return filepath.Join(c.DataDir, "tunploy.db") }

// Load applies defaults and fails only on values that are present but
// unusable, so a bare `docker run` works out of the box.
func Load() (Config, error) {
	cfg := Config{
		Listen:        env("TUNPLOY_LISTEN", ":3000"),
		DataDir:       env("TUNPLOY_DATA_DIR", "/var/lib/tunploy"),
		DockerHost:    env("TUNPLOY_DOCKER_HOST", ""),
		PublicHost:    env("TUNPLOY_PUBLIC_HOST", ""),
		SessionTTL:    7 * 24 * time.Hour,
		SecureCookies: false,
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
