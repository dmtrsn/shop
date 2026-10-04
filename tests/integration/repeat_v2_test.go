//go:build integration

package integration_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"sweetshop/internal/domain"
	"sweetshop/tests/contract"
)

func TestRepeatV2BulkCartLockOrder(t *testing.T) {
	for _, writer := range []string{"clear", "selection"} {
		t.Run(writer, func(t *testing.T) {
			d := newDatabase(t, true)
			// Physical insertion order deliberately opposes canonical product order.
			d.exec(t, `DELETE FROM cart_items WHERE user_id='usr-demo'; INSERT INTO cart_items(user_id,product_id,quantity,selected) VALUES('usr-demo','p07',1,true),('usr-demo','p01',2,true)`)
			preview, err := d.service.RepeatPreviewV2(d.ctx, "usr-demo", "ord-2026-002")
			if err != nil {
				t.Fatal(err)
			}
			input := contract.RepeatInput(preview.Items[0], 1)
			input.Items = append(input.Items, contract.RepeatInput(preview.Items[1], 1).Items[0])
			blocker, err := d.pool.Begin(d.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(context.Background())
			if _, err = blocker.Exec(d.ctx, `SELECT 1 FROM cart_items WHERE user_id='usr-demo' AND product_id='p07' FOR UPDATE`); err != nil {
				t.Fatal(err)
			}
			repeated := make(chan error, 1)
			go func() { _, err := d.service.CommitRepeatV2(d.ctx, "usr-demo", "ord-2026-002", input); repeated <- err }()
			d.waitForBlocked(t, 1)
			other := make(chan error, 1)
			go func() {
				var err error
				if writer == "clear" {
					_, err = d.store.ClearCart(d.ctx, "usr-demo", false)
				} else {
					_, err = d.store.SetCartSelection(d.ctx, "usr-demo", nil, false)
				}
				other <- err
			}()
			d.waitForBlocked(t, 2)
			if err = blocker.Commit(d.ctx); err != nil {
				t.Fatal(err)
			}
			if err = receiveError(t, d.ctx, repeated); err != nil {
				t.Fatal(err)
			}
			if err = receiveError(t, d.ctx, other); err != nil {
				t.Fatal(err)
			}
			if d.number(t, `SELECT count(*) FROM repeat_commits`) != 1 {
				t.Fatal("receipt missing")
			}
		})
	}
}

func TestRepeatV2Contract(t *testing.T) {
	contract.RunRepeatV2(t, func(t *testing.T) contract.RepeatFixture {
		d := newDatabase(t, true)
		return contract.RepeatFixture{Repo: d.store,
			ChangeProduct: func(change func(*domain.Product)) {
				p, err := d.store.Product(d.ctx, "p01")
				if err != nil {
					t.Fatal(err)
				}
				change(&p)
				d.exec(t, `UPDATE products SET price_minor=$1,currency=$2,minimum_quantity=$3,quantity_step=$4,stock=$5 WHERE id='p01'`, p.PriceMinor, p.Currency, p.MinimumQuantity, p.QuantityStep, p.Stock)
			},
			DeleteProduct: func() { d.exec(t, `DELETE FROM products WHERE id='p01'`) },
			DuplicateSource: func() {
				d.exec(t, `INSERT INTO order_items(order_id,position,product_id,sku,name,image,manufacturer_id,manufacturer,quantity,unit_price_minor,line_total_minor) SELECT order_id,9,product_id,sku,name,image,manufacturer_id,manufacturer,quantity,unit_price_minor,line_total_minor FROM order_items WHERE order_id='ord-2026-002' AND position=1`)
			},
			ReceiptCount: func() int { return int(d.number(t, `SELECT count(*) FROM repeat_commits`)) },
		}
	})
}

func repeatReview(t *testing.T, d *database) domain.RepeatV2Input {
	t.Helper()
	preview, err := d.service.RepeatPreviewV2(d.ctx, "usr-demo", "ord-2026-002")
	if err != nil {
		t.Fatal(err)
	}
	return contract.RepeatInput(preview.Items[0], 2)
}

func TestRepeatV2RevalidatesAfterLockWait(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		want      error
	}{
		{"price", `UPDATE products SET price_minor=price_minor+1 WHERE id='p01'`, domain.ErrRepeatChanged},
		{"currency", `UPDATE products SET currency='USD' WHERE id='p01'`, domain.ErrRepeatChanged},
		{"minimum", `UPDATE products SET minimum_quantity=3 WHERE id='p01'`, domain.ErrRepeatChanged},
		{"step", `UPDATE products SET quantity_step=2 WHERE id='p01'`, domain.ErrRepeatChanged},
		{"stock insufficient", `UPDATE products SET stock=3 WHERE id='p01'`, domain.ErrOutOfStock},
		{"stock sufficient", `UPDATE products SET stock=4 WHERE id='p01'`, nil},
		{"delete", `DELETE FROM products WHERE id='p01'`, domain.ErrRepeatChanged},
		{"cart", `UPDATE cart_items SET quantity=3 WHERE user_id='usr-demo' AND product_id='p01'`, domain.ErrRepeatChanged},
		{"source", `UPDATE order_items SET product_id='p02' WHERE order_id='ord-2026-002' AND position=1`, domain.ErrRepeatChanged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDatabase(t, true)
			input := repeatReview(t, d)
			tx, err := d.pool.Begin(d.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err := tx.Exec(d.ctx, tc.sql); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := d.service.CommitRepeatV2(d.ctx, "usr-demo", "ord-2026-002", input); done <- err }()
			d.waitForBlocked(t, 1)
			if err := tx.Commit(d.ctx); err != nil {
				t.Fatal(err)
			}
			if err := receiveError(t, d.ctx, done); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			want := int64(0)
			if tc.want == nil {
				want = 1
			}
			if d.number(t, `SELECT count(*) FROM repeat_commits`) != want {
				t.Fatal("wrong receipt count")
			}
		})
	}
}

