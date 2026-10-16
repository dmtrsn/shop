package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sweetshop/internal/domain"
)

const (
	// StaffPasswordCost equals CustomerPasswordCost, so dummyPasswordHash
	// equalizes unknown-staff timing too.
	StaffPasswordCost     = CustomerPasswordCost
	MinStaffPasswordRunes = 12
	// StaffSessionLifetime is absolute; A4 has no idle timeout.
	StaffSessionLifetime = 12 * time.Hour
)

// StaffStore is the staff port; only PostgreSQL implements it. It shares no
// table, session or method with customer identity.
type StaffStore interface {
	FindStaffByEmail(ctx context.Context, email string) (domain.StaffCredential, error)
	// CreateStaffSession honors domain.NewStaffSession's guard and returns
	// ErrUnauthorized when it fails, changing nothing.
	CreateStaffSession(ctx context.Context, session domain.NewStaffSession, now time.Time) error
	// StaffSessionByTokenHash returns ErrUnauthorized unless an unexpired
	// session of an enabled account has the hash.
	StaffSessionByTokenHash(ctx context.Context, hash string, now time.Time) (domain.StaffSessionRecord, error)
	// DeleteStaffSession is idempotent.
	DeleteStaffSession(ctx context.Context, hash string) error

	// CreateStaff returns domain.ErrEmailTaken for a used email.
	CreateStaff(ctx context.Context, staff domain.NewStaff) (domain.StaffAccount, error)
	// SetStaffPassword and SetStaffDisabled revoke every session of the
	// account in the same transaction; a missing account is ErrNotFound.
	SetStaffPassword(ctx context.Context, email, passwordHash string) (domain.StaffChange, error)
	SetStaffDisabled(ctx context.Context, email string, disabled bool) (domain.StaffChange, error)
}

// ValidateStaffPassword applies the customer rules with a 12-character minimum.
func ValidateStaffPassword(password string) string {
	return validatePassword(password, MinStaffPasswordRunes)
}

// StaffAccounts provisions and maintains staff accounts for operators.
type StaffAccounts struct {
	store StaffStore
	cost  int
}

func NewStaffAccounts(store StaffStore) *StaffAccounts {
	return &StaffAccounts{store: store, cost: StaffPasswordCost}
}

type NewStaffInput struct {
	Email       string
	DisplayName string
	Password    string
}

func (a *StaffAccounts) Create(ctx context.Context, input NewStaffInput) (domain.StaffAccount, error) {
	fields := []domain.FieldError{}
	email, ok := NormalizeEmail(input.Email)
	if !ok {
		fields = append(fields, domain.FieldError{Field: "email", Message: "Укажите корректный email в ASCII (для международных доменов — punycode)"})
	}
	name := strings.TrimSpace(input.DisplayName)
	if message := validateName(name); message != "" {
		fields = append(fields, domain.FieldError{Field: "name", Message: message})
	}
	if message := ValidateStaffPassword(input.Password); message != "" {
		fields = append(fields, domain.FieldError{Field: "password", Message: message})
	}
	if len(fields) > 0 {
		return domain.StaffAccount{}, &domain.ValidationError{Fields: fields}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), a.cost)
	if err != nil {
		return domain.StaffAccount{}, fmt.Errorf("hash password: %w", err)
	}
	id, err := newAccountID("stf_")
	if err != nil {
		return domain.StaffAccount{}, err
	}
	return a.store.CreateStaff(ctx, domain.NewStaff{ID: id, Email: email, DisplayName: name, PasswordHash: string(hash)})
}

// SetPassword replaces the password and revokes every staff session.
func (a *StaffAccounts) SetPassword(ctx context.Context, email, password string) (domain.StaffChange, error) {
	normalized, fields := lookupEmail(email)
	if message := ValidateStaffPassword(password); message != "" {
		fields = append(fields, domain.FieldError{Field: "password", Message: message})
	}
	if len(fields) > 0 {
		return domain.StaffChange{}, &domain.ValidationError{Fields: fields}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), a.cost)
	if err != nil {
		return domain.StaffChange{}, fmt.Errorf("hash password: %w", err)
	}
	return a.store.SetStaffPassword(ctx, normalized, string(hash))
}

