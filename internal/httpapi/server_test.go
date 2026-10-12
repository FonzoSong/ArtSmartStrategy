package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
