package application

import (
	"bytes"
	"context"
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

type fakeResets struct {
	mu        sync.Mutex
	outcome   domain.PasswordResetOutcome
	requested []string
	resets    map[string]domain.PasswordReset
	consumed  []string
	hashes    []string
	deletes   []int64
}

func (f *fakeResets) RequestPasswordReset(_ context.Context, address, tokenHash string, _ time.Time, _ domain.EmailAllowance, _ time.Time) (domain.PasswordResetOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requested = append(f.requested, address+" "+tokenHash)
	return f.outcome, nil
}

func (f *fakeResets) PasswordReset(_ context.Context, tokenHash string, _ time.Time) (domain.PasswordReset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if reset, ok := f.resets[tokenHash]; ok {
		return reset, nil
	}
	return domain.PasswordReset{}, domain.ErrPasswordResetLinkInvalid
}

func (f *fakeResets) ResetPassword(_ context.Context, userID, tokenHash, passwordHash string, _ time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reset, ok := f.resets[tokenHash]
	if !ok || reset.UserID != userID {
		return 0, domain.ErrPasswordResetLinkInvalid
	}
	delete(f.resets, tokenHash)
	f.consumed = append(f.consumed, tokenHash)
	f.hashes = append(f.hashes, passwordHash)
	return 3, nil
}

func (f *fakeResets) DeleteExpiredPasswordResets(context.Context, time.Time, int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deletes) == 0 {
		return 0, nil
	}
	n := f.deletes[0]
	f.deletes = f.deletes[1:]
	return n, nil
}

type resetFixture struct {
	resets *PasswordResets
	store  *fakeResets
	outbox *emailtest.Outbox
	logs   *bytes.Buffer
}

