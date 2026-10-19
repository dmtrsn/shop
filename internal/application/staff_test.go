package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sweetshop/internal/domain"
)

type fakeStaffStore struct {
	credential *domain.StaffCredential
	sessions   []domain.NewStaffSession
	created    []domain.NewStaff
}

func (f *fakeStaffStore) FindStaffByEmail(_ context.Context, email string) (domain.StaffCredential, error) {
	if f.credential == nil || f.credential.Staff.Email != email {
		return domain.StaffCredential{}, domain.ErrNotFound
	}
	return *f.credential, nil
}

func (f *fakeStaffStore) CreateStaffSession(_ context.Context, session domain.NewStaffSession, _ time.Time) error {
	f.sessions = append(f.sessions, session)
	return nil
}

func (f *fakeStaffStore) StaffSessionByTokenHash(context.Context, string, time.Time) (domain.StaffSessionRecord, error) {
	return domain.StaffSessionRecord{}, domain.ErrUnauthorized
}

func (f *fakeStaffStore) DeleteStaffSession(context.Context, string) error { return nil }

func (f *fakeStaffStore) CreateStaff(_ context.Context, staff domain.NewStaff) (domain.StaffAccount, error) {
	f.created = append(f.created, staff)
	return domain.StaffAccount{ID: staff.ID, Email: staff.Email, DisplayName: staff.DisplayName}, nil
}

func (f *fakeStaffStore) SetStaffPassword(context.Context, string, string) (domain.StaffChange, error) {
	return domain.StaffChange{}, nil
}

func (f *fakeStaffStore) SetStaffDisabled(context.Context, string, bool) (domain.StaffChange, error) {
	return domain.StaffChange{}, nil
}

func TestValidateStaffPasswordNeedsTwelveCharacters(t *testing.T) {
	if ValidateStaffPassword("eleven char") == "" || ValidateStaffPassword("twelve chars") != "" {
		t.Fatal("staff minimum")
	}
	if ValidateStaffPassword(strings.Repeat("a", 73)) == "" || ValidateStaffPassword(" twelve chars") == "" || ValidateStaffPassword("twelve\tchars") == "" {
		t.Fatal("shared rules")
	}
	// Customers keep their own minimum.
	if ValidateCustomerPassword("ten chars!") != "" {
		t.Fatal("customer minimum changed")
	}
}

func TestCreateStaffUsesItsOwnIdentifiersAndCost(t *testing.T) {
	store := &fakeStaffStore{}
	account, err := NewStaffAccounts(store).Create(context.Background(), NewStaffInput{Email: " Ops@Example.com ", DisplayName: " Оператор ", Password: "staff password 1"})
	if err != nil || len(store.created) != 1 {
		t.Fatal(err)
	}
	created := store.created[0]
	if !strings.HasPrefix(created.ID, "stf_") || len(created.ID) != 36 || created.Email != "ops@example.com" || created.DisplayName != "Оператор" || account.ID != created.ID {
		t.Fatalf("created: %+v", created)
	}
	if cost, err := bcrypt.Cost([]byte(created.PasswordHash)); err != nil || cost != StaffPasswordCost || StaffPasswordCost != 12 {
		t.Fatalf("cost=%d err=%v", cost, err)
	}
	if _, err := NewStaffAccounts(store).Create(context.Background(), NewStaffInput{Email: "ops@example.com", DisplayName: "Ops", Password: "short pass"}); !errors.Is(err, domain.ErrValidation) || len(store.created) != 1 {
		t.Fatal("short staff password accepted")
	}
}

func TestStaffLogin(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("staff password 1"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	staff := domain.StaffUser{ID: "stf_" + strings.Repeat("a", 32), Email: "ops@example.com", DisplayName: "Ops"}
	store := &fakeStaffStore{credential: &domain.StaffCredential{Staff: staff, PasswordHash: string(hash)}}
	auth := NewStaffAuth(store)
	ctx := context.Background()
	for _, tc := range []struct{ email, password string }{{"nobody@example.com", "staff password 1"}, {"ops@example.com", "wrong password 1"}} {
		if _, err := auth.Login(ctx, tc.email, tc.password, ""); !errors.Is(err, domain.ErrUnauthorized) || errors.Is(err, domain.ErrStaffDisabled) {
			t.Fatalf("%s: %v", tc.email, err)
		}
	}
	store.credential.Disabled = true
	if _, err := auth.Login(ctx, "ops@example.com", "staff password 1", ""); !errors.Is(err, domain.ErrStaffDisabled) || !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("disabled: %v", err)
	}
	if _, err := auth.Login(ctx, "ops@example.com", "wrong password 1", ""); errors.Is(err, domain.ErrStaffDisabled) {
		t.Fatal("a wrong password must not reveal that the account is disabled")
	}
	if len(store.sessions) != 0 {
		t.Fatal("session for a refused login")
	}
	store.credential.Disabled = false
	before := time.Now()
	result, err := auth.Login(ctx, " OPS@example.com ", "staff password 1", "presented-old-token")
	if err != nil || len(store.sessions) != 1 {
		t.Fatal(err)
	}
	session := store.sessions[0]
	if session.TokenHash == result.Token || session.TokenHash != tokenHash(result.Token) || session.PasswordHash != string(hash) || session.StaffID != staff.ID {
		t.Fatalf("session: %+v", session)
	}
	if session.ReplacesTokenHash != tokenHash("presented-old-token") {
		t.Fatal("presented session not replaced by its digest")
	}
	if lifetime := result.ExpiresAt.Sub(before); lifetime < StaffSessionLifetime-time.Minute || lifetime > StaffSessionLifetime+time.Minute {
		t.Fatalf("lifetime %v", lifetime)
	}
	second, err := auth.Login(ctx, "ops@example.com", "staff password 1", "")
	if err != nil || second.Token == result.Token || len(second.Token) < 43 {
		t.Fatal("tokens must be fresh 256-bit values")
	}
	if _, err := auth.Authenticate(ctx, ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("empty token")
	}
}

func TestAuthorizeIsTheSingleStaffEntrypoint(t *testing.T) {
	auth := NewStaffAuth(&fakeStaffStore{})
	if err := auth.Authorize(domain.StaffUser{ID: "stf_x"}, "orders.update"); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(auth.Authorize(domain.StaffUser{}, "orders.update"), domain.ErrForbidden) || !errors.Is(auth.Authorize(domain.StaffUser{ID: "stf_x"}, ""), domain.ErrForbidden) {
		t.Fatal("anonymous or unnamed action authorized")
	}
}

func TestAuthorizeMutationAuthorizesTheStaffActor(t *testing.T) {
	auth := NewStaffAuth(&fakeStaffStore{})
	staff := domain.StaffUser{ID: "stf_" + strings.Repeat("b", 32)}
	mutation, err := auth.AuthorizeMutation(staff, "product.update", "req-1")
	if err != nil || mutation.Actor() != staff.ID || mutation.Action() != "product.update" || mutation.RequestID() != "req-1" {
		t.Fatalf("%+v %v", mutation, err)
	}
	if _, err := auth.AuthorizeMutation(domain.StaffUser{}, "product.update", ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("anonymous actor authorized")
	}
	if _, err := auth.AuthorizeMutation(staff, "", ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("unnamed action authorized")
	}
	if _, err := auth.AuthorizeMutation(domain.StaffUser{ID: "usr-demo"}, "product.update", ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("customer ID authorized as an actor")
	}
}
