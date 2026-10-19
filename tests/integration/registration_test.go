//go:build integration

package integration_test

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"sweetshop/internal/application"
	"sweetshop/internal/delivery/httpapi"
	"sweetshop/internal/domain"
	"sweetshop/internal/email"
	"sweetshop/internal/email/emailtest"
	postgres "sweetshop/internal/repository/postgres"
	"sweetshop/tests/contract"
)

const (
	registrant         = "ann@example.com"
	registrantPassword = "registrant password 1"
	otherPassword      = "another password 22"
)

// testClock is the registration clock; sessions keep the wall clock.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *testClock) set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at
}

type selfService struct {
	*database
	store         *postgres.Store
	outbox        *emailtest.Outbox
	clock         *testClock
	logs          *lockedBuffer
	registrations *application.CustomerRegistrations
	resets        *application.PasswordResets
	service       *application.Service
	handler       http.Handler
}

// selfService is a production-shaped handler with self-service over
// store (the test database's own unless given), a recording outbox and a
// controllable registration clock. cost is the registration bcrypt cost; 0
// is the production cost, which takes seconds under the race detector, so
// only the test asserting it uses 0.
func (d *database) selfService(t *testing.T, store *postgres.Store, outbox *emailtest.Outbox, clock *testClock, cost int) *selfService {
	t.Helper()
	if store == nil {
		store = d.store
	}
	if outbox == nil {
		outbox = &emailtest.Outbox{}
	}
	if clock == nil {
		clock = &testClock{now: time.Now().UTC().Truncate(time.Microsecond)}
	}
	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	origin, err := email.ParsePublicOrigin(contract.TrustedOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(store, application.Options{AllowDemoAccount: true})
	registrations, err := application.NewCustomerRegistrations(store, service, outbox, origin, logger, application.RegistrationOptions{PasswordCost: cost, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	resets, err := application.NewPasswordResets(store, outbox, origin, logger, application.RegistrationOptions{PasswordCost: cost, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(service, logger, "", "postgres", contract.SessionConfig(), httpapi.WithSelfService(registrations, resets))
	return &selfService{database: d, store: store, outbox: outbox, clock: clock, logs: logs, registrations: registrations, resets: resets, service: service, handler: handler}
}

// secondStore is a separate pool on the same database, standing in for
// another replica.
func (d *database) secondStore(t *testing.T) *postgres.Store {
	t.Helper()
	pool, err := pgxpool.NewWithConfig(d.ctx, d.pool.Config().Copy())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return postgres.New(pool)
}

func registrationBody(address, password string) string {
	return `{"email":"` + address + `","password":"` + password + `","name":"Анна","phone":"+7 900 000-00-00"}`
}

var shopOrigin = map[string]string{"Origin": contract.TrustedOrigin}

func (f *selfService) register(t *testing.T, address, password string) exchange {
	t.Helper()
	return serve(f.handler, "POST", "/api/auth/registration", registrationBody(address, password), shopOrigin)
}

func (f *selfService) resend(t *testing.T, address string) exchange {
	t.Helper()
	return serve(f.handler, "POST", "/api/auth/registration/resend", `{"email":"`+address+`"}`, shopOrigin)
}

func (f *selfService) confirm(t *testing.T, token, password string) exchange {
	t.Helper()
	return serve(f.handler, "POST", "/api/auth/registration/confirm", `{"token":"`+token+`","password":"`+password+`"}`, shopOrigin)
}

// lastToken is the token of the newest confirmation email to address.
func (f *selfService) lastToken(t *testing.T, address string) string {
	t.Helper()
	messages := f.outbox.To(address)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Kind == email.KindRegistrationConfirmation {
			return emailtest.Token(messages[i])
		}
	}
	t.Fatalf("no confirmation email to %s", address)
	return ""
}

func (f *selfService) kinds(address string) []email.Kind {
	kinds := []email.Kind{}
	for _, message := range f.outbox.To(address) {
		kinds = append(kinds, message.Kind)
	}
	return kinds
}

// operatorCustomer inserts an account the way shopctl customer create does
// (the store call), with a fast hash.
func (d *database) operatorCustomer(t *testing.T, address, password string) domain.CustomerAccount {
	t.Helper()
	account, err := d.store.CreateCustomer(d.ctx, operatorInput(t, address, password))
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func operatorInput(t *testing.T, address, password string) domain.NewCustomer {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		t.Fatal(err)
	}
	return domain.NewCustomer{ID: "usr_" + hex.EncodeToString(id), Email: address, Name: "Оператор", PasswordHash: string(hash)}
}

func (d *database) usersWithEmail(t *testing.T, address string) int64 {
	t.Helper()
	return d.number(t, "SELECT count(*) FROM users WHERE lower(email)=lower($1)", address)
}

func (d *database) pendingRows(t *testing.T, address string) int64 {
	t.Helper()
	return d.number(t, "SELECT count(*) FROM customer_registrations WHERE email=$1", address)
}

// assertQuiet checks that logs never carry an address, password, hash or token.
func (f *selfService) assertQuiet(t *testing.T, secrets ...string) {
	t.Helper()
	logs := f.logs.String()
	for _, secret := range append(secrets, "$2a$", "@example.com") {
		if secret != "" && strings.Contains(logs, secret) {
			t.Fatalf("logs contain %q", secret)
		}
	}
}

func TestSelfServiceContractOnPostgreSQL(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	contract.RunSelfService(t, f.handler, f.outbox)
}

// 1-3: a registration is a pending row with a cost-12 hash and a token
// digest, never a customer; nothing can sign in with it.
func TestRegistrationStoresAPendingRowNotAnAccount(t *testing.T) {
	d := productionDatabase(t)
	f := d.selfService(t, nil, nil, nil, 0)
	if ex := f.register(t, " Ann@Example.COM ", registrantPassword); ex.Code != 202 {
		t.Fatalf("register: %d %s", ex.Code, ex.Body)
	}
	if d.usersWithEmail(t, registrant) != 0 || d.number(t, "SELECT count(*) FROM users") != 0 {
		t.Fatal("a registration created a users row")
	}
	var name, phone, hash, digest string
	var created, expires time.Time
	if err := d.pool.QueryRow(d.ctx, `SELECT name,phone,password_hash,token_hash,created_at,expires_at FROM customer_registrations WHERE email=$1`, registrant).
		Scan(&name, &phone, &hash, &digest, &created, &expires); err != nil {
		t.Fatal(err)
	}
	if cost, err := bcrypt.Cost([]byte(hash)); err != nil || cost != application.CustomerPasswordCost || bcrypt.CompareHashAndPassword([]byte(hash), []byte(registrantPassword)) != nil {
		t.Fatalf("stored hash cost %d: %v", cost, err)
	}
	if name != "Анна" || phone != "+7 900 000-00-00" || !expires.Equal(created.Add(48*time.Hour)) {
		t.Fatalf("row %s %s %v %v", name, phone, created, expires)
	}
	token := f.lastToken(t, registrant)
	if len(token) != 43 || digest == token || len(digest) != 43 {
		t.Fatal("the token is stored instead of its digest")
	}
	everything := d.text(t, `SELECT string_agg(row_to_json(r)::text, ' ') FROM (
		SELECT * FROM customer_registrations) r`) + d.text(t, `SELECT coalesce(string_agg(row_to_json(t)::text, ' '), '') FROM customer_email_throttle t`)
	if strings.Contains(everything, registrantPassword) || strings.Contains(everything, token) {
		t.Fatal("the raw password or token is persisted")
	}
	// The pending address cannot sign in and has no identity of any kind.
	if ex := serve(f.handler, "POST", "/api/auth/session", `{"email":"`+registrant+`","password":"`+registrantPassword+`"}`, shopOrigin); ex.Code != 401 {
		t.Fatalf("pending address signed in: %d", ex.Code)
	}
	for _, table := range []string{"sessions", "cart_items", "favorites", "orders"} {
		if d.number(t, "SELECT count(*) FROM "+table) != 0 {
			t.Fatalf("%s has rows", table)
		}
	}
	f.assertQuiet(t, registrantPassword, token, digest, hash)
}

// 4, 5 and the HTTP enumeration checks: the four address states get the
// same status, body and headers.
func TestRegistrationAnswersAlikeForEveryAddressState(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	existing := d.operatorCustomer(t, "existing@example.com", otherPassword)
	existingHash := d.text(t, "SELECT password_hash FROM users WHERE id=$1", existing.ID)
	if ex := f.register(t, "pending@example.com", registrantPassword); ex.Code != 202 {
		t.Fatal(ex.Body)
	}
	if ex := f.register(t, "throttled@example.com", registrantPassword); ex.Code != 202 {
		t.Fatal(ex.Body)
	}
	pendingDigest := d.text(t, "SELECT token_hash FROM customer_registrations WHERE email='pending@example.com'")
	throttledDigest := d.text(t, "SELECT token_hash FROM customer_registrations WHERE email='throttled@example.com'")
	// The pending address is past its cooldown, the throttled one is not.
	d.exec(t, "UPDATE customer_email_throttle SET last_sent_at=last_sent_at-interval '2 minutes', sent_at=ARRAY[last_sent_at-interval '2 minutes'] WHERE email='pending@example.com'")
	before := len(f.outbox.Messages())

	answers := map[string]exchange{}
	for _, address := range []string{"new@example.com", "existing@example.com", "pending@example.com", "throttled@example.com", "DEMO@sugar.ru"} {
		answers[address] = f.register(t, address, otherPassword)
	}
	reference := answers["new@example.com"]
	for address, ex := range answers {
		headers := ex.Header().Clone()
		headers.Del("X-Request-ID")
		want := reference.Header().Clone()
		want.Del("X-Request-ID")
		if ex.Code != 202 || ex.Body.String() != `{"status":"pending"}`+"\n" || !equalHeaders(headers, want) || headers.Get("Cache-Control") != "no-store" || len(ex.Result().Cookies()) != 0 {
			t.Fatalf("%s answered differently: %d %q %v", address, ex.Code, ex.Body.String(), headers)
		}
	}
	// The effects differ, invisibly to the client.
	if d.text(t, "SELECT password_hash FROM users WHERE id=$1", existing.ID) != existingHash || d.pendingRows(t, "existing@example.com") != 0 || d.usersWithEmail(t, "existing@example.com") != 1 {
		t.Fatal("registration touched an existing account")
	}
	if d.pendingRows(t, "demo@sugar.ru") != 0 {
		t.Fatal("pending registration for the demo account")
	}
	if d.text(t, "SELECT token_hash FROM customer_registrations WHERE email='pending@example.com'") == pendingDigest {
		t.Fatal("a pending registration past its cooldown was not replaced")
	}
	if d.text(t, "SELECT token_hash FROM customer_registrations WHERE email='throttled@example.com'") != throttledDigest {
		t.Fatal("a throttled registration changed state")
	}
	sent := map[string][]email.Kind{}
	for _, message := range f.outbox.Messages()[before:] {
		sent[message.To] = append(sent[message.To], message.Kind)
	}
	if !slices.Equal(sent["new@example.com"], []email.Kind{email.KindRegistrationConfirmation}) ||
		!slices.Equal(sent["existing@example.com"], []email.Kind{email.KindAccountExists}) ||
		!slices.Equal(sent["pending@example.com"], []email.Kind{email.KindRegistrationConfirmation}) ||
		!slices.Equal(sent["demo@sugar.ru"], []email.Kind{email.KindAccountExists}) ||
		len(sent["throttled@example.com"]) != 0 {
		t.Fatalf("emails %v", sent)
	}
	// Legitimate validation errors stay visible, and Origin is required.
	if ex := f.register(t, "not-an-address", "short"); ex.Code != 422 || !strings.Contains(ex.errorCode(), "validation_error") {
		t.Fatalf("validation: %d", ex.Code)
	}
	for _, path := range []string{"/api/auth/registration", "/api/auth/registration/resend", "/api/auth/registration/confirm"} {
		if ex := serve(f.handler, "POST", path, registrationBody("new2@example.com", otherPassword), nil); ex.Code != 403 {
			t.Fatalf("%s without Origin: %d", path, ex.Code)
		}
	}
	for _, ex := range answers {
		for _, secret := range []string{otherPassword, "$2a$", existing.ID} {
			if strings.Contains(ex.Body.String(), secret) || strings.Contains(strings.Join(ex.Header().Values("Set-Cookie"), ""), secret) {
				t.Fatalf("response contains %q", secret)
			}
		}
	}
	f.assertQuiet(t, registrantPassword, otherPassword, pendingDigest, throttledDigest)
}

func equalHeaders(a, b http.Header) bool {
	if len(a) != len(b) {
		return false
	}
	for key, values := range a {
		if !slices.Equal(values, b[key]) {
			return false
		}
	}
	return true
}

// 6: a newer registration replaces the password and the token.
func TestNewerRegistrationInvalidatesTheOldLink(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	f.register(t, registrant, registrantPassword)
	old := f.lastToken(t, registrant)
	f.clock.advance(61 * time.Second)
	f.register(t, registrant, otherPassword)
	newer := f.lastToken(t, registrant)
	if newer == old {
		t.Fatal("same token")
	}
	if ex := f.confirm(t, old, registrantPassword); ex.Code != 410 || !strings.HasPrefix(ex.errorCode(), "link_invalid|") {
		t.Fatalf("old link: %d %s", ex.Code, ex.errorCode())
	}
	if ex := f.confirm(t, newer, registrantPassword); ex.Code != 401 {
		t.Fatalf("old password on the new link: %d", ex.Code)
	}
	if ex := f.confirm(t, newer, otherPassword); ex.Code != 200 {
		t.Fatalf("new link: %d %s", ex.Code, ex.Body)
	}
}

// 7 and 17: concurrent registrations for one address, from two pools
// (replicas), serialize: one stores and emails, the rest are throttled.
func TestConcurrentRegistrationsSerializeAcrossReplicas(t *testing.T) {
	d := newDatabase(t, true)
	outbox := &emailtest.Outbox{}
	clock := &testClock{now: time.Now().UTC().Truncate(time.Microsecond)}
	replicas := []*selfService{d.selfService(t, nil, outbox, clock, bcrypt.MinCost), d.selfService(t, d.secondStore(t), outbox, clock, bcrypt.MinCost)}
	const attempts = 12
	outcomes := make(chan domain.RegistrationOutcome, attempts)
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := replicas[i%2].registrations.Register(d.ctx, application.NewCustomerInput{Email: registrant, Name: "Анна", Password: registrantPassword})
			outcomes <- receipt.Outcome
			errs <- err
		}()
	}
	wg.Wait()
	close(outcomes)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	count := map[domain.RegistrationOutcome]int{}
	for outcome := range outcomes {
		count[outcome]++
	}
	if count[domain.RegistrationPending] != 1 || count[domain.RegistrationThrottled] != attempts-1 {
		t.Fatalf("outcomes %v", count)
	}
	if len(outbox.To(registrant)) != 1 || d.pendingRows(t, registrant) != 1 ||
		d.number(t, "SELECT cardinality(sent_at) FROM customer_email_throttle WHERE email=$1", registrant) != 1 {
		t.Fatal("more than one registration or email")
	}
	if ex := replicas[1].confirm(t, replicas[0].lastToken(t, registrant), registrantPassword); ex.Code != 200 {
		t.Fatalf("the single link: %d", ex.Code)
	}
}

// 8: registration racing operator provisioning never yields two accounts
// and never overwrites the operator's.
func TestOperatorProvisioningRacingRegistration(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	for i := range 8 {
		address := "race" + string(rune('a'+i)) + "@example.com"
		input := operatorInput(t, address, otherPassword)
		var wg sync.WaitGroup
		var operatorErr, registerErr error
		wg.Add(2)
		go func() { defer wg.Done(); _, operatorErr = d.store.CreateCustomer(d.ctx, input) }()
		go func() {
			defer wg.Done()
			_, registerErr = f.registrations.Register(d.ctx, application.NewCustomerInput{Email: address, Name: "Анна", Password: registrantPassword})
		}()
		wg.Wait()
		if operatorErr != nil || registerErr != nil {
			t.Fatal(operatorErr, registerErr)
		}
		if d.usersWithEmail(t, address) != 1 {
			t.Fatal("not exactly one account")
		}
		// If the registration won the race it is pending; its link must not
		// replace or duplicate the operator's account.
		if d.pendingRows(t, address) == 1 {
			if ex := f.confirm(t, f.lastToken(t, address), registrantPassword); ex.Code != 410 {
				t.Fatalf("confirmation over an operator account: %d", ex.Code)
			}
		}
		if d.pendingRows(t, address) != 0 || d.usersWithEmail(t, address) != 1 ||
			d.text(t, "SELECT password_hash FROM users WHERE lower(email)=$1", address) != input.PasswordHash {
			t.Fatal("operator account changed")
		}
	}
}

// 9, 15, 16: confirmation creates exactly one customer, consumes the
// registration and signs in through the normal session path.
func TestConfirmationCreatesOneCustomerAndANormalSession(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	f.register(t, registrant, registrantPassword)
	pendingHash := d.text(t, "SELECT password_hash FROM customer_registrations WHERE email=$1", registrant)
	token := f.lastToken(t, registrant)
	previous := cookieLogin(t, f.handler, "demo@sugar.ru", "Sweet2026!")
	ex := serve(f.handler, "POST", "/api/auth/registration/confirm", `{"token":"`+token+`","password":"`+registrantPassword+`"}`,
		map[string]string{"Origin": contract.TrustedOrigin, "Cookie": shopCookie + "=" + previous})
	if ex.Code != 200 {
		t.Fatalf("confirm: %d %s", ex.Code, ex.Body)
	}
	user := ex.body["user"].(map[string]any)
	id := user["id"].(string)
	if !strings.HasPrefix(id, "usr_") || user["email"] != registrant || user["name"] != "Анна" || user["phone"] != "+7 900 000-00-00" {
		t.Fatalf("user %v", user)
	}
	if d.usersWithEmail(t, registrant) != 1 || d.pendingRows(t, registrant) != 0 ||
		d.text(t, "SELECT password_hash FROM users WHERE id=$1", id) != pendingHash ||
		d.number(t, "SELECT count(*) FROM users WHERE id=$1 AND disabled_at IS NULL", id) != 1 {
		t.Fatal("customer row")
	}
	cookie := ex.cookie(shopCookie)
	if cookie == "" || cookie == token || strings.Contains(ex.Body.String(), cookie) {
		t.Fatal("session cookie")
	}
	if d.number(t, "SELECT count(*) FROM sessions WHERE user_id=$1 AND transport='cookie' AND token_hash=$2", id, hashToken(cookie)) != 1 ||
		d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1", cookie) != 0 {
		t.Fatal("the session is not a digest-only cookie session")
	}
	// The presented cookie of another account was replaced, as on login.
	if d.number(t, "SELECT count(*) FROM sessions WHERE token_hash=$1", hashToken(previous)) != 0 {
		t.Fatal("previous cookie session survived")
	}
	me := serve(f.handler, "GET", "/api/me", "", map[string]string{"Cookie": shopCookie + "=" + cookie})
	if me.Code != 200 || me.Header().Get("X-Authenticated-User-ID") != id {
		t.Fatalf("/api/me: %d", me.Code)
	}
	// From now on it is an ordinary account: password sign-in works.
	if cookieLogin(t, f.handler, registrant, registrantPassword) == "" {
		t.Fatal("login")
	}
	f.assertQuiet(t, registrantPassword, token, cookie, pendingHash)
}

// cookieLogin signs in through the cookie endpoint and returns the cookie.
func cookieLogin(t *testing.T, handler http.Handler, address, password string) string {
	t.Helper()
	ex := serve(handler, "POST", "/api/auth/session", `{"email":"`+address+`","password":"`+password+`"}`, shopOrigin)
	if ex.Code != 200 {
		t.Fatalf("login %s: %d", address, ex.Code)
	}
	return ex.cookie(shopCookie)
}

// 10: of concurrent confirmations of one link exactly one succeeds.
func TestConcurrentConfirmationsHaveOneWinner(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	other := d.selfService(t, d.secondStore(t), f.outbox, f.clock, bcrypt.MinCost)
	f.register(t, registrant, registrantPassword)
	token := f.lastToken(t, registrant)
	const attempts = 8
	codes := make(chan int, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handler := []*selfService{f, other}[i%2]
			codes <- handler.confirm(t, token, registrantPassword).Code
		}()
	}
	wg.Wait()
	close(codes)
	count := map[int]int{}
	for code := range codes {
		count[code]++
	}
	if count[200] != 1 || count[410] != attempts-1 {
		t.Fatalf("codes %v", count)
	}
	if d.usersWithEmail(t, registrant) != 1 || d.number(t, "SELECT count(*) FROM sessions s JOIN users u ON u.id=s.user_id WHERE u.email=$1", registrant) != 1 {
		t.Fatal("not exactly one account and session")
	}
}

