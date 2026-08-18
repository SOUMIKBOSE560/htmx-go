package config

import (
	"log/slog"
	"os"
	"time"
)

// Config holds all runtime configuration, sourced from environment variables.
type Config struct {
	Port          string
	DatabaseURL   string
	SessionSecret string
	SessionTTL    time.Duration
	LogFormat     string
}

// Load reads configuration from the environment, applying development-friendly
// defaults. Production deployments should set every variable explicitly.
func Load() Config {
	cfg := Config{
		Port:          getenv("PORT", "8080"),
		DatabaseURL:   getenv("DATABASE_URL", "postgres://pageturner:pageturner@localhost:5433/pageturner?sslmode=disable"),
		SessionSecret: getenv("SESSION_SECRET", "dev-secret-change-me-in-production"),
		SessionTTL:    durationEnv("SESSION_TTL", 168*time.Hour),
		LogFormat:     getenv("LOG_FORMAT", "text"),
	}

	if cfg.SessionSecret == "dev-secret-change-me-in-production" {
		slog.Warn("SESSION_SECRET is unset; using an insecure development secret. Set SESSION_SECRET in production.")
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
