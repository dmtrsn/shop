package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
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
