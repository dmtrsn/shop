//go:build integration

package integration_test

import (
	"errors"
	"reflect"
	"testing"

	"sweetshop/internal/domain"
)

func TestCheckoutSelectionAndHistoricalSnapshots(t *testing.T) {
	d := newDatabase(t, true)
	before := d.cart(t, "usr-demo")
	order, err := d.service.Checkout(d.ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "  Test address  ", Comment: "Test comment"})
	if err != nil {
		t.Fatal(err)
	}
	if order.TotalMinor != 246500 || order.TotalMinor != before.Summary.TotalMinor || order.ItemCount != 5 || len(order.Items) != 2 || order.DeliveryAddress != "Test address" || order.Comment != "Test comment" {
		t.Fatalf("unexpected checkout: %+v", order)
	}
	for _, item := range order.Items {
		var original domain.Product
		for _, line := range before.Items {
			if line.Product.ID == item.ProductID {
				original = line.Product
			}
		}
		if item.SKU != original.SKU || item.Name != original.Name || item.Image != original.Image || item.Manufacturer != original.Manufacturer.Name || item.ManufacturerID != original.Manufacturer.ID || item.UnitPriceMinor != original.PriceMinor || item.LineTotalMinor != int64(item.Quantity)*original.PriceMinor {
			t.Fatalf("snapshot mismatch: %+v", item)
		}
		if got := d.number(t, "SELECT stock FROM products WHERE id=$1", item.ProductID); got != int64(original.Stock-item.Quantity) {
			t.Fatalf("stock=%d", got)
		}
	}
	remaining := d.cart(t, "usr-demo")
	if len(remaining.Items) != 2 || quantity(remaining, "p16") != 1 || quantity(remaining, "p18") != 2 {
		t.Fatal("checkout removed unrelated lines")
	}
	d.exec(t, "UPDATE products SET name='Changed name',sku='CHANGED-SKU',image='/changed.webp',price_minor=1 WHERE id='p01'")
	d.exec(t, "UPDATE manufacturers SET name='Changed manufacturer' WHERE id='m-choco'")
	d.exec(t, "DELETE FROM products WHERE id='p10'")
	loaded, err := d.service.Order(d.ctx, "usr-demo", order.Number)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Items, order.Items) || loaded.TotalMinor != order.TotalMinor {
		t.Fatal("live changes modified historical snapshots")
	}
	d.customer(t, "other")
	if _, err := d.service.Order(d.ctx, "other", order.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign order: %v", err)
	}
}

func TestCheckoutFailureIsAtomic(t *testing.T) {
	d := newDatabase(t, true)
	// p01 is scanned before p10: fail after at least one eligible line.
	d.exec(t, "UPDATE products SET stock=2 WHERE id='p10'")
	before := d.cart(t, "usr-demo")
	_, err := d.service.Checkout(d.ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "Test"})
	if !errors.Is(err, domain.ErrOutOfStock) {
		t.Fatalf("error=%v", err)
	}
	if !reflect.DeepEqual(before, d.cart(t, "usr-demo")) {
		t.Fatal("failed checkout changed cart")
	}
	if d.number(t, "SELECT count(*) FROM orders") != 1 || d.number(t, "SELECT count(*) FROM order_items") != 4 || d.number(t, "SELECT stock FROM products WHERE id='p01'") != 24 {
		t.Fatal("failed checkout made partial purchase")
	}
	if _, err := d.service.SetCartSelection(d.ctx, "usr-demo", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := d.service.Checkout(d.ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "Test"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("empty selection: %v", err)
	}
}

func TestRepeatPreviewAndExplicitCommit(t *testing.T) {
	d := newDatabase(t, true)
	before := d.cart(t, "usr-demo")
	preview, err := d.service.RepeatPreview(d.ctx, "usr-demo", "ord-2026-002")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, d.cart(t, "usr-demo")) {
		t.Fatal("preview mutated cart")
	}
	if preview.RequestedCount != 19 || preview.AvailableCount != 1 || preview.LimitedCount != 1 || preview.UnavailableCount != 2 || preview.EstimatedTotalMinor != 653800 {
		t.Fatalf("preview counters: %+v", preview)
	}
	byID := map[string]domain.RepeatItem{}
	for _, item := range preview.Items {
		byID[item.Product.ID] = item
	}
	if p := byID["p01"]; p.Status != "available" || p.AlreadyInCartQuantity != 2 || p.PriceDeltaMinor != 3000 || !p.PriceChanged {
		t.Fatalf("available: %+v", p)
	}
	if p := byID["p07"]; p.Status != "limited" || p.SuggestedQuantity != 4 || p.RequestedQuantity != 12 {
		t.Fatalf("limited: %+v", p)
	}
	if p := byID["p18"]; p.Status != "unavailable" || p.Selected {
		t.Fatalf("unavailable: %+v", p)
	}
	if p := byID["legacy-01"]; p.Status != "discontinued" || p.Selected || p.Replacement == nil || p.Replacement.ID != "p06" {
		t.Fatalf("discontinued: %+v", p)
	}
	for _, choices := range [][]domain.RepeatSelection{
		{{ProductID: "p01", Quantity: 1, Selected: true}, {ProductID: "p07", Quantity: 12, Selected: true}},
		{{ProductID: "p18", Quantity: 1, Selected: true}},
		{{ProductID: "legacy-01", Quantity: 1, Selected: true}},
		{{ProductID: "p06", Quantity: 1, Selected: true}},
		{{ProductID: "p01", Quantity: 1, Selected: true}, {ProductID: "p01", Selected: false}},
	} {
		if _, err := d.service.CommitRepeat(d.ctx, "usr-demo", preview.OrderID, choices); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("invalid choices: %v", err)
		}
		if !reflect.DeepEqual(before, d.cart(t, "usr-demo")) {
			t.Fatal("invalid repeat changed cart")
		}
	}
	result, err := d.service.CommitRepeat(d.ctx, "usr-demo", preview.OrderID, []domain.RepeatSelection{{ProductID: "p01", Quantity: 2, Selected: true}, {ProductID: "p07", Selected: false}})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedCount != 1 || result.AddedQuantity != 2 || result.SkippedCount != 1 || quantity(result.Cart, "p01") != 4 || quantity(result.Cart, "p07") != 0 {
		t.Fatalf("explicit subset ignored: %+v", result)
	}
	if d.number(t, "SELECT stock FROM products WHERE id='p01'") != 24 {
		t.Fatal("repeat must not reserve stock")
	}
}

