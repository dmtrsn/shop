//go:build integration

package integration_test

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"sweetshop/internal/application"
	"sweetshop/internal/delivery/httpapi"
	"sweetshop/internal/domain"
	"sweetshop/internal/email"
	"sweetshop/internal/email/emailtest"
	"sweetshop/tests/contract"
)

const (
	customer         = "kate@example.com"
	customerPassword = "original password 1"
	newPassword      = "recovered password 2"
)

func (f *selfService) requestReset(t *testing.T, address string) exchange {
	t.Helper()
	return serve(f.handler, "POST", "/api/auth/password-reset", `{"email":"`+address+`"}`, shopOrigin)
}

func (f *selfService) confirmReset(t *testing.T, token, password string, headers map[string]string) exchange {
	t.Helper()
	all := map[string]string{"Origin": contract.TrustedOrigin}
	for key, value := range headers {
		all[key] = value
	}
	return serve(f.handler, "POST", "/api/auth/password-reset/confirm", `{"token":"`+token+`","password":"`+password+`"}`, all)
}

// lastResetToken is the token of the newest reset email to address.
func (f *selfService) lastResetToken(t *testing.T, address string) string {
	t.Helper()
	messages := f.outbox.To(address)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Kind == email.KindPasswordReset {
			return emailtest.Token(messages[i])
		}
	}
	t.Fatalf("no reset email to %s", address)
	return ""
}

func (d *database) resetRows(t *testing.T, userID string) int64 {
	t.Helper()
	return d.number(t, "SELECT count(*) FROM customer_password_resets WHERE user_id=$1", userID)
}

func (d *database) passwordHash(t *testing.T, userID string) string {
	t.Helper()
	return d.text(t, "SELECT password_hash FROM users WHERE id=$1", userID)
}

// 1, 5, 6, 7: one row per customer, digest only, unique digests, and a
// newer request replaces the token.
func TestPasswordResetIssuesOneDigestPerCustomer(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	account := d.operatorCustomer(t, customer, customerPassword)
	if ex := f.requestReset(t, " Kate@Example.com "); ex.Code != 202 {
		t.Fatalf("request: %d", ex.Code)
	}
	first := f.lastResetToken(t, customer)
	digest := d.text(t, "SELECT token_hash FROM customer_password_resets WHERE user_id=$1", account.ID)
	if d.resetRows(t, account.ID) != 1 || digest == first || len(digest) != 43 {
		t.Fatal("not one digest-only row")
	}
	if strings.Contains(d.text(t, "SELECT row_to_json(r)::text FROM customer_password_resets r"), first) {
		t.Fatal("raw token persisted")
	}
	var expires, created time.Time
	if err := d.pool.QueryRow(d.ctx, "SELECT created_at, expires_at FROM customer_password_resets WHERE user_id=$1", account.ID).Scan(&created, &expires); err != nil || !expires.Equal(created.Add(time.Hour)) {
		t.Fatalf("lifetime %v %v %v", created, expires, err)
	}
	other := d.operatorCustomer(t, "other@example.com", customerPassword)
	_, err := d.pool.Exec(d.ctx, "INSERT INTO customer_password_resets(user_id,token_hash,created_at,expires_at) VALUES ($1,$2,now(),now()+interval '1 hour')", other.ID, digest)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("duplicate digest accepted: %v", err)
	}
	f.clock.advance(61 * time.Second)
	f.requestReset(t, customer)
	second := f.lastResetToken(t, customer)
	if second == first || d.resetRows(t, account.ID) != 1 {
		t.Fatal("a newer request did not replace the token")
	}
	if ex := f.confirmReset(t, first, newPassword, nil); ex.Code != 410 || !strings.HasPrefix(ex.errorCode(), "link_invalid|") {
		t.Fatalf("older link: %d", ex.Code)
	}
	if ex := f.confirmReset(t, second, newPassword, nil); ex.Code != 204 {
		t.Fatalf("newer link: %d %s", ex.Code, ex.Body)
	}
}

