package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"sweetshop/internal/domain"
)

// registrationLockClass is the first key of the per-address advisory locks.
// The two-integer key space never overlaps the migrator's bigint lock.
const registrationLockClass int32 = 0x52454731 // "REG1"

// lockAddress serializes, until the transaction ends, every registration
// and resend for one normalized address on every replica. Two addresses
// whose hashes collide merely wait for each other.
func lockAddress(ctx context.Context, tx pgx.Tx, email string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`, registrationLockClass, email); err != nil {
		return fmt.Errorf("lock address: %w", err)
	}
	return nil
}

// claimEmail takes one message from the address's allowance, or reports
// false and changes nothing: the last message must be at least Cooldown old
// and fewer than Limit may fall inside the sliding Window. Kept send times
// are only those inside the window, so the array stays at most Limit long.
func claimEmail(ctx context.Context, tx pgx.Tx, email string, allowance domain.EmailAllowance, now time.Time) (bool, error) {
	var claimed bool
	err := tx.QueryRow(ctx, `
		INSERT INTO customer_email_throttle AS t (email, sent_at, last_sent_at)
		VALUES ($1, ARRAY[$2::timestamptz], $2)
		ON CONFLICT (email) DO UPDATE
		SET sent_at = ARRAY(SELECT s FROM unnest(t.sent_at) AS s WHERE s > $4 ORDER BY s) || $2::timestamptz,
		    last_sent_at = $2
		WHERE t.last_sent_at <= $3
		  AND (SELECT count(*) FROM unnest(t.sent_at) AS s WHERE s > $4) < $5
		RETURNING true`,
		email, now, now.Add(-allowance.Cooldown), now.Add(-allowance.Window), int64(allowance.Limit)).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim account email: %w", err)
	}
	return claimed, nil
}

func customerExists(ctx context.Context, tx pgx.Tx, email string) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email) = lower($1))`, email).Scan(&exists); err != nil {
		return false, fmt.Errorf("check customer: %w", err)
	}
	return exists, nil
}

func (s *Store) StartRegistration(ctx context.Context, pending domain.PendingRegistration, allowance domain.EmailAllowance, now time.Time) (domain.RegistrationOutcome, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if err := lockAddress(ctx, tx, pending.Email); err != nil {
		return "", err
	}
	claimed, err := claimEmail(ctx, tx, pending.Email, allowance, now)
	if err != nil {
		return "", err
	}
	if !claimed {
		return domain.RegistrationThrottled, nil
	}
	outcome := domain.RegistrationAccountExists
	exists, err := customerExists(ctx, tx, pending.Email)
	if err != nil {
		return "", err
	}
	if !exists {
		// One row per address: a newer registration replaces the older one,
		// and with it the token, so the earlier link stops working.
		if _, err := tx.Exec(ctx, `
			INSERT INTO customer_registrations(email,name,phone,password_hash,token_hash,created_at,expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (email) DO UPDATE SET name=EXCLUDED.name, phone=EXCLUDED.phone, password_hash=EXCLUDED.password_hash,
				token_hash=EXCLUDED.token_hash, created_at=EXCLUDED.created_at, expires_at=EXCLUDED.expires_at`,
			pending.Email, pending.Name, pending.Phone, pending.PasswordHash, pending.TokenHash, pending.CreatedAt, pending.ExpiresAt); err != nil {
			return "", fmt.Errorf("store registration: %w", err)
		}
		outcome = domain.RegistrationPending
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit registration: %w", err)
	}
	return outcome, nil
}