func newResetFixture(t *testing.T, cost int) resetFixture {
	t.Helper()
	origin, err := email.ParsePublicOrigin("https://shop.example", true)
	if err != nil {
		t.Fatal(err)
	}
	f := resetFixture{store: &fakeResets{resets: map[string]domain.PasswordReset{}}, outbox: &emailtest.Outbox{}, logs: &bytes.Buffer{}}
	f.resets, err = NewPasswordResets(f.store, f.outbox, origin, slog.New(slog.NewTextHandler(f.logs, nil)), RegistrationOptions{PasswordCost: cost})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Each purpose has its own digest namespace: no token can stand in for
// another, and none equals a session hash.
func TestEmailedTokenPurposesNeverOverlap(t *testing.T) {
	token, digest, err := newPasswordResetToken()
	if err != nil {
		t.Fatal(err)
	}
	registration, _ := registrationTokenDigest(token)
	if again, ok := passwordResetTokenDigest(token); !ok || again != digest || len(token) != 43 {
		t.Fatal("reset digest is not stable")
	}
	if digest == registration || digest == tokenHash(token) || registration == tokenHash(token) || digest == token {
		t.Fatal("token purposes share a digest")
	}
	for _, bad := range []string{"", token[:42], token + "A", token[:42] + "B"} {
		if _, ok := passwordResetTokenDigest(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestPasswordResetRequestLooksAlikeAndEmailsOnlyWhenIssued(t *testing.T) {
	for _, tc := range []struct {
		outcome  domain.PasswordResetOutcome
		delivery Delivery
	}{
		{domain.PasswordResetIssued, DeliveryQueued}, {domain.PasswordResetNone, DeliveryNone},
		{domain.PasswordResetDisabled, DeliveryNone}, {domain.PasswordResetThrottled, DeliveryNone},
	} {
		t.Run(string(tc.outcome), func(t *testing.T) {
			f := newResetFixture(t, bcrypt.MinCost)
			f.store.outcome = tc.outcome
			receipt, err := f.resets.Request(context.Background(), " Ann@Example.COM ")
			if err != nil || receipt.Outcome != tc.outcome || receipt.Delivery != tc.delivery {
				t.Fatalf("receipt %+v %v", receipt, err)
			}
			address, digest, _ := strings.Cut(f.store.requested[0], " ")
			if address != "ann@example.com" || len(f.store.requested) != 1 {
				t.Fatal("not normalized or not asked once")
			}
			messages := f.outbox.Messages()
			if tc.outcome != domain.PasswordResetIssued {
				if len(messages) != 0 {
					t.Fatal("email without an issued token")
				}
				return
			}
			if len(messages) != 1 || messages[0].Kind != email.KindPasswordReset || messages[0].To != "ann@example.com" {
				t.Fatalf("messages %v", messages)
			}
			token := emailtest.Token(messages[0])
			if !strings.Contains(messages[0].Body, "https://shop.example/reset-password#t="+token) || !strings.Contains(messages[0].Body, "1 час") {
				t.Fatal("link or expiry")
			}
			if want, _ := passwordResetTokenDigest(token); want != digest {
				t.Fatal("stored value is not the reset digest of the emailed token")
			}
		})
	}
	var validation *domain.ValidationError
	f := newResetFixture(t, bcrypt.MinCost)
	if _, err := f.resets.Request(context.Background(), "nope"); !errors.As(err, &validation) || len(f.store.requested) != 0 {
		t.Fatalf("malformed address: %v", err)
	}
}

func TestPasswordResetRequestSurvivesARefusedEmail(t *testing.T) {
	f := newResetFixture(t, bcrypt.MinCost)
	f.store.outcome = domain.PasswordResetIssued
	f.outbox.Refuse = email.ErrQueueFull
	receipt, err := f.resets.Request(context.Background(), "ann@example.com")
	if err != nil || receipt.Delivery != DeliveryNotQueued {
		t.Fatalf("%+v %v", receipt, err)
	}
	logs := f.logs.String()
	_, digest, _ := strings.Cut(f.store.requested[0], " ")
	if !strings.Contains(logs, "password reset email not queued") || strings.Contains(logs, "ann@example.com") || strings.Contains(logs, digest) || strings.Contains(logs, "reset-password") {
		t.Fatalf("logs: %s", logs)
	}
}

// The token is checked first: a password 422 goes only to a usable link,
// and nothing reaches the store or bcrypt for an unusable one.
func TestPasswordResetChecksTheTokenBeforeThePassword(t *testing.T) {
	ctx := context.Background()
	f := newResetFixture(t, bcrypt.MinCost)
	token, digest, _ := newPasswordResetToken()
	f.store.resets[digest] = domain.PasswordReset{UserID: "usr_1", TokenHash: digest}
	other, _, _ := newPasswordResetToken()
	for _, bad := range []string{"", "short", other} {
		if _, err := f.resets.Reset(ctx, bad, "short"); !errors.Is(err, domain.ErrLinkInvalid) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	// A registration token never works here.
	registration, registrationDigest, _ := newRegistrationToken()
	f.store.resets[registrationDigest] = domain.PasswordReset{UserID: "usr_2", TokenHash: registrationDigest}
	if _, err := f.resets.Reset(ctx, registration, "new password 1234"); !errors.Is(err, domain.ErrLinkInvalid) {
		t.Fatalf("registration token accepted: %v", err)
	}
	var validation *domain.ValidationError
	if _, err := f.resets.Reset(ctx, token, "short"); !errors.As(err, &validation) || validation.Fields[0].Field != "password" {
		t.Fatalf("policy: %v", err)
	}
	if len(f.store.consumed) != 0 {
		t.Fatal("a failed reset consumed the token")
	}
	revoked, err := f.resets.Reset(ctx, token, "new password 1234")
	if err != nil || revoked != 3 || len(f.store.consumed) != 1 {
		t.Fatalf("reset: %d %v", revoked, err)
	}
	if bcrypt.CompareHashAndPassword([]byte(f.store.hashes[0]), []byte("new password 1234")) != nil {
		t.Fatal("new password not hashed")
	}
	if strings.Contains(f.logs.String(), token) || strings.Contains(f.logs.String(), "new password") {
		t.Fatal("logs leak the token or password")
	}
}

func TestPasswordResetHashesAtCustomerCostUnderTheGuard(t *testing.T) {
	f := newResetFixture(t, 0)
	if f.resets.cost != CustomerPasswordCost || f.resets.allowance != AccountEmailAllowance || f.resets.now == nil {
		t.Fatal("production defaults")
	}
	saved := passwords
	t.Cleanup(func() { passwords = saved })
	passwords = newPasswordGuard(1, 0, 10*time.Millisecond)
	passwords.slots <- struct{}{}
	token, digest, _ := newPasswordResetToken()
	f.store.resets[digest] = domain.PasswordReset{UserID: "usr_1", TokenHash: digest}
	if _, err := f.resets.Reset(context.Background(), token, "new password 1234"); !errors.Is(err, domain.ErrBusy) || len(f.store.consumed) != 0 {
		t.Fatalf("saturated guard: %v", err)
	}
}

func TestPasswordResetCleanupAndConstruction(t *testing.T) {
	f := newResetFixture(t, bcrypt.MinCost)
	f.store.deletes = []int64{cleanupBatch, 4}
	if n, err := f.resets.Cleanup(context.Background()); err != nil || n != cleanupBatch+4 {
		t.Fatalf("cleanup %d %v", n, err)
	}
	origin, _ := email.ParsePublicOrigin("https://shop.example", true)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	if _, err := NewPasswordResets(f.store, nil, origin, logger, RegistrationOptions{}); err == nil {
		t.Fatal("accepted no outbox")
	}
	if _, err := NewPasswordResets(f.store, f.outbox, email.PublicOrigin{}, logger, RegistrationOptions{}); err == nil {
		t.Fatal("accepted no origin")
	}
	if _, err := NewPasswordResets(f.store, f.outbox, origin, logger, RegistrationOptions{PasswordCost: 99}); err == nil {
		t.Fatal("accepted an invalid cost")
	}
}