// 2-4 and the HTTP enumeration checks.
func TestPasswordResetRequestAnswersAlike(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	enabled := d.operatorCustomer(t, "enabled@example.com", customerPassword)
	disabled := d.operatorCustomer(t, "disabled@example.com", customerPassword)
	if _, err := d.store.SetCustomerDisabled(d.ctx, "disabled@example.com", true); err != nil {
		t.Fatal(err)
	}
	throttled := d.operatorCustomer(t, "throttled@example.com", customerPassword)
	pending := d.operatorCustomer(t, "pending@example.com", customerPassword)
	f.requestReset(t, "throttled@example.com")
	f.requestReset(t, "pending@example.com")
	d.exec(t, "UPDATE customer_email_throttle SET last_sent_at=last_sent_at-interval '2 minutes', sent_at=ARRAY[last_sent_at-interval '2 minutes'] WHERE email='pending@example.com'")
	throttledDigest := d.text(t, "SELECT token_hash FROM customer_password_resets WHERE user_id=$1", throttled.ID)
	pendingDigest := d.text(t, "SELECT token_hash FROM customer_password_resets WHERE user_id=$1", pending.ID)
	before := len(f.outbox.Messages())
	hashes := map[string]string{}
	for _, account := range []domain.CustomerAccount{enabled, disabled, throttled, pending} {
		hashes[account.ID] = d.passwordHash(t, account.ID)
	}

	answers := map[string]exchange{}
	for _, address := range []string{"enabled@example.com", "unknown@example.com", "disabled@example.com", "throttled@example.com", "pending@example.com"} {
		answers[address] = f.requestReset(t, address)
	}
	reference := answers["enabled@example.com"]
	for address, ex := range answers {
		headers, want := ex.Header().Clone(), reference.Header().Clone()
		headers.Del("X-Request-ID")
		want.Del("X-Request-ID")
		if ex.Code != 202 || ex.Body.String() != `{"status":"pending"}`+"\n" || !equalHeaders(headers, want) || headers.Get("Cache-Control") != "no-store" || len(ex.Result().Cookies()) != 0 {
			t.Fatalf("%s answered differently: %d %q %v", address, ex.Code, ex.Body.String(), headers)
		}
	}
	sent := map[string]int{}
	for _, message := range f.outbox.Messages()[before:] {
		sent[message.To]++
	}
	if sent["enabled@example.com"] != 1 || sent["pending@example.com"] != 1 || len(sent) != 2 {
		t.Fatalf("emails %v", sent)
	}
	if d.resetRows(t, disabled.ID) != 0 || d.number(t, "SELECT count(*) FROM customer_email_throttle WHERE email IN ('disabled@example.com','unknown@example.com')") != 0 {
		t.Fatal("unknown or disabled address left state")
	}
	if d.text(t, "SELECT token_hash FROM customer_password_resets WHERE user_id=$1", throttled.ID) != throttledDigest {
		t.Fatal("throttled request replaced the token")
	}
	if d.text(t, "SELECT token_hash FROM customer_password_resets WHERE user_id=$1", pending.ID) == pendingDigest {
		t.Fatal("request past the cooldown did not replace an active reset")
	}
	for id, hash := range hashes {
		if d.passwordHash(t, id) != hash {
			t.Fatal("a reset request changed a password")
		}
	}
	if ex := f.requestReset(t, "not-an-address"); ex.Code != 422 {
		t.Fatalf("malformed: %d", ex.Code)
	}
	if ex := serve(f.handler, "POST", "/api/auth/password-reset", `{"email":"enabled@example.com"}`, nil); ex.Code != 403 {
		t.Fatalf("no Origin: %d", ex.Code)
	}
	f.assertQuiet(t, throttledDigest, pendingDigest, customerPassword)
}