func TestRepeatFinalQuantityRulesAndLegacyDefaults(t *testing.T) {
	d := newDatabase(t, true)
	d.exec(t, "UPDATE products SET minimum_quantity=3,quantity_step=2 WHERE id='p01'")
	d.set(t, "usr-demo", "p01", 3, false)
	before := d.cart(t, "usr-demo")
	// Additions themselves need not meet minimum: 3 existing + 2 added = 5.
	for _, addition := range []int{1, 3} {
		if _, err := d.service.CommitRepeat(d.ctx, "usr-demo", "ord-2026-002", []domain.RepeatSelection{{ProductID: "p01", Quantity: addition, Selected: true}}); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("quantity rule: %v", err)
		}
		if !reflect.DeepEqual(before, d.cart(t, "usr-demo")) {
			t.Fatal("invalid quantity mutated cart")
		}
	}
	result, err := d.service.CommitRepeat(d.ctx, "usr-demo", "ord-2026-002", []domain.RepeatSelection{{ProductID: "p01", Quantity: 2, Selected: true}})
	if err != nil {
		t.Fatal(err)
	}
	if quantity(result.Cart, "p01") != 5 {
		t.Fatal("repeat addition not applied")
	}
	for _, item := range result.Cart.Items {
		if item.Product.ID == "p01" && !item.Selected {
			t.Fatal("repeat should select an added line")
		}
	}
	result, err = d.service.CommitRepeat(d.ctx, "usr-demo", "ord-2026-002", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedCount != 2 || result.SkippedCount != 2 || quantity(result.Cart, "p01") != 7 || quantity(result.Cart, "p07") != 4 || quantity(result.Cart, "p18") != 2 {
		t.Fatal("legacy empty selection defaults changed")
	}
	// All false is an explicit no-op; unlike missing/empty items.
	before = d.cart(t, "usr-demo")
	if _, err := d.service.CommitRepeat(d.ctx, "usr-demo", "ord-2026-002", []domain.RepeatSelection{{ProductID: "p01", Selected: false}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, d.cart(t, "usr-demo")) {
		t.Fatal("all false changed cart")
	}
}

func TestRepeatRepositoryBatchRollback(t *testing.T) {
	d := newDatabase(t, true)
	before := d.cart(t, "usr-demo")
	for _, mutations := range [][]domain.CartMutation{
		{{ProductID: "p01", Quantity: 1, Selected: true}, {ProductID: "p07", Quantity: 5, Selected: true}},
		{{ProductID: "p01", Quantity: 1}, {ProductID: "p01", Quantity: 1}},
		{{ProductID: "p01", Quantity: 1}, {ProductID: "missing", Quantity: 1}},
	} {
		if _, err := d.store.AddCartItems(d.ctx, "usr-demo", mutations); err == nil {
			t.Fatal("invalid batch succeeded")
		}
		if !reflect.DeepEqual(before, d.cart(t, "usr-demo")) {
			t.Fatal("repository batch partially changed cart")
		}
	}
}

func TestCheckoutWriteFailureRollsBackOrderStockAndCart(t *testing.T) {
	d := newDatabase(t, true)
	before := d.cart(t, "usr-demo")
	// Force an error after the real checkout inserted order and snapshot rows.
	d.exec(t, `CREATE FUNCTION reject_test_stock_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected integration failure'; END $$;
 CREATE TRIGGER reject_test_stock_change BEFORE UPDATE OF stock ON products FOR EACH ROW EXECUTE FUNCTION reject_test_stock_change();`)
	if _, err := d.service.Checkout(d.ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "Test"}); err == nil {
		t.Fatal("injected write failure was ignored")
	}
	if d.number(t, "SELECT count(*) FROM orders") != 1 || d.number(t, "SELECT count(*) FROM order_items") != 4 || d.number(t, "SELECT stock FROM products WHERE id='p01'") != 24 || d.number(t, "SELECT stock FROM products WHERE id='p10'") != 42 {
		t.Fatal("write failure left partial order or stock changes")
	}
	if !reflect.DeepEqual(before, d.cart(t, "usr-demo")) {
		t.Fatal("write failure changed cart")
	}
}
