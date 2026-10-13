// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/arts-smart/strategy/internal/config"
	"github.com/arts-smart/strategy/internal/httpapi"
	"github.com/arts-smart/strategy/internal/migrate"
	"github.com/arts-smart/strategy/internal/repository"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := http.Client{Timeout: 3 * time.Second}
		response, err := client.Get(config.HealthcheckURL())
		if err != nil || response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_ = response.Body.Close()
		return
	}
	if err := run(logger); err != nil {
		logger.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(cfg.PoolMax)
	db.SetMaxIdleConns(cfg.PoolMin)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err = waitForDatabase(ctx, db, logger); err != nil {
		return err
	}
	if err = (migrate.Runner{Logger: logger}).Apply(ctx, db); err != nil {
		return err
	}
	handler := httpapi.New(cfg, repository.New(db), logger)
	server := &http.Server{
		Addr: cfg.Address, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 20 * time.Second, WriteTimeout: 90 * time.Second,
		IdleTimeout: 60 * time.Second,
	}
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	logger.Info("API listening", "address", cfg.Address, "database", cfg.RedactedURL())
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func waitForDatabase(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	var lastErr error
	for attempt := 1; attempt <= 6; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		lastErr = db.PingContext(pingCtx)
		cancel()
		if lastErr == nil {
			return nil
		}
		logger.Warn("database connection attempt failed", "attempt", attempt, "limit", 6, "error", lastErr)
		if attempt == 6 {
			break
		}
		delay := time.Duration(attempt) * time.Second
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}
