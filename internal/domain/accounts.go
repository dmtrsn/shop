package domain

import (
	"fmt"
	"strings"
	"time"
)

var (
	// ErrEmailTaken: another account already uses the normalized email.
	ErrEmailTaken = fmt.Errorf("email is already registered: %w", ErrConflict)
	// ErrAccountDisabled is an ErrUnauthorized, so clients see the generic
	// failure; only server logs distinguish it.
	ErrAccountDisabled = fmt.Errorf("account is disabled: %w", ErrUnauthorized)
)

// CustomerAccount is the operator view of a customer account. It never
// carries credential material.
type CustomerAccount struct {
	ID         string     `json:"id"`
	Email      string     `json:"email"`
	Name       string     `json:"name"`
	Phone      string     `json:"phone,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	DisabledAt *time.Time `json:"disabledAt,omitempty"`
}

// NewCustomer is an already validated account with its password hash.
type NewCustomer struct {
	ID           string
	Email        string
	Name         string
	Phone        string
	PasswordHash string
}

// AccountChange is the result of an operator change to an account.
type AccountChange struct {
	Account         CustomerAccount `json:"account"`
	RevokedSessions int64           `json:"revokedSessions"`
}

const (
	// MaxEmailBytes bounds a normalized address (RFC 5321 path limit).
	MaxEmailBytes = 254
	maxEmailLocal = 64
)

// NormalizeEmail trims and lowercases an ASCII address and checks its shape.
// Only ASCII is accepted, so Go's and PostgreSQL's lowercasing agree with the
// users_email_lower_uidx index and with login's normalization.
func NormalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > MaxEmailBytes {
		return "", false
	}
	local, host, ok := strings.Cut(email, "@")
	if !ok || local == "" || len(local) > maxEmailLocal || strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return "", false
	}
	for i := 0; i < len(local); i++ {
		c := local[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte(".!#$%&'*+/=?^_`{|}~-", c) >= 0) {
			return "", false
		}
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "", false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", false
			}
		}
	}
	return email, true
}