// 11-13 and the expiry boundary.
func TestConfirmationRefusesWrongPasswordExpiryAndReplay(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	f.register(t, registrant, registrantPassword)
	token := f.lastToken(t, registrant)
	for range 3 {
		if ex := f.confirm(t, token, "wrong password 99"); ex.Code != 401 || !strings.HasPrefix(ex.errorCode(), "unauthorized|") {
			t.Fatalf("wrong password: %d", ex.Code)
		}
	}
	if d.pendingRows(t, registrant) != 1 || d.usersWithEmail(t, registrant) != 0 {
		t.Fatal("a wrong password consumed the registration or created an account")
	}
	var expires time.Time
	if err := d.pool.QueryRow(d.ctx, "SELECT expires_at FROM customer_registrations WHERE email=$1", registrant).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	f.clock.set(expires)
	if ex := f.confirm(t, token, registrantPassword); ex.Code != 410 {
		t.Fatalf("at the expiry instant: %d", ex.Code)
	}
	f.clock.set(expires.Add(-time.Microsecond))
	if ex := f.confirm(t, token, registrantPassword); ex.Code != 200 {
		t.Fatalf("just before expiry: %d %s", ex.Code, ex.Body)
	}
	if ex := f.confirm(t, token, registrantPassword); ex.Code != 410 {
		t.Fatalf("replay: %d", ex.Code)
	}
	if d.usersWithEmail(t, registrant) != 1 {
		t.Fatal("replay created an account")
	}
}

