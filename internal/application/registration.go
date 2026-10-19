package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sweetshop/internal/domain"
	"sweetshop/internal/email"
)

const (
	// RegistrationLifetime is how long a confirmation token works.
	RegistrationLifetime = 48 * time.Hour
	// registrationTokenBytes of randomness, sent as base64url (43 characters).
	registrationTokenBytes = 32
	// registrationDigestPrefix separates registration token digests from
	// session token hashes and CSRF derivations.
	registrationDigestPrefix = "sweetshop/registration-confirmation/v1\x00"

	registrationConfirmPath = "/register/confirm"
	signInPath              = "/login"
	forgotPasswordPath      = "/forgot-password"
)

// AccountEmailAllowance is the per-address bound on account email: one
// message a minute and ten a day, across every replica.
var AccountEmailAllowance = domain.EmailAllowance{Cooldown: time.Minute, Window: 24 * time.Hour, Limit: 10}

var registrationTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// RegistrationStore is the self-registration port; only PostgreSQL
// implements it. Every method is one transaction, and the methods that send
// email serialize on the address, so concurrent requests for one address
// apply one after another on every replica.
type RegistrationStore interface {
	// StartRegistration claims one message from the address's allowance and,
	// unless a customer owns the address, creates or replaces its pending
	// registration. Throttled changes nothing; an existing account is never
	// touched.
	StartRegistration(ctx context.Context, pending domain.PendingRegistration, allowance domain.EmailAllowance, now time.Time) (domain.RegistrationOutcome, error)
	// ReissueRegistration gives an unexpired pending registration a new token
	// (the old one stops working), or reports an existing account; either
	// claims one message from the allowance. Any other address is
	// RegistrationNone and claims nothing.
	ReissueRegistration(ctx context.Context, email, tokenHash string, expiresAt time.Time, allowance domain.EmailAllowance, now time.Time) (domain.RegistrationOutcome, error)
	// PendingRegistration returns the unexpired registration holding
	// tokenHash, or domain.ErrRegistrationLinkInvalid.
	PendingRegistration(ctx context.Context, tokenHash string, now time.Time) (domain.PendingRegistration, error)
	// ConfirmRegistration deletes the registration if it still holds
	// tokenHash and passwordHash and has not expired, and inserts the
	// customer with userID from it. A registration that is gone or changed is
	// domain.ErrRegistrationLinkInvalid; so is an address a customer already
	// owns, in which case the registration is still consumed.
	ConfirmRegistration(ctx context.Context, tokenHash, passwordHash, userID string, now time.Time) (domain.UserCredential, error)
	// DeleteExpiredRegistrations removes up to limit expired registrations.
	DeleteExpiredRegistrations(ctx context.Context, now time.Time, limit int) (int64, error)
	// DeleteIdleEmailAllowances removes up to limit allowance rows whose last
	// message is at or before idleSince.
	DeleteIdleEmailAllowances(ctx context.Context, idleSince time.Time, limit int) (int64, error)
}

// Outbox accepts a message for delivery without blocking. *email.Dispatcher
// implements it; delivery is in memory and not durable.
type Outbox interface {
	Enqueue(email.Message) error
}

// Delivery is what happened to the email of a registration or resend. It is
// for the server and tests only; clients always get the same answer.
type Delivery string

const (
	DeliveryQueued Delivery = "queued"
	// DeliveryNotQueued: the outbox refused the message (full or shut down).
	// The state change stands; the customer recovers with a resend.
	DeliveryNotQueued Delivery = "not_queued"
	// DeliveryNone: nothing to send (throttled, or no such registration).
	DeliveryNone Delivery = "none"
)

// RegistrationReceipt reports a registration or resend internally.
type RegistrationReceipt struct {
	Outcome  domain.RegistrationOutcome
	Delivery Delivery
}

// CustomerRegistrations is customer self-registration with mandatory email
// confirmation. A registration stores a pending row, never a customer; only
// Confirm, given the emailed token and the chosen password, creates the
// customer and signs it in through Service's session path.
type CustomerRegistrations struct {
	store     RegistrationStore
	sessions  *Service
	outbox    Outbox
	origin    email.PublicOrigin
	logger    *slog.Logger
	now       func() time.Time
	cost      int
	allowance domain.EmailAllowance
}

// RegistrationOptions are test seams; the zero value is the production
// policy (CustomerPasswordCost and the wall clock), and the server never sets
// them.
type RegistrationOptions struct {
	// PasswordCost overrides the bcrypt cost of new registrations, so tests
	// under the race detector stay fast.
	PasswordCost int
	Now          func() time.Time
}

