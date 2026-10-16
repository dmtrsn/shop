package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

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
