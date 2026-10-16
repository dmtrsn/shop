package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"sweetshop/internal/application"
	"sweetshop/internal/delivery/httpapi"
	"sweetshop/internal/email"
	"sweetshop/internal/repository/memory"
	postgresrepo "sweetshop/internal/repository/postgres"
)

func TestRepositoryMode(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, mode := range []string{"", "production", "development", "demo", "typo"} {
		t.Run("mode="+mode, func(t *testing.T) {
			t.Setenv("APP_MODE", mode)
			t.Setenv("DATABASE_URL", " \t")
			repo, storage, cleanup, err := repository(context.Background(), logger)
			defer cleanup()
			if mode == "demo" || mode == "development" {
				if err != nil || repo == nil || storage != "memory" {
					t.Fatalf("explicit demo storage: %v", err)
				}
			} else if err == nil || repo != nil {
				t.Fatal("missing persistent configuration must fail closed")
			}
		})
	}
}

func TestInvalidDSNDoesNotFallBackOrExposeCredentials(t *testing.T) {
	t.Setenv("APP_MODE", "demo")
	t.Setenv("DATABASE_URL", "postgres://example:synthetic-secret@localhost/%zz")
	repo, _, cleanup, err := repository(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer cleanup()
	if repo != nil || err == nil {
		t.Fatal("invalid DSN should fail, including in demo mode")
	}
	if strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("configuration error exposed credential")
	}
}

func TestMigrationsEnabledDefaultsToVerifyOnlyInProduction(t *testing.T) {
	for _, tc := range []struct {
		mode, value string
		run, ok     bool
	}{
		{"production", "", false, true},
		{"development", "", true, true},
		{"demo", "", true, true},
		{"production", "true", true, true},
		{"production", "false", false, true},
		{"demo", "false", false, true},
		{"production", "TRUE", false, false},
		{"production", "1", false, false},
		{"demo", "flase", false, false},
	} {
		t.Run(tc.mode+"="+tc.value, func(t *testing.T) {
			t.Setenv("RUN_MIGRATIONS", tc.value)
			run, err := migrationsEnabled(tc.mode)
			if run != tc.run || (err == nil) != tc.ok {
				t.Fatalf("run=%v err=%v", run, err)
			}
		})
	}
}

