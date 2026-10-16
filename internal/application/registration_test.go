package application

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sweetshop/internal/domain"
	"sweetshop/internal/email"
	"sweetshop/internal/email/emailtest"
)

const registrationPassword = "registration secret 1"

// fakeRegistrations answers with a fixed outcome and records what it got.
type fakeRegistrations struct {
	mu        sync.Mutex
	outcome   domain.RegistrationOutcome
	started   []domain.PendingRegistration
	reissued  []string
	pending   map[string]domain.PendingRegistration
	confirmed []string
	deletes   []int64
	failAt    int
}

func (f *fakeRegistrations) StartRegistration(_ context.Context, pending domain.PendingRegistration, _ domain.EmailAllowance, _ time.Time) (domain.RegistrationOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, pending)
	return f.outcome, nil
}

func (f *fakeRegistrations) ReissueRegistration(_ context.Context, address, tokenHash string, _ time.Time, _ domain.EmailAllowance, _ time.Time) (domain.RegistrationOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reissued = append(f.reissued, address+" "+tokenHash)
	return f.outcome, nil
}

func (f *fakeRegistrations) PendingRegistration(_ context.Context, tokenHash string, _ time.Time) (domain.PendingRegistration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if pending, ok := f.pending[tokenHash]; ok {
		return pending, nil
	}
	return domain.PendingRegistration{}, domain.ErrRegistrationLinkInvalid
}

func (f *fakeRegistrations) ConfirmRegistration(_ context.Context, tokenHash, passwordHash, userID string, _ time.Time) (domain.UserCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.confirmed = append(f.confirmed, tokenHash)
	pending, ok := f.pending[tokenHash]
	if !ok || pending.PasswordHash != passwordHash {
		return domain.UserCredential{}, domain.ErrRegistrationLinkInvalid
	}
	delete(f.pending, tokenHash)
	return domain.UserCredential{User: domain.User{ID: userID, Name: pending.Name, Email: pending.Email}, PasswordHash: pending.PasswordHash}, nil
}

// deleteBatch returns the next scripted batch size, or an error at failAt.
func (f *fakeRegistrations) deleteBatch() (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAt > 0 {
		f.failAt--
		if f.failAt == 0 {
			return 0, errors.New("storage down")
		}
	}
	if len(f.deletes) == 0 {
		return 0, nil
	}
	n := f.deletes[0]
	f.deletes = f.deletes[1:]
	return n, nil
}

func (f *fakeRegistrations) DeleteExpiredRegistrations(context.Context, time.Time, int) (int64, error) {
	return f.deleteBatch()
}

func (f *fakeRegistrations) DeleteIdleEmailAllowances(context.Context, time.Time, int) (int64, error) {
	return f.deleteBatch()
}

// sessionRecorder is the session repository: only CreateSession is used.
type sessionRecorder struct {
	domain.Repository
	mu       sync.Mutex
	sessions []domain.NewSession
}

func (r *sessionRecorder) CreateSession(_ context.Context, session domain.NewSession, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions = append(r.sessions, session)
	return nil
}

type registrationFixture struct {
	registrations *CustomerRegistrations
	store         *fakeRegistrations
	sessions      *sessionRecorder
	outbox        *emailtest.Outbox
	logs          *bytes.Buffer
}

