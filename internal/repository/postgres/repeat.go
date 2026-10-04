package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"sweetshop/internal/domain"
)

const repeatIdempotencyConstraint = "repeat_commits_user_key"

func isRepeatIdempotencyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == repeatIdempotencyConstraint
}

type repeatQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func repeatReceipt(ctx context.Context, q repeatQuerier, userID string, input domain.RepeatV2Input) (domain.RepeatReceipt, bool, error) {
	var digest string
	var encoded []byte
	err := q.QueryRow(ctx, `SELECT request_digest,receipt FROM repeat_commits WHERE user_id=$1 AND idempotency_key=$2`, userID, input.IdempotencyKey).Scan(&digest, &encoded)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RepeatReceipt{}, false, nil
	}
	if err != nil {
		return domain.RepeatReceipt{}, false, fmt.Errorf("load repeat receipt: %w", err)
	}
	if digest != input.RequestDigest {
		return domain.RepeatReceipt{}, true, domain.ErrIdempotencyKeyReused
	}
	var receipt domain.RepeatReceipt
	if err := json.Unmarshal(encoded, &receipt); err != nil {
		return domain.RepeatReceipt{}, true, fmt.Errorf("decode repeat receipt: %w", err)
	}
	return receipt, true, nil
}

func (s *Store) CommitRepeatV2(ctx context.Context, userID, orderID string, input domain.RepeatV2Input) (domain.RepeatV2Result, error) {
	if input.IdempotencyKey == "" || input.RequestDigest == "" {
		return domain.RepeatV2Result{}, errors.New("repeat without idempotency metadata")
	}
	// Already committed receipts need no mutation lock. Recheck inside the
	// transaction after waiting for the Cart lock to resolve concurrent keys.
	receipt, replayed, err := repeatReceipt(ctx, s.pool, userID, input)
	if !replayed && err == nil {
		receipt, replayed, err = s.commitRepeatV2(ctx, userID, orderID, input)
	}
	// Only this named unique constraint may resolve to a concurrent receipt.
	if isRepeatIdempotencyViolation(err) {
		if replay, found, lookupErr := repeatReceipt(ctx, s.pool, userID, input); found || lookupErr != nil {
			receipt, replayed, err = replay, found && lookupErr == nil, lookupErr
		}
	}
	if err != nil {
		return domain.RepeatV2Result{}, err
	}
	cart, err := s.Cart(ctx, userID)
	if err != nil {
		return domain.RepeatV2Result{}, err
	}
	return domain.RepeatV2Result{Receipt: receipt, Cart: cart, Replayed: replayed}, nil
}

func (s *Store) commitRepeatV2(ctx context.Context, userID, orderID string, input domain.RepeatV2Input) (domain.RepeatReceipt, bool, error) {
	tx, err := s.beginCartMutation(ctx, userID)
	if err != nil {
		return domain.RepeatReceipt{}, false, err
	}
	defer tx.Rollback(ctx)
	// The per-user Cart lock also serializes same-key commits. A receipt wins
	// before historical/product/Cart review validation; no second key lock exists.
	if receipt, found, err := repeatReceipt(ctx, tx, userID, input); found || err != nil {
		return receipt, found && err == nil, err
	}
	var order domain.Order
	err = tx.QueryRow(ctx, `SELECT id,number FROM orders WHERE user_id=$1 AND id=$2 FOR SHARE`, userID, orderID).Scan(&order.ID, &order.Number)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RepeatReceipt{}, false, domain.ErrNotFound
	}
	if err != nil {
		return domain.RepeatReceipt{}, false, err
	}
	rows, err := tx.Query(ctx, `SELECT position,product_id FROM order_items WHERE order_id=$1 ORDER BY position FOR SHARE`, orderID)
	if err != nil {
		return domain.RepeatReceipt{}, false, err
	}
	for rows.Next() {
		var item domain.OrderItem
		if err := rows.Scan(&item.SourcePosition, &item.ProductID); err != nil {
			rows.Close()
			return domain.RepeatReceipt{}, false, err
		}
		order.Items = append(order.Items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.RepeatReceipt{}, false, err
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, item := range input.Items {
		if !seen[item.ProductID] {
			ids = append(ids, item.ProductID)
			seen[item.ProductID] = true
		}
	}
	sort.Strings(ids)
	products := map[string]domain.Product{}
	rows, err = tx.Query(ctx, `SELECT id,price_minor,currency,minimum_quantity,quantity_step,stock FROM products WHERE id=ANY($1) ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return domain.RepeatReceipt{}, false, err
	}
	for rows.Next() {
		var p domain.Product
		if err := rows.Scan(&p.ID, &p.PriceMinor, &p.Currency, &p.MinimumQuantity, &p.QuantityStep, &p.Stock); err != nil {
			rows.Close()
			return domain.RepeatReceipt{}, false, err
		}
		products[p.ID] = p
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.RepeatReceipt{}, false, err
	}
	existing := map[string]int{}
	rows, err = tx.Query(ctx, `SELECT product_id,quantity FROM cart_items WHERE user_id=$1 AND product_id=ANY($2) ORDER BY product_id FOR UPDATE`, userID, ids)
	if err != nil {
		return domain.RepeatReceipt{}, false, err
	}
	for rows.Next() {
		var id string
		var quantity int
		if err := rows.Scan(&id, &quantity); err != nil {
			rows.Close()
			return domain.RepeatReceipt{}, false, err
		}
		existing[id] = quantity
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.RepeatReceipt{}, false, err
	}
	receipt, mutations, err := domain.PlanRepeatV2(order, input, products, existing)
	if err != nil {
		return domain.RepeatReceipt{}, false, err
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return domain.RepeatReceipt{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repeat_commits(user_id,idempotency_key,request_digest,source_order_id,receipt) VALUES($1,$2,$3,$4,$5)`, userID, input.IdempotencyKey, input.RequestDigest, orderID, encoded); err != nil {
		return domain.RepeatReceipt{}, false, fmt.Errorf("insert repeat receipt: %w", err)
	}
	for _, mutation := range mutations {
		_, err := tx.Exec(ctx, `INSERT INTO cart_items(user_id,product_id,quantity,selected,updated_at) VALUES($1,$2,$3,true,now()) ON CONFLICT(user_id,product_id) DO UPDATE SET quantity=cart_items.quantity+EXCLUDED.quantity,selected=true,updated_at=now()`, userID, mutation.ProductID, mutation.Quantity)
		if err != nil {
			return domain.RepeatReceipt{}, false, fmt.Errorf("apply repeat addition: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.RepeatReceipt{}, false, fmt.Errorf("commit repeat: %w", err)
	}
	return receipt, false, nil
}
