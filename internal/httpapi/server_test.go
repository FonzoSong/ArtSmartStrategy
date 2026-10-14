package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arts-smart/strategy/internal/config"
	"github.com/arts-smart/strategy/internal/repository"
)

func TestLiveHealthDoesNotRequireDatabaseQuery(t *testing.T) {
	cfg := config.Config{AppSecret: "01234567890123456789012345678901", AllowedOrigins: []string{"http://localhost:5173"}}
	handler := New(cfg, &repository.Postgres{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "/api/health/live", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestCorsRejectsUnknownOrigin(t *testing.T) {
	cfg := config.Config{AppSecret: "01234567890123456789012345678901", AllowedOrigins: []string{"http://localhost:5173"}}
	handler := New(cfg, &repository.Postgres{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "/api/health/live", nil)
	request.Header.Set("Origin", "https://bad.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d", response.Code)
	}
}

// The browser preflights every call the web client makes, because that client
// attaches X-Team-Id (and Authorization once signed in) to all of them. A
// preflight that does not echo those headers back makes the browser report a
// CORS failure and never send the real request, so login and every save appear
// to do nothing.
func TestCorsPreflightAllowsClientHeaders(t *testing.T) {
	cfg := config.Config{AppSecret: "01234567890123456789012345678901", AllowedOrigins: []string{"http://localhost:5173"}}
	handler := New(cfg, &repository.Postgres{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodOptions, "/api/auth/login", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "content-type,x-team-id")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("allow-origin = %q", got)
	}
	allowed := response.Header().Get("Access-Control-Allow-Headers")
	for _, header := range []string{"Content-Type", "X-Team-Id", "Authorization"} {
		if !strings.Contains(allowed, header) {
			t.Fatalf("allow-headers = %q, missing %s", allowed, header)
		}
	}
	if methods := response.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(methods, http.MethodPost) {
		t.Fatalf("allow-methods = %q, missing POST", methods)
	}
}

func TestOpenAIURLAvoidsDuplicatingVersionPrefix(t *testing.T) {
	if got := openAIURL("https://api.example/v1/", "/chat/completions"); got != "https://api.example/v1/chat/completions" {
		t.Fatalf("endpoint = %q", got)
	}
	if got := openAIURL("https://api.example/v1/v1", "/models"); got != "https://api.example/v1/models" {
		t.Fatalf("endpoint = %q", got)
	}
	if got := openAIURL("https://api.example", "/models"); got != "https://api.example/v1/models" {
		t.Fatalf("endpoint = %q", got)
	}
}
