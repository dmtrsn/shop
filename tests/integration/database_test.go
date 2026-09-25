//go:build integration

package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	postgres "sweetshop/internal/repository/postgres"
)

type database struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	store   *postgres.Store
	service *application.Service
}

func newDatabase(t *testing.T, migrate bool) *database {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL required: run make test-integration for a disposable cluster")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid TEST_DATABASE_URL")
	}
	// The caller must explicitly identify a dedicated test administrator.
	if config.ConnConfig.User != "shop_test_admin" || config.ConnConfig.Database != "postgres" {
		t.Fatal("TEST_DATABASE_URL must use shop_test_admin and the postgres maintenance database on a disposable cluster")
	}
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("connect test administrator")
	}
	t.Cleanup(admin.Close)
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	name := "shop_test_" + hex.EncodeToString(nonce[:])
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" TEMPLATE template0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("drop isolated test database: %v", err)
		}
	})
	config = config.Copy()
	config.ConnConfig.Database = name
	config.MaxConns = 8
	config.MinConns = 0
	config.ConnConfig.RuntimeParams["application_name"] = name
	config.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := postgres.New(pool)
	db := &database{ctx, pool, store, application.New(store)}
	if migrate {
		if err := postgres.Migrate(ctx, pool, "../../migrations"); err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func (d *database) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := d.pool.Exec(d.ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}
func (d *database) number(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := d.pool.QueryRow(d.ctx, sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func (d *database) cart(t *testing.T, user string) domain.Cart {
	t.Helper()
	c, err := d.store.Cart(d.ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func (d *database) set(t *testing.T, user, id string, qty int, selected bool) domain.Cart {
	t.Helper()
	c, err := d.service.SetCartItem(d.ctx, user, id, qty, selected)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func quantity(c domain.Cart, id string) int {
	for _, item := range c.Items {
		if item.Product.ID == id {
			return item.Quantity
		}
	}
	return 0
}
func (d *database) customer(t *testing.T, id string) {
	t.Helper()
	d.exec(t, "INSERT INTO users(id,name,email,password_hash) VALUES($1,$1,$1||'@example.invalid','not-a-password-hash')", id)
}
