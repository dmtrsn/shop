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
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"sweetshop/internal/application"
	"sweetshop/internal/delivery/httpapi"
	"sweetshop/internal/domain"
	"sweetshop/internal/email"
	"sweetshop/internal/repository/memory"
	postgresrepo "sweetshop/internal/repository/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel()}))
	slog.SetDefault(logger)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	mode, err := appMode()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	sessions, err := sessionConfig(mode)
	if err != nil {
		logger.Error("invalid session configuration", "error", err)
		os.Exit(1)
	}
	staffOrigins, err := staffOriginsConfig(mode, sessions)
	if err != nil {
		logger.Error("invalid staff session configuration", "error", err)
		os.Exit(1)
	}
	mail, err := emailDispatcher(mode, sessions, logger)
	if err != nil {
		logger.Error("invalid email configuration", "error", err)
		os.Exit(1)
	}
	repo, storage, cleanup, err := repository(ctx, logger)
	if err != nil {
		logger.Error("initialize repository", "error", err)
		os.Exit(1)
	}
	defer cleanup()
	service := application.New(repo, application.Options{AllowDemoAccount: mode != "production"})
	options, err := staffOptions(repo, staffOrigins)
	if err != nil {
		logger.Error("invalid staff session configuration", "error", err)
		os.Exit(1)
	}
	registrations, resets, err := selfService(repo, service, mail, logger)
	if err != nil {
		logger.Error("invalid self-service configuration", "error", err)
		os.Exit(1)
	}
	cleanupDone := make(chan struct{})
	if registrations != nil {
		options = append(options, httpapi.WithSelfService(registrations, resets))
		go func() {
			defer close(cleanupDone)
			application.RunSelfServiceCleanup(ctx, selfServiceCleanupInterval, logger, registrations, resets)
		}()
	} else {
		close(cleanupDone)
	}
	handler := httpapi.New(service, logger, env("WEB_DIR", "web"), storage, sessions, options...)
	server := &http.Server{Addr: env("HTTP_ADDR", ":"+env("PORT", "8080")), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 1 << 20}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown", "error", err)
		}
	}()
	if mode == "production" {
		logger.Info("sweetshop server started", "address", server.Addr, "storage", storage, "mode", mode)
	} else {
		logger.Info("sweetshop server started", "address", server.Addr, "storage", storage, "mode", mode, "demoEmail", "demo@sugar.ru")
	}
	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server stopped", "error", err)
		os.Exit(1)
	}
	// ListenAndServe returns as soon as Shutdown starts; requests still in
	// flight may enqueue email, so drain only after Shutdown has finished.
	<-shutdownDone
	// The cleanup loop stops with ctx; wait so it never outlives the pool.
	<-cleanupDone
	if mail != nil {
		drainCtx, drainCancel := context.WithTimeout(context.Background(), emailDrainTimeout)
		defer drainCancel()
		if err := mail.dispatcher.Shutdown(drainCtx); err != nil {
			logger.Error("email drain", "error", err)
		}
	}
}

const (
	emailDrainTimeout          = 10 * time.Second
	selfServiceCleanupInterval = 15 * time.Minute
)

// outgoingEmail is a running dispatcher and the origin its links use.
type outgoingEmail struct {
	dispatcher *email.Dispatcher
	origin     email.PublicOrigin
}

// emailDispatcher validates the email configuration and starts the
// dispatcher, or returns nil when no transport is configured; the shop then
// works without email and without self-registration. PUBLIC_SHOP_ORIGIN,
// which emailed links use, must be one of the shop's own TRUSTED_ORIGINS.
func emailDispatcher(mode string, sessions httpapi.SessionConfig, logger *slog.Logger) (*outgoingEmail, error) {
	config, err := email.LoadConfig(os.Getenv, mode == "production")
	if err != nil {
		return nil, err
	}
	if origin := config.PublicOrigin.String(); origin != "" {
		trusted := false
		for _, candidate := range sessions.TrustedOrigins {
			trusted = trusted || candidate == origin
		}
		if !trusted {
			return nil, errors.New("PUBLIC_SHOP_ORIGIN must be one of TRUSTED_ORIGINS")
		}
	}
	if !config.Enabled() {
		logger.Info("email disabled")
		return nil, nil
	}
	mailer, err := config.NewMailer(logger)
	if err != nil {
		return nil, err
	}
	dispatcher, err := email.NewDispatcher(mailer, logger, email.DefaultDispatcherOptions())
	if err != nil {
		return nil, err
	}
	dispatcher.Start()
	logger.Info("email transport configured", "transport", config.Describe())
	return &outgoingEmail{dispatcher: dispatcher, origin: config.PublicOrigin}, nil
}