func TestInvalidRunMigrationsFailsBeforeConnecting(t *testing.T) {
	t.Setenv("APP_MODE", "production")
	t.Setenv("RUN_MIGRATIONS", "yes")
	// Port 1 is never a PostgreSQL server; configuration must fail first.
	t.Setenv("DATABASE_URL", "postgres://example:synthetic-secret@127.0.0.1:1/shop?connect_timeout=1")
	repo, _, cleanup, err := repository(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer cleanup()
	if repo != nil || err == nil || !strings.Contains(err.Error(), "RUN_MIGRATIONS") {
		t.Fatalf("invalid RUN_MIGRATIONS: %v", err)
	}
}

func TestSchemaErrorGuidesTheOperator(t *testing.T) {
	behind := schemaError("verify schema", fmt.Errorf("%w: 1 pending", postgresrepo.ErrSchemaNotCurrent))
	if !strings.Contains(behind.Error(), "run `shopctl migrate`") {
		t.Fatal(behind)
	}
	fixtures := schemaError("verify schema", fmt.Errorf("%w: %w: use a fresh production database", postgresrepo.ErrFixturePolicyConflict, postgresrepo.ErrDemoFixturesApplied))
	if strings.Contains(fixtures.Error(), "shopctl migrate") || !strings.Contains(fixtures.Error(), "fresh production database") || !errors.Is(fixtures, postgresrepo.ErrDemoFixturesApplied) {
		t.Fatal(fixtures)
	}
	other := schemaError("migrate", errors.New("checksum mismatch"))
	if strings.Contains(other.Error(), "shopctl") || !strings.Contains(other.Error(), "migrate: checksum mismatch") {
		t.Fatal(other)
	}
}

func TestSessionConfigFailsClosedInProduction(t *testing.T) {
	for _, tc := range []struct {
		name, mode, origins, secure string
		ok                          bool
	}{
		{"production requires origins", "production", "", "", false},
		{"production rejects http", "production", "http://shop.example", "", false},
		{"production rejects insecure cookie", "production", "https://shop.example", "false", false},
		{"production rejects invalid entry", "production", "https://shop.example,https://*.example", "", false},
		{"production rejects empty entry", "production", "https://shop.example,,https://admin.example", "", false},
		{"invalid secure flag", "demo", "", "yes", false},
		{"production https", "production", "https://shop.example", "", true},
		{"production explicit secure", "production", "https://shop.example", "true", true},
		{"development defaults", "development", "", "", true},
		{"demo insecure http origin", "demo", "http://127.0.0.1:8080", "false", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TRUSTED_ORIGINS", tc.origins)
			t.Setenv("SESSION_COOKIE_SECURE", tc.secure)
			config, err := sessionConfig(tc.mode)
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v", err)
			}
			if err != nil && strings.Contains(err.Error(), "shop.example") {
				t.Fatal("configuration error exposed the configured value")
			}
			if tc.ok && tc.mode == "production" && (!config.Secure || len(config.TrustedOrigins) != 1) {
				t.Fatalf("production config: %#v", config)
			}
		})
	}
	t.Setenv("TRUSTED_ORIGINS", "")
	t.Setenv("SESSION_COOKIE_SECURE", "")
	config, err := sessionConfig("demo")
	if err != nil || config.Secure || strings.Join(config.TrustedOrigins, ",") != developmentTrustedOrigins {
		t.Fatalf("development defaults: %#v %v", config, err)
	}
	t.Setenv("SESSION_COOKIE_SECURE", "true")
	if config, _ := sessionConfig("development"); !config.Secure {
		t.Fatal("development may opt in to Secure cookies")
	}
}

func TestStaffOriginsConfig(t *testing.T) {
	production := httpapi.SessionConfig{Secure: true, TrustedOrigins: []string{"https://shop.example"}}
	for _, tc := range []struct {
		name, mode, raw string
		want            []string
		ok              bool
	}{
		{"unset disables staff", "production", "", nil, true},
		{"blank disables staff", "production", "  ", nil, true},
		{"production https", "production", "https://admin.shop.example", []string{"https://admin.shop.example"}, true},
		{"normalized", "production", " HTTPS://Admin.Shop.Example:443 ", []string{"https://admin.shop.example"}, true},
		{"production refuses http", "production", "http://admin.shop.example", nil, false},
		{"overlap is refused", "production", "https://admin.shop.example,https://shop.example", nil, false},
		{"overlap after normalization", "production", "https://SHOP.example:443", nil, false},
		{"wildcard", "production", "https://*.shop.example", nil, false},
		{"path", "production", "https://admin.shop.example/admin", nil, false},
		{"empty entry", "production", "https://admin.shop.example,", nil, false},
		{"development http loopback", "development", "http://127.0.0.1:8081", []string{"http://127.0.0.1:8081"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("STAFF_TRUSTED_ORIGINS", tc.raw)
			sessions := production
			if tc.mode == "development" {
				sessions = httpapi.SessionConfig{TrustedOrigins: []string{"http://127.0.0.1:8080"}}
			}
			origins, err := staffOriginsConfig(tc.mode, sessions)
			if (err == nil) != tc.ok || strings.Join(origins, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("origins=%v err=%v", origins, err)
			}
			if err != nil && strings.Contains(err.Error(), "shop.example") {
				t.Fatal("configuration error exposed the configured value")
			}
			if err != nil && !strings.HasPrefix(err.Error(), "STAFF_TRUSTED_ORIGINS entry") {
				t.Fatalf("error must name the staff variable: %v", err)
			}
		})
	}
	// Development defaults are customer origins too; reusing one is refused.
	t.Setenv("STAFF_TRUSTED_ORIGINS", "http://127.0.0.1:8080")
	if _, err := staffOriginsConfig("development", httpapi.SessionConfig{TrustedOrigins: strings.Split(developmentTrustedOrigins, ",")}); err == nil {
		t.Fatal("staff reused a development customer origin")
	}
}

