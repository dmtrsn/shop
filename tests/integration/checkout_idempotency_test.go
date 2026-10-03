//go:build integration

package integration_test

import (
	"errors"
	"os"
	"reflect"
	"regexp"
	"testing"

	"sweetshop/internal/domain"
)

const idempotencyKey = "integration-key-000001"

func keyRef(value string) *string { return &value }

// reviewOf is the expectation a client builds from the authoritative cart.
func (d *database) reviewOf(t *testing.T, user string) (*[]domain.ExpectedCheckoutItem, *int64) {
	t.Helper()
	cart := d.cart(t, user)
	items := []domain.ExpectedCheckoutItem{}
	for _, line := range cart.Items {
		if line.Selected {
			items = append(items, domain.ExpectedCheckoutItem{ProductID: line.Product.ID, Quantity: line.Quantity, UnitPriceMinor: line.Product.PriceMinor})
		}
	}
	total := cart.Summary.TotalMinor
	return &items, &total
}

type purchaseState struct {
	cart                          domain.Cart
	orders, orderItems, keyedRows int64
	stockP01, stockP10, sequence  int64
}

func (d *database) purchaseState(t *testing.T, user string) purchaseState {
	t.Helper()
	return purchaseState{
		cart:       d.cart(t, user),
		orders:     d.number(t, "SELECT count(*) FROM orders"),
		orderItems: d.number(t, "SELECT count(*) FROM order_items"),
		keyedRows:  d.number(t, "SELECT count(*) FROM orders WHERE idempotency_key IS NOT NULL"),
		stockP01:   d.number(t, "SELECT stock FROM products WHERE id='p01'"),
		stockP10:   d.number(t, "SELECT stock FROM products WHERE id='p10'"),
		sequence:   d.number(t, "SELECT last_value FROM order_number_seq"),
	}
}

func TestKeyedCheckoutReplayAndStoredRecord(t *testing.T) {
	d := newDatabase(t, true)
	items, total := d.reviewOf(t, "usr-demo")
	input := domain.CheckoutInput{DeliveryAddress: "  Test  ", ExpectedItems: items, ExpectedTotalMinor: total, IdempotencyKey: keyRef(idempotencyKey)}
	first, err := d.service.Checkout(d.ctx, "usr-demo", input)
	if err != nil || first.Replayed || first.Order.TotalMinor != *total || len(first.Order.Items) != 2 {
		t.Fatalf("first: %+v %v", first, err)
	}
	var storedKey, digest string
	if err := d.pool.QueryRow(d.ctx, "SELECT idempotency_key,request_digest FROM orders WHERE id=$1", first.Order.ID).Scan(&storedKey, &digest); err != nil {
		t.Fatal(err)
	}
	if storedKey != idempotencyKey || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(digest) {
		t.Fatalf("stored key=%q digest=%q", storedKey, digest)
	}
	committed := d.purchaseState(t, "usr-demo")
	if len(committed.cart.Items) != 2 {
		t.Fatal("unselected lines must remain")
	}
	// The selected lines are consumed; the same request replays the order.
	replay, err := d.service.Checkout(d.ctx, "usr-demo", input)
	if err != nil || !replay.Replayed || !reflect.DeepEqual(replay.Order, first.Order) {
		t.Fatalf("replay differs: %+v %v\nfirst: %+v", replay, err, first)
	}
	reused := input
	reused.DeliveryAddress = "Other"
	if _, err := d.service.Checkout(d.ctx, "usr-demo", reused); !errors.Is(err, domain.ErrIdempotencyKeyReused) {
		t.Fatalf("different digest: %v", err)
	}
	fresh := input
	fresh.IdempotencyKey = keyRef("integration-key-000002")
	if _, err := d.service.Checkout(d.ctx, "usr-demo", fresh); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("new key after consumption: %v", err)
	}
	if !reflect.DeepEqual(committed, d.purchaseState(t, "usr-demo")) {
		t.Fatal("replay or rejected retries wrote")
	}
	// Keys are unique per user only.
	d.customer(t, "buyer")
	d.set(t, "buyer", "p02", 1, true)
	other, err := d.service.Checkout(d.ctx, "buyer", domain.CheckoutInput{DeliveryAddress: "Test", IdempotencyKey: keyRef(idempotencyKey)})
	if err != nil || other.Replayed || other.Order.Items[0].ProductID != "p02" {
		t.Fatalf("same key, other user: %+v %v", other, err)
	}
}

func TestKeyedCheckoutFailureStoresNothing(t *testing.T) {
	d := newDatabase(t, true)
	if _, err := d.service.SetCartSelection(d.ctx, "usr-demo", []string{"p18"}, true); err != nil {
		t.Fatal(err)
	}
	before := d.purchaseState(t, "usr-demo")
	input := domain.CheckoutInput{DeliveryAddress: "Test", IdempotencyKey: keyRef(idempotencyKey)}
	if _, err := d.service.Checkout(d.ctx, "usr-demo", input); !errors.Is(err, domain.ErrOutOfStock) {
		t.Fatalf("unavailable selected line: %v", err)
	}
	after := d.purchaseState(t, "usr-demo")
	// nextval is not transactional; everything else must be untouched.
	before.sequence, after.sequence = 0, 0
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed keyed checkout left writes or a key record")
	}
	if _, err := d.service.SetCartSelection(d.ctx, "usr-demo", []string{"p18"}, false); err != nil {
		t.Fatal(err)
	}
	if result, err := d.service.Checkout(d.ctx, "usr-demo", input); err != nil || result.Replayed {
		t.Fatalf("retry of a definitively failed key: %+v %v", result, err)
	}
}