func (s *Store) ReissueRegistration(ctx context.Context, email, tokenHash string, expiresAt time.Time, allowance domain.EmailAllowance, now time.Time) (domain.RegistrationOutcome, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if err := lockAddress(ctx, tx, email); err != nil {
		return "", err
	}
	exists, err := customerExists(ctx, tx, email)
	if err != nil {
		return "", err
	}
	outcome := domain.RegistrationAccountExists
	if !exists {
		var pending bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM customer_registrations WHERE email=$1 AND expires_at > $2)`, email, now).Scan(&pending); err != nil {
			return "", fmt.Errorf("check registration: %w", err)
		}
		if !pending {
			return domain.RegistrationNone, nil
		}
		outcome = domain.RegistrationPending
	}
	claimed, err := claimEmail(ctx, tx, email, allowance, now)
	if err != nil {
		return "", err
	}
	if !claimed {
		return domain.RegistrationThrottled, nil
	}
	if outcome == domain.RegistrationPending {
		if _, err := tx.Exec(ctx, `UPDATE customer_registrations SET token_hash=$2, expires_at=$3 WHERE email=$1`, email, tokenHash, expiresAt); err != nil {
			return "", fmt.Errorf("reissue registration: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit resend: %w", err)
	}
	return outcome, nil
}

func (s *Store) PendingRegistration(ctx context.Context, tokenHash string, now time.Time) (domain.PendingRegistration, error) {
	var pending domain.PendingRegistration
	err := s.pool.QueryRow(ctx, `
		SELECT email,name,phone,password_hash,token_hash,created_at,expires_at
		FROM customer_registrations WHERE token_hash=$1 AND expires_at > $2`, tokenHash, now).Scan(
		&pending.Email, &pending.Name, &pending.Phone, &pending.PasswordHash, &pending.TokenHash, &pending.CreatedAt, &pending.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PendingRegistration{}, domain.ErrRegistrationLinkInvalid
	}
	if err != nil {
		return domain.PendingRegistration{}, fmt.Errorf("load registration: %w", err)
	}
	return pending, nil
}

// ConfirmRegistration claims the registration with one DELETE: of two
// concurrent confirmations only one gets the row, and a registration that a
// newer one replaced no longer matches the token. The customer INSERT skips
// an address an operator provisioned meanwhile; the registration is then
// consumed anyway and the caller gets the generic invalid-link answer.
func (s *Store) ConfirmRegistration(ctx context.Context, tokenHash, passwordHash, userID string, now time.Time) (domain.UserCredential, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.UserCredential{}, err
	}
	defer tx.Rollback(ctx)
	var pending domain.PendingRegistration
	err = tx.QueryRow(ctx, `
		DELETE FROM customer_registrations WHERE token_hash=$1 AND password_hash=$2 AND expires_at > $3
		RETURNING email,name,phone,password_hash`, tokenHash, passwordHash, now).Scan(
		&pending.Email, &pending.Name, &pending.Phone, &pending.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.UserCredential{}, domain.ErrRegistrationLinkInvalid
	}
	if err != nil {
		return domain.UserCredential{}, fmt.Errorf("claim registration: %w", err)
	}
	var credential domain.UserCredential
	err = tx.QueryRow(ctx, `
		INSERT INTO users(id,name,email,phone,password_hash) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT DO NOTHING
		RETURNING id,name,email,phone,created_at,password_hash`,
		userID, pending.Name, pending.Email, pending.Phone, pending.PasswordHash).Scan(
		&credential.User.ID, &credential.User.Name, &credential.User.Email, &credential.User.Phone, &credential.User.CreatedAt, &credential.PasswordHash)
	taken := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !taken {
		return domain.UserCredential{}, fmt.Errorf("create customer: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.UserCredential{}, fmt.Errorf("commit confirmation: %w", err)
	}
	if taken {
		return domain.UserCredential{}, domain.ErrRegistrationLinkInvalid
	}
	credential.User.CreatedAt = credential.User.CreatedAt.UTC()
	return credential, nil
}

// The cleanup deletes skip rows that another transaction holds (a
// registration in progress, or another replica's cleanup), so they never
// wait and never delete the same row twice.
func (s *Store) DeleteExpiredRegistrations(ctx context.Context, now time.Time, limit int) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM customer_registrations WHERE email IN (
			SELECT email FROM customer_registrations WHERE expires_at <= $1
			ORDER BY expires_at LIMIT $2 FOR UPDATE SKIP LOCKED)`, now, limit)
	if err != nil {
		return 0, fmt.Errorf("delete expired registrations: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *Store) DeleteIdleEmailAllowances(ctx context.Context, idleSince time.Time, limit int) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM customer_email_throttle WHERE email IN (
			SELECT email FROM customer_email_throttle WHERE last_sent_at <= $1
			ORDER BY last_sent_at LIMIT $2 FOR UPDATE SKIP LOCKED)`, idleSince, limit)
	if err != nil {
		return 0, fmt.Errorf("delete idle email allowances: %w", err)
	}
	return tag.RowsAffected(), nil
}
