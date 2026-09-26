//go:build integration

package integration_test

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	postgres "sweetshop/internal/repository/postgres"
)

func hashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func TestPostgresIdentityAndSessionExpiry(t *testing.T) {
	d := newDatabase(t, true)
	result, err := d.service.Login(d.ctx, " DEMO@SUGAR.RU ", "Sweet2026!")
	if err != nil {
		t.Fatal(err)
	}
	if result.User.ID != "usr-demo" || len(result.Token) != 43 {
		t.Fatal("login contract")
	}
	hash := hashToken(result.Token)
	if d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1 AND transport='bearer'", hash) != 1 || d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1", result.Token) != 0 {
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
	if _, err := d.store.SessionByTokenHash(d.ctx, hash, domain.SessionBearer, result.ExpiresAt.Add(time.Second)); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expiry: %v", err)
	}
	if _, err := d.store.SessionByTokenHash(d.ctx, hash, domain.SessionBearer, result.ExpiresAt); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expiry boundary is exclusive: %v", err)
	}
	if _, err := d.service.Login(d.ctx, "demo@sugar.ru", "wrong"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := d.service.Login(d.ctx, "nobody@example.invalid", "Sweet2026!"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("unknown email: %v", err)
	}
}

func TestPostgresCookieSessionLifecycle(t *testing.T) {
	d := newDatabase(t, true)
	cookie, err := d.service.StartCookieSession(d.ctx, "demo@sugar.ru", "Sweet2026!", "")
	if err != nil {
		t.Fatal(err)
	}
	hash := hashToken(cookie.Token)
	if d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1 AND transport='cookie' AND user_id='usr-demo'", hash) != 1 {
		t.Fatal("cookie session row")
	}
	if d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1 OR user_id=$1 OR transport=$1", cookie.Token) != 0 {
		t.Fatal("raw session token stored")
	}
	record, err := d.service.AuthenticateSession(d.ctx, cookie.Token, domain.SessionCookie)
	if err != nil || record.User.ID != "usr-demo" || !record.ExpiresAt.Equal(cookie.ExpiresAt) {
		t.Fatalf("cookie authentication: %#v %v", record, err)
	}
	if _, err := d.service.Authenticate(d.ctx, cookie.Token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("cookie token accepted as bearer")
	}
	bearer, err := d.service.Login(d.ctx, "demo@sugar.ru", "Sweet2026!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.service.AuthenticateSession(d.ctx, bearer.Token, domain.SessionCookie); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("bearer token accepted as cookie")
	}
	if err := d.service.Logout(d.ctx, cookie.Token, domain.SessionBearer); err != nil || d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1", hash) != 1 {
		t.Fatal("logout through the wrong transport")
	}
	if err := d.service.Logout(d.ctx, cookie.Token, domain.SessionCookie); err != nil {
		t.Fatal(err)
	}
	if d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1", hash) != 0 {
		t.Fatal("logout did not delete the session")
	}
	if _, err := d.service.AuthenticateSession(d.ctx, cookie.Token, domain.SessionCookie); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("revoked session still authenticates")
	}
	if err := d.service.Logout(d.ctx, cookie.Token, domain.SessionCookie); err != nil {
		t.Fatal("logout must be idempotent")
	}
	if _, err := d.service.Authenticate(d.ctx, bearer.Token); err != nil {
		t.Fatal("cookie logout revoked the bearer session")
	}
}