func TestCheckoutExpectationGuardPostgres(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(t *testing.T, d *database, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64)
		want   error
	}{
		"exact review": {nil, nil},
		"price changed after review": {func(t *testing.T, d *database, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64) {
			d.exec(t, "UPDATE products SET price_minor=price_minor+100 WHERE id='p01'")
			return items, total
		}, domain.ErrCheckoutChanged},
		"quantity changed after review": {func(t *testing.T, d *database, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64) {
			d.set(t, "usr-demo", "p01", 3, true)
			return items, total
		}, domain.ErrCheckoutChanged},
		"selected line added after review": {func(t *testing.T, d *database, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64) {
			d.set(t, "usr-demo", "p02", 1, true)
			return items, total
		}, domain.ErrCheckoutChanged},
		"reviewed line deselected": {func(t *testing.T, d *database, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64) {
			if _, err := d.service.SetCartSelection(d.ctx, "usr-demo", []string{"p10"}, false); err != nil {
				t.Fatal(err)
			}
			return items, total
		}, domain.ErrCheckoutChanged},
		"total differs": {func(_ *testing.T, _ *database, items *[]domain.ExpectedCheckoutItem, total *int64) (*[]domain.ExpectedCheckoutItem, *int64) {
			changed := *total + 1
			return items, &changed
		}, domain.ErrCheckoutChanged},
	} {
		for _, keyed := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/unkeyed", true: "/keyed"}[keyed], func(t *testing.T) {
				d := newDatabase(t, true)
				items, total := d.reviewOf(t, "usr-demo")
				if tc.change != nil {
					items, total = tc.change(t, d, items, total)
				}
				before := d.purchaseState(t, "usr-demo")
				input := domain.CheckoutInput{DeliveryAddress: "Test", ExpectedItems: items, ExpectedTotalMinor: total}
				if keyed {
					input.IdempotencyKey = keyRef(idempotencyKey)
				}
				result, err := d.service.Checkout(d.ctx, "usr-demo", input)
				if tc.want == nil {
					if err != nil || result.Order.TotalMinor != *total {
						t.Fatalf("exact review: %+v %v", result, err)
					}
					return
				}
				if !errors.Is(err, tc.want) {
					t.Fatalf("err=%v want %v", err, tc.want)
				}
				// Compared before any write, even the order number sequence.
				if !reflect.DeepEqual(before, d.purchaseState(t, "usr-demo")) {
					t.Fatal("expectation mismatch wrote")
				}
			})
		}
	}
}

// Both requests pass the pre-lookup and wait on the same locked product. The
// winner commits; the loser then sees no selected rows and must resolve the
// key record instead of reporting an empty-cart conflict.
func TestConcurrentSameKeyCheckout(t *testing.T) {
	for _, sameDigest := range []bool{true, false} {
		name := "same digest replays"
		if !sameDigest {
			name = "different digest is rejected"
		}
		t.Run(name, func(t *testing.T) {
			d := newDatabase(t, true)
			blocker := d.lockProduct(t, "p01")
			type outcome struct {
				result domain.CheckoutResult
				err    error
			}
			results := make(chan outcome, 2)
			for _, address := range []string{"Test", map[bool]string{true: "Test", false: "Other"}[sameDigest]} {
				go func(address string) {
					result, err := d.service.Checkout(d.ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: address, IdempotencyKey: keyRef(idempotencyKey)})
					results <- outcome{result, err}
				}(address)
			}
			d.waitForBlocked(t, 2)
			if err := blocker.Commit(d.ctx); err != nil {
				t.Fatal(err)
			}
			var created, replayed []domain.Order
			reused := 0
			for i := 0; i < 2; i++ {
				var got outcome
				select {
				case got = <-results:
				case <-d.ctx.Done():
					t.Fatal("checkout did not finish")
				}
				switch {
				case got.err == nil && got.result.Replayed:
					replayed = append(replayed, got.result.Order)
				case got.err == nil:
					created = append(created, got.result.Order)
				case errors.Is(got.err, domain.ErrIdempotencyKeyReused):
					reused++
				default:
					t.Fatalf("unexpected outcome: %v", got.err)
				}
			}
			if len(created) != 1 {
				t.Fatalf("created %d orders", len(created))
			}
			if sameDigest && (len(replayed) != 1 || replayed[0].ID != created[0].ID) {
				t.Fatal("loser did not replay the winner")
			}
			if !sameDigest && reused != 1 {
				t.Fatal("loser with a different digest was not rejected")
			}
			if d.number(t, "SELECT count(*) FROM orders WHERE user_id='usr-demo'") != 2 ||
				d.number(t, "SELECT stock FROM products WHERE id='p01'") != 22 ||
				d.number(t, "SELECT stock FROM products WHERE id='p10'") != 39 {
				t.Fatal("second order, stock decrement or cart consumption")
			}
		})
	}
}

