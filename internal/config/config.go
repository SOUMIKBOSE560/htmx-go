package config

import (
	"log/slog"
	"os"
	"time"
)

// Config holds all runtime configuration, sourced from environment variables.
type Config struct {
	Port            string
	DatabaseURL     string
	SessionSecret   string
	SessionTTL      time.Duration
	LogFormat       string
	SelfPingEnabled bool
	SelfPingURL     string
	SelfPingEvery   time.Duration
}

// Load reads configuration from the environment, applying development-friendly
// defaults. Production deployments should set every variable explicitly.
func Load() Config {
	cfg := Config{
		Port:            getenv("PORT", "8909"),
		DatabaseURL:     getenv("DATABASE_URL", "file:data/pageturner.db?_busy_timeout=5000&_foreign_keys=on"),
		SessionSecret:   getenv("SESSION_SECRET", "dev-secret-change-me-in-production"),
		SessionTTL:      durationEnv("SESSION_TTL", 168*time.Hour),
		LogFormat:       getenv("LOG_FORMAT", "text"),
		SelfPingEnabled: boolEnv("SELF_PING_ENABLED", false),
		SelfPingURL:     getenv("SELF_PING_URL", ""),
		SelfPingEvery:   durationEnv("SELF_PING_INTERVAL", 15*time.Minute),
	}

	if cfg.SessionSecret == "dev-secret-change-me-in-production" {
		slog.Warn("SESSION_SECRET is unset; using an insecure development secret. Set SESSION_SECRET in production.")
	}
	if cfg.SelfPingEnabled && cfg.SelfPingURL == "" {
		slog.Warn("SELF_PING_ENABLED is true but SELF_PING_URL is empty; self-ping scheduler will stay idle.")
	}
	return cfg
}

// CookieSecure reports whether session/CSRF cookies should set the Secure flag.
func (c Config) CookieSecure() bool {
	return os.Getenv("COOKIE_SECURE") == "true"
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func boolEnv(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		switch v {
		case "1", "t", "T", "true", "TRUE", "True":
			return true
		case "0", "f", "F", "false", "FALSE", "False":
			return false
		}
	}
	return fallback
}