// 14: an account the operator created meanwhile wins; the link dies.
func TestOperatorAccountCreatedBeforeConfirmationWins(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	f.register(t, registrant, registrantPassword)
	token := f.lastToken(t, registrant)
	account := d.operatorCustomer(t, registrant, otherPassword)
	hash := d.text(t, "SELECT password_hash FROM users WHERE id=$1", account.ID)
	if ex := f.confirm(t, token, registrantPassword); ex.Code != 410 || ex.cookie(shopCookie) != "" {
		t.Fatalf("confirm over an operator account: %d", ex.Code)
	}
	if d.pendingRows(t, registrant) != 0 || d.usersWithEmail(t, registrant) != 1 ||
		d.text(t, "SELECT password_hash FROM users WHERE id=$1", account.ID) != hash ||
		d.number(t, "SELECT count(*) FROM sessions WHERE user_id=$1", account.ID) != 0 {
		t.Fatal("operator account changed or registration kept")
	}
}

// 17: the per-address allowance is one email a minute and ten a day,
// shared by registration and resend.
func TestEmailAllowanceIsSharedAndSliding(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	start := f.clock.Now()
	f.register(t, registrant, registrantPassword)
	f.resend(t, registrant)
	f.register(t, registrant, registrantPassword)
	if len(f.outbox.To(registrant)) != 1 {
		t.Fatal("cooldown not applied")
	}
	for i := 1; i < 10; i++ {
		f.clock.advance(61 * time.Second)
		if i%2 == 0 {
			f.register(t, registrant, registrantPassword)
		} else {
			f.resend(t, registrant)
		}
	}
	if len(f.outbox.To(registrant)) != 10 {
		t.Fatalf("emails %d", len(f.outbox.To(registrant)))
	}
	f.clock.advance(time.Hour)
	f.resend(t, registrant)
	f.register(t, registrant, registrantPassword)
	if len(f.outbox.To(registrant)) != 10 {
		t.Fatal("daily limit exceeded")
	}
	// The window slides: once the first message is a day old, one more fits.
	f.clock.set(start.Add(24*time.Hour + time.Second))
	f.resend(t, registrant)
	if len(f.outbox.To(registrant)) != 11 || d.number(t, "SELECT cardinality(sent_at) FROM customer_email_throttle WHERE email=$1", registrant) != 10 {
		t.Fatal("sliding window")
	}
	// Another address has its own allowance.
	f.register(t, "other@example.com", registrantPassword)
	if len(f.outbox.To("other@example.com")) != 1 {
		t.Fatal("allowance is not per address")
	}
}

