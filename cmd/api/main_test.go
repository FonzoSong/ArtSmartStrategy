package main

import (
	"testing"
	"time"

	"github.com/arts-smart/strategy/internal/config"
)

func TestAPIWriteTimeoutAllowsConfiguredAIRecommendationAndPersistence(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.Config
		want time.Duration
	}{
		{name: "default server budget", cfg: config.Config{RequestTimeout: 15 * time.Second, AIRecommendationTimeout: 30 * time.Second}, want: 90 * time.Second},
		{name: "long AI wait", cfg: config.Config{RequestTimeout: 15 * time.Second, AIRecommendationTimeout: 3 * time.Minute}, want: 3*time.Minute + 30*time.Second},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := apiWriteTimeout(test.cfg); got != test.want {
				t.Fatalf("apiWriteTimeout() = %s, want %s", got, test.want)
			}
		})
	}
}