// NewCustomerRegistrations requires a working outbox and the public origin
// that emailed links use.
func NewCustomerRegistrations(store RegistrationStore, sessions *Service, outbox Outbox, origin email.PublicOrigin, logger *slog.Logger, options RegistrationOptions) (*CustomerRegistrations, error) {
	if store == nil || sessions == nil || outbox == nil || logger == nil {
		return nil, errors.New("self-registration needs a store, sessions, an outbox and a logger")
	}
	if origin.String() == "" {
		return nil, errors.New("self-registration needs PUBLIC_SHOP_ORIGIN")
	}
	r := &CustomerRegistrations{store: store, sessions: sessions, outbox: outbox, origin: origin, logger: logger,
		now: time.Now, cost: CustomerPasswordCost, allowance: AccountEmailAllowance}
	if options.PasswordCost != 0 {
		if options.PasswordCost < bcrypt.MinCost || options.PasswordCost > bcrypt.MaxCost {
			return nil, errors.New("invalid bcrypt cost")
		}
		r.cost = options.PasswordCost
	}
	if options.Now != nil {
		r.now = options.Now
	}
	return r, nil
}

// Register validates the input with the operator rules and answers the same
// way for a new address, a pending one and an existing account. Every valid
// request hashes the password first (under the bcrypt guard), then the store
// decides; only the store's transaction differs between branches.
func (r *CustomerRegistrations) Register(ctx context.Context, input NewCustomerInput) (RegistrationReceipt, error) {
	customer, err := validateNewCustomer(input)
	if err != nil {
		return RegistrationReceipt{}, err
	}
	hash, err := hashPassword(ctx, input.Password, r.cost)
	if err != nil {
		return RegistrationReceipt{}, fmt.Errorf("hash password: %w", err)
	}
	token, digest, err := newRegistrationToken()
	if err != nil {
		return RegistrationReceipt{}, err
	}
	now := r.now().UTC().Truncate(time.Microsecond)
	outcome, err := r.store.StartRegistration(ctx, domain.PendingRegistration{
		Email: customer.Email, Name: customer.Name, Phone: customer.Phone, PasswordHash: hash,
		TokenHash: digest, CreatedAt: now, ExpiresAt: now.Add(RegistrationLifetime),
	}, r.allowance, now)
	if err != nil {
		return RegistrationReceipt{}, err
	}
	return r.deliver(outcome, customer.Email, token), nil
}

// Resend mints a new confirmation token for a pending registration (the old
// link stops working) or repeats the "account exists" notice, within the
// allowance. It never creates a registration: that needs the password.
func (r *CustomerRegistrations) Resend(ctx context.Context, rawEmail string) (RegistrationReceipt, error) {
	address, ok := NormalizeEmail(rawEmail)
	if !ok {
		return RegistrationReceipt{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "email", Message: "Укажите корректный email"}}}
	}
	token, digest, err := newRegistrationToken()
	if err != nil {
		return RegistrationReceipt{}, err
	}
	now := r.now().UTC().Truncate(time.Microsecond)
	outcome, err := r.store.ReissueRegistration(ctx, address, digest, now.Add(RegistrationLifetime), r.allowance, now)
	if err != nil {
		return RegistrationReceipt{}, err
	}
	return r.deliver(outcome, address, token), nil
}

// Confirm creates the customer from the pending registration of token when
// password is the one chosen at registration, then starts a cookie session
// for it exactly as a login would. previousToken is the raw session cookie
// the browser presented, replaced atomically like on login.
//
// An unknown, replaced, used or expired token, and an address that already
// has an account, are all ErrRegistrationLinkInvalid; a wrong password is
// ErrUnauthorized and leaves the registration usable.
func (r *CustomerRegistrations) Confirm(ctx context.Context, token, password, previousToken string) (LoginResult, error) {
	if password == "" {
		return LoginResult{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "password", Message: "Укажите пароль"}}}
	}
	digest, ok := registrationTokenDigest(token)
	if !ok {
		return LoginResult{}, domain.ErrRegistrationLinkInvalid
	}
	now := r.now().UTC().Truncate(time.Microsecond)
	pending, err := r.store.PendingRegistration(ctx, digest, now)
	if err != nil {
		return LoginResult{}, err
	}
	// Outside any transaction: a quarter second of bcrypt must not hold row
	// locks. ConfirmRegistration rechecks that the token and hash are unchanged.
	matched, err := passwordMatches(ctx, pending.PasswordHash, password)
	if err != nil {
		return LoginResult{}, err
	}
	if !matched {
		return LoginResult{}, domain.ErrUnauthorized
	}
	id, err := newCustomerID()
	if err != nil {
		return LoginResult{}, err
	}
	credential, err := r.store.ConfirmRegistration(ctx, digest, pending.PasswordHash, id, now)
	if err != nil {
		return LoginResult{}, err
	}
	r.logger.Info("customer self-registration confirmed", "userId", credential.User.ID)
	return r.sessions.startSession(ctx, credential, domain.SessionCookie, previousToken)
}

