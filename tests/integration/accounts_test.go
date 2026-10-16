//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

var (
	shopctlOnce sync.Once
	shopctlPath string
	shopctlErr  error
)

// shopctl builds the real operator binary once per test run.
func shopctl(t *testing.T) string {
	t.Helper()
	shopctlOnce.Do(func() {
		dir, err := os.MkdirTemp("", "shopctl-test-")
		if err != nil {
			shopctlErr = err
			return
		}
		shopctlPath = filepath.Join(dir, "shopctl")
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", shopctlPath, "../../cmd/shopctl")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			shopctlErr = fmt.Errorf("build shopctl: %v\n%s", err, out)
		}
	})
	if shopctlErr != nil {
		t.Fatal(shopctlErr)
	}
	return shopctlPath
}

// dsn is a key/value DSN for a child process; the test cluster uses trust auth.
func (d *database) dsn() string {
	c := d.pool.Config().ConnConfig
	return fmt.Sprintf("host=%s port=%d user=%s dbname=%s sslmode=disable", c.Host, c.Port, c.User, c.Database)
}

type shopctlResult struct {
	code           int
	stdout, stderr string
}

func (d *database) shopctl(t *testing.T, mode, stdin string, args ...string) shopctlResult {
	t.Helper()
	migrations, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(shopctl(t), args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "APP_MODE=" + mode, "DATABASE_URL=" + d.dsn(), "MIGRATIONS_DIR=" + migrations}
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	result := shopctlResult{stdout: stdout.String(), stderr: stderr.String()}
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		result.code = exit.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	return result
}

type shopctlOutput struct {
	Action          string                 `json:"action"`
	Account         domain.CustomerAccount `json:"account"`
	RevokedSessions *int64                 `json:"revokedSessions"`
}

func (r shopctlResult) output(t *testing.T) shopctlOutput {
	t.Helper()
	var out shopctlOutput
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("stdout %q: %v (stderr %s)", r.stdout, err, r.stderr)
	}
	return out
}

func (r shopctlResult) assertNoSecrets(t *testing.T, secrets ...string) {
	t.Helper()
	for _, secret := range append(secrets, "$2a$", "$2y$", "password_hash") {
		if strings.Contains(r.stdout+r.stderr, secret) {
			t.Fatalf("output contains %q", secret)
		}
	}
}

