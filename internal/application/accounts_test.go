package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sweetshop/internal/domain"
	"sweetshop/internal/repository/memory"
)

func TestNormalizeEmail(t *testing.T) {
	for raw, want := range map[string]string{
		" Ann.Lee+shop@Example.COM ": "ann.lee+shop@example.com",
		"o'brien@sub.example.ru":     "o'brien@sub.example.ru",
		"user@xn--80ak6aa92e.com":    "user@xn--80ak6aa92e.com",
		"a@b.co":                     "a@b.co",
	} {
		if got, ok := NormalizeEmail(raw); !ok || got != want {
			t.Errorf("%q: %q %v", raw, got, ok)
		}
	}
	for _, raw := range []string{
		"", "   ", "plain", "@example.com", "user@", "user@localhost", "a@@b.com", "a@b@c.com",
		".user@example.com", "user.@example.com", "us..er@example.com", "user name@example.com",
		"user@-example.com", "user@example-.com", "user@exa_mple.com", "user@example..com",
		"пользователь@example.com", "user@пример.рф", "user\x00@example.com", "\"quoted\"@example.com",
		strings.Repeat("a", 65) + "@example.com", "user@" + strings.Repeat("a", 64) + ".com",
		strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 63),
	} {
		if got, ok := NormalizeEmail(raw); ok {
			t.Errorf("%q accepted as %q", raw, got)
		}
	}
}