// deliver enqueues the email an outcome calls for. A refused message is
// logged without recipient, token or body and does not undo the state
// change: the customer can ask for a resend.
func (r *CustomerRegistrations) deliver(outcome domain.RegistrationOutcome, address, token string) RegistrationReceipt {
	receipt := RegistrationReceipt{Outcome: outcome, Delivery: DeliveryNone}
	var (
		message email.Message
		err     error
	)
	ref := newMessageRef()
	switch outcome {
	case domain.RegistrationPending:
		var link email.Link
		if link, err = r.origin.Link(registrationConfirmPath, "t="+token); err == nil {
			message, err = email.RegistrationConfirmation(address, ref, link, RegistrationLifetime)
		}
	case domain.RegistrationAccountExists:
		var signIn, recovery email.Link
		if signIn, err = r.origin.Link(signInPath, ""); err == nil {
			if recovery, err = r.origin.Link(forgotPasswordPath, ""); err == nil {
				message, err = email.AccountExists(address, ref, signIn, recovery)
			}
		}
	default:
		return receipt
	}
	if err == nil {
		err = r.outbox.Enqueue(message)
	}
	if err != nil {
		// The dispatcher has already logged a refusal; this records which
		// account flow lost its message.
		r.logger.Error("account email not queued; the customer can request it again", "outcome", outcome, "ref", ref, "error", err)
		receipt.Delivery = DeliveryNotQueued
		return receipt
	}
	receipt.Delivery = DeliveryQueued
	return receipt
}

const (
	cleanupBatch      = 500
	cleanupMaxBatches = 20
)

// Cleanup deletes expired registrations and idle allowance rows in bounded
// batches. Replicas may run it at the same time: each batch skips rows
// another transaction holds, and deleting a row twice is impossible.
func (r *CustomerRegistrations) Cleanup(ctx context.Context) (registrations, allowances int64, err error) {
	now := r.now().UTC()
	for range cleanupMaxBatches {
		n, err := r.store.DeleteExpiredRegistrations(ctx, now, cleanupBatch)
		registrations += n
		if err != nil {
			return registrations, allowances, err
		}
		if n < cleanupBatch {
			break
		}
	}
	// A row whose last message left the window counts nothing any more, so
	// dropping it changes no future decision.
	idleSince := now.Add(-r.allowance.Window)
	for range cleanupMaxBatches {
		n, err := r.store.DeleteIdleEmailAllowances(ctx, idleSince, cleanupBatch)
		allowances += n
		if err != nil {
			return registrations, allowances, err
		}
		if n < cleanupBatch {
			break
		}
	}
	return registrations, allowances, nil
}

// RunSelfServiceCleanup runs both cleanups now and then every interval
// until ctx is done. Failures are logged and retried on the next tick.
func RunSelfServiceCleanup(ctx context.Context, interval time.Duration, logger *slog.Logger, registrations *CustomerRegistrations, resets *PasswordResets) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		expired, allowances, err := registrations.Cleanup(ctx)
		resetCount, resetErr := resets.Cleanup(ctx)
		switch err = errors.Join(err, resetErr); {
		case err != nil && ctx.Err() == nil:
			logger.Warn("self-service cleanup failed", "error", err)
		case expired+allowances+resetCount > 0:
			logger.Info("self-service cleanup", "expiredRegistrations", expired, "idleEmailAllowances", allowances, "expiredPasswordResets", resetCount)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// newRegistrationToken returns a raw token for the email and its digest for
// the database.
func newRegistrationToken() (token, digest string, err error) {
	raw := make([]byte, registrationTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("create registration token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	digest, _ = registrationTokenDigest(token)
	return token, digest, nil
}

// registrationTokenDigest accepts only the exact token shape: 43 base64url
// characters that decode to 32 bytes.
func registrationTokenDigest(token string) (string, bool) {
	if !validEmailToken(token) {
		return "", false
	}
	sum := sha256.Sum256([]byte(registrationDigestPrefix + token))
	return base64.RawURLEncoding.EncodeToString(sum[:]), true
}

// validEmailToken is the shape of every emailed token: 43 base64url
// characters that decode to 32 bytes. Purpose comes from the digest prefix.
func validEmailToken(token string) bool {
	if !registrationTokenPattern.MatchString(token) {
		return false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	return err == nil && len(raw) == registrationTokenBytes
}

// newMessageRef correlates the application and dispatcher log lines of one
// message. It is random, so it reveals nothing about the address or token.
func newMessageRef() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "msg"
	}
	return "msg_" + hex.EncodeToString(b)
}