// Resend: a pending registration gets a new token, an account the notice,
// and an unknown address nothing at all.
func TestResendBranches(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	if ex := f.resend(t, "unknown@example.com"); ex.Code != 202 || ex.Body.String() != `{"status":"pending"}`+"\n" {
		t.Fatalf("unknown: %d", ex.Code)
	}
	if len(f.outbox.Messages()) != 0 || d.number(t, "SELECT count(*) FROM customer_email_throttle") != 0 || d.number(t, "SELECT count(*) FROM customer_registrations") != 0 {
		t.Fatal("resend for an unknown address left state or email")
	}
	if ex := f.resend(t, "not-an-address"); ex.Code != 422 {
		t.Fatalf("invalid: %d", ex.Code)
	}
	d.operatorCustomer(t, "existing@example.com", otherPassword)
	f.resend(t, "existing@example.com")
	if !slices.Equal(f.kinds("existing@example.com"), []email.Kind{email.KindAccountExists}) {
		t.Fatal("account notice")
	}
	f.register(t, registrant, registrantPassword)
	old := f.lastToken(t, registrant)
	f.clock.advance(61 * time.Second)
	f.resend(t, registrant)
	newer := f.lastToken(t, registrant)
	if newer == old {
		t.Fatal("resend kept the token")
	}
	if ex := f.confirm(t, old, registrantPassword); ex.Code != 410 {
		t.Fatalf("old link after resend: %d", ex.Code)
	}
	// An expired registration is not resent; registering again is needed.
	f.clock.advance(49 * time.Hour)
	before := len(f.outbox.Messages())
	f.resend(t, registrant)
	if len(f.outbox.Messages()) != before {
		t.Fatal("expired registration resent")
	}
	if ex := f.confirm(t, newer, registrantPassword); ex.Code != 410 {
		t.Fatal("expired link accepted")
	}
}

