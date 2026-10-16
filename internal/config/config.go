// Package config loads and validates environment-backed service configuration.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains runtime settings for the API and PostgreSQL service.
type Config struct {
	Address                 string
	DatabaseURL             string
	AppSecret               string
	AllowedOrigins          []string
	RequestTimeout          time.Duration
	AIRecommendationTimeout time.Duration
	PoolMax                 int
	PoolMin                 int
	AIBaseURL               string
	AIModel                 string
	AIAPIKey                string
	AIMockMode              bool
}

// Load reads and validates environment-backed service configuration.
func Load() (Config, error) {
	aiRecommendationTimeout := 30 * time.Second
	if raw := strings.TrimSpace(os.Getenv("AI_RECOMMENDATION_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, errors.New("AI_RECOMMENDATION_TIMEOUT must be a valid Go duration")
		}
		aiRecommendationTimeout = parsed
	}
	cfg := Config{
		Address:                 envOr("API_ADDR", ":8080"),
		DatabaseURL:             strings.TrimSpace(os.Getenv("DATABASE_URL")),
		AppSecret:               os.Getenv("APP_SECRET"),
		AllowedOrigins:          splitList(envOr("CORS_ALLOWED_ORIGINS", "http://localhost:5173")),
		RequestTimeout:          durationOr("REQUEST_TIMEOUT", 15*time.Second),
		AIRecommendationTimeout: aiRecommendationTimeout,
		PoolMax:                 intOr("DB_POOL_MAX", 20),
		PoolMin:                 intOr("DB_POOL_MIN", 2),
		AIBaseURL:               strings.TrimRight(envOr("AI_BASE_URL", "https://api.kunyou.asia/v1"), "/"),
		AIModel:                 envOr("AI_MODEL", "qwen3.8-max"),
		AIAPIKey:                os.Getenv("AI_API_KEY"),
		AIMockMode:              boolOr("AI_MOCK", boolOr("AI_MOCK_MODE", false)),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL must point to the PostgreSQL service")
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
	if cfg.AIRecommendationTimeout < time.Second || cfg.AIRecommendationTimeout > 10*time.Minute {
		return Config{}, errors.New("AI_RECOMMENDATION_TIMEOUT must be between 1s and 10m")
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// HealthcheckURL is the address the `healthcheck` subcommand probes. It follows
// API_ADDR, so moving the API off :8080 cannot silently leave the container
// healthcheck knocking on a port nothing listens on. HEALTHCHECK_URL overrides
// it for topologies where the health endpoint is reachable only through another
// host, port or scheme (a TLS terminator in front of the API, for example).
func HealthcheckURL() string {
	if override := strings.TrimSpace(os.Getenv("HEALTHCHECK_URL")); override != "" {
		return override
	}
	return LocalHealthURL(envOr("API_ADDR", ":8080"))
}

// LocalHealthURL turns a listen address — ":8080", "0.0.0.0:9090",
// "127.0.0.1:8080" or a bare port — into a probe URL. A wildcard or empty host
// becomes the loopback host, because that is the only address guaranteed to
// exist inside the container.
func LocalHealthURL(address string) string {
	host, port := splitHostPort(address)
	if port == "" {
		port = "8080"
	}
	host = strings.Trim(host, "[]")
	switch host {
	case "", "0.0.0.0", "::", "::0":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/api/health/ready"
}

func splitHostPort(address string) (string, string) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", ""
	}
	if host, port, err := net.SplitHostPort(address); err == nil {
		return host, port
	}
	if _, err := strconv.Atoi(address); err == nil {
		return "", address
	}
	return address, ""
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

func boolOr(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
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