func newRegistrationFixture(t *testing.T) registrationFixture {
	t.Helper()
	origin, err := email.ParsePublicOrigin("https://shop.example", true)
	if err != nil {
		t.Fatal(err)
	}
	f := registrationFixture{store: &fakeRegistrations{pending: map[string]domain.PendingRegistration{}}, sessions: &sessionRecorder{}, outbox: &emailtest.Outbox{}, logs: &bytes.Buffer{}}
	logger := slog.New(slog.NewTextHandler(f.logs, nil))
	// The minimum cost keeps unit tests fast; the cost-12 policy is asserted
	// in TestRegistrationHashesAtCustomerCost and against PostgreSQL.
	f.registrations, err = NewCustomerRegistrations(f.store, New(f.sessions, Options{}), f.outbox, origin, logger, RegistrationOptions{PasswordCost: bcrypt.MinCost})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func validRegistration() NewCustomerInput {
	return NewCustomerInput{Email: " Ann@Example.COM ", Name: " Анна ", Phone: "+7 900 000-00-00", Password: registrationPassword}
}

func TestRegistrationTokenShapeAndDigest(t *testing.T) {
	token, digest, err := newRegistrationToken()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 || len(token) != 43 {
		t.Fatalf("token %d chars, %d bytes: %v", len(token), len(raw), err)
	}
	if again, ok := registrationTokenDigest(token); !ok || again != digest || len(digest) != 43 {
		t.Fatal("digest is not a stable 43-character value")
	}
	// Domain separation: neither the raw token nor its session-style hash.
	if digest == token || digest == tokenHash(token) {
		t.Fatal("registration digest shares a namespace")
	}
	other, _, _ := newRegistrationToken()
	if other == token {
		t.Fatal("tokens repeat")
	}
	// 32 bytes leave the last base64url character's two low bits zero, so a
	// final "B" is never canonical.
	for _, bad := range []string{"", token[:42], token + "A", token[:42] + "=", token[:42] + "B", strings.Repeat("!", 43), " " + token[1:]} {
		if _, ok := registrationTokenDigest(bad); ok {
			t.Errorf("accepted malformed token %q", bad)
		}
	}
}

func TestRegisterValidatesLikeOperatorProvisioning(t *testing.T) {
	f := newRegistrationFixture(t)
	_, err := f.registrations.Register(context.Background(), NewCustomerInput{Email: "not-an-email", Name: "", Phone: "abc", Password: "short"})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error %v", err)
	}
	fields := map[string]bool{}
	for _, field := range validation.Fields {
		fields[field.Field] = true
	}
	for _, name := range []string{"email", "name", "phone", "password"} {
		if !fields[name] {
			t.Errorf("no %s error", name)
		}
	}
	_, operatorErr := validateNewCustomer(NewCustomerInput{Email: "not-an-email", Name: "", Phone: "abc", Password: "short"})
	if operatorErr.Error() != err.Error() || len(f.store.started) != 0 || len(f.outbox.Messages()) != 0 {
		t.Fatal("registration validation differs from provisioning or reached the store")
	}
}

// Every outcome returns success to the caller; only the email differs.
func TestRegisterBranchesLookAlikeToTheCaller(t *testing.T) {
	for _, tc := range []struct {
		outcome  domain.RegistrationOutcome
		kind     email.Kind
		delivery Delivery
	}{
		{domain.RegistrationPending, email.KindRegistrationConfirmation, DeliveryQueued},
		{domain.RegistrationAccountExists, email.KindAccountExists, DeliveryQueued},
		{domain.RegistrationThrottled, "", DeliveryNone},
	} {
		t.Run(string(tc.outcome), func(t *testing.T) {
			f := newRegistrationFixture(t)
			f.store.outcome = tc.outcome
			receipt, err := f.registrations.Register(context.Background(), validRegistration())
			if err != nil || receipt.Outcome != tc.outcome || receipt.Delivery != tc.delivery {
				t.Fatalf("receipt %+v err %v", receipt, err)
			}
			// The password is hashed in every branch, before the store decides.
			if len(f.store.started) != 1 {
				t.Fatal("store not consulted exactly once")
			}
			started := f.store.started[0]
			if started.Email != "ann@example.com" || started.Name != "Анна" || started.Phone != "+7 900 000-00-00" {
				t.Fatalf("normalized input %+v", started)
			}
			if bcrypt.CompareHashAndPassword([]byte(started.PasswordHash), []byte(registrationPassword)) != nil || strings.Contains(started.PasswordHash, registrationPassword) {
				t.Fatal("password not hashed")
			}
			if !started.ExpiresAt.Equal(started.CreatedAt.Add(48 * time.Hour)) {
				t.Fatal("lifetime is not 48 hours")
			}
			messages := f.outbox.Messages()
			if tc.kind == "" {
				if len(messages) != 0 {
					t.Fatal("throttled registration sent email")
				}
				return
			}
			if len(messages) != 1 || messages[0].Kind != tc.kind || messages[0].To != "ann@example.com" {
				t.Fatalf("messages %v", messages)
			}
			body := messages[0].Body
			if strings.Contains(body, registrationPassword) || strings.Contains(body, started.PasswordHash) || strings.Contains(body, "Анна") {
				t.Fatal("email quotes the password, its hash or the typed name")
			}
			token := emailtest.Token(messages[0])
			switch tc.outcome {
			case domain.RegistrationPending:
				if !strings.Contains(body, "https://shop.example/register/confirm#t="+token) {
					t.Fatal("confirmation link is not origin + fixed path + fragment")
				}
				if digest, ok := registrationTokenDigest(token); !ok || digest != started.TokenHash || started.TokenHash == token {
					t.Fatal("stored value is not the digest of the emailed token")
				}
			case domain.RegistrationAccountExists:
				if token != "" || !strings.Contains(body, "https://shop.example/login") {
					t.Fatal("account notice carries a token or lacks the sign-in link")
				}
			}
		})
	}
}