// selfService enables customer self-registration and password recovery
// only when they can work end to end: PostgreSQL keeps their state and a
// running dispatcher sends their email. Otherwise their routes do not exist
// and GET /api/auth/session reports selfService=false; operators still
// create accounts and set passwords with shopctl.
func selfService(repo domain.Repository, service *application.Service, mail *outgoingEmail, logger *slog.Logger) (*application.CustomerRegistrations, *application.PasswordResets, error) {
	if mail == nil {
		logger.Info("self-service disabled: email is not configured")
		return nil, nil, nil
	}
	store, ok := repo.(interface {
		application.RegistrationStore
		application.PasswordResetStore
	})
	if !ok {
		logger.Warn("self-service disabled: it needs PostgreSQL storage")
		return nil, nil, nil
	}
	registrations, err := application.NewCustomerRegistrations(store, service, mail.dispatcher, mail.origin, logger, application.RegistrationOptions{})
	if err != nil {
		return nil, nil, err
	}
	resets, err := application.NewPasswordResets(store, mail.dispatcher, mail.origin, logger, application.RegistrationOptions{})
	if err != nil {
		return nil, nil, err
	}
	logger.Info("self-service enabled: registration and password recovery")
	return registrations, resets, nil
}

func repository(ctx context.Context, logger *slog.Logger) (domain.Repository, string, func(), error) {
	url := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	mode, err := appMode()
	if err != nil {
		return nil, "", func() {}, err
	}
	if url == "" {
		if mode == "production" {
			return nil, "", func() {}, errors.New("DATABASE_URL is required in production mode; use APP_MODE=demo for local memory storage")
		}
		logger.Warn("using explicit in-memory demo storage", "mode", mode)
		return memory.NewDemo(), "memory", func() {}, nil
	}
	runMigrations, err := migrationsEnabled(mode)
	if err != nil {
		return nil, "", func() {}, err
	}
	fixtures, err := postgresrepo.FixturePolicyForAppMode(mode)
	if err != nil {
		return nil, "", func() {}, err
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		// pgx parse errors can include the supplied DSN and its credentials.
		return nil, "", func() {}, errors.New("invalid DATABASE_URL")
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
	migrator := postgresrepo.Migrator{Directory: env("MIGRATIONS_DIR", "migrations"), Fixtures: fixtures, Logger: logger}
	if runMigrations {
		if mode == "production" {
			logger.Warn("RUN_MIGRATIONS=true: applying migrations at production startup")
		}
		migrateCtx, migrateCancel := context.WithTimeout(ctx, startupMigrationTimeout)
		err := migrator.Migrate(migrateCtx, pool)
		migrateCancel()
		if err != nil {
			cleanup()
			return nil, "", func() {}, schemaError("migrate", err)
		}
	}
	// Every mode serves only a verified schema; verification never changes it.
	verifyCtx, verifyCancel := context.WithTimeout(ctx, 30*time.Second)
	err = migrator.Verify(verifyCtx, pool)
	verifyCancel()
	if err != nil {
		cleanup()
		return nil, "", func() {}, schemaError("verify schema", err)
	}
	return postgresrepo.New(pool), "postgres", cleanup, nil
}

// schemaError adds the operator action. Only a schema that is merely behind
// points to shopctl migrate; applied demo fixtures in production have no
// automatic remedy.
func schemaError(stage string, err error) error {
	switch {
	case errors.Is(err, postgresrepo.ErrDemoFixturesApplied):
		return fmt.Errorf("%s: %w (refusing to serve production from it; no automatic remediation exists)", stage, err)
	case errors.Is(err, postgresrepo.ErrSchemaNotCurrent):
		return fmt.Errorf("%w; run `shopctl migrate` before starting the server", err)
	default:
		return fmt.Errorf("%s: %w", stage, err)
	}
}

const startupMigrationTimeout = 10 * time.Minute

// migrationsEnabled reads RUN_MIGRATIONS. Unset, production only verifies the
// schema (apply it with `shopctl migrate`); development and demo apply pending
// migrations at startup. Only exact true/false override the mode default.
func migrationsEnabled(mode string) (bool, error) {
	switch os.Getenv("RUN_MIGRATIONS") {
	case "":
		return mode != "production", nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errors.New("RUN_MIGRATIONS must be true or false")
	}
}

func appMode() (string, error) {
	mode := env("APP_MODE", "production")
	if mode != "production" && mode != "development" && mode != "demo" {
		return "", errors.New("APP_MODE must be production, development, or demo")
	}
	return mode, nil
}

// developmentTrustedOrigins covers the Go server and the Next dev server on
// loopback; production has no default and must configure https origins.
const developmentTrustedOrigins = "http://127.0.0.1:8080,http://localhost:8080,http://127.0.0.1:3000,http://localhost:3000"

// sessionConfig fails closed in production: TRUSTED_ORIGINS is required and
// https-only, and SESSION_COOKIE_SECURE cannot be disabled.
func sessionConfig(mode string) (httpapi.SessionConfig, error) {
	production := mode == "production"
	secure := production
	switch raw := os.Getenv("SESSION_COOKIE_SECURE"); raw {
	case "":
	case "true":
		secure = true
	case "false":
		if production {
			return httpapi.SessionConfig{}, errors.New("SESSION_COOKIE_SECURE=false is not allowed in production")
		}
		secure = false
	default:
		return httpapi.SessionConfig{}, errors.New("SESSION_COOKIE_SECURE must be true or false")
	}
	raw := os.Getenv("TRUSTED_ORIGINS")
	if strings.TrimSpace(raw) == "" {
		if production {
			return httpapi.SessionConfig{}, errors.New("TRUSTED_ORIGINS is required in production mode")
		}
		raw = developmentTrustedOrigins
	}
	origins, err := httpapi.ParseTrustedOrigins(raw, production)
	if err != nil {
		return httpapi.SessionConfig{}, err
	}
	return httpapi.SessionConfig{Secure: secure, TrustedOrigins: origins}, nil
}

// staffOriginsConfig reads STAFF_TRUSTED_ORIGINS. Unset means no staff API.
// Origins follow the customer rules (https-only in production, exact,
// normalized) and must not overlap TRUSTED_ORIGINS, so a staff session and a
// customer session never share an origin.
func staffOriginsConfig(mode string, sessions httpapi.SessionConfig) ([]string, error) {
	raw := os.Getenv("STAFF_TRUSTED_ORIGINS")
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	origins, err := httpapi.ParseStaffTrustedOrigins(raw, mode == "production")
	if err != nil {
		return nil, err
	}
	customer := map[string]bool{}
	for _, origin := range sessions.TrustedOrigins {
		customer[origin] = true
	}
	for i, origin := range origins {
		if customer[origin] {
			return nil, fmt.Errorf("STAFF_TRUSTED_ORIGINS entry %d is also in TRUSTED_ORIGINS; staff need a separate origin", i+1)
		}
	}
	return origins, nil
}

// staffOptions enables the staff API only with PostgreSQL: the memory store
// has no staff persistence, and configuring staff for it is an error.
func staffOptions(repo domain.Repository, origins []string) ([]httpapi.Option, error) {
	if len(origins) == 0 {
		return nil, nil
	}
	store, ok := repo.(application.StaffStore)
	if !ok {
		return nil, errors.New("STAFF_TRUSTED_ORIGINS requires PostgreSQL storage")
	}
	options := []httpapi.Option{httpapi.WithStaff(application.NewStaffAuth(store), httpapi.StaffConfig{TrustedOrigins: origins})}
	if products, ok := repo.(application.ProductAdminStore); ok {
		options = append(options, httpapi.WithProductAdmin(application.NewProductAdmin(products)))
	}
	return options, nil
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