// 10-14: the reset stores a cost-12 hash, revokes every cookie and bearer
// session, signs nobody in and keeps the customer's data.
func TestPasswordResetRevokesEverySessionAndSignsNobodyIn(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, 0)
	account := d.operatorCustomer(t, customer, customerPassword)
	d.set(t, account.ID, "p01", 2, true)
	if _, err := d.service.SetFavorite(d.ctx, account.ID, "p02", true); err != nil {
		t.Fatal(err)
	}
	cookie := cookieLogin(t, f.handler, customer, customerPassword)
	other := cookieLogin(t, f.handler, customer, customerPassword)
	bearer := serve(f.handler, "POST", "/api/auth/login", `{"email":"`+customer+`","password":"`+customerPassword+`"}`, nil).body["token"].(string)
	if d.number(t, "SELECT count(*) FROM sessions WHERE user_id=$1", account.ID) != 3 {
		t.Fatal("setup sessions")
	}
	f.requestReset(t, customer)
	token := f.lastResetToken(t, customer)
	ex := f.confirmReset(t, token, newPassword, map[string]string{"Cookie": shopCookie + "=" + cookie})
	if ex.Code != 204 || ex.Body.Len() != 0 {
		t.Fatalf("reset: %d %s", ex.Code, ex.Body)
	}
	cleared := false
	for _, c := range ex.Result().Cookies() {
		if c.Name == shopCookie && c.Value != "" {
			t.Fatal("reset issued a session cookie")
		}
		cleared = cleared || (c.Name == shopCookie && c.MaxAge < 0)
	}
	if !cleared {
		t.Fatal("reset did not clear the cookie")
	}
	hash := d.passwordHash(t, account.ID)
	if cost, err := bcrypt.Cost([]byte(hash)); err != nil || cost != application.CustomerPasswordCost || bcrypt.CompareHashAndPassword([]byte(hash), []byte(newPassword)) != nil {
		t.Fatalf("new hash cost %d %v", cost, err)
	}
	if d.number(t, "SELECT count(*) FROM sessions WHERE user_id=$1", account.ID) != 0 || d.resetRows(t, account.ID) != 0 {
		t.Fatal("sessions or reset survived")
	}
	for _, old := range []string{cookie, other} {
		if me := serve(f.handler, "GET", "/api/me", "", map[string]string{"Cookie": shopCookie + "=" + old}); me.Code != 401 {
			t.Fatalf("old cookie: %d", me.Code)
		}
	}
	if me := serve(f.handler, "GET", "/api/me", "", map[string]string{"Authorization": "Bearer " + bearer}); me.Code != 401 {
		t.Fatalf("old bearer: %d", me.Code)
	}
	if ex := serve(f.handler, "POST", "/api/auth/session", `{"email":"`+customer+`","password":"`+customerPassword+`"}`, shopOrigin); ex.Code != 401 {
		t.Fatalf("old password: %d", ex.Code)
	}
	if cookieLogin(t, f.handler, customer, newPassword) == "" {
		t.Fatal("new password")
	}
	if quantity(d.cart(t, account.ID), "p01") != 2 || d.number(t, "SELECT count(*) FROM favorites WHERE user_id=$1 AND product_id='p02'", account.ID) != 1 {
		t.Fatal("recovery changed customer data")
	}
	f.assertQuiet(t, token, cookie, bearer, customerPassword, newPassword, hash)
}

// 15: a disabled account never recovers, however the disable happened.
func TestDisabledAccountCannotRecover(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	account := d.operatorCustomer(t, customer, customerPassword)
	hash := d.passwordHash(t, account.ID)

	// The operator disable deletes the outstanding link, and enabling the
	// account again does not revive it.
	f.requestReset(t, customer)
	token := f.lastResetToken(t, customer)
	if _, err := d.store.SetCustomerDisabled(d.ctx, customer, true); err != nil {
		t.Fatal(err)
	}
	if d.resetRows(t, account.ID) != 0 {
		t.Fatal("disable kept the reset")
	}
	if _, err := d.store.SetCustomerDisabled(d.ctx, customer, false); err != nil {
		t.Fatal(err)
	}
	if ex := f.confirmReset(t, token, newPassword, nil); ex.Code != 410 {
		t.Fatalf("link after disable and enable: %d", ex.Code)
	}

	// A disable that left the row behind (an older binary) is still refused
	// at confirmation, and the token is spent.
	f.clock.advance(61 * time.Second)
	f.requestReset(t, customer)
	token = f.lastResetToken(t, customer)
	d.exec(t, "UPDATE users SET disabled_at=now() WHERE id=$1", account.ID)
	if ex := f.confirmReset(t, token, newPassword, nil); ex.Code != 410 || !strings.HasPrefix(ex.errorCode(), "link_invalid|") {
		t.Fatalf("disabled account recovered: %d", ex.Code)
	}
	if d.passwordHash(t, account.ID) != hash || d.resetRows(t, account.ID) != 0 || d.number(t, "SELECT count(*) FROM users WHERE id=$1 AND disabled_at IS NOT NULL", account.ID) != 1 {
		t.Fatal("disabled account changed")
	}
	// A request for a disabled account issues nothing.
	f.clock.advance(61 * time.Second)
	before := len(f.outbox.Messages())
	f.requestReset(t, customer)
	if d.resetRows(t, account.ID) != 0 || len(f.outbox.Messages()) != before {
		t.Fatal("reset issued for a disabled account")
	}
	// An operator password change also kills an outstanding link.
	d.exec(t, "UPDATE users SET disabled_at=NULL WHERE id=$1", account.ID)
	f.clock.advance(61 * time.Second)
	f.requestReset(t, customer)
	token = f.lastResetToken(t, customer)
	if _, err := d.store.SetCustomerPassword(d.ctx, customer, hash); err != nil {
		t.Fatal(err)
	}
	if ex := f.confirmReset(t, token, newPassword, nil); ex.Code != 410 {
		t.Fatalf("link after operator set-password: %d", ex.Code)
	}
}

