package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sweetshop/internal/domain"
	"sweetshop/internal/email"
)

const (
	// PasswordResetLifetime is how long a reset token works.
	PasswordResetLifetime = time.Hour
	// passwordResetDigestPrefix separates reset digests from registration
	// digests, session token hashes and CSRF derivations.
	passwordResetDigestPrefix = "sweetshop/password-reset/v1\x00"

	passwordResetPath = "/reset-password"
)

// PasswordResetStore is the recovery port; only PostgreSQL implements it.
// Each method is one transaction, and both lock the customer row before any
// other customer state, as every other account change does.
type PasswordResetStore interface {
	// RequestPasswordReset serializes on the address and, for an enabled
	// customer whose allowance permits one more email, creates or replaces
	// its single reset token. Unknown and disabled addresses change nothing
	// and claim nothing.
	RequestPasswordReset(ctx context.Context, email, tokenHash string, expiresAt time.Time, allowance domain.EmailAllowance, now time.Time) (domain.PasswordResetOutcome, error)
	// PasswordReset returns the unexpired reset holding tokenHash, or
	// domain.ErrPasswordResetLinkInvalid.
	PasswordReset(ctx context.Context, tokenHash string, now time.Time) (domain.PasswordReset, error)
	// ResetPassword locks the customer, consumes the reset if it still holds
	// tokenHash and has not expired, requires an enabled account, stores
	// passwordHash and deletes every session of the customer; all or
	// nothing. It returns the number of revoked sessions, or
	// domain.ErrPasswordResetLinkInvalid with nothing changed (except that a
	// disabled account's reset is still consumed).
	ResetPassword(ctx context.Context, userID, tokenHash, passwordHash string, now time.Time) (int64, error)
	// DeleteExpiredPasswordResets removes up to limit expired resets.
	DeleteExpiredPasswordResets(ctx context.Context, now time.Time, limit int) (int64, error)
}

// PasswordResets is self-service password recovery. A request emails a
// one-hour, single-use link to an enabled customer; the reset replaces the
// password and revokes every session. It never signs anyone in.
type PasswordResets struct {
	store     PasswordResetStore
	outbox    Outbox
	origin    email.PublicOrigin
	logger    *slog.Logger
	now       func() time.Time
	cost      int
	allowance domain.EmailAllowance
}

// NewPasswordResets shares the registration options (test seams).
func NewPasswordResets(store PasswordResetStore, outbox Outbox, origin email.PublicOrigin, logger *slog.Logger, options RegistrationOptions) (*PasswordResets, error) {
	if store == nil || outbox == nil || logger == nil {
		return nil, errors.New("password recovery needs a store, an outbox and a logger")
	}
	if origin.String() == "" {
		return nil, errors.New("password recovery needs PUBLIC_SHOP_ORIGIN")
	}
	p := &PasswordResets{store: store, outbox: outbox, origin: origin, logger: logger, now: time.Now, cost: CustomerPasswordCost, allowance: AccountEmailAllowance}
	if options.PasswordCost != 0 {
		if options.PasswordCost < bcrypt.MinCost || options.PasswordCost > bcrypt.MaxCost {
			return nil, errors.New("invalid bcrypt cost")
		}
		p.cost = options.PasswordCost
	}
	if options.Now != nil {
		p.now = options.Now
	}
	return p, nil
}

// PasswordResetReceipt reports a request internally; clients always get the
// same answer.
type PasswordResetReceipt struct {
	Outcome  domain.PasswordResetOutcome
	Delivery Delivery
}

// Request asks for a reset email. Every syntactically valid address takes
// the same path: one token is minted and the store decides; no branch hashes
// a password, so no branch is a bcrypt operation cheaper than another.
func (p *PasswordResets) Request(ctx context.Context, rawEmail string) (PasswordResetReceipt, error) {
	address, ok := NormalizeEmail(rawEmail)
	if !ok {
		return PasswordResetReceipt{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "email", Message: "Укажите корректный email"}}}
	}
	token, digest, err := newPasswordResetToken()
	if err != nil {
		return PasswordResetReceipt{}, err
	}
	now := p.now().UTC().Truncate(time.Microsecond)
	outcome, err := p.store.RequestPasswordReset(ctx, address, digest, now.Add(PasswordResetLifetime), p.allowance, now)
	if err != nil {
		return PasswordResetReceipt{}, err
	}
	receipt := PasswordResetReceipt{Outcome: outcome, Delivery: DeliveryNone}
	if outcome != domain.PasswordResetIssued {
		return receipt, nil
	}
	ref := newMessageRef()
	link, err := p.origin.Link(passwordResetPath, "t="+token)
	var message email.Message
	if err == nil {
		message, err = email.PasswordReset(address, ref, link, PasswordResetLifetime)
	}
	if err == nil {
		err = p.outbox.Enqueue(message)
	}
	if err != nil {
		// The token stands and the password is unchanged; the customer asks
		// again once the allowance permits.
		p.logger.Error("password reset email not queued; the customer can request it again", "ref", ref, "error", err)
		receipt.Delivery = DeliveryNotQueued
		return receipt, nil
	}
	receipt.Delivery = DeliveryQueued
	return receipt, nil
}

// Reset replaces the password of the customer holding token and revokes all
// of its sessions. It does not sign in. Order of checks: the token first, so
// a 422 for the password is only ever given to a usable link, and nothing
// about the account is revealed to anyone without one.
func (p *PasswordResets) Reset(ctx context.Context, token, password string) (int64, error) {
	digest, ok := passwordResetTokenDigest(token)
	if !ok {
		return 0, domain.ErrPasswordResetLinkInvalid
	}
	now := p.now().UTC().Truncate(time.Microsecond)
	reset, err := p.store.PasswordReset(ctx, digest, now)
	if err != nil {
		return 0, err
	}
	if message := ValidateCustomerPassword(password); message != "" {
		return 0, &domain.ValidationError{Fields: []domain.FieldError{{Field: "password", Message: message}}}
	}
	// Hashed before the transaction, so no row is locked during bcrypt.
	hash, err := hashPassword(ctx, password, p.cost)
	if err != nil {
		return 0, fmt.Errorf("hash password: %w", err)
	}
	revoked, err := p.store.ResetPassword(ctx, reset.UserID, digest, hash, now)
	if err != nil {
		return 0, err
	}
	p.logger.Info("customer password reset", "userId", reset.UserID, "revokedSessions", revoked)
	return revoked, nil
}

// Cleanup deletes expired resets in bounded batches, like registrations.
func (p *PasswordResets) Cleanup(ctx context.Context) (int64, error) {
	now := p.now().UTC()
	var total int64
	for range cleanupMaxBatches {
		n, err := p.store.DeleteExpiredPasswordResets(ctx, now, cleanupBatch)
		total += n
		if err != nil {
			return total, err
		}
		if n < cleanupBatch {
			break
		}
	}
	return total, nil
}

func newPasswordResetToken() (token, digest string, err error) {
	raw := make([]byte, registrationTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("create password reset token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	digest, _ = passwordResetTokenDigest(token)
	return token, digest, nil
}

// passwordResetTokenDigest accepts the same token shape as registration but
// digests it in its own namespace, so neither token works for the other.
func passwordResetTokenDigest(token string) (string, bool) {
	if !validEmailToken(token) {
		return "", false
	}
	sum := sha256.Sum256([]byte(passwordResetDigestPrefix + token))
	return base64.RawURLEncoding.EncodeToString(sum[:]), true
}
