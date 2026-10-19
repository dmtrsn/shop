package domain

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrLinkInvalid is the one answer clients get for any unusable emailed
	// link, whatever its purpose.
	ErrLinkInvalid = errors.New("link is invalid or expired")
	// ErrRegistrationLinkInvalid: the confirmation token is unknown, used,
	// replaced or expired, or the address already has an account.
	ErrRegistrationLinkInvalid = fmt.Errorf("registration link: %w", ErrLinkInvalid)
	// ErrPasswordResetLinkInvalid: the reset token is unknown, used, replaced
	// or expired, or the account is disabled.
	ErrPasswordResetLinkInvalid = fmt.Errorf("password reset link: %w", ErrLinkInvalid)
	// ErrBusy: bounded server work (password hashing) is saturated. Nothing
	// changed and the request may be retried shortly.
	ErrBusy = errors.New("server is busy")
)

// PendingRegistration is an unconfirmed self-registration. It is not an
// account: login ignores it and it owns no cart, favorites, orders or
// sessions. TokenHash is the digest of the emailed token, never the token.
type PendingRegistration struct {
	Email        string
	Name         string
	Phone        string
	PasswordHash string
	TokenHash    string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// RegistrationOutcome is what a registration or resend did. Only the server
// sees it; every client gets the same answer.
type RegistrationOutcome string

const (
	// RegistrationPending: the pending registration was stored or given a new
	// token; its confirmation email is due.
	RegistrationPending RegistrationOutcome = "pending"
	// RegistrationAccountExists: an account owns the address and nothing was
	// stored; the "account exists" notice is due.
	RegistrationAccountExists RegistrationOutcome = "account_exists"
	// RegistrationThrottled: the address's email allowance is spent; nothing
	// changed.
	RegistrationThrottled RegistrationOutcome = "throttled"
	// RegistrationNone: a resend for an address with neither an account nor
	// an unexpired pending registration; nothing changed.
	RegistrationNone RegistrationOutcome = "none"
)

// EmailAllowance bounds account email per normalized address: at most Limit
// messages in any Window, at least Cooldown apart.
type EmailAllowance struct {
	Cooldown time.Duration
	Window   time.Duration
	Limit    int
}

// PasswordResetOutcome is what a reset request did; only the server sees it.
type PasswordResetOutcome string

const (
	// PasswordResetIssued: the customer's reset token was created or
	// replaced; the reset email is due.
	PasswordResetIssued PasswordResetOutcome = "issued"
	// PasswordResetThrottled: the address's email allowance is spent.
	PasswordResetThrottled PasswordResetOutcome = "throttled"
	// PasswordResetDisabled: the account is disabled; nothing was issued.
	PasswordResetDisabled PasswordResetOutcome = "disabled"
	// PasswordResetNone: no account has the address; nothing was issued.
	PasswordResetNone PasswordResetOutcome = "none"
)

// PasswordReset is a customer's active reset token, by digest.
type PasswordReset struct {
	UserID    string
	TokenHash string
	ExpiresAt time.Time
}