// 18: a refused email keeps the registration, and resend recovers.
func TestRefusedEmailIsRecoverableByResend(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	f.outbox.Refuse = email.ErrQueueFull
	if ex := f.register(t, registrant, registrantPassword); ex.Code != 202 {
		t.Fatalf("register with a full queue: %d", ex.Code)
	}
	if d.pendingRows(t, registrant) != 1 || f.outbox.Refused() != 1 || len(f.outbox.Messages()) != 0 {
		t.Fatal("registration lost or email accepted")
	}
	if !strings.Contains(f.logs.String(), "account email not queued") {
		t.Fatal("no operational error")
	}
	lost := d.text(t, "SELECT token_hash FROM customer_registrations WHERE email=$1", registrant)
	f.outbox.Refuse = nil
	f.clock.advance(61 * time.Second)
	f.resend(t, registrant)
	if d.text(t, "SELECT token_hash FROM customer_registrations WHERE email=$1", registrant) == lost {
		t.Fatal("resend did not mint a new token")
	}
	if ex := f.confirm(t, f.lastToken(t, registrant), registrantPassword); ex.Code != 200 {
		t.Fatalf("confirm after recovery: %d", ex.Code)
	}
	f.assertQuiet(t, registrantPassword, lost)
}

// 19: cleanup removes only expired registrations and idle allowances, and
// concurrent runs (two replicas) are harmless.
func TestCleanupRemovesOnlyExpiredState(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	other := d.selfService(t, d.secondStore(t), f.outbox, f.clock, bcrypt.MinCost)
	f.register(t, "expired@example.com", registrantPassword)
	f.register(t, "confirmed@example.com", registrantPassword)
	f.confirm(t, f.lastToken(t, "confirmed@example.com"), registrantPassword)
	f.clock.advance(47 * time.Hour)
	f.register(t, "fresh@example.com", registrantPassword)
	f.clock.advance(2 * time.Hour)
	// Now t0+49h: expired@ is past its 48 hours, fresh@ has 46 left. The
	// allowances of expired@ and confirmed@ (last email at t0) left their
	// 24-hour window; fresh@'s (t0+47h) did not.
	var wg sync.WaitGroup
	results := make([][2]int64, 2)
	for i, replica := range []*selfService{f, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			registrations, allowances, err := replica.registrations.Cleanup(d.ctx)
			if err != nil {
				t.Error(err)
			}
			results[i] = [2]int64{registrations, allowances}
		}()
	}
	wg.Wait()
	if results[0][0]+results[1][0] != 1 || results[0][1]+results[1][1] != 2 {
		t.Fatalf("cleanup counts %v", results)
	}
	if d.pendingRows(t, "expired@example.com") != 0 || d.pendingRows(t, "fresh@example.com") != 1 || d.usersWithEmail(t, "confirmed@example.com") != 1 ||
		d.number(t, "SELECT count(*) FROM customer_email_throttle WHERE email='fresh@example.com'") != 1 {
		t.Fatal("cleanup removed the wrong rows")
	}
	// t0+72h: fresh@'s last email is now a day old; its registration is not.
	f.clock.advance(23 * time.Hour)
	if registrations, allowances, err := f.registrations.Cleanup(d.ctx); err != nil || registrations != 0 || allowances != 1 {
		t.Fatalf("second cleanup: %d %d %v", registrations, allowances, err)
	}
	if d.pendingRows(t, "fresh@example.com") != 1 || d.number(t, "SELECT count(*) FROM users WHERE email='demo@sugar.ru'") != 1 {
		t.Fatal("cleanup removed an unexpired registration or a user")
	}
}

