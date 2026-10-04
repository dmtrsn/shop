package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"sweetshop/internal/domain"
)

// beginCartMutation serializes every persisted Cart writer for one user until
// commit/rollback. Take this lock before any product, historical or Cart locks.
// NO KEY UPDATE conflicts with itself but permits foreign-key KEY SHARE locks
// from sessions/favorites/orders. FOR UPDATE would unnecessarily conflict with
// those readers and could introduce a product-FK -> user-FK lock dependency.
// No runtime repository flow updates/deletes users or locks them in reverse order.
func (s *Store) beginCartMutation(ctx context.Context, userID string) (pgx.Tx, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE`, userID).Scan(&id)
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("lock cart owner: %w", err)
	}
	return tx, nil
}