// 8, 9: expiry is exact; a used link never works again.
func TestPasswordResetExpiryAndReplay(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	account := d.operatorCustomer(t, customer, customerPassword)
	f.requestReset(t, customer)
	token := f.lastResetToken(t, customer)
	var expires time.Time
	if err := d.pool.QueryRow(d.ctx, "SELECT expires_at FROM customer_password_resets WHERE user_id=$1", account.ID).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	f.clock.set(expires)
	if ex := f.confirmReset(t, token, newPassword, nil); ex.Code != 410 {
		t.Fatalf("at expiry: %d", ex.Code)
	}
	// The token is checked before the password: an expired link never gets a 422.
	if ex := f.confirmReset(t, token, "short", nil); ex.Code != 410 {
		t.Fatalf("expired link with a bad password: %d", ex.Code)
	}
	f.clock.set(expires.Add(-time.Microsecond))
	if ex := f.confirmReset(t, token, "short", nil); ex.Code != 422 {
		t.Fatalf("usable link with a bad password: %d", ex.Code)
	}
	if ex := f.confirmReset(t, token, newPassword, nil); ex.Code != 204 {
		t.Fatalf("just before expiry: %d %s", ex.Code, ex.Body)
	}
	if ex := f.confirmReset(t, token, "third password 33", nil); ex.Code != 410 {
		t.Fatalf("replay: %d", ex.Code)
	}
	if bcrypt.CompareHashAndPassword([]byte(d.passwordHash(t, account.ID)), []byte(newPassword)) != nil {
		t.Fatal("replay changed the password")
	}
}

