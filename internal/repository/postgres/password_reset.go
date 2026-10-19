package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"sweetshop/internal/domain"
)

// Lock order, shared with every account change: the per-address advisory
// lock (only flows that may send email take it), then the users row, then
// the customer's other rows (throttle, reset, sessions). Cart writers and
// account changes lock the users row first too, so no cycle can form.

// RequestPasswordReset holds the users row FOR SHARE while it decides, so a
// concurrent disable or operator password change (which update the row and
// then delete the reset) is ordered strictly before or after it.
func (s *Store) RequestPasswordReset(ctx context.Context, email, tokenHash string, expiresAt time.Time, allowance domain.EmailAllowance, now time.Time) (domain.PasswordResetOutcome, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if err := lockAddress(ctx, tx, email); err != nil {
		return "", err
	}
	var userID string
	var disabled bool
	err = tx.QueryRow(ctx, `SELECT id, disabled_at IS NOT NULL FROM users WHERE lower(email)=lower($1) FOR SHARE`, email).Scan(&userID, &disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PasswordResetNone, nil
	}
	if err != nil {
		return "", fmt.Errorf("find customer: %w", err)
	}
	if disabled {
		return domain.PasswordResetDisabled, nil
	}
	claimed, err := claimEmail(ctx, tx, email, allowance, now)
	if err != nil {
		return "", err
	}
	if !claimed {
		return domain.PasswordResetThrottled, nil
	}
	// One row per customer: a newer request replaces the token, so the
	// earlier link stops working.
	if _, err := tx.Exec(ctx, `
		INSERT INTO customer_password_resets(user_id,token_hash,created_at,expires_at) VALUES ($1,$2,$3,$4)
		ON CONFLICT (user_id) DO UPDATE SET token_hash=EXCLUDED.token_hash, created_at=EXCLUDED.created_at, expires_at=EXCLUDED.expires_at`,
		userID, tokenHash, now, expiresAt); err != nil {
		return "", fmt.Errorf("store password reset: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit password reset: %w", err)
	}
	return domain.PasswordResetIssued, nil
}

func (s *Store) PasswordReset(ctx context.Context, tokenHash string, now time.Time) (domain.PasswordReset, error) {
	var reset domain.PasswordReset
	err := s.pool.QueryRow(ctx, `SELECT user_id, token_hash, expires_at FROM customer_password_resets WHERE token_hash=$1 AND expires_at > $2`, tokenHash, now).
		Scan(&reset.UserID, &reset.TokenHash, &reset.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PasswordReset{}, domain.ErrPasswordResetLinkInvalid
	}
	if err != nil {
		return domain.PasswordReset{}, fmt.Errorf("load password reset: %w", err)
	}
	return reset, nil
}

// ResetPassword locks the users row FOR NO KEY UPDATE (the mode of the
// password UPDATE itself and of cart writers), then claims the reset with
// one DELETE, so of two concurrent resets only one finds the row. The new
// hash and the deletion of every session commit together: a login that
// verified the old password is refused by CreateSession's hash guard, which
// waits for this row lock and then sees the new hash.
func (s *Store) ResetPassword(ctx context.Context, userID, tokenHash, passwordHash string, now time.Time) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var disabled bool
	err = tx.QueryRow(ctx, `SELECT disabled_at IS NOT NULL FROM users WHERE id=$1 FOR NO KEY UPDATE`, userID).Scan(&disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrPasswordResetLinkInvalid
	}
	if err != nil {
		return 0, fmt.Errorf("lock customer: %w", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM customer_password_resets WHERE user_id=$1 AND token_hash=$2 AND expires_at > $3`, userID, tokenHash, now)
	if err != nil {
		return 0, fmt.Errorf("claim password reset: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return 0, domain.ErrPasswordResetLinkInvalid
	}
	if disabled {
		// A disabled account never recovers itself; its token is spent.
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("commit password reset: %w", err)
		}
		return 0, domain.ErrPasswordResetLinkInvalid
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, userID, passwordHash); err != nil {
		return 0, fmt.Errorf("store password: %w", err)
	}
	revoked, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID)
	if err != nil {
		return 0, fmt.Errorf("revoke customer sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit password reset: %w", err)
	}
	return revoked.RowsAffected(), nil
}

func (s *Store) DeleteExpiredPasswordResets(ctx context.Context, now time.Time, limit int) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM customer_password_resets WHERE user_id IN (
			SELECT user_id FROM customer_password_resets WHERE expires_at <= $1
			ORDER BY expires_at LIMIT $2 FOR UPDATE SKIP LOCKED)`, now, limit)
	if err != nil {
		return 0, fmt.Errorf("delete expired password resets: %w", err)
	}
	return tag.RowsAffected(), nil
}
