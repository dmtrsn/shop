//go:build integration

package integration_test

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"sweetshop/internal/domain"
)

func TestPostgresIdentityAndSessionExpiry(t *testing.T) {
	d := newDatabase(t, true)
	result, err := d.service.Login(d.ctx, " DEMO@SUGAR.RU ", "Sweet2026!")
	if err != nil {
		t.Fatal(err)
	}
	if result.User.ID != "usr-demo" || len(result.Token) != 43 {
		t.Fatal("login contract")
	}
	digest := sha256.Sum256([]byte(result.Token))
	hash := base64.RawURLEncoding.EncodeToString(digest[:])
	if d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1", hash) != 1 || d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1", result.Token) != 0 {
		t.Fatal("session token not hashed")
	}
	user, err := d.service.Authenticate(d.ctx, result.Token)
	if err != nil || user.ID != "usr-demo" {
		t.Fatalf("authenticate: %v", err)
	}
	for _, token := range []string{"", "invalid"} {
		if _, err := d.service.Authenticate(d.ctx, token); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("bad token: %v", err)
		}
	}
	if _, err := d.store.UserBySession(d.ctx, hash, result.ExpiresAt.Add(time.Second)); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expiry: %v", err)
	}
	if _, err := d.service.Login(d.ctx, "demo@sugar.ru", "wrong"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("wrong password: %v", err)
	}
}
