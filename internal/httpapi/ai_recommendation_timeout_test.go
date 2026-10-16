package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/arts-smart/strategy/internal/config"
)

func TestAIRecommendationGenerationGetsPersistenceTimeAfterModelTimeout(t *testing.T) {
	server := Server{Config: config.Config{RequestTimeout: 15 * time.Second, AIRecommendationTimeout: 42 * time.Second}}
	cases := []struct {
		method string
		path   string
		want   time.Duration
	}{
		{method: http.MethodPost, path: "/api/ai/recommend", want: 72 * time.Second},
		{method: http.MethodPost, path: "/api/ai/recommend/draft-id/regenerate", want: 72 * time.Second},
		{method: http.MethodGet, path: "/api/ai/recommend/draft-id", want: 15 * time.Second},
		{method: http.MethodPost, path: "/api/ai/recommend/draft-id/apply", want: 15 * time.Second},
	}
	for _, test := range cases {
		request, err := http.NewRequest(test.method, "http://example.test"+test.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := server.requestTimeout(request); got != test.want {
			t.Errorf("requestTimeout(%s %s) = %s, want %s", test.method, test.path, got, test.want)
		}
	}
}
