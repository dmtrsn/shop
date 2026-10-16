package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"sweetshop/internal/domain"
)

// CustomerPasswordCost is the bcrypt cost of provisioned customer passwords.
// dummyPasswordHash uses the same cost.
const CustomerPasswordCost = 12

const (
	MinCustomerPasswordRunes = 10
	// MaxPasswordBytes is bcrypt's input limit. Longer passwords are rejected,
	// never truncated.
	MaxPasswordBytes = 72
	maxEmailBytes    = 254
	maxEmailLocal    = 64
	maxNameRunes     = 100
	maxPhoneRunes    = 32
)

// CustomerAccountStore is the operator account port; only PostgreSQL
// implements it. Each method is one transaction.
type CustomerAccountStore interface {
	// CreateCustomer returns domain.ErrEmailTaken for a used email.
	CreateCustomer(context.Context, domain.NewCustomer) (domain.CustomerAccount, error)
	// SetCustomerPassword replaces the hash and revokes every session of the
	// account. A missing account is domain.ErrNotFound.
	SetCustomerPassword(ctx context.Context, email, passwordHash string) (domain.AccountChange, error)
	// SetCustomerDisabled disables or enables the account; both revoke every
	// session. Repeating either keeps the current state; a repeated disable
	// keeps the original time.
	SetCustomerDisabled(ctx context.Context, email string, disabled bool) (domain.AccountChange, error)
}

// CustomerAccounts provisions and maintains customer accounts for operators.
type CustomerAccounts struct {
	store CustomerAccountStore
	cost  int
}

func NewCustomerAccounts(store CustomerAccountStore) *CustomerAccounts {
	return &CustomerAccounts{store: store, cost: CustomerPasswordCost}
}

type NewCustomerInput struct {
	Email    string
	Name     string
	Phone    string
	Password string
}

// Create validates the input, hashes the password and inserts the account.
func (a *CustomerAccounts) Create(ctx context.Context, input NewCustomerInput) (domain.CustomerAccount, error) {
	fields := []domain.FieldError{}
	email, ok := NormalizeEmail(input.Email)
	if !ok {
		fields = append(fields, domain.FieldError{Field: "email", Message: "Укажите корректный email в ASCII (для международных доменов — punycode)"})
	}
	name := strings.TrimSpace(input.Name)
	if message := validateName(name); message != "" {
		fields = append(fields, domain.FieldError{Field: "name", Message: message})
	}
	phone := strings.TrimSpace(input.Phone)
	if message := validatePhone(phone); message != "" {
		fields = append(fields, domain.FieldError{Field: "phone", Message: message})
	}
	if message := ValidateCustomerPassword(input.Password); message != "" {
		fields = append(fields, domain.FieldError{Field: "password", Message: message})
	}
	if len(fields) > 0 {
		return domain.CustomerAccount{}, &domain.ValidationError{Fields: fields}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), a.cost)
	if err != nil {
		return domain.CustomerAccount{}, fmt.Errorf("hash password: %w", err)
	}
	id, err := newCustomerID()
	if err != nil {
		return domain.CustomerAccount{}, err
	}
	return a.store.CreateCustomer(ctx, domain.NewCustomer{ID: id, Email: email, Name: name, Phone: phone, PasswordHash: string(hash)})
}

// SetPassword replaces the password and revokes every session.
func (a *CustomerAccounts) SetPassword(ctx context.Context, email, password string) (domain.AccountChange, error) {
	normalized, fields := lookupEmail(email)
	if message := ValidateCustomerPassword(password); message != "" {
		fields = append(fields, domain.FieldError{Field: "password", Message: message})
	}
	if len(fields) > 0 {
		return domain.AccountChange{}, &domain.ValidationError{Fields: fields}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), a.cost)
	if err != nil {
		return domain.AccountChange{}, fmt.Errorf("hash password: %w", err)
	}
	return a.store.SetCustomerPassword(ctx, normalized, string(hash))
}

// Disable refuses future sign-ins and revokes every session.
func (a *CustomerAccounts) Disable(ctx context.Context, email string) (domain.AccountChange, error) {
	return a.setDisabled(ctx, email, true)
}

// Enable allows sign-in again. It revokes any session, so nothing written
// while the account was disabled survives.
func (a *CustomerAccounts) Enable(ctx context.Context, email string) (domain.AccountChange, error) {
	return a.setDisabled(ctx, email, false)
}

func (a *CustomerAccounts) setDisabled(ctx context.Context, email string, disabled bool) (domain.AccountChange, error) {
	normalized, fields := lookupEmail(email)
	if len(fields) > 0 {
		return domain.AccountChange{}, &domain.ValidationError{Fields: fields}
	}
	return a.store.SetCustomerDisabled(ctx, normalized, disabled)
}

func lookupEmail(raw string) (string, []domain.FieldError) {
	email, ok := NormalizeEmail(raw)
	if !ok {
		return "", []domain.FieldError{{Field: "email", Message: "Укажите корректный email"}}
	}
	return email, []domain.FieldError{}
}

// NormalizeEmail trims and lowercases an ASCII address and checks its shape.
// Only ASCII is accepted, so Go's and PostgreSQL's lowercasing agree with the
// users_email_lower_uidx index and with login's normalization.
func NormalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > maxEmailBytes {
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

// ValidateCustomerPassword returns a message for an unacceptable password:
// at least MinCustomerPasswordRunes characters, at most MaxPasswordBytes
// bytes of UTF-8, no control characters and no surrounding whitespace.
func ValidateCustomerPassword(password string) string {
	switch {
	case !utf8.ValidString(password):
		return "Пароль должен быть в кодировке UTF-8"
	case len(password) > MaxPasswordBytes:
		return fmt.Sprintf("Пароль длиннее %d байт", MaxPasswordBytes)
	case utf8.RuneCountInString(password) < MinCustomerPasswordRunes:
		return fmt.Sprintf("Пароль должен быть не короче %d символов", MinCustomerPasswordRunes)
	case strings.TrimSpace(password) != password:
		return "Пароль не должен начинаться или заканчиваться пробелом"
	case strings.IndexFunc(password, unicode.IsControl) >= 0:
		return "Пароль не должен содержать управляющих символов"
	}
	return ""
}

func validateName(name string) string {
	switch {
	case name == "":
		return "Укажите имя"
	case !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxNameRunes:
		return fmt.Sprintf("Имя должно быть не длиннее %d символов", maxNameRunes)
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return "Имя не должно содержать управляющих символов"
	}
	return ""
}

// validatePhone accepts an optional number written with digits, spaces, +,
// parentheses and hyphens.
func validatePhone(phone string) string {
	if phone == "" {
		return ""
	}
	if len(phone) > maxPhoneRunes || strings.IndexFunc(phone, unicode.IsDigit) < 0 {
		return "Укажите телефон цифрами, не длиннее 32 символов"
	}
	for _, r := range phone {
		if !(r >= '0' && r <= '9' || r == ' ' || r == '+' || r == '(' || r == ')' || r == '-') {
			return "Телефон может содержать только цифры, пробелы, +, скобки и дефисы"
		}
	}
	return ""
}

// newCustomerID is "usr_" and 128 random bits, distinct from fixture IDs.
func newCustomerID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("create customer id: %w", err)
	}
	return "usr_" + hex.EncodeToString(b), nil
}