func TestStaffRequiresPostgreSQL(t *testing.T) {
	if options, err := staffOptions(memory.NewDemo(), nil); err != nil || options != nil {
		t.Fatal("no staff origins means no staff option")
	}
	if _, err := staffOptions(memory.NewDemo(), []string{"https://admin.shop.example"}); err == nil {
		t.Fatal("staff enabled on the memory store")
	}
}

func TestEmailDispatcherConfiguration(t *testing.T) {
	sessions := httpapi.SessionConfig{Secure: true, TrustedOrigins: []string{"https://shop.example"}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	clear := func() {
		for _, key := range []string{"EMAIL_TRANSPORT", "SMTP_HOST", "SMTP_PORT", "SMTP_TLS", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_FROM", "PUBLIC_SHOP_ORIGIN"} {
			t.Setenv(key, "")
		}
	}
	clear()
	if d, err := emailDispatcher("production", sessions, logger); err != nil || d != nil {
		t.Fatalf("email disabled by default: %v", err)
	}
	t.Setenv("PUBLIC_SHOP_ORIGIN", "https://other.example")
	if _, err := emailDispatcher("production", sessions, logger); err == nil {
		t.Fatal("public origin outside TRUSTED_ORIGINS accepted")
	}
	t.Setenv("PUBLIC_SHOP_ORIGIN", "https://SHOP.example:443")
	if d, err := emailDispatcher("production", sessions, logger); err != nil || d != nil {
		t.Fatalf("normalized public origin without transport: %v", err)
	}
	t.Setenv("EMAIL_TRANSPORT", "log")
	if _, err := emailDispatcher("production", sessions, logger); err == nil {
		t.Fatal("log transport in production")
	}
	dev := httpapi.SessionConfig{TrustedOrigins: []string{"http://127.0.0.1:8080"}}
	t.Setenv("PUBLIC_SHOP_ORIGIN", "http://127.0.0.1:8080")
	d, err := emailDispatcher("development", dev, logger)
	if err != nil || d == nil {
		t.Fatalf("development log transport: %v", err)
	}
	if err := d.dispatcher.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	clear()
	t.Setenv("EMAIL_TRANSPORT", "smtp")
	t.Setenv("SMTP_HOST", "smtp.provider.example")
	t.Setenv("SMTP_TLS", "none")
	t.Setenv("SMTP_FROM", "shop@example.com")
	t.Setenv("SMTP_PASSWORD", "SENTINEL-SMTP-PASSWORD")
	t.Setenv("PUBLIC_SHOP_ORIGIN", "https://shop.example")
	if _, err := emailDispatcher("production", sessions, logger); err == nil || strings.Contains(err.Error(), "SENTINEL") {
		t.Fatalf("plaintext production SMTP: %v", err)
	}
}

// Self-registration exists only with PostgreSQL and a running dispatcher.
func TestSelfServiceNeedsEmailAndPostgreSQL(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	origin, err := email.ParsePublicOrigin("https://shop.example", true)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := email.NewDispatcher(email.LogMailer{Logger: logger}, logger, email.DefaultDispatcherOptions())
	if err != nil {
		t.Fatal(err)
	}
	mail := &outgoingEmail{dispatcher: dispatcher, origin: origin}
	postgresStore := postgresrepo.New(nil)
	memoryStore := memory.NewDemo()
	if r, err := selfService(postgresStore, application.New(postgresStore, application.Options{}), nil, logger); err != nil || r != nil {
		t.Fatalf("enabled without email: %v", err)
	}
	if r, err := selfService(memoryStore, application.New(memoryStore, application.Options{}), mail, logger); err != nil || r != nil {
		t.Fatalf("enabled on the memory store: %v", err)
	}
	if r, err := selfService(postgresStore, application.New(postgresStore, application.Options{}), mail, logger); err != nil || r == nil {
		t.Fatalf("not enabled with PostgreSQL and email: %v", err)
	}
}