func TestRepeatV2Rollback(t *testing.T) {
	d := newDatabase(t, true)
	input := repeatReview(t, d)
	preview, err := d.service.RepeatPreviewV2(d.ctx, "usr-demo", "ord-2026-002")
	if err != nil {
		t.Fatal(err)
	}
	input.Items = append(input.Items, contract.RepeatInput(preview.Items[1], 1).Items[0])
	before := d.cart(t, "usr-demo")
	// A later Cart write fails after the receipt insert. Even an unrelated
	// unique violation must roll back the receipt and every preceding addition.
	d.exec(t, `CREATE FUNCTION reject_repeat_cart() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.product_id='p07' THEN RAISE unique_violation USING CONSTRAINT='unrelated_unique'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_repeat_cart BEFORE INSERT OR UPDATE ON cart_items FOR EACH ROW EXECUTE FUNCTION reject_repeat_cart()`)
	if _, err := d.service.CommitRepeatV2(d.ctx, "usr-demo", "ord-2026-002", input); err == nil || errors.Is(err, domain.ErrIdempotencyKeyReused) {
		t.Fatal(err)
	}
	if d.number(t, `SELECT count(*) FROM repeat_commits`) != 0 || !reflect.DeepEqual(before, d.cart(t, "usr-demo")) {
		t.Fatal("transaction did not roll back")
	}
	d.exec(t, `DROP TRIGGER reject_repeat_cart ON cart_items`)
	if result, err := d.service.CommitRepeatV2(d.ctx, "usr-demo", "ord-2026-002", input); err != nil || result.Replayed {
		t.Fatal(err)
	}
}

func TestRepeatV2ConcurrentWriters(t *testing.T) {
	for _, writer := range []string{"set", "remove", "clear", "selection", "checkout", "legacy", "v2 same key", "v2 different body"} {
		t.Run(writer, func(t *testing.T) {
			d := newDatabase(t, true)
			input := repeatReview(t, d)
			blocker := d.lockProduct(t, "p01")
			repeated := make(chan error, 1)
			go func() { _, err := d.service.CommitRepeatV2(d.ctx, "usr-demo", "ord-2026-002", input); repeated <- err }()
			d.waitForBlocked(t, 1)
			other := make(chan error, 1)
			go func() {
				var err error
				switch writer {
				case "set":
					_, err = d.store.SetCartItem(d.ctx, "usr-demo", "p01", 7, false)
				case "remove":
					_, err = d.store.RemoveCartItem(d.ctx, "usr-demo", "p01")
				case "clear":
					_, err = d.store.ClearCart(d.ctx, "usr-demo", false)
				case "selection":
					_, err = d.store.SetCartSelection(d.ctx, "usr-demo", nil, false)
				case "checkout":
					_, err = d.service.Checkout(d.ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "Test"})
				case "legacy":
					_, err = d.service.CommitRepeat(d.ctx, "usr-demo", "ord-2026-002", []domain.RepeatSelection{{ProductID: "p01", Quantity: 1, Selected: true}})
				default:
					request := input
					request.Items = append([]domain.RepeatV2Selection(nil), input.Items...)
					if writer == "v2 different body" {
						request.Items[0].Quantity++
					}
					_, err = d.service.CommitRepeatV2(d.ctx, "usr-demo", "ord-2026-002", request)
				}
				other <- err
			}()
			// Repeat holds the common user lock while waiting for the product.
			// Every other Cart writer, including clear/remove/selection, must wait.
			d.waitForBlocked(t, 2)
			if err := blocker.Commit(d.ctx); err != nil {
				t.Fatal(err)
			}
			err := receiveError(t, d.ctx, repeated)
			if err != nil {
				t.Fatal(err)
			}
			err = receiveError(t, d.ctx, other)
			if writer == "v2 different body" {
				if !errors.Is(err, domain.ErrIdempotencyKeyReused) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if writer == "v2 same key" || writer == "v2 different body" {
				if quantity(d.cart(t, "usr-demo"), "p01") != 4 || d.number(t, `SELECT count(*) FROM repeat_commits`) != 1 {
					t.Fatal("key race added twice")
				}
			}
		})
	}
}