// 16: a login that verified the old password while the reset held the row
// cannot create a session after the reset commits.
func TestLoginCannotOutrunAPasswordReset(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	account := d.operatorCustomer(t, customer, customerPassword)
	f.requestReset(t, customer)
	token := f.lastResetToken(t, customer)
	gated, gate := d.gatedStore(t, "commit")
	resets, err := application.NewPasswordResets(gated, f.outbox, mustOrigin(t), slog.New(slog.NewTextHandler(io.Discard, nil)), application.RegistrationOptions{PasswordCost: bcrypt.MinCost, Now: f.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	resetDone := make(chan error, 1)
	go func() {
		_, err := resets.Reset(d.ctx, token, newPassword)
		resetDone <- err
	}()
	// The reset holds the user row with the new hash written, not committed.
	gate.wait(t, d.ctx)
	loginDone := make(chan error, 1)
	go func() {
		_, err := f.service.StartCookieSession(d.ctx, customer, customerPassword, "")
		loginDone <- err
	}()
	d.waitForLockWaiters(t, 1)
	gate.release()
	if err := <-resetDone; err != nil {
		t.Fatal(err)
	}
	if err := <-loginDone; !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("old-password login after the reset: %v", err)
	}
	if d.number(t, "SELECT count(*) FROM sessions WHERE user_id=$1", account.ID) != 0 {
		t.Fatal("an old-password session survived")
	}
}

func mustOrigin(t *testing.T) email.PublicOrigin {
	t.Helper()
	origin, err := email.ParsePublicOrigin(contract.TrustedOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	return origin
}

// 17: two replicas confirming one link: exactly one wins.
func TestConcurrentPasswordResetsHaveOneWinner(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	other := d.selfService(t, d.secondStore(t), f.outbox, f.clock, bcrypt.MinCost)
	account := d.operatorCustomer(t, customer, customerPassword)
	f.requestReset(t, customer)
	token := f.lastResetToken(t, customer)
	const attempts = 8
	codes := make(chan int, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- []*selfService{f, other}[i%2].confirmReset(t, token, newPassword+string(rune('a'+i)), nil).Code
		}()
	}
	wg.Wait()
	close(codes)
	count := map[int]int{}
	for code := range codes {
		count[code]++
	}
	if count[204] != 1 || count[410] != attempts-1 || d.resetRows(t, account.ID) != 0 {
		t.Fatalf("codes %v", count)
	}
}

// 18 and the lock-order review: operator set-password, disable, login and
// cart writers racing a reset finish without deadlock, and the operator's
// later or earlier password always wins over a reset.
func TestPasswordResetRacesAccountChangesWithoutDeadlock(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	account := d.operatorCustomer(t, customer, customerPassword)
	operatorHash, _ := bcrypt.GenerateFromPassword([]byte("operator password 9"), bcrypt.MinCost)
	for round := range 6 {
		d.exec(t, "UPDATE users SET disabled_at=NULL WHERE id=$1", account.ID)
		f.clock.advance(61 * time.Second)
		f.requestReset(t, customer)
		token := f.lastResetToken(t, customer)
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		run := func(work func() error) {
			wg.Add(1)
			go func() { defer wg.Done(); errs <- work() }()
		}
		var resetCode int
		run(func() error { resetCode = f.confirmReset(t, token, newPassword, nil).Code; return nil })
		run(func() error { _, err := d.store.SetCustomerPassword(d.ctx, customer, string(operatorHash)); return err })
		run(func() error {
			_, err := f.service.StartCookieSession(d.ctx, customer, customerPassword, "")
			if errors.Is(err, domain.ErrUnauthorized) {
				return nil
			}
			return err
		})
		run(func() error { _, err := d.store.SetCartItem(d.ctx, account.ID, "p01", round+1, true); return err })
		run(func() error { _, err := d.store.SetFavorite(d.ctx, account.ID, "p02", round%2 == 0); return err })
		if round%3 == 2 {
			run(func() error { _, err := d.store.SetCustomerDisabled(d.ctx, customer, true); return err })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
				t.Fatalf("round %d: deadlock", round)
			}
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if resetCode != 204 && resetCode != 410 {
			t.Fatalf("round %d: reset %d", round, resetCode)
		}
		// Whichever order: the operator's change either deleted the link
		// first or overwrote the reset afterwards.
		if d.passwordHash(t, account.ID) != string(operatorHash) || d.resetRows(t, account.ID) != 0 {
			t.Fatalf("round %d: the reset outlived the operator change", round)
		}
		// The racing login used the old password: whether it ran before or
		// after the changes, no session of it may remain.
		if d.number(t, "SELECT count(*) FROM sessions WHERE user_id=$1", account.ID) != 0 {
			t.Fatalf("round %d: a session survived", round)
		}
	}
}

// 19: registration and recovery spend one per-address allowance.
func TestEmailAllowanceIsSharedByRegistrationAndRecovery(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	account := d.operatorCustomer(t, customer, customerPassword)
	// Registering the address of an account sends the notice...
	f.register(t, customer, registrantPassword)
	// ...so a reset right after it is throttled.
	f.requestReset(t, customer)
	if d.resetRows(t, account.ID) != 0 || len(f.outbox.To(customer)) != 1 {
		t.Fatal("reset bypassed the registration cooldown")
	}
	for i := 1; i < 10; i++ {
		f.clock.advance(61 * time.Second)
		if i%2 == 0 {
			f.register(t, customer, registrantPassword)
		} else {
			f.requestReset(t, customer)
		}
	}
	if len(f.outbox.To(customer)) != 10 {
		t.Fatalf("emails %d", len(f.outbox.To(customer)))
	}
	f.clock.advance(61 * time.Second)
	f.requestReset(t, customer)
	f.clock.advance(61 * time.Second)
	f.register(t, customer, registrantPassword)
	if len(f.outbox.To(customer)) != 10 {
		t.Fatal("alternating flows exceeded the daily limit")
	}
}

// 20: cleanup removes expired resets only, and never users.
func TestCleanupRemovesExpiredPasswordResets(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	expired := d.operatorCustomer(t, "expired@example.com", customerPassword)
	fresh := d.operatorCustomer(t, "fresh@example.com", customerPassword)
	f.requestReset(t, "expired@example.com")
	f.clock.advance(50 * time.Minute)
	f.requestReset(t, "fresh@example.com")
	f.clock.advance(15 * time.Minute)
	if n, err := f.resets.Cleanup(d.ctx); err != nil || n != 1 {
		t.Fatalf("cleanup %d %v", n, err)
	}
	if d.resetRows(t, expired.ID) != 0 || d.resetRows(t, fresh.ID) != 1 || d.usersWithEmail(t, "expired@example.com") != 1 {
		t.Fatal("cleanup removed the wrong rows")
	}
}

// 21: no emailed token stands in for another, nor for a session or CSRF token.
func TestEmailedTokensNeverCrossPurposes(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	d.operatorCustomer(t, customer, customerPassword)
	f.requestReset(t, customer)
	resetToken := f.lastResetToken(t, customer)
	f.register(t, registrant, registrantPassword)
	registrationToken := f.lastToken(t, registrant)

	if ex := f.confirmReset(t, registrationToken, newPassword, nil); ex.Code != 410 {
		t.Fatalf("registration token reset a password: %d", ex.Code)
	}
	if ex := f.confirm(t, resetToken, registrantPassword); ex.Code != 410 {
		t.Fatalf("reset token confirmed a registration: %d", ex.Code)
	}
	for _, headers := range []map[string]string{{"Cookie": shopCookie + "=" + resetToken}, {"Authorization": "Bearer " + resetToken}} {
		if me := serve(f.handler, "GET", "/api/me", "", headers); me.Code != 401 {
			t.Fatalf("reset token authenticated: %d", me.Code)
		}
	}
	cookie := cookieLogin(t, f.handler, customer, customerPassword)
	if ex := serve(f.handler, "POST", "/api/cart/items", `{"productId":"p01","quantity":1}`,
		map[string]string{"Origin": contract.TrustedOrigin, "Cookie": shopCookie + "=" + cookie, "X-CSRF-Token": resetToken}); ex.Code != 403 {
		t.Fatalf("reset token passed as CSRF: %d", ex.Code)
	}
	// Both still work for their own purpose.
	if ex := f.confirmReset(t, resetToken, newPassword, nil); ex.Code != 204 {
		t.Fatalf("reset: %d", ex.Code)
	}
	if ex := f.confirm(t, registrationToken, registrantPassword); ex.Code != 200 {
		t.Fatalf("registration: %d", ex.Code)
	}
}

// Queue failure: the token stands, the password is unchanged, and a later
// request recovers.
func TestRefusedResetEmailIsRecoverable(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	account := d.operatorCustomer(t, customer, customerPassword)
	hash := d.passwordHash(t, account.ID)
	f.outbox.Refuse = email.ErrQueueFull
	if ex := f.requestReset(t, customer); ex.Code != 202 {
		t.Fatalf("request with a full queue: %d", ex.Code)
	}
	if d.resetRows(t, account.ID) != 1 || d.passwordHash(t, account.ID) != hash || !strings.Contains(f.logs.String(), "password reset email not queued") {
		t.Fatal("state after a refused email")
	}
	f.outbox.Refuse = nil
	f.clock.advance(61 * time.Second)
	f.requestReset(t, customer)
	if ex := f.confirmReset(t, f.lastResetToken(t, customer), newPassword, nil); ex.Code != 204 {
		t.Fatalf("recovery: %d", ex.Code)
	}
	f.assertQuiet(t, customerPassword, newPassword)
}

// Rollback: the A7 binary (migrations up to 010) accepts the A8 schema and
// lacks the routes; customers keep signing in; links work after roll-forward.
func TestPreviousBinaryToleratesPasswordResetSchema(t *testing.T) {
	d := productionDatabase(t)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	d.operatorCustomer(t, customer, customerPassword)
	f.requestReset(t, customer)
	token := f.lastResetToken(t, customer)
	previous := productionMigrator(migrationDir(t, []string{"001_", "002_", "003_", "004_", "005_", "006_", "007_", "008_", "009_", "010_"}, nil), &lockedBuffer{})
	if err := previous.Verify(d.ctx, d.pool); err != nil {
		t.Fatalf("A7 schema check on the A8 schema: %v", err)
	}
	if err := previous.Migrate(d.ctx, d.pool); err != nil {
		t.Fatalf("A7 migrate on the A8 schema: %v", err)
	}
	plain := httpapi.New(application.New(d.store, application.Options{}), slog.New(slog.NewTextHandler(io.Discard, nil)), "", "postgres", contract.SessionConfig())
	for _, path := range []string{"/api/auth/password-reset", "/api/auth/password-reset/confirm"} {
		if ex := serve(plain, "POST", path, `{"email":"`+customer+`","token":"`+token+`","password":"`+newPassword+`"}`, shopOrigin); ex.Code != 404 {
			t.Fatalf("%s on an image without self-service: %d", path, ex.Code)
		}
	}
	if cookieLogin(t, plain, customer, customerPassword) == "" {
		t.Fatal("customer login on the previous image")
	}
	if ex := f.confirmReset(t, token, newPassword, nil); ex.Code != 204 {
		t.Fatalf("roll forward: %d", ex.Code)
	}
}