// Disable and Enable both revoke every staff session of the account.
func (a *StaffAccounts) Disable(ctx context.Context, email string) (domain.StaffChange, error) {
	return a.setDisabled(ctx, email, true)
}

func (a *StaffAccounts) Enable(ctx context.Context, email string) (domain.StaffChange, error) {
	return a.setDisabled(ctx, email, false)
}

func (a *StaffAccounts) setDisabled(ctx context.Context, email string, disabled bool) (domain.StaffChange, error) {
	normalized, fields := lookupEmail(email)
	if len(fields) > 0 {
		return domain.StaffChange{}, &domain.ValidationError{Fields: fields}
	}
	return a.store.SetStaffDisabled(ctx, normalized, disabled)
}

// StaffAuth signs staff in and resolves staff sessions. It never reads or
// creates customer sessions.
type StaffAuth struct {
	store StaffStore
	now   func() time.Time
}

func NewStaffAuth(store StaffStore) *StaffAuth {
	return &StaffAuth{store: store, now: time.Now}
}

type StaffLoginResult struct {
	Token     string
	ExpiresAt time.Time
	Staff     domain.StaffUser
}

// Login verifies the password and creates a fresh session. When
// previousToken is the raw staff cookie the client presented, that session
// is replaced atomically. Unknown, wrong-password and disabled accounts all
// cost one bcrypt comparison and return ErrUnauthorized.
func (a *StaffAuth) Login(ctx context.Context, email, password, previousToken string) (StaffLoginResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return StaffLoginResult{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "email", Message: "Укажите email"}, {Field: "password", Message: "Укажите пароль"}}}
	}
	credential, err := a.store.FindStaffByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			_ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(password))
			return StaffLoginResult{}, domain.ErrUnauthorized
		}
		return StaffLoginResult{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(credential.PasswordHash), []byte(password)) != nil {
		return StaffLoginResult{}, domain.ErrUnauthorized
	}
	if credential.Disabled {
		return StaffLoginResult{}, domain.ErrStaffDisabled
	}
	token, err := randomToken(32)
	if err != nil {
		return StaffLoginResult{}, fmt.Errorf("create staff session token: %w", err)
	}
	now := a.now().UTC()
	session := domain.NewStaffSession{
		StaffID: credential.Staff.ID, TokenHash: tokenHash(token),
		ExpiresAt: now.Add(StaffSessionLifetime).Truncate(time.Microsecond), PasswordHash: credential.PasswordHash,
	}
	if previousToken != "" {
		session.ReplacesTokenHash = tokenHash(previousToken)
	}
	if err := a.store.CreateStaffSession(ctx, session, now); err != nil {
		return StaffLoginResult{}, err
	}
	return StaffLoginResult{Token: token, ExpiresAt: session.ExpiresAt, Staff: credential.Staff}, nil
}

// Authenticate resolves a raw staff cookie token.
func (a *StaffAuth) Authenticate(ctx context.Context, token string) (domain.StaffSessionRecord, error) {
	if token == "" {
		return domain.StaffSessionRecord{}, domain.ErrUnauthorized
	}
	return a.store.StaffSessionByTokenHash(ctx, tokenHash(token), a.now().UTC())
}

// Logout revokes the session of the raw token; it is idempotent.
func (a *StaffAuth) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return a.store.DeleteStaffSession(ctx, tokenHash(token))
}

// Authorize is the single authorization entrypoint for staff operations.
// In 1.0 every active staff account may perform every staff action; roles or
// permissions would be enforced here without changing authentication.
func (a *StaffAuth) Authorize(staff domain.StaffUser, action string) error {
	if staff.ID == "" || action == "" {
		return domain.ErrForbidden
	}
	return nil
}