// The loser reaches the insert while the winner's row is uncommitted, so only
// the intended partial unique index can detect the duplicate.
func TestIdempotencyUniqueIndexRace(t *testing.T) {
	for _, sameDigest := range []bool{true, false} {
		t.Run(map[bool]string{true: "same digest replays", false: "different digest is rejected"}[sameDigest], func(t *testing.T) {
			d := newDatabase(t, true)
			input := domain.CheckoutInput{DeliveryAddress: "Test", IdempotencyKey: keyRef(idempotencyKey)}
			// The digest covers only the logical request, so a probe customer
			// yields the digest usr-demo's identical request will compute.
			d.customer(t, "probe")
			d.set(t, "probe", "p02", 1, true)
			if _, err := d.service.Checkout(d.ctx, "probe", input); err != nil {
				t.Fatal(err)
			}
			digest := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
			if sameDigest {
				if err := d.pool.QueryRow(d.ctx, "SELECT request_digest FROM orders WHERE user_id='probe'").Scan(&digest); err != nil {
					t.Fatal(err)
				}
			}
			blocker, err := d.pool.Begin(d.ctx)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = blocker.Rollback(d.ctx) })
			if _, err := blocker.Exec(d.ctx, `INSERT INTO orders(id,number,user_id,status,status_label,item_count,total_minor,delivery_address,idempotency_key,request_digest) VALUES('ord-race','#race','usr-demo','processing','Собираем заказ',1,1,'Test',$1,$2)`, idempotencyKey, digest); err != nil {
				t.Fatal(err)
			}
			before := d.purchaseState(t, "usr-demo")
			done := make(chan struct{})
			var result domain.CheckoutResult
			go func() {
				defer close(done)
				result, err = d.service.Checkout(d.ctx, "usr-demo", input)
			}()
			d.waitForBlocked(t, 1)
			if err := blocker.Commit(d.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-d.ctx.Done():
				t.Fatal("checkout did not finish")
			}
			if sameDigest {
				if err != nil || !result.Replayed || result.Order.ID != "ord-race" {
					t.Fatalf("unique race replay: %+v %v", result, err)
				}
			} else if !errors.Is(err, domain.ErrIdempotencyKeyReused) {
				t.Fatalf("unique race with different digest: %v", err)
			}
			after := d.purchaseState(t, "usr-demo")
			if !reflect.DeepEqual(before.cart, after.cart) || before.stockP01 != after.stockP01 || before.stockP10 != after.stockP10 ||
				after.orderItems != before.orderItems || after.orders != before.orders+1 {
				t.Fatal("loser left partial writes")
			}
		})
	}
}

// Only the idempotency index means "already committed". Any other unique
// violation stays an ordinary failure, never a replay or key reuse.
func TestUnrelatedUniqueViolationIsNotReplayed(t *testing.T) {
	d := newDatabase(t, true)
	// The next number becomes the seeded '#10482'.
	d.exec(t, "SELECT setval('order_number_seq', 10481)")
	before := d.purchaseState(t, "usr-demo")
	_, err := d.service.Checkout(d.ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "Test", IdempotencyKey: keyRef(idempotencyKey)})
	if err == nil || errors.Is(err, domain.ErrIdempotencyKeyReused) || errors.Is(err, domain.ErrConflict) {
		t.Fatalf("unrelated unique violation: %v", err)
	}
	after := d.purchaseState(t, "usr-demo")
	before.sequence, after.sequence = 0, 0
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed checkout wrote")
	}
}

func TestOrderIdempotencyMigrationDownAndUp(t *testing.T) {
	d := newDatabase(t, true)
	schema := func() int64 {
		return d.number(t, `SELECT
			(SELECT count(*) FROM information_schema.columns WHERE table_name='orders' AND column_name IN ('idempotency_key','request_digest')) +
			(SELECT count(*) FROM pg_indexes WHERE indexname='orders_user_idempotency_key_idx') +
			(SELECT count(*) FROM pg_constraint WHERE conname='orders_idempotency_pair_chk')`)
	}
	if got := schema(); got != 4 {
		t.Fatalf("idempotency schema objects=%d", got)
	}
	for _, file := range []string{"006_order_idempotency.down.sql", "006_order_idempotency.up.sql"} {
		sql, err := os.ReadFile("../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		d.exec(t, string(sql))
		want := int64(4)
		if file == "006_order_idempotency.down.sql" {
			want = 0
			if d.number(t, "SELECT count(*) FROM orders") != 1 {
				t.Fatal("down migration removed orders")
			}
		}
		if got := schema(); got != want {
			t.Fatalf("%s left %d idempotency schema objects", file, got)
		}
	}
	if d.number(t, "SELECT count(*) FROM orders WHERE idempotency_key IS NULL AND request_digest IS NULL") != 1 {
		t.Fatal("existing orders must stay NULL/NULL")
	}
}
