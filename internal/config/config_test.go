package config

import (
	"testing"
)

func TestLocalHealthURLFollowsListenAddress(t *testing.T) {
	cases := map[string]string{
		":8080":          "http://127.0.0.1:8080/api/health/ready",
		":9090":          "http://127.0.0.1:9090/api/health/ready",
		"0.0.0.0:8080":   "http://127.0.0.1:8080/api/health/ready",
		"127.0.0.1:8080": "http://127.0.0.1:8080/api/health/ready",
		"localhost:8081": "http://localhost:8081/api/health/ready",
		"8080":           "http://127.0.0.1:8080/api/health/ready",
		"10.0.0.5:8080":  "http://10.0.0.5:8080/api/health/ready",
		"[::]:8080":      "http://127.0.0.1:8080/api/health/ready",
		"":               "http://127.0.0.1:8080/api/health/ready",
		"  :18080  ":     "http://127.0.0.1:18080/api/health/ready",
	}
	for address, want := range cases {
		if got := LocalHealthURL(address); got != want {
			t.Errorf("LocalHealthURL(%q) = %q, want %q", address, got, want)
		}
	}
}

func TestHealthcheckURLUsesAPIAddrAndOverride(t *testing.T) {
	t.Setenv("API_ADDR", ":19000")
	t.Setenv("HEALTHCHECK_URL", "")
	if got, want := HealthcheckURL(), "http://127.0.0.1:19000/api/health/ready"; got != want {
		t.Errorf("HealthcheckURL() = %q, want %q", got, want)
	}

	// An explicit override wins, so deployments that expose the health endpoint
	// through another host or scheme keep a working container healthcheck.
	t.Setenv("HEALTHCHECK_URL", "https://arts.example.com/api/health/ready")
	if got, want := HealthcheckURL(), "https://arts.example.com/api/health/ready"; got != want {
		t.Errorf("HealthcheckURL() with override = %q, want %q", got, want)
	}
}

func TestHealthcheckURLDefaultsToTheDocumentedPort(t *testing.T) {
	t.Setenv("API_ADDR", "")
	t.Setenv("HEALTHCHECK_URL", "")
	if got, want := HealthcheckURL(), "http://127.0.0.1:8080/api/health/ready"; got != want {
		t.Errorf("HealthcheckURL() = %q, want %q", got, want)
	}
}
