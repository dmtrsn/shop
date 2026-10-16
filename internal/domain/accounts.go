package domain

import (
	"fmt"
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
