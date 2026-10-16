package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"sweetshop/internal/domain"
)

// usersEmailIndex is the case-insensitive unique email index from 001_init.
const usersEmailIndex = "users_email_lower_uidx"

const customerColumns = `id,email,name,phone,created_at,disabled_at`

func scanCustomer(row pgx.Row) (domain.CustomerAccount, error) {
	var account domain.CustomerAccount
	var disabled *time.Time
	if err := row.Scan(&account.ID, &account.Email, &account.Name, &account.Phone, &account.CreatedAt, &disabled); err != nil {
		return domain.CustomerAccount{}, err
	}
	account.CreatedAt = account.CreatedAt.UTC()
	if disabled != nil {
		utc := disabled.UTC()
		account.DisabledAt = &utc
	}
	return account, nil
}

func (s *Store) CreateCustomer(ctx context.Context, customer domain.NewCustomer) (domain.CustomerAccount, error) {
	account, err := scanCustomer(s.pool.QueryRow(ctx, `
		INSERT INTO users(id,name,email,phone,password_hash) VALUES ($1,$2,$3,$4,$5)
		RETURNING `+customerColumns, customer.ID, customer.Name, customer.Email, customer.Phone, customer.PasswordHash))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == usersEmailIndex {
		return domain.CustomerAccount{}, domain.ErrEmailTaken
	}
	if err != nil {
		return domain.CustomerAccount{}, fmt.Errorf("create customer: %w", err)
	}
	return account, nil
}

// SetCustomerPassword replaces the hash and deletes every session of the
// account in one transaction. The UPDATE takes the user row lock that Cart
// writers take first, so no new lock order arises; session rows follow.
func (s *Store) SetCustomerPassword(ctx context.Context, email, passwordHash string) (domain.AccountChange, error) {
	return s.changeCustomer(ctx, `UPDATE users SET password_hash=$2 WHERE lower(email)=lower($1) RETURNING `+customerColumns, email, passwordHash)
}

// SetCustomerDisabled disables (keeping an earlier disable time) or enables
// the account. Both revoke every session: none exists legitimately while an
// account is disabled, and one written meanwhile (for example by an earlier
// binary after a rollback) must not come back to life on enable.
func (s *Store) SetCustomerDisabled(ctx context.Context, email string, disabled bool) (domain.AccountChange, error) {
	if disabled {
		return s.changeCustomer(ctx, `UPDATE users SET disabled_at=COALESCE(disabled_at, now()) WHERE lower(email)=lower($1) RETURNING `+customerColumns, email)
	}
	return s.changeCustomer(ctx, `UPDATE users SET disabled_at=NULL WHERE lower(email)=lower($1) RETURNING `+customerColumns, email)
}

func (s *Store) changeCustomer(ctx context.Context, update string, args ...any) (domain.AccountChange, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AccountChange{}, err
	}
	defer tx.Rollback(ctx)
	account, err := scanCustomer(tx.QueryRow(ctx, update, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AccountChange{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AccountChange{}, fmt.Errorf("update customer: %w", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, account.ID)
	if err != nil {
		return domain.AccountChange{}, fmt.Errorf("revoke customer sessions: %w", err)
	}
	change := domain.AccountChange{Account: account, RevokedSessions: tag.RowsAffected()}
	if err := tx.Commit(ctx); err != nil {
		return domain.AccountChange{}, fmt.Errorf("commit customer change: %w", err)
	}
	return change, nil
}
