package domain

import (
	"fmt"
	"time"
)

// Staff are a principal separate from customers: their own accounts,
// sessions, cookie and CSRF namespace. No staff type converts to User.

// ErrStaffDisabled is an ErrUnauthorized, so clients see the generic failure;
// only server logs distinguish it.
var ErrStaffDisabled = fmt.Errorf("staff account is disabled: %w", ErrUnauthorized)

// StaffUser is the authenticated staff principal. It carries no credential.
type StaffUser struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	CreatedAt   time.Time `json:"createdAt"`
}

type StaffCredential struct {
	Staff        StaffUser
	PasswordHash string
	Disabled     bool
}

// NewStaffSession is inserted only if PasswordHash is still the account's
// password and the account is enabled, so a login cannot outrun a reset or
// disable. ReplacesTokenHash is the presented previous staff session.
type NewStaffSession struct {
	StaffID           string
	TokenHash         string
	ExpiresAt         time.Time
	ReplacesTokenHash string
	PasswordHash      string
}

type StaffSessionRecord struct {
	Staff     StaffUser
	ExpiresAt time.Time
}

// StaffAccount is the operator view of a staff account.
type StaffAccount struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"displayName"`
	CreatedAt   time.Time  `json:"createdAt"`
	DisabledAt  *time.Time `json:"disabledAt,omitempty"`
}

type NewStaff struct {
	ID           string
	Email        string
	DisplayName  string
	PasswordHash string
}

type StaffChange struct {
	Account         StaffAccount `json:"account"`
	RevokedSessions int64        `json:"revokedSessions"`
}
