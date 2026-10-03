package application_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/internal/repository/memory"
)

const testKey = "service-test-key-0001"

func key(value string) *string { return &value }

// review returns the expectation a client would build from the current cart.
func review(t *testing.T, service *application.Service, user string) (*[]domain.ExpectedCheckoutItem, *int64) {
	t.Helper()
	cart, err := service.Cart(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	items := []domain.ExpectedCheckoutItem{}
	for _, line := range cart.Items {
		if line.Selected {
			items = append(items, domain.ExpectedCheckoutItem{ProductID: line.Product.ID, Quantity: line.Quantity, UnitPriceMinor: line.Product.PriceMinor})
		}
	}
	total := cart.Summary.TotalMinor
	return &items, &total
}

type checkoutState struct {
	cart   domain.Cart
	orders []domain.Order
	stock  map[string]int
}

func state(t *testing.T, service *application.Service, user string) checkoutState {
	t.Helper()
	ctx := context.Background()
	cart, err := service.Cart(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	orders, err := service.Orders(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	stock := map[string]int{}
	for _, id := range []string{"p01", "p02", "p10", "p16", "p18"} {
		product, err := service.Product(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		stock[id] = product.Stock
	}
	return checkoutState{cart, orders, stock}
}

func TestKeyedCheckoutReplaysCommittedOrder(t *testing.T) {
	service := newService()
	ctx := context.Background()
	items, total := review(t, service, "usr-demo")
	input := domain.CheckoutInput{DeliveryAddress: "Test", ExpectedItems: items, ExpectedTotalMinor: total, IdempotencyKey: key(testKey)}
	first, err := service.Checkout(ctx, "usr-demo", input)
	if err != nil || first.Replayed {
		t.Fatalf("first checkout: %+v %v", first, err)
	}
	committed := state(t, service, "usr-demo")
	// The selected lines are gone; the retry must replay, not conflict.
	replay, err := service.Checkout(ctx, "usr-demo", input)
	if err != nil || !replay.Replayed || !reflect.DeepEqual(replay.Order, first.Order) {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	if !reflect.DeepEqual(committed, state(t, service, "usr-demo")) {
		t.Fatal("replay decremented stock or consumed cart again")
	}
	if len(committed.cart.Items) != 2 {
		t.Fatal("unselected lines must remain")
	}
	// Without a key the same request is the legacy non-idempotent conflict.
	if _, err := service.Checkout(ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "Test"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("unkeyed retry: %v", err)
	}
	// A new key after the cart was consumed is an ordinary conflict.
	fresh := input
	fresh.IdempotencyKey = key("service-test-key-0002")
	if _, err := service.Checkout(ctx, "usr-demo", fresh); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("new key: %v", err)
	}
}

// The digest is computed by Go from the normalized logical request.
func TestCheckoutDigestCanonicalization(t *testing.T) {
	ctx := context.Background()
	reversed := func(items *[]domain.ExpectedCheckoutItem) *[]domain.ExpectedCheckoutItem {
		out := append([]domain.ExpectedCheckoutItem(nil), (*items)...)
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		return &out
	}
	plus := func(value int64) *int64 { return &value }
	for name, tc := range map[string]struct {
		retry  func(first domain.CheckoutInput) domain.CheckoutInput
		replay bool
	}{
		"surrounding address whitespace": {func(in domain.CheckoutInput) domain.CheckoutInput { in.DeliveryAddress = "  Test \n"; return in }, true},
		"expected items in another order": {func(in domain.CheckoutInput) domain.CheckoutInput {
			in.ExpectedItems = reversed(in.ExpectedItems)
			return in
		}, true},
		"different address":      {func(in domain.CheckoutInput) domain.CheckoutInput { in.DeliveryAddress = "Other"; return in }, false},
		"comment is not trimmed": {func(in domain.CheckoutInput) domain.CheckoutInput { in.Comment = " note"; return in }, false},
		"expectations removed": {func(in domain.CheckoutInput) domain.CheckoutInput {
			in.ExpectedItems, in.ExpectedTotalMinor = nil, nil
			return in
		}, false},
		"expected total differs": {func(in domain.CheckoutInput) domain.CheckoutInput {
			in.ExpectedTotalMinor = plus(*in.ExpectedTotalMinor + 1)
			return in
		}, false},
		"expected quantity differs": {func(in domain.CheckoutInput) domain.CheckoutInput {
			items := append([]domain.ExpectedCheckoutItem(nil), (*in.ExpectedItems)...)
			items[0].Quantity++
			in.ExpectedItems = &items
			return in
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			service := newService()
			items, total := review(t, service, "usr-demo")
			first := domain.CheckoutInput{DeliveryAddress: "Test", Comment: "note", ExpectedItems: items, ExpectedTotalMinor: total, IdempotencyKey: key(testKey)}
			original, err := service.Checkout(ctx, "usr-demo", first)
			if err != nil {
				t.Fatal(err)
			}
			committed := state(t, service, "usr-demo")
			result, err := service.Checkout(ctx, "usr-demo", tc.retry(first))
			if tc.replay {
				if err != nil || !result.Replayed || result.Order.ID != original.Order.ID {
					t.Fatalf("want replay, got %+v %v", result, err)
				}
			} else if !errors.Is(err, domain.ErrIdempotencyKeyReused) {
				t.Fatalf("want idempotency_key_reused, got %+v %v", result, err)
			}
			if !reflect.DeepEqual(committed, state(t, service, "usr-demo")) {
				t.Fatal("retry wrote")
			}
		})
	}
}

func TestKeyedCheckoutFailureRecordsNothingAndKeysArePerUser(t *testing.T) {
	service := newService()
	ctx := context.Background()
	if _, err := service.SetCartSelection(ctx, "usr-demo", []string{"p18"}, true); err != nil {
		t.Fatal(err)
	}
	input := domain.CheckoutInput{DeliveryAddress: "Test", IdempotencyKey: key(testKey)}
	if _, err := service.Checkout(ctx, "usr-demo", input); !errors.Is(err, domain.ErrOutOfStock) {
		t.Fatalf("selected unavailable line: %v", err)
	}
	if _, err := service.SetCartSelection(ctx, "usr-demo", []string{"p18"}, false); err != nil {
		t.Fatal(err)
	}
	// The failed attempt stored nothing, so the same key now checks out.
	result, err := service.Checkout(ctx, "usr-demo", input)
	if err != nil || result.Replayed {
		t.Fatalf("retry after definitive failure: %+v %v", result, err)
	}
	// Keys are scoped per user: another customer may use the same key.
	if _, err := service.SetCartItem(ctx, "usr-other", "p02", 1, true); err != nil {
		t.Fatal(err)
	}
	other, err := service.Checkout(ctx, "usr-other", input)
	if err != nil || other.Replayed || len(other.Order.Items) != 1 || other.Order.Items[0].ProductID != "p02" {
		t.Fatalf("other user's key: %+v %v", other, err)
	}
}

func TestCheckoutExpectationGuard(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		change func(t *testing.T, service *application.Service, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64)
		want   error
	}{
		"exact review": {func(*testing.T, *application.Service, *[]domain.ExpectedCheckoutItem, *int64) (*[]domain.ExpectedCheckoutItem, *int64) {
			return nil, nil
		}, nil},
		"current price differs from reviewed price": {func(_ *testing.T, _ *application.Service, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64) {
			changed := append([]domain.ExpectedCheckoutItem(nil), (*items)...)
			changed[0].UnitPriceMinor--
			return &changed, total
		}, domain.ErrCheckoutChanged},
		"quantity changed after review": {func(t *testing.T, service *application.Service, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64) {
			if _, err := service.SetCartItem(ctx, "usr-demo", "p01", 3, true); err != nil {
				t.Fatal(err)
			}
			return items, total
		}, domain.ErrCheckoutChanged},
		"selected line added after review": {func(t *testing.T, service *application.Service, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64) {
			if _, err := service.SetCartItem(ctx, "usr-demo", "p02", 1, true); err != nil {
				t.Fatal(err)
			}
			return items, total
		}, domain.ErrCheckoutChanged},
		"reviewed line deselected": {func(t *testing.T, service *application.Service, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64) {
			if _, err := service.SetCartSelection(ctx, "usr-demo", []string{"p10"}, false); err != nil {
				t.Fatal(err)
			}
			return items, total
		}, domain.ErrCheckoutChanged},
		"extra expected line": {func(_ *testing.T, _ *application.Service, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64) {
			changed := append(append([]domain.ExpectedCheckoutItem(nil), (*items)...), domain.ExpectedCheckoutItem{ProductID: "p16", Quantity: 1, UnitPriceMinor: 1})
			return &changed, total
		}, domain.ErrCheckoutChanged},
		"alias instead of canonical ID": {func(_ *testing.T, _ *application.Service, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64) {
			changed := append([]domain.ExpectedCheckoutItem(nil), (*items)...)
			for i := range changed {
				if changed[i].ProductID == "p01" {
					changed[i].ProductID = "CHA-701"
				}
			}
			return &changed, total
		}, domain.ErrCheckoutChanged},
		"total differs": {func(_ *testing.T, _ *application.Service, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64) {
			changed := *total + 1
			return items, &changed
		}, domain.ErrCheckoutChanged},
	} {
		t.Run(name, func(t *testing.T) {
			for _, keyed := range []bool{false, true} {
				service := application.New(memory.NewDemo(), application.Options{AllowDemoAccount: true})
				items, total := review(t, service, "usr-demo")
				if changedItems, changedTotal := tc.change(t, service, items, total); changedItems != nil {
					items, total = changedItems, changedTotal
				}
				before := state(t, service, "usr-demo")
				input := domain.CheckoutInput{DeliveryAddress: "Test", ExpectedItems: items, ExpectedTotalMinor: total}
				if keyed {
					input.IdempotencyKey = key(testKey)
				}
				result, err := service.Checkout(ctx, "usr-demo", input)
				if !errors.Is(err, tc.want) && !(tc.want == nil && err == nil) {
					t.Fatalf("keyed=%v err=%v want %v", keyed, err, tc.want)
				}
				if tc.want != nil {
					if !reflect.DeepEqual(before, state(t, service, "usr-demo")) {
						t.Fatal("mismatch wrote")
					}
					continue
				}
				if result.Order.TotalMinor != *total {
					t.Fatal("exact review total")
				}
			}
		})
	}
}

// The expectation is comparison-only: Go prices and persists the order.
func TestExpectationNeverPricesTheOrder(t *testing.T) {
	service := newService()
	items, total := review(t, service, "usr-demo")
	result, err := service.Checkout(context.Background(), "usr-demo", domain.CheckoutInput{DeliveryAddress: "Test", ExpectedItems: items, ExpectedTotalMinor: total})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range result.Order.Items {
		product, err := service.Product(context.Background(), line.ProductID)
		if err != nil {
			t.Fatal(err)
		}
		if line.UnitPriceMinor != product.PriceMinor || line.LineTotalMinor != int64(line.Quantity)*product.PriceMinor {
			t.Fatal("order line not priced by Go")
		}
	}
}

func TestKeyedCheckoutChangedRecordsNothing(t *testing.T) {
	service := newService()
	ctx := context.Background()
	items, total := review(t, service, "usr-demo")
	stale := *total - 1
	if _, err := service.Checkout(ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "Test", ExpectedItems: items, ExpectedTotalMinor: &stale, IdempotencyKey: key(testKey)}); !errors.Is(err, domain.ErrCheckoutChanged) {
		t.Fatalf("stale review: %v", err)
	}
	// No record exists, so the same key with the refreshed review is a new
	// request rather than idempotency_key_reused.
	result, err := service.Checkout(ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "Test", ExpectedItems: items, ExpectedTotalMinor: total, IdempotencyKey: key(testKey)})
	if err != nil || result.Replayed {
		t.Fatalf("refreshed review: %+v %v", result, err)
	}
}
