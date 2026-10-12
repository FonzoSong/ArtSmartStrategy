// Package config loads and validates environment-backed service configuration.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains runtime settings for the API and cloud PostgreSQL service.
type Config struct {
	Address        string
	DatabaseURL    string
	AppSecret      string
	AllowedOrigins []string
	RequestTimeout time.Duration
	PoolMax        int
	PoolMin        int
	AIBaseURL      string
	AIModel        string
	AIAPIKey       string
}

// Load reads environment variables and refuses local or implicit databases.
func Load() (Config, error) {
	cfg := Config{
		Address:        envOr("API_ADDR", ":8080"),
		DatabaseURL:    strings.TrimSpace(os.Getenv("DATABASE_URL")),
		AppSecret:      os.Getenv("APP_SECRET"),
		AllowedOrigins: splitList(envOr("CORS_ALLOWED_ORIGINS", "http://localhost:5173")),
		RequestTimeout: durationOr("REQUEST_TIMEOUT", 15*time.Second),
		PoolMax:        intOr("DB_POOL_MAX", 20),
		PoolMin:        intOr("DB_POOL_MIN", 2),
		AIBaseURL:      strings.TrimRight(envOr("AI_BASE_URL", "https://api.kunyou.asia/v1"), "/"),
		AIModel:        envOr("AI_MODEL", "qwen3.8-max"),
		AIAPIKey:       os.Getenv("AI_API_KEY"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL must point to the managed PostgreSQL service")
	}
	parsed, err := url.Parse(cfg.DatabaseURL)
	if err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return Config{}, errors.New("DATABASE_URL must use the PostgreSQL protocol")
	}
	if parsed.Hostname() == "" {
		return Config{}, errors.New("DATABASE_URL must include a database hostname")
	}
	if len(cfg.AppSecret) < 32 {
		return Config{}, errors.New("APP_SECRET must contain at least 32 characters")
	}
	if cfg.PoolMin < 0 || cfg.PoolMax < 1 || cfg.PoolMin > cfg.PoolMax {
		return Config{}, errors.New("DB_POOL_MIN/MAX configuration is invalid")
	}
	if cfg.RequestTimeout < time.Second || cfg.RequestTimeout > time.Minute {
		return Config{}, errors.New("REQUEST_TIMEOUT must be between 1s and 60s")
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitList(value string) []string {
	values := strings.Split(value, ",")
	result := make([]string, 0, len(values))
	for _, item := range values {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func durationOr(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return duration
}

func intOr(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// RedactedURL provides safe connection diagnostics without credentials.
func (c Config) RedactedURL() string {
	parsed, err := url.Parse(c.DatabaseURL)
	if err != nil {
		return "postgresql://<invalid>"
	}
	return fmt.Sprintf("postgresql://%s/%s", parsed.Host, strings.TrimPrefix(parsed.Path, "/"))
}
