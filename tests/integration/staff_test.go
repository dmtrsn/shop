//go:build integration

package integration_test

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"sweetshop/internal/application"
	"sweetshop/internal/delivery/httpapi"
	"sweetshop/internal/domain"
	"sweetshop/tests/contract"
)

const (
	staffEmail    = "ops@example.com"
	staffPassword = "staff password 1"
	staffCookie   = "__Host-sweetshop_staff"
	shopCookie    = "__Host-sweetshop_session"
)

// staffServer is the production-shaped handler with the staff API enabled.
func (d *database) staffServer(t *testing.T, logs io.Writer) http.Handler {
	t.Helper()
	if logs == nil {
		logs = io.Discard
	}
	return httpapi.New(d.service, slog.New(slog.NewTextHandler(logs, nil)), "", "postgres", contract.SessionConfig(),
		httpapi.WithStaff(application.NewStaffAuth(d.store), contract.StaffConfig()))
}

func (d *database) createStaff(t *testing.T) domain.StaffAccount {
	t.Helper()
	account, err := application.NewStaffAccounts(d.store).Create(d.ctx, application.NewStaffInput{Email: staffEmail, DisplayName: "Ops", Password: staffPassword})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

type exchange struct {
	*httptest.ResponseRecorder
	body map[string]any
}

func serve(handler http.Handler, method, path, body string, headers map[string]string) exchange {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	result := exchange{ResponseRecorder: rec}
	_ = json.Unmarshal(rec.Body.Bytes(), &result.body)
	return result
}

func (e exchange) cookie(name string) string {
	for _, c := range e.Result().Cookies() {
		if c.Name == name && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

func (e exchange) errorCode() string {
	if inner, ok := e.body["error"].(map[string]any); ok {
		return inner["code"].(string) + "|" + inner["message"].(string)
	}
	return ""
}

type staffLogin struct{ token, csrf, id string }

func loginStaff(t *testing.T, handler http.Handler, password string, headers map[string]string) (staffLogin, exchange) {
	t.Helper()
	all := map[string]string{"Origin": contract.AdminTrustedOrigin}
	for key, value := range headers {
		all[key] = value
	}
	ex := serve(handler, "POST", "/api/admin/v1/auth/session", `{"email":"`+staffEmail+`","password":"`+password+`"}`, all)
	if ex.Code != 200 {
		return staffLogin{}, ex
	}
	staff := ex.body["staff"].(map[string]any)
	return staffLogin{token: ex.cookie(staffCookie), csrf: ex.body["csrfToken"].(string), id: staff["id"].(string)}, ex
}

func staffState(handler http.Handler, token string) bool {
	ex := serve(handler, "GET", "/api/admin/v1/auth/session", "", map[string]string{"Cookie": staffCookie + "=" + token})
	return ex.Code == 200 && ex.body["authenticated"] == true
}

func TestAdminContractOnPostgreSQL(t *testing.T) {
	d := newDatabase(t, true)
	d.createStaff(t)
	handler := d.staffServer(t, nil)
	// The customer contract is unchanged with the staff API enabled.
	contract.Run(t, handler)
	contract.RunAdmin(t, handler, staffEmail, staffPassword)
}

func TestStaffRoutesAbsentWithoutConfiguration(t *testing.T) {
	d := newDatabase(t, true)
	d.createStaff(t)
	handler := httpapi.New(d.service, slog.New(slog.NewTextHandler(io.Discard, nil)), "", "postgres", contract.SessionConfig())
	for _, method := range []string{"GET", "POST", "DELETE"} {
		ex := serve(handler, method, "/api/admin/v1/auth/session", `{"email":"`+staffEmail+`","password":"`+staffPassword+`"}`, map[string]string{"Origin": contract.AdminTrustedOrigin})
		if ex.Code != 404 || ex.cookie(staffCookie) != "" {
			t.Fatalf("%s: %d", method, ex.Code)
		}
	}
	if d.number(t, "SELECT count(*) FROM staff_sessions") != 0 {
		t.Fatal("session created through an absent route")
	}
}

func TestStaffLifecycleThroughShopctl(t *testing.T) {
	d := productionDatabase(t)
	var logs lockedBuffer
	handler := d.staffServer(t, &logs)

	created := d.shopctl(t, "production", staffPassword+"\n", "staff", "create", "--email", " Ops@Example.COM ", "--name", " Оператор ", "--password-stdin")
	if created.code != 0 {
		t.Fatalf("create: %+v", created)
	}
	created.assertNoSecrets(t, staffPassword)
	type staffOutput struct {
		Action          string              `json:"action"`
		Account         domain.StaffAccount `json:"account"`
		RevokedSessions *int64              `json:"revokedSessions"`
	}
	var out staffOutput
	decode := func(r shopctlResult) {
		t.Helper()
		out = staffOutput{} // absent JSON members must not keep earlier values
		if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
			t.Fatalf("%q: %v", r.stdout, err)
		}
	}
	decode(created)
	if !strings.HasPrefix(out.Account.ID, "stf_") || out.Account.Email != staffEmail || out.Account.DisplayName != "Оператор" {
		t.Fatalf("account: %+v", out.Account)
	}
	hash := d.text(t, "SELECT password_hash FROM staff_users")
	if cost, err := bcrypt.Cost([]byte(hash)); err != nil || cost != 12 {
		t.Fatalf("cost=%d err=%v", cost, err)
	}
	if d.number(t, "SELECT count(*) FROM users") != 0 {
		t.Fatal("staff provisioning touched customer accounts")
	}
	if r := d.shopctl(t, "production", "another password\n", "staff", "create", "--email", "OPS@example.com", "--name", "Dup", "--password-stdin"); r.code != 3 {
		t.Fatalf("duplicate: %+v", r)
	}
	if r := d.shopctl(t, "production", "eleven char\n", "staff", "create", "--email", "short@example.com", "--name", "Short", "--password-stdin"); r.code != 2 {
		t.Fatalf("short password: %+v", r)
	}
	// A customer account with the same email is a different principal.
	if r := d.shopctl(t, "production", staffPassword+"\n", "customer", "create", "--email", staffEmail, "--name", "Customer", "--password-stdin"); r.code != 0 {
		t.Fatalf("customer with a staff email: %+v", r)
	}
	if _, err := d.service.StartCookieSession(d.ctx, staffEmail, staffPassword, ""); err != nil {
		t.Fatal(err)
	}
	if d.number(t, "SELECT count(*) FROM staff_sessions") != 0 {
		t.Fatal("customer login created a staff session")
	}

	login, ex := loginStaff(t, handler, staffPassword, nil)
	if ex.Code != 200 || login.id != out.Account.ID || login.token == "" {
		t.Fatalf("login: %d %s", ex.Code, ex.Body.String())
	}
	// Only the digest is stored.
	if d.number(t, "SELECT count(*) FROM staff_sessions WHERE token_hash=$1", login.token) != 0 || d.number(t, "SELECT count(*) FROM staff_sessions WHERE token_hash=$1", hashToken(login.token)) != 1 {
		t.Fatal("raw staff token stored or digest missing")
	}
	if d.number(t, "SELECT count(*) FROM sessions WHERE user_id IN (SELECT id FROM users)") != 1 {
		t.Fatal("staff login created a customer session")
	}

	disabled := d.shopctl(t, "production", "", "staff", "disable", "--email", staffEmail)
	decode(disabled)
	if disabled.code != 0 || *out.RevokedSessions != 1 || out.Account.DisabledAt == nil || staffState(handler, login.token) {
		t.Fatalf("disable: %+v", disabled)
	}
	unknown := serve(handler, "POST", "/api/admin/v1/auth/session", `{"email":"nobody@example.com","password":"`+staffPassword+`"}`, map[string]string{"Origin": contract.AdminTrustedOrigin})
	wrong := serve(handler, "POST", "/api/admin/v1/auth/session", `{"email":"`+staffEmail+`","password":"wrong password 1"}`, map[string]string{"Origin": contract.AdminTrustedOrigin})
	_, refused := loginStaff(t, handler, staffPassword, nil)
	if refused.Code != 401 || unknown.Code != 401 || wrong.Code != 401 || refused.errorCode() != unknown.errorCode() || refused.errorCode() != wrong.errorCode() {
		t.Fatalf("disabled=%d %s unknown=%s wrong=%s", refused.Code, refused.errorCode(), unknown.errorCode(), wrong.errorCode())
	}
	if !strings.Contains(logs.String(), "disabled staff refused") {
		t.Fatal("disabled refusal not logged")
	}
	// A session written while disabled (bypassing the guard) is refused and
	// does not survive enable.
	d.exec(t, "INSERT INTO staff_sessions(token_hash,staff_user_id,expires_at) VALUES ($1,$2,now()+interval '1 hour')", hashToken("unguarded"), out.Account.ID)
	if staffState(handler, "unguarded") {
		t.Fatal("disabled staff session authenticated")
	}
	enabled := d.shopctl(t, "production", "", "staff", "enable", "--email", staffEmail)
	decode(enabled)
	if enabled.code != 0 || *out.RevokedSessions != 1 || out.Account.DisabledAt != nil || staffState(handler, "unguarded") {
		t.Fatalf("enable: %+v", enabled)
	}

	login, _ = loginStaff(t, handler, staffPassword, nil)
	reset := d.shopctl(t, "production", "second password 2\n", "staff", "set-password", "--email", staffEmail, "--password-stdin")
	decode(reset)
	if reset.code != 0 || *out.RevokedSessions != 1 || staffState(handler, login.token) {
		t.Fatalf("set-password: %+v", reset)
	}
	reset.assertNoSecrets(t, "second password 2")
	if _, ex := loginStaff(t, handler, staffPassword, nil); ex.Code != 401 {
		t.Fatal("old staff password accepted")
	}
	if _, ex := loginStaff(t, handler, "second password 2", nil); ex.Code != 200 {
		t.Fatal("new staff password refused")
	}
	if r := d.shopctl(t, "production", "", "staff", "disable", "--email", "nobody@example.com"); r.code != 3 {
		t.Fatalf("unknown staff: %+v", r)
	}
	if _, err := d.service.StartCookieSession(d.ctx, staffEmail, staffPassword, ""); err != nil {
		t.Fatal("staff changes affected the customer account with the same email")
	}
	d.assertNoDemoIdentity(t)
}

func TestStaffSessionExpiryAndReplacement(t *testing.T) {
	d := productionDatabase(t)
	account := d.createStaff(t)
	handler := d.staffServer(t, nil)

	first, _ := loginStaff(t, handler, staffPassword, nil)
	d.exec(t, "UPDATE staff_sessions SET expires_at=now()-interval '1 second' WHERE token_hash=$1", hashToken(first.token))
	ex := serve(handler, "GET", "/api/admin/v1/auth/session", "", map[string]string{"Cookie": staffCookie + "=" + first.token})
	if ex.Code != 200 || ex.body["authenticated"] != false || !strings.Contains(ex.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("expired session: %d %v", ex.Code, ex.body)
	}

	// A new login purges the expired row and replaces the presented session.
	second, _ := loginStaff(t, handler, staffPassword, nil)
	third, _ := loginStaff(t, handler, staffPassword, map[string]string{"Cookie": staffCookie + "=" + second.token})
	if third.token == second.token || staffState(handler, second.token) || !staffState(handler, third.token) {
		t.Fatal("presented staff session not replaced")
	}
	if d.number(t, "SELECT count(*) FROM staff_sessions WHERE staff_user_id=$1", account.ID) != 1 {
		t.Fatal("expired or replaced sessions remain")
	}
	// A caller-chosen token is never adopted.
	fixed, _ := loginStaff(t, handler, staffPassword, map[string]string{"Cookie": staffCookie + "=attacker-chosen-token"})
	if fixed.token == "attacker-chosen-token" || d.number(t, "SELECT count(*) FROM staff_sessions WHERE token_hash=$1", hashToken("attacker-chosen-token")) != 0 {
		t.Fatal("session fixation")
	}
	// Customer session material is never reused by a staff login.
	customer, err := d.service.StartCookieSession(d.ctx, "demo@sugar.ru", "Sweet2026!", "")
	if err == nil {
		t.Fatal("production fixture has no demo customer")
	}
	d.exec(t, "INSERT INTO users(id,name,email,password_hash) VALUES ('usr_customer','C','c@example.com',$1)", d.text(t, "SELECT password_hash FROM staff_users"))
	customer, err = d.service.StartCookieSession(d.ctx, "c@example.com", staffPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	mixed, _ := loginStaff(t, handler, staffPassword, map[string]string{"Cookie": shopCookie + "=" + customer.Token + "; " + staffCookie + "=" + customer.Token})
	if mixed.token == customer.Token || d.number(t, "SELECT count(*) FROM staff_sessions WHERE token_hash=$1", hashToken(customer.Token)) != 0 {
		t.Fatal("customer token reused as a staff session")
	}
	if _, err := d.service.AuthenticateSession(d.ctx, customer.Token, domain.SessionCookie); err != nil {
		t.Fatal("staff login revoked a customer session")
	}
}

func TestStaffLoginCannotOutrunResetOrDisable(t *testing.T) {
	d := productionDatabase(t)
	account := d.createStaff(t)
	auth := application.NewStaffAuth(d.store)
	known, err := bcrypt.GenerateFromPassword([]byte("known password 12"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	other, err := bcrypt.GenerateFromPassword([]byte("other password 12"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, change string }{
		{"set-password", "UPDATE staff_users SET password_hash=$2 WHERE id=$1"},
		{"disable", "UPDATE staff_users SET disabled_at=now() WHERE id=$1 AND $2::text IS NOT NULL"},
	} {
		name, change := tc.name, tc.change
		t.Run(name, func(t *testing.T) {
			d.exec(t, "UPDATE staff_users SET password_hash=$2, disabled_at=NULL WHERE id=$1", account.ID, string(known))
			tx, err := d.pool.Begin(d.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(d.ctx)
			if _, err := tx.Exec(d.ctx, change, account.ID, string(other)); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, err := auth.Login(d.ctx, staffEmail, "known password 12", "")
				result <- err
			}()
			d.waitForLockWaiters(t, 1)
			if _, err := tx.Exec(d.ctx, "DELETE FROM staff_sessions WHERE staff_user_id=$1", account.ID); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(d.ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("login verified the old state but got: %v", err)
			}
			if d.number(t, "SELECT count(*) FROM staff_sessions WHERE staff_user_id=$1", account.ID) != 0 {
				t.Fatal("a staff session outlived the change")
			}
		})
	}
}

func TestConcurrentDuplicateStaffCreation(t *testing.T) {
	d := productionDatabase(t)
	accounts := application.NewStaffAccounts(d.store)
	emails := []string{"race@example.com", "RACE@example.com", " race@EXAMPLE.com", "Race@Example.Com"}
	errs := make(chan error, len(emails))
	var start sync.WaitGroup
	start.Add(1)
	for _, email := range emails {
		go func(email string) {
			start.Wait()
			_, err := accounts.Create(d.ctx, application.NewStaffInput{Email: email, DisplayName: "Race", Password: staffPassword})
			errs <- err
		}(email)
	}
	start.Done()
	created, taken := 0, 0
	for range emails {
		switch err := <-errs; {
		case err == nil:
			created++
		case errors.Is(err, domain.ErrEmailTaken):
			taken++
		default:
			t.Fatal(err)
		}
	}
	if created != 1 || taken != len(emails)-1 || d.number(t, "SELECT count(*) FROM staff_users") != 1 {
		t.Fatalf("created=%d taken=%d", created, taken)
	}
}

func TestCustomerAndStaffPrincipalsStayIsolated(t *testing.T) {
	d := newDatabase(t, true)
	d.createStaff(t)
	handler := d.staffServer(t, nil)
	customer := serve(handler, "POST", "/api/auth/session", `{"email":"demo@sugar.ru","password":"Sweet2026!"}`, map[string]string{"Origin": contract.TrustedOrigin})
	customerToken, customerCSRF := customer.cookie(shopCookie), customer.body["csrfToken"].(string)
	if customer.Code != 200 || customerToken == "" {
		t.Fatal("customer login")
	}
	bearer := serve(handler, "POST", "/api/auth/login", `{"email":"demo@sugar.ru","password":"Sweet2026!"}`, nil).body["token"].(string)
	staff, _ := loginStaff(t, handler, staffPassword, nil)
	if d.number(t, "SELECT count(*) FROM staff_sessions WHERE token_hash IN (SELECT token_hash FROM sessions)") != 0 {
		t.Fatal("shared session material")
	}
	customerCookie := shopCookie + "=" + customerToken
	staffOnly := staffCookie + "=" + staff.token
	both := customerCookie + "; " + staffOnly

	// Customer credentials never authenticate staff.
	if staffState(handler, customerToken) {
		t.Fatal("customer token under the staff cookie name")
	}
	if ex := serve(handler, "GET", "/api/admin/v1/auth/session", "", map[string]string{"Cookie": customerCookie}); ex.body["authenticated"] != false {
		t.Fatal("customer cookie authenticated staff")
	}
	for _, method := range []string{"GET", "POST", "DELETE"} {
		ex := serve(handler, method, "/api/admin/v1/auth/session", `{"email":"`+staffEmail+`","password":"`+staffPassword+`"}`, map[string]string{"Authorization": "Bearer " + bearer, "Origin": contract.AdminTrustedOrigin})
		if ex.Code != 401 {
			t.Fatalf("customer bearer on staff %s: %d", method, ex.Code)
		}
	}
	// Staff credentials never authenticate customers.
	for _, path := range []string{"/api/me", "/api/cart", "/api/orders", "/api/favorites"} {
		for _, cookie := range []string{staffOnly, shopCookie + "=" + staff.token} {
			if ex := serve(handler, "GET", path, "", map[string]string{"Cookie": cookie}); ex.Code != 401 || ex.Header().Get("X-Authenticated-User-ID") != "" {
				t.Fatalf("staff credential on %s: %d", path, ex.Code)
			}
		}
	}
	// With both cookies each route sees only its own principal.
	me := serve(handler, "GET", "/api/me", "", map[string]string{"Cookie": both})
	state := serve(handler, "GET", "/api/admin/v1/auth/session", "", map[string]string{"Cookie": both})
	if me.Header().Get("X-Authenticated-User-ID") != "usr-demo" || me.Header().Get("X-Authenticated-Staff-ID") != "" ||
		state.Header().Get("X-Authenticated-Staff-ID") != staff.id || state.Header().Get("X-Authenticated-User-ID") != "" || state.body["authenticated"] != true {
		t.Fatal("principals mixed with both cookies")
	}

	// CSRF and origins are per principal.
	if ex := serve(handler, "DELETE", "/api/admin/v1/auth/session", "", map[string]string{"Cookie": both, "Origin": contract.AdminTrustedOrigin, "X-CSRF-Token": customerCSRF}); ex.Code != 403 || !staffState(handler, staff.token) {
		t.Fatalf("customer CSRF on staff logout: %d", ex.Code)
	}
	if ex := serve(handler, "DELETE", "/api/admin/v1/auth/session", "", map[string]string{"Cookie": both, "Origin": contract.TrustedOrigin, "X-CSRF-Token": staff.csrf}); ex.Code != 403 || !staffState(handler, staff.token) {
		t.Fatalf("customer origin on staff logout: %d", ex.Code)
	}
	favorite := func(headers map[string]string) int {
		return serve(handler, "PUT", "/api/favorites/p01", "", headers).Code
	}
	if code := favorite(map[string]string{"Cookie": both, "Origin": contract.TrustedOrigin, "X-CSRF-Token": staff.csrf}); code != 403 {
		t.Fatalf("staff CSRF on a customer mutation: %d", code)
	}
	if code := favorite(map[string]string{"Cookie": both, "Origin": contract.AdminTrustedOrigin, "X-CSRF-Token": customerCSRF}); code != 403 {
		t.Fatalf("staff origin on a customer mutation: %d", code)
	}
	// The other principal's expected header has no effect.
	if code := favorite(map[string]string{"Cookie": both, "Origin": contract.TrustedOrigin, "X-CSRF-Token": customerCSRF, "X-Expected-Staff-ID": "stf_other"}); code != 200 {
		t.Fatalf("staff expected header affected a customer mutation: %d", code)
	}
	if ex := serve(handler, "DELETE", "/api/admin/v1/auth/session", "", map[string]string{"Cookie": both, "Origin": contract.AdminTrustedOrigin, "X-CSRF-Token": staff.csrf, "X-Expected-Authenticated-User-ID": "stf_other"}); ex.Code != 204 || staffState(handler, staff.token) {
		t.Fatalf("customer expected header acted on staff logout: %d", ex.Code)
	}
	if _, err := d.service.AuthenticateSession(d.ctx, customerToken, domain.SessionCookie); err != nil {
		t.Fatal("staff logout revoked the customer session")
	}
}
