package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"sweetshop/internal/application"
	"sweetshop/internal/delivery/httpapi"
	"sweetshop/internal/domain"
	"sweetshop/internal/repository/memory"
	postgresrepo "sweetshop/internal/repository/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel()}))
	slog.SetDefault(logger)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	repo, storage, cleanup, err := repository(ctx, logger)
	if err != nil {
		logger.Error("initialize repository", "error", err)
		os.Exit(1)
	}
	defer cleanup()
	service := application.New(repo)
	handler := httpapi.New(service, logger, env("WEB_DIR", "web"), storage)
	server := &http.Server{Addr: env("HTTP_ADDR", ":"+env("PORT", "8080")), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 1 << 20}
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown", "error", err)
		}
	}()
	logger.Info("sweetshop server started", "address", server.Addr, "storage", storage, "demoEmail", "demo@sugar.ru")
	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server stopped", "error", err)
		os.Exit(1)
	}
}

func repository(ctx context.Context, logger *slog.Logger) (domain.Repository, string, func(), error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		logger.Warn("DATABASE_URL is not set; using in-memory demo storage")
		return memory.NewDemo(), "memory", func() {}, nil
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, "", func() {}, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	config.MaxConns = int32(envInt("DB_MAX_CONNS", max(4, runtime.NumCPU())))
	config.MinConns = int32(envInt("DB_MIN_CONNS", 1))
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 15 * time.Minute
	config.HealthCheckPeriod = time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, "", func() {}, fmt.Errorf("connect postgres: %w", err)
	}
	cleanup := func() { pool.Close() }
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err := pool.Ping(pingCtx); err != nil {
		cleanup()
		return nil, "", func() {}, fmt.Errorf("ping postgres: %w", err)
	}
	if env("RUN_MIGRATIONS", "true") != "false" {
		if err := postgresrepo.Migrate(ctx, pool, env("MIGRATIONS_DIR", "migrations")); err != nil {
			cleanup()
			return nil, "", func() {}, err
		}
	}
	return postgresrepo.New(pool), "postgres", cleanup, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}
func logLevel() slog.Level {
	switch env("LOG_LEVEL", "info") {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