func TestPostgresCreateSessionPurgesAndReplacesAtomically(t *testing.T) {
	d := newDatabase(t, true)
	d.customer(t, "other")
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	insert := func(hash, user, transport string, expires time.Time) {
		d.exec(t, "INSERT INTO sessions(token_hash,user_id,transport,expires_at) VALUES($1,$2,$3,$4)", hash, user, transport, expires)
	}
	insert("demo-expired", "usr-demo", "cookie", now.Add(-time.Hour))
	insert("demo-boundary", "usr-demo", "bearer", now)
	insert("other-expired", "other", "bearer", now.Add(-time.Hour))
	insert("old-cookie", "usr-demo", "cookie", now.Add(time.Hour))
	insert("same-hash-bearer", "usr-demo", "bearer", now.Add(time.Hour))
	insert("taken", "other", "cookie", now.Add(time.Hour))
	count := func(hash string) int64 { return d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1", hash) }

	// A failed INSERT rolls back the purge and the replacement with it.
	failing := domain.NewSession{UserID: "usr-demo", TokenHash: "taken", Transport: domain.SessionCookie, ExpiresAt: now.Add(time.Hour), ReplacesTokenHash: "old-cookie"}
	if err := d.store.CreateSession(d.ctx, failing, now); err == nil {
		t.Fatal("duplicate token hash must fail, not upsert")
	}
	if count("old-cookie") != 1 || count("demo-expired") != 1 || count("demo-boundary") != 1 || d.number(t, "SELECT count(*) FROM sessions WHERE token_hash='taken' AND user_id='other'") != 1 {
		t.Fatal("failed CreateSession changed existing sessions")
	}
	if err := d.store.CreateSession(d.ctx, domain.NewSession{UserID: "missing", TokenHash: "new", Transport: domain.SessionCookie, ExpiresAt: now.Add(time.Hour), ReplacesTokenHash: "old-cookie"}, now); err == nil || count("old-cookie") != 1 {
		t.Fatal("unknown user must fail without side effects")
	}
	if err := d.store.CreateSession(d.ctx, domain.NewSession{UserID: "usr-demo", TokenHash: "new", Transport: domain.SessionBearer, ExpiresAt: now.Add(time.Hour), ReplacesTokenHash: "old-cookie"}, now); err == nil || count("old-cookie") != 1 {
		t.Fatal("bearer sessions must not replace sessions")
	}

	if err := d.store.CreateSession(d.ctx, domain.NewSession{UserID: "usr-demo", TokenHash: "new-cookie", Transport: domain.SessionCookie, ExpiresAt: now.Add(time.Hour), ReplacesTokenHash: "old-cookie"}, now); err != nil {
		t.Fatal(err)
	}
	if count("old-cookie") != 0 || count("new-cookie") != 1 {
		t.Fatal("replacement did not swap sessions")
	}
	if count("demo-expired") != 0 || count("demo-boundary") != 0 {
		t.Fatal("expired sessions (including expires_at = now) were not purged")
	}
	if count("other-expired") != 1 || count("taken") != 1 {
		t.Fatal("purge touched another user's sessions")
	}
	// Replacement is cookie-only: a bearer row with the presented hash survives.
	if err := d.store.CreateSession(d.ctx, domain.NewSession{UserID: "usr-demo", TokenHash: "newer-cookie", Transport: domain.SessionCookie, ExpiresAt: now.Add(time.Hour), ReplacesTokenHash: "same-hash-bearer"}, now); err != nil || count("same-hash-bearer") != 1 {
		t.Fatalf("cookie replacement deleted a bearer session: %v", err)
	}
	if _, err := d.pool.Exec(d.ctx, "INSERT INTO sessions(token_hash,user_id,transport,expires_at) VALUES('bad','usr-demo','jwt',now())"); err == nil {
		t.Fatal("transport check constraint missing")
	}
}

func TestPostgresServiceCookieLoginReplacesPresentedSession(t *testing.T) {
	d := newDatabase(t, true)
	first, err := d.service.StartCookieSession(d.ctx, "demo@sugar.ru", "Sweet2026!", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.service.StartCookieSession(d.ctx, "demo@sugar.ru", "wrong", first.Token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal(err)
	}
	if _, err := d.service.AuthenticateSession(d.ctx, first.Token, domain.SessionCookie); err != nil {
		t.Fatal("failed login removed the presented session")
	}
	second, err := d.service.StartCookieSession(d.ctx, "demo@sugar.ru", "Sweet2026!", first.Token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.service.AuthenticateSession(d.ctx, first.Token, domain.SessionCookie); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("replaced session still authenticates")
	}
	if _, err := d.service.AuthenticateSession(d.ctx, second.Token, domain.SessionCookie); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresProductionRefusesDemoPrincipal(t *testing.T) {
	d := newDatabase(t, true)
	production := application.New(d.store, application.Options{})
	seeded := ""
	if err := d.pool.QueryRow(d.ctx, "SELECT password_hash FROM users WHERE id=$1", domain.DemoUserID).Scan(&seeded); err != nil {
		t.Fatal(err)
	}
	if cost, err := bcrypt.Cost([]byte(seeded)); err != nil || cost != 10 {
		t.Fatalf("seeded hash cost=%d: the dummy hash must use the same cost", cost)
	}
	bearer, err := d.service.Login(d.ctx, "demo@sugar.ru", "Sweet2026!")
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := d.service.StartCookieSession(d.ctx, "demo@sugar.ru", "Sweet2026!", "")
	if err != nil {
		t.Fatal(err)
	}
	refused := func(stage string) {
		t.Helper()
		for _, err := range []error{
			func() error { _, err := production.Authenticate(d.ctx, bearer.Token); return err }(),
			func() error {
				_, err := production.AuthenticateSession(d.ctx, cookie.Token, domain.SessionCookie)
				return err
			}(),
		} {
			if !errors.Is(err, domain.ErrDemoAccountDisabled) {
				t.Fatalf("%s: existing demo session accepted: %v", stage, err)
			}
		}
	}
	if _, err := production.Login(d.ctx, "demo@sugar.ru", "Sweet2026!"); !errors.Is(err, domain.ErrDemoAccountDisabled) {
		t.Fatalf("production demo login: %v", err)
	}
	refused("seeded password")

	// Changing the password does not convert the seeded principal.
	changed, err := bcrypt.GenerateFromPassword([]byte("operator-changed"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	d.exec(t, "UPDATE users SET password_hash=$1 WHERE id=$2", string(changed), domain.DemoUserID)
	if _, err := production.Login(d.ctx, "demo@sugar.ru", "operator-changed"); !errors.Is(err, domain.ErrDemoAccountDisabled) {
		t.Fatalf("changed password re-enabled the demo principal: %v", err)
	}
	if _, err := production.StartCookieSession(d.ctx, "demo@sugar.ru", "operator-changed", ""); !errors.Is(err, domain.ErrDemoAccountDisabled) {
		t.Fatalf("changed password re-enabled cookie login: %v", err)
	}
	refused("changed password")
	if _, err := d.service.Login(d.ctx, "demo@sugar.ru", "operator-changed"); err != nil {
		t.Fatalf("demo mode keeps the principal usable: %v", err)
	}

	// Ordinary customers are unaffected in production.
	hash, err := bcrypt.GenerateFromPassword([]byte("customer-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	d.exec(t, "INSERT INTO users(id,name,email,password_hash) VALUES('usr-customer','Customer','customer@example.invalid',$1)", string(hash))
	customer, err := production.StartCookieSession(d.ctx, "customer@example.invalid", "customer-password", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := production.AuthenticateSession(d.ctx, customer.Token, domain.SessionCookie); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresMigration005UpgradesExistingBearerSessions(t *testing.T) {
	d := newDatabase(t, false)
	early := t.TempDir()
	for _, name := range []string{"001_init.up.sql", "002_demo_seed.up.sql", "003_catalog_scale_indexes.up.sql", "004_brand_manufacturer_integrity.up.sql"} {
		body, err := os.ReadFile(filepath.Join("../../migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(early, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := postgres.Migrate(d.ctx, d.pool, early); err != nil {
		t.Fatal(err)
	}
	// A session issued by the pre-005 binary (no transport column yet).
	d.exec(t, "INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,'usr-demo',now()+interval '1 day')", hashToken("pre-upgrade-token"))
	if err := postgres.Migrate(d.ctx, d.pool, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1 AND transport='bearer'", hashToken("pre-upgrade-token")) != 1 {
		t.Fatal("existing session did not become bearer")
	}
	if user, err := d.service.Authenticate(d.ctx, "pre-upgrade-token"); err != nil || user.ID != "usr-demo" {
		t.Fatalf("upgraded bearer session: %v", err)
	}
	if _, err := d.service.AuthenticateSession(d.ctx, "pre-upgrade-token", domain.SessionCookie); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("upgraded bearer session accepted as cookie")
	}
	// Rollback compatibility: the pre-005 INSERT shape still works.
	d.exec(t, "INSERT INTO sessions(token_hash,user_id,expires_at) VALUES('old-binary','usr-demo',now()+interval '1 day')")
	if d.number(t, "SELECT count(*) FROM sessions WHERE token_hash='old-binary' AND transport='bearer'") != 1 {
		t.Fatal("default transport")
	}
}