func TestShopctlProvisionsAProductionCustomer(t *testing.T) {
	d := productionDatabase(t)
	production := application.New(d.store, application.Options{AllowDemoAccount: false})
	const first, second = "correct horse battery", "new password 123"

	created := d.shopctl(t, "production", first+"\n", "customer", "create", "--email", " Ann.Lee@Example.COM ", "--name", " Анна Ли ", "--phone", "+7 999 000-00-00", "--password-stdin")
	if created.code != 0 {
		t.Fatalf("create: %+v", created)
	}
	created.assertNoSecrets(t, first)
	account := created.output(t).Account
	if created.output(t).Action != "create" || !strings.HasPrefix(account.ID, "usr_") || account.Email != "ann.lee@example.com" || account.Name != "Анна Ли" || account.DisabledAt != nil {
		t.Fatalf("account: %+v", account)
	}
	hash := d.text(t, "SELECT password_hash FROM users WHERE id=$1", account.ID)
	if cost, err := bcrypt.Cost([]byte(hash)); err != nil || cost != application.CustomerPasswordCost {
		t.Fatalf("bcrypt cost=%d err=%v", cost, err)
	}
	if d.number(t, "SELECT count(*) FROM users") != 1 {
		t.Fatal("expected exactly the provisioned customer")
	}

	cookie, err := production.StartCookieSession(d.ctx, "ANN.LEE@example.com", first, "")
	if err != nil {
		t.Fatalf("production login: %v", err)
	}
	if _, err := production.Login(d.ctx, "ann.lee@example.com", first); err != nil {
		t.Fatalf("bearer login: %v", err)
	}
	if record, err := production.AuthenticateSession(d.ctx, cookie.Token, domain.SessionCookie); err != nil || record.User.ID != account.ID {
		t.Fatalf("session: %v", err)
	}

	for name, args := range map[string][]string{
		"duplicate normalized email": {"customer", "create", "--email", "ann.lee@EXAMPLE.com", "--name", "Other", "--password-stdin"},
	} {
		if r := d.shopctl(t, "production", "another password\n", args...); r.code != 3 {
			t.Fatalf("%s: %+v", name, r)
		}
	}
	for name, args := range map[string][]string{
		"bad email":   {"customer", "create", "--email", "not-an-email", "--name", "Other", "--password-stdin"},
		"no name":     {"customer", "create", "--email", "other@example.com", "--name", " ", "--password-stdin"},
		"bad phone":   {"customer", "create", "--email", "other@example.com", "--name", "Other", "--phone", "call me", "--password-stdin"},
		"short input": {"customer", "set-password", "--email", "ann.lee@example.com", "--password-stdin"},
	} {
		stdin := "another password\n"
		if name == "short input" {
			stdin = "short\n"
		}
		r := d.shopctl(t, "production", stdin, args...)
		if r.code != 2 {
			t.Fatalf("%s: %+v", name, r)
		}
		r.assertNoSecrets(t, strings.TrimSpace(stdin))
	}
	if d.number(t, "SELECT count(*) FROM users") != 1 || d.text(t, "SELECT password_hash FROM users") != hash {
		t.Fatal("refused commands changed accounts")
	}

	// A new password revokes every session, bearer and cookie alike.
	reset := d.shopctl(t, "production", second+"\n", "customer", "set-password", "--email", "Ann.Lee@example.com", "--password-stdin")
	if reset.code != 0 || *reset.output(t).RevokedSessions != 2 {
		t.Fatalf("set-password: %+v", reset)
	}
	reset.assertNoSecrets(t, second)
	if d.number(t, "SELECT count(*) FROM sessions WHERE user_id=$1", account.ID) != 0 {
		t.Fatal("sessions survived the reset")
	}
	if _, err := production.AuthenticateSession(d.ctx, cookie.Token, domain.SessionCookie); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("old session still authenticates")
	}
	if _, err := production.Login(d.ctx, "ann.lee@example.com", first); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("old password still accepted")
	}
	if _, err := production.StartCookieSession(d.ctx, "ann.lee@example.com", second, ""); err != nil {
		t.Fatal(err)
	}

	// Disable revokes sessions and refuses sign-in; repeating it is stable.
	disabled := d.shopctl(t, "production", "", "customer", "disable", "--email", "ann.lee@example.com")
	disabledAccount := disabled.output(t).Account
	if disabled.code != 0 || *disabled.output(t).RevokedSessions != 1 || disabledAccount.DisabledAt == nil {
		t.Fatalf("disable: %+v", disabled)
	}
	if _, err := production.StartCookieSession(d.ctx, "ann.lee@example.com", second, ""); !errors.Is(err, domain.ErrAccountDisabled) || !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("disabled login: %v", err)
	}
	again := d.shopctl(t, "production", "", "customer", "disable", "--email", "ann.lee@example.com")
	if again.code != 0 || !again.output(t).Account.DisabledAt.Equal(*disabledAccount.DisabledAt) || *again.output(t).RevokedSessions != 0 {
		t.Fatalf("repeated disable: %+v", again)
	}
	// A session written without the guard (as a previous binary would) is still refused.
	d.exec(t, "INSERT INTO sessions(token_hash,user_id,transport,expires_at) VALUES ($1,$2,'cookie',now()+interval '1 day')", hashToken("unguarded"), account.ID)
	if _, err := production.AuthenticateSession(d.ctx, "unguarded", domain.SessionCookie); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("disabled account's session authenticated")
	}

	enabled := d.shopctl(t, "production", "", "customer", "enable", "--email", "ann.lee@example.com")
	if enabled.code != 0 || enabled.output(t).Account.DisabledAt != nil || *enabled.output(t).RevokedSessions != 1 {
		t.Fatalf("enable: %+v", enabled)
	}
	// A session written while disabled does not come back to life.
	if _, err := production.AuthenticateSession(d.ctx, "unguarded", domain.SessionCookie); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("a session from the disabled period survived enable")
	}
	if _, err := production.StartCookieSession(d.ctx, "ann.lee@example.com", second, ""); err != nil {
		t.Fatalf("enabled login: %v", err)
	}
	if r := d.shopctl(t, "production", "", "customer", "disable", "--email", "nobody@example.com"); r.code != 3 {
		t.Fatalf("unknown email: %+v", r)
	}
	d.assertNoDemoIdentity(t)
}

func (d *database) assertNoDemoIdentity(t *testing.T) {
	t.Helper()
	if d.number(t, "SELECT count(*) FROM users WHERE id='usr-demo' OR lower(email)='demo@sugar.ru'")+d.number(t, "SELECT count(*) FROM products") != 0 {
		t.Fatal("demo data in a production database")
	}
}

