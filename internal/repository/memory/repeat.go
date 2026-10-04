package memory

import (
	"context"
	"errors"
	"time"

	"sweetshop/internal/domain"
)

type repeatKey struct {
	digest  string
	receipt domain.RepeatReceipt
}

func cloneReceipt(receipt domain.RepeatReceipt) domain.RepeatReceipt {
	receipt.Additions = append([]domain.RepeatAddition(nil), receipt.Additions...)
	return receipt
}

func (s *Store) CommitRepeatV2(_ context.Context, userID, orderID string, input domain.RepeatV2Input) (domain.RepeatV2Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.IdempotencyKey == "" || input.RequestDigest == "" {
		return domain.RepeatV2Result{}, errors.New("repeat without idempotency metadata")
	}
	if record, ok := s.repeatKeys[userID][input.IdempotencyKey]; ok {
		if record.digest != input.RequestDigest {
			return domain.RepeatV2Result{}, domain.ErrIdempotencyKeyReused
		}
		return domain.RepeatV2Result{Receipt: cloneReceipt(record.receipt), Cart: s.buildCart(userID), Replayed: true}, nil
	}
	var order domain.Order
	for _, candidate := range s.orders[userID] {
		if candidate.ID == orderID {
			order = candidate
			break
		}
	}
	if order.ID == "" {
		return domain.RepeatV2Result{}, domain.ErrNotFound
	}
	existing := map[string]int{}
	for id, row := range s.carts[userID] {
		existing[id] = row.quantity
	}
	receipt, mutations, err := domain.PlanRepeatV2(order, input, s.products, existing)
	if err != nil {
		return domain.RepeatV2Result{}, err
	}
	if s.carts[userID] == nil {
		s.carts[userID] = map[string]cartRow{}
	}
	for _, mutation := range mutations {
		row := s.carts[userID][mutation.ProductID]
		row.quantity += mutation.Quantity
		row.selected = true
		row.updated = time.Now().UTC()
		s.carts[userID][mutation.ProductID] = row
	}
	if s.repeatKeys == nil {
		s.repeatKeys = map[string]map[string]repeatKey{}
	}
	if s.repeatKeys[userID] == nil {
		s.repeatKeys[userID] = map[string]repeatKey{}
	}
	s.repeatKeys[userID][input.IdempotencyKey] = repeatKey{input.RequestDigest, cloneReceipt(receipt)}
	return domain.RepeatV2Result{Receipt: receipt, Cart: s.buildCart(userID)}, nil
}
