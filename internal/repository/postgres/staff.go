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

// Staff persistence uses only staff_users and staff_sessions; nothing here
// reads or writes customer users or sessions.

const staffEmailIndex = "staff_users_email_lower_uidx"

const staffColumns = `id,email,display_name,created_at,disabled_at`

func scanStaffAccount(row pgx.Row) (domain.StaffAccount, error) {
	var account domain.StaffAccount
	var disabled *time.Time
	if err := row.Scan(&account.ID, &account.Email, &account.DisplayName, &account.CreatedAt, &disabled); err != nil {
		return domain.StaffAccount{}, err
	}
	account.CreatedAt = account.CreatedAt.UTC()
	if disabled != nil {
		utc := disabled.UTC()
		account.DisabledAt = &utc
	}
	return account, nil
}

func (s *Store) FindStaffByEmail(ctx context.Context, email string) (domain.StaffCredential, error) {
	var credential domain.StaffCredential
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, display_name, created_at, password_hash, disabled_at IS NOT NULL
		FROM staff_users WHERE lower(email) = lower($1)`, email).Scan(
		&credential.Staff.ID, &credential.Staff.Email, &credential.Staff.DisplayName, &credential.Staff.CreatedAt,
		&credential.PasswordHash, &credential.Disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StaffCredential{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.StaffCredential{}, fmt.Errorf("find staff: %w", err)
	}
	credential.Staff.CreatedAt = credential.Staff.CreatedAt.UTC()
	return credential, nil
}

// CreateStaffSession is one statement. The staff row is locked FOR SHARE, so
// it conflicts with the row lock of a password reset or disable: a change
// that commits first makes the guard fail, and a change that waits for this
// statement then deletes the new session. A failed guard changes nothing.
func (s *Store) CreateStaffSession(ctx context.Context, session domain.NewStaffSession, now time.Time) error {
	if session.PasswordHash == "" {
		return errors.New("create staff session: the verified credential is required")
	}
	tag, err := s.pool.Exec(ctx, `
		WITH allowed AS (
			SELECT id FROM staff_users
			WHERE id = $1 AND password_hash = $6 AND disabled_at IS NULL
			FOR SHARE),
		removed AS (
			DELETE FROM staff_sessions
			WHERE EXISTS (SELECT 1 FROM allowed)
			  AND ((staff_user_id = $1 AND expires_at <= $5) OR ($4 <> '' AND token_hash = $4)))
		INSERT INTO staff_sessions(token_hash, staff_user_id, expires_at) SELECT $2,$1,$3 FROM allowed`,
		session.StaffID, session.TokenHash, session.ExpiresAt, session.ReplacesTokenHash, now, session.PasswordHash)
	if err != nil {
		return fmt.Errorf("create staff session: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrUnauthorized
	}
	return nil
}

func (s *Store) StaffSessionByTokenHash(ctx context.Context, hash string, now time.Time) (domain.StaffSessionRecord, error) {
	var record domain.StaffSessionRecord
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.email, u.display_name, u.created_at, s.expires_at
		FROM staff_sessions s JOIN staff_users u ON u.id = s.staff_user_id
		WHERE s.token_hash = $1 AND s.expires_at > $2 AND u.disabled_at IS NULL`, hash, now).Scan(
		&record.Staff.ID, &record.Staff.Email, &record.Staff.DisplayName, &record.Staff.CreatedAt, &record.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StaffSessionRecord{}, domain.ErrUnauthorized
	}
	if err != nil {
		return domain.StaffSessionRecord{}, fmt.Errorf("load staff session: %w", err)
	}
	record.Staff.CreatedAt = record.Staff.CreatedAt.UTC()
	record.ExpiresAt = record.ExpiresAt.UTC()
	return record, nil
}

func (s *Store) DeleteStaffSession(ctx context.Context, hash string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM staff_sessions WHERE token_hash = $1`, hash); err != nil {
		return fmt.Errorf("delete staff session: %w", err)
	}
	return nil
}

func (s *Store) CreateStaff(ctx context.Context, staff domain.NewStaff) (domain.StaffAccount, error) {
	account, err := scanStaffAccount(s.pool.QueryRow(ctx, `
		INSERT INTO staff_users(id,email,display_name,password_hash) VALUES ($1,$2,$3,$4)
		RETURNING `+staffColumns, staff.ID, staff.Email, staff.DisplayName, staff.PasswordHash))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == staffEmailIndex {
		return domain.StaffAccount{}, domain.ErrEmailTaken
	}
	if err != nil {
		return domain.StaffAccount{}, fmt.Errorf("create staff: %w", err)
	}
	return account, nil
}

func (s *Store) SetStaffPassword(ctx context.Context, email, passwordHash string) (domain.StaffChange, error) {
	return s.changeStaff(ctx, `UPDATE staff_users SET password_hash=$2 WHERE lower(email)=lower($1) RETURNING `+staffColumns, email, passwordHash)
}

// SetStaffDisabled keeps an earlier disable time. Enable also revokes, so a
// session written while disabled never comes back to life.
func (s *Store) SetStaffDisabled(ctx context.Context, email string, disabled bool) (domain.StaffChange, error) {
	if disabled {
		return s.changeStaff(ctx, `UPDATE staff_users SET disabled_at=COALESCE(disabled_at, now()) WHERE lower(email)=lower($1) RETURNING `+staffColumns, email)
	}
	return s.changeStaff(ctx, `UPDATE staff_users SET disabled_at=NULL WHERE lower(email)=lower($1) RETURNING `+staffColumns, email)
}

// changeStaff applies one account update and deletes every session of the
// account in the same transaction; the UPDATE takes the staff row lock first.
func (s *Store) changeStaff(ctx context.Context, update string, args ...any) (domain.StaffChange, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.StaffChange{}, err
	}
	defer tx.Rollback(ctx)
	account, err := scanStaffAccount(tx.QueryRow(ctx, update, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StaffChange{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.StaffChange{}, fmt.Errorf("update staff: %w", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM staff_sessions WHERE staff_user_id=$1`, account.ID)
	if err != nil {
		return domain.StaffChange{}, fmt.Errorf("revoke staff sessions: %w", err)
	}
	change := domain.StaffChange{Account: account, RevokedSessions: tag.RowsAffected()}
	if err := tx.Commit(ctx); err != nil {
		return domain.StaffChange{}, fmt.Errorf("commit staff change: %w", err)
	}
	return change, nil
}