// A refused message keeps the registration and leaks nothing into logs.
func TestRegisterSurvivesARefusedEmail(t *testing.T) {
	f := newRegistrationFixture(t)
	f.store.outcome = domain.RegistrationPending
	f.outbox.Refuse = email.ErrQueueFull
	receipt, err := f.registrations.Register(context.Background(), validRegistration())
	if err != nil || receipt.Delivery != DeliveryNotQueued || len(f.store.started) != 1 || f.outbox.Refused() != 1 {
		t.Fatalf("receipt %+v err %v", receipt, err)
	}
	logs := f.logs.String()
	if !strings.Contains(logs, "account email not queued") || !strings.Contains(logs, "ref=msg_") {
		t.Fatalf("no operational log: %s", logs)
	}
	for _, secret := range []string{"ann@example.com", registrationPassword, f.store.started[0].PasswordHash, f.store.started[0].TokenHash, "confirm"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("log contains %q", secret)
		}
	}
}

func TestResend(t *testing.T) {
	f := newRegistrationFixture(t)
	var validation *domain.ValidationError
	if _, err := f.registrations.Resend(context.Background(), "nobody"); !errors.As(err, &validation) || len(f.store.reissued) != 0 {
		t.Fatalf("malformed address: %v", err)
	}
	for _, tc := range []struct {
		outcome domain.RegistrationOutcome
		sent    int
	}{
		{domain.RegistrationNone, 0}, {domain.RegistrationThrottled, 0},
		{domain.RegistrationAccountExists, 1}, {domain.RegistrationPending, 1},
	} {
		f := newRegistrationFixture(t)
		f.store.outcome = tc.outcome
		receipt, err := f.registrations.Resend(context.Background(), " Ann@Example.com")
		if err != nil || receipt.Outcome != tc.outcome || len(f.outbox.Messages()) != tc.sent {
			t.Fatalf("%s: %+v %v", tc.outcome, receipt, err)
		}
		address, digest, _ := strings.Cut(f.store.reissued[0], " ")
		if address != "ann@example.com" {
			t.Fatal("resend did not normalize the address")
		}
		if tc.outcome == domain.RegistrationPending {
			token := emailtest.Token(f.outbox.Messages()[0])
			if want, _ := registrationTokenDigest(token); want != digest {
				t.Fatal("reissued digest does not match the emailed token")
			}
		}
	}
}