func TestShopctlCustomerRefusesUnsafeSchemas(t *testing.T) {
	create := []string{"customer", "create", "--email", "ops@example.com", "--name", "Ops", "--password-stdin"}
	empty := newDatabase(t, false)
	if r := empty.shopctl(t, "production", "correct horse battery\n", create...); r.code != 1 || !strings.Contains(r.stderr, "shopctl migrate") {
		t.Fatalf("unmigrated: %+v", r)
	}
	if empty.number(t, "SELECT count(*) FROM pg_class WHERE relname='users'") != 0 {
		t.Fatal("customer command changed an unmigrated database")
	}
	demo := newDatabase(t, true)
	r := demo.shopctl(t, "production", "correct horse battery\n", create...)
	if r.code != 1 || !strings.Contains(r.stderr, "demo fixtures are applied") || strings.Contains(r.stderr, "shopctl migrate") {
		t.Fatalf("production on a demo database: %+v", r)
	}
	if demo.number(t, "SELECT count(*) FROM users") != 1 {
		t.Fatal("refused command created an account")
	}
	// Development provisioning on a fixture database works.
	if r := demo.shopctl(t, "demo", "correct horse battery\n", create...); r.code != 0 {
		t.Fatalf("demo provisioning: %+v", r)
	}
}

// waitForLockWaiters waits until n backends of this database wait on a lock.
func (d *database) waitForLockWaiters(t *testing.T, n int64) {
	t.Helper()
	waitFor(t, "a backend waiting on a lock", func() bool {
		return d.number(t, "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'") >= n
	})
}

func TestLoginCannotOutrunAPasswordResetOrDisable(t *testing.T) {
	d := productionDatabase(t)
	accounts := application.NewCustomerAccounts(d.store)
	account, err := accounts.Create(d.ctx, application.NewCustomerInput{Email: "race@example.com", Name: "Race", Password: "first password 1"})
	if err != nil {
		t.Fatal(err)
	}
	production := application.New(d.store, application.Options{})
	known, err := bcrypt.GenerateFromPassword([]byte("known password 1"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte("second password 2"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, change string }{
		{"password reset", "UPDATE users SET password_hash=$2 WHERE id=$1"},
		{"disable", "UPDATE users SET disabled_at=now() WHERE id=$1 AND $2::text IS NOT NULL"},
	} {
		name, change := tc.name, tc.change
		t.Run(name, func(t *testing.T) {
			// Each case starts from an enabled account with a known password.
			d.exec(t, "UPDATE users SET password_hash=$2, disabled_at=NULL WHERE id=$1", account.ID, string(known))
			first := "known password 1"
			// The operator's change holds the user row while the login is in flight.
			tx, err := d.pool.Begin(d.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(d.ctx)
			if _, err := tx.Exec(d.ctx, change, account.ID, string(newHash)); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, err := production.StartCookieSession(d.ctx, "race@example.com", first, "")
				result <- err
			}()
			d.waitForLockWaiters(t, 1)
			if _, err := tx.Exec(d.ctx, "DELETE FROM sessions WHERE user_id=$1", account.ID); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(d.ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("login verified the old state but got: %v", err)
			}
			if d.number(t, "SELECT count(*) FROM sessions WHERE user_id=$1", account.ID) != 0 {
				t.Fatal("a session outlived the change")
			}
		})
	}
}

func TestDisableSerializesWithCartWriters(t *testing.T) {
	d := productionDatabase(t)
	accounts := application.NewCustomerAccounts(d.store)
	account, err := accounts.Create(d.ctx, application.NewCustomerInput{Email: "cart@example.com", Name: "Cart", Password: "first password 1"})
	if err != nil {
		t.Fatal(err)
	}
	// The lock every Cart writer takes first (beginCartMutation).
	tx, err := d.pool.Begin(d.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(d.ctx)
	if _, err := tx.Exec(d.ctx, "SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE", account.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := accounts.Disable(d.ctx, "cart@example.com")
		done <- err
	}()
	d.waitForLockWaiters(t, 1)
	select {
	case err := <-done:
		t.Fatalf("disable did not wait for the cart writer: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(d.ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if d.number(t, "SELECT count(*) FROM users WHERE id=$1 AND disabled_at IS NOT NULL", account.ID) != 1 {
		t.Fatal("not disabled")
	}
}