func TestValidateCustomerPassword(t *testing.T) {
	for _, ok := range []string{"correct horse", "0123456789", "пароль-длинный", strings.Repeat("a", 72), strings.Repeat("я", 36)} {
		if message := ValidateCustomerPassword(ok); message != "" {
			t.Errorf("%q rejected: %s", ok, message)
		}
	}
	for _, bad := range []string{"", "short", "123456789", strings.Repeat("a", 73), strings.Repeat("я", 37), " leading-space1", "trailing-space1 ", "tab\tinside1", "line\nbreak12", "nul\x00inside1", "\xff\xfeinvalid-utf8"} {
		if message := ValidateCustomerPassword(bad); message == "" {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestValidateNameAndPhone(t *testing.T) {
	if validateName("") == "" || validateName(strings.Repeat("я", 101)) == "" || validateName("a\nb") == "" || validateName("Анна Ли") != "" {
		t.Fatal("name rules")
	}
	for _, ok := range []string{"", "+7 999 123-45-67", "(495) 123 45 67"} {
		if message := validatePhone(ok); message != "" {
			t.Errorf("%q rejected: %s", ok, message)
		}
	}
	for _, bad := range []string{"call me", "+7 999 123-45-67 ext 5", "--", strings.Repeat("1", 33), "+7 ٩٩٩"} {
		if validatePhone(bad) == "" {
			t.Errorf("%q accepted", bad)
		}
	}
}

type fakeAccountStore struct {
	created  []domain.NewCustomer
	password struct{ email, hash string }
	disabled map[string]bool
	err      error
}

func (f *fakeAccountStore) CreateCustomer(_ context.Context, customer domain.NewCustomer) (domain.CustomerAccount, error) {
	f.created = append(f.created, customer)
	return domain.CustomerAccount{ID: customer.ID, Email: customer.Email, Name: customer.Name, Phone: customer.Phone}, f.err
}

func (f *fakeAccountStore) SetCustomerPassword(_ context.Context, email, hash string) (domain.AccountChange, error) {
	f.password.email, f.password.hash = email, hash
	return domain.AccountChange{Account: domain.CustomerAccount{Email: email}, RevokedSessions: 2}, f.err
}

func (f *fakeAccountStore) SetCustomerDisabled(_ context.Context, email string, disabled bool) (domain.AccountChange, error) {
	if f.disabled == nil {
		f.disabled = map[string]bool{}
	}
	f.disabled[email] = disabled
	return domain.AccountChange{Account: domain.CustomerAccount{Email: email}}, f.err
}

func TestCreateCustomerValidatesBeforeTheStore(t *testing.T) {
	store := &fakeAccountStore{}
	accounts := NewCustomerAccounts(store)
	_, err := accounts.Create(context.Background(), NewCustomerInput{Email: "not an email", Name: " ", Phone: "call me", Password: "short"})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || len(validation.Fields) != 4 || len(store.created) != 0 {
		t.Fatalf("err=%v store=%v", err, store.created)
	}
	for _, field := range validation.Fields {
		if strings.Contains(field.Message, "short") {
			t.Fatal("validation message echoes the password")
		}
	}
}

func TestCreateCustomerHashesAtCustomerCost(t *testing.T) {
	store := &fakeAccountStore{}
	account, err := NewCustomerAccounts(store).Create(context.Background(), NewCustomerInput{Email: " Ann@Example.com ", Name: "  Анна  ", Phone: " +7 999 000-00-00 ", Password: "correct horse"})
	if err != nil || len(store.created) != 1 {
		t.Fatal(err)
	}
	created := store.created[0]
	if created.Email != "ann@example.com" || created.Name != "Анна" || created.Phone != "+7 999 000-00-00" || account.Email != "ann@example.com" {
		t.Fatalf("normalized: %+v", created)
	}
	if !strings.HasPrefix(created.ID, "usr_") || len(created.ID) != 36 {
		t.Fatalf("id %q", created.ID)
	}
	if cost, err := bcrypt.Cost([]byte(created.PasswordHash)); err != nil || cost != CustomerPasswordCost {
		t.Fatalf("cost=%d err=%v", cost, err)
	}
	if bcrypt.CompareHashAndPassword([]byte(created.PasswordHash), []byte("correct horse")) != nil {
		t.Fatal("hash does not verify")
	}
}

func TestAccountChangesNormalizeAndValidate(t *testing.T) {
	store := &fakeAccountStore{}
	accounts := NewCustomerAccounts(store)
	accounts.cost = bcrypt.MinCost
	change, err := accounts.SetPassword(context.Background(), " ANN@example.com", "new password 1")
	if err != nil || store.password.email != "ann@example.com" || change.RevokedSessions != 2 || bcrypt.CompareHashAndPassword([]byte(store.password.hash), []byte("new password 1")) != nil {
		t.Fatalf("set password: %v %+v", err, store.password)
	}
	if _, err := accounts.SetPassword(context.Background(), "bad", "short"); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("set password validation")
	}
	if _, err := accounts.Disable(context.Background(), "Ann@Example.com"); err != nil || !store.disabled["ann@example.com"] {
		t.Fatal("disable")
	}
	if _, err := accounts.Enable(context.Background(), "ann@example.com"); err != nil || store.disabled["ann@example.com"] {
		t.Fatal("enable")
	}
	if _, err := accounts.Disable(context.Background(), "nope"); !errors.Is(err, domain.ErrValidation) {
		t.Fatal("disable validation")
	}
}

// recordingRepo reports the demo credential as disabled on demand and records
// the sessions login asks for.
type recordingRepo struct {
	*memory.Store
	disabled bool
	sessions []domain.NewSession
}

func (r *recordingRepo) FindUserByEmail(ctx context.Context, email string) (domain.UserCredential, error) {
	credential, err := r.Store.FindUserByEmail(ctx, email)
	credential.Disabled = r.disabled
	return credential, err
}

func (r *recordingRepo) CreateSession(ctx context.Context, session domain.NewSession, now time.Time) error {
	r.sessions = append(r.sessions, session)
	return r.Store.CreateSession(ctx, session, now)
}

func TestLoginRefusesDisabledAccountsAfterThePasswordCheck(t *testing.T) {
	repo := &recordingRepo{Store: memory.NewDemo(), disabled: true}
	service := New(repo, Options{AllowDemoAccount: true})
	if _, err := service.Login(context.Background(), "demo@sugar.ru", "wrong password"); !errors.Is(err, domain.ErrUnauthorized) || errors.Is(err, domain.ErrAccountDisabled) {
		t.Fatalf("wrong password on a disabled account must look ordinary: %v", err)
	}
	if _, err := service.StartCookieSession(context.Background(), "demo@sugar.ru", "Sweet2026!", ""); !errors.Is(err, domain.ErrAccountDisabled) || !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("disabled: %v", err)
	}
	if len(repo.sessions) != 0 {
		t.Fatal("session created for a disabled account")
	}
	repo.disabled = false
	if _, err := service.StartCookieSession(context.Background(), "demo@sugar.ru", "Sweet2026!", ""); err != nil {
		t.Fatal(err)
	}
	if len(repo.sessions) != 1 || repo.sessions[0].PasswordHash == "" {
		t.Fatalf("login must bind the session to the verified credential: %+v", repo.sessions)
	}
}