func pendingFor(t *testing.T, f registrationFixture) (token string, pending domain.PendingRegistration) {
	t.Helper()
	token, digest, err := newRegistrationToken()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(registrationPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	pending = domain.PendingRegistration{Email: "ann@example.com", Name: "Анна", PasswordHash: string(hash), TokenHash: digest}
	f.store.pending[digest] = pending
	return token, pending
}

func TestConfirm(t *testing.T) {
	ctx := context.Background()
	f := newRegistrationFixture(t)
	token, pending := pendingFor(t, f)

	if _, err := f.registrations.Confirm(ctx, token, "", ""); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("empty password: %v", err)
	}
	for _, bad := range []string{"", "short", token + "x", strings.Repeat("A", 42) + "="} {
		if _, err := f.registrations.Confirm(ctx, bad, registrationPassword, ""); !errors.Is(err, domain.ErrRegistrationLinkInvalid) {
			t.Fatalf("token %q: %v", bad, err)
		}
	}
	if _, err := f.registrations.Confirm(ctx, token, "wrong password 1", ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("wrong password: %v", err)
	}
	if len(f.store.confirmed) != 0 || len(f.sessions.sessions) != 0 {
		t.Fatal("a failed confirmation reached the account or session step")
	}
	result, err := f.registrations.Confirm(ctx, token, registrationPassword, "previous-cookie-token")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.User.ID, "usr_") || result.User.Email != "ann@example.com" || result.Token == "" || result.Token == token {
		t.Fatalf("result %+v", result)
	}
	if len(f.sessions.sessions) != 1 {
		t.Fatal("no session")
	}
	session := f.sessions.sessions[0]
	if session.Transport != domain.SessionCookie || session.UserID != result.User.ID || session.PasswordHash != pending.PasswordHash ||
		session.TokenHash != tokenHash(result.Token) || session.ReplacesTokenHash != tokenHash("previous-cookie-token") {
		t.Fatalf("session %+v is not a normal cookie login", session)
	}
	if _, err := f.registrations.Confirm(ctx, token, registrationPassword, ""); !errors.Is(err, domain.ErrRegistrationLinkInvalid) {
		t.Fatalf("replay: %v", err)
	}
}

func TestCleanupIsBatchedAndStopsOnErrors(t *testing.T) {
	f := newRegistrationFixture(t)
	f.store.deletes = []int64{cleanupBatch, cleanupBatch, 3, cleanupBatch, 7}
	registrations, allowances, err := f.registrations.Cleanup(context.Background())
	if err != nil || registrations != 2*cleanupBatch+3 || allowances != cleanupBatch+7 {
		t.Fatalf("cleanup %d %d %v", registrations, allowances, err)
	}
	f.store.deletes = nil
	for range 2*cleanupMaxBatches + 2 {
		f.store.deletes = append(f.store.deletes, cleanupBatch)
	}
	registrations, allowances, _ = f.registrations.Cleanup(context.Background())
	if registrations != cleanupMaxBatches*cleanupBatch || allowances != cleanupMaxBatches*cleanupBatch {
		t.Fatalf("one run is not bounded: %d %d", registrations, allowances)
	}
	f.store.deletes, f.store.failAt = []int64{cleanupBatch}, 2
	if _, _, err := f.registrations.Cleanup(context.Background()); err == nil {
		t.Fatal("storage error swallowed")
	}
}

func TestRegistrationHashesAtCustomerCost(t *testing.T) {
	f := newRegistrationFixture(t)
	defaults, err := NewCustomerRegistrations(f.store, f.registrations.sessions, f.outbox, f.registrations.origin, f.registrations.logger, RegistrationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f.registrations = defaults
	f.store.outcome = domain.RegistrationPending
	if _, err := f.registrations.Register(context.Background(), validRegistration()); err != nil {
		t.Fatal(err)
	}
	if cost, err := bcrypt.Cost([]byte(f.store.started[0].PasswordHash)); err != nil || cost != CustomerPasswordCost {
		t.Fatalf("cost %d %v", cost, err)
	}
}

func TestNewCustomerRegistrationsNeedsEverything(t *testing.T) {
	origin, _ := email.ParsePublicOrigin("https://shop.example", true)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	service := New(&sessionRecorder{}, Options{})
	if _, err := NewCustomerRegistrations(&fakeRegistrations{}, service, nil, origin, logger, RegistrationOptions{}); err == nil {
		t.Fatal("accepted no outbox")
	}
	if _, err := NewCustomerRegistrations(&fakeRegistrations{}, service, &emailtest.Outbox{}, email.PublicOrigin{}, logger, RegistrationOptions{}); err == nil {
		t.Fatal("accepted no public origin")
	}
	if _, err := NewCustomerRegistrations(&fakeRegistrations{}, service, &emailtest.Outbox{}, origin, logger, RegistrationOptions{PasswordCost: 2}); err == nil {
		t.Fatal("accepted an invalid cost")
	}
	r, err := NewCustomerRegistrations(&fakeRegistrations{}, service, &emailtest.Outbox{}, origin, logger, RegistrationOptions{})
	if err != nil || r.cost != CustomerPasswordCost || r.allowance != AccountEmailAllowance {
		t.Fatalf("defaults: %v", err)
	}
}
