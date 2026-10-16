package domain

import (
	"errors"
	"time"
)

var (
	// ErrRegistrationLinkInvalid: the confirmation token is unknown, used,
	// replaced or expired, or the address already has an account. Clients get
	// one answer for all of these.
	ErrRegistrationLinkInvalid = errors.New("registration link is invalid or expired")
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