// 20: registration never changes a real account, enabled or disabled.
func TestRegistrationLeavesDisabledAccountsAlone(t *testing.T) {
	d := newDatabase(t, true)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	account := d.operatorCustomer(t, registrant, otherPassword)
	if _, err := d.store.SetCustomerDisabled(d.ctx, registrant, true); err != nil {
		t.Fatal(err)
	}
	hash := d.text(t, "SELECT password_hash FROM users WHERE id=$1", account.ID)
	if ex := f.register(t, registrant, registrantPassword); ex.Code != 202 {
		t.Fatal(ex.Code)
	}
	if d.pendingRows(t, registrant) != 0 || d.text(t, "SELECT password_hash FROM users WHERE id=$1", account.ID) != hash ||
		d.number(t, "SELECT count(*) FROM users WHERE id=$1 AND disabled_at IS NOT NULL", account.ID) != 1 {
		t.Fatal("registration changed a disabled account")
	}
	for _, password := range []string{registrantPassword, otherPassword} {
		if _, err := d.service.StartCookieSession(d.ctx, registrant, password, ""); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("disabled account signed in: %v", err)
		}
	}
}

// Rollback: the A6 binary (migrations up to 009, no registration code)
// accepts the A7 schema, ignores pending registrations, and rolling forward
// confirms a link issued before the rollback.
func TestPreviousBinaryToleratesRegistrationSchema(t *testing.T) {
	d := productionDatabase(t)
	f := d.selfService(t, nil, nil, nil, bcrypt.MinCost)
	f.register(t, registrant, registrantPassword)
	token := f.lastToken(t, registrant)
	previous := productionMigrator(migrationDir(t, []string{"001_", "002_", "003_", "004_", "005_", "006_", "007_", "008_", "009_"}, nil), &lockedBuffer{})
	if err := previous.Verify(d.ctx, d.pool); err != nil {
		t.Fatalf("A6 schema check on the A7 schema: %v", err)
	}
	if err := previous.Migrate(d.ctx, d.pool); err != nil {
		t.Fatalf("A6 migrate on the A7 schema: %v", err)
	}
	plain := httpapi.New(application.New(d.store, application.Options{}), slog.New(slog.NewTextHandler(&lockedBuffer{}, nil)), "", "postgres", contract.SessionConfig())
	if ex := serve(plain, "POST", "/api/auth/registration/confirm", `{"token":"`+token+`","password":"`+registrantPassword+`"}`, shopOrigin); ex.Code != 404 {
		t.Fatalf("an image without self-service has the route: %d", ex.Code)
	}
	if ex := serve(plain, "POST", "/api/auth/session", `{"email":"`+registrant+`","password":"`+registrantPassword+`"}`, shopOrigin); ex.Code != 401 {
		t.Fatalf("a pending registration signs in on the old path: %d", ex.Code)
	}
	if ex := f.confirm(t, token, registrantPassword); ex.Code != 200 {
		t.Fatalf("roll forward: %d", ex.Code)
	}
}
