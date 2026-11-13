//go:build integration

package integration_test

import (
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"reflect"
	"testing"

	"sweetshop/internal/application"
	"sweetshop/internal/delivery/httpapi"
	"sweetshop/internal/domain"
	"sweetshop/tests/contract"
)

const cartOwnerStatement = "SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE"

func (d *database) cartHTTP(t *testing.T, repo domain.Repository) (http.Handler, map[string]string) {
	t.Helper()
	login, err := d.service.Login(d.ctx, "demo@sugar.ru", "Sweet2026!")
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(application.New(repo, application.Options{AllowDemoAccount: true}), slog.New(slog.NewTextHandler(io.Discard, nil)), "../../web", "postgres", contract.SessionConfig())
	return handler, map[string]string{"Authorization": "Bearer " + login.Token}
}

// The SQL tracer pauses A immediately before the owner lock. On the old path,
// A has already read/merged the cart and validated the product at this point.
// B uses the real repository, commits, and only then is A allowed to continue.
func TestCartPatchConcurrentSelectionAndRemoval(t *testing.T) {
	for _, mutation := range []string{"deselect", "remove", "quantity"} {
		t.Run(mutation, func(t *testing.T) {
			d := newDatabase(t, true)
			d.set(t, "usr-demo", "p01", 2, true)
			store, gate := d.gatedStore(t, cartOwnerStatement)
			handler, headers := d.cartHTTP(t, store)
			body := `{"quantity":4}`
			if mutation == "quantity" {
				body = `{"selected":false}`
			}
			result := make(chan exchange, 1)
			go func() { result <- serve(handler, "PATCH", "/api/cart/items/p01", body, headers) }()
			gate.wait(t, d.ctx)
			if err := cartWriter(d.ctx, d.store, mutation); err != nil {
				t.Fatal(err)
			}
			gate.release()
			var response exchange
			select {
			case response = <-result:
			case <-d.ctx.Done():
				t.Fatal("PATCH did not finish")
			}
			if mutation == "remove" {
				if response.Code != http.StatusNotFound || quantity(d.cart(t, "usr-demo"), "p01") != 0 {
					t.Fatalf("removed line recreated or wrong error: status=%d quantity=%d", response.Code, quantity(d.cart(t, "usr-demo"), "p01"))
				}
				return
			}
			if response.Code != http.StatusOK {
				t.Fatalf("PATCH: %d %s", response.Code, response.Body.String())
			}
			wantQuantity := 4
			if mutation == "quantity" {
				wantQuantity = 3
			}
			for _, line := range d.cart(t, "usr-demo").Items {
				if line.Product.ID == "p01" {
					if line.Quantity != wantQuantity || line.Selected {
						t.Fatalf("omitted field overwritten: quantity=%d selected=%v", line.Quantity, line.Selected)
					}
					return
				}
			}
			t.Fatal("cart line missing")
		})
	}
}

func TestCartQuantityRevalidatesLockedRules(t *testing.T) {
	for _, method := range []string{"POST", "PATCH"} {
		for _, rule := range []string{"minimum", "step"} {
			t.Run(method+"/"+rule, func(t *testing.T) {
				d := newDatabase(t, true)
				d.set(t, "usr-demo", "p01", 2, true)
				store, gate := d.gatedStore(t, cartOwnerStatement)
				handler, headers := d.cartHTTP(t, store)
				path, body := "/api/cart/items/p01", `{"quantity":4}`
				if method == "POST" {
					path, body = "/api/cart/items", `{"productId":"p01","quantity":4}`
				}
				result := make(chan exchange, 1)
				go func() { result <- serve(handler, method, path, body, headers) }()
				gate.wait(t, d.ctx)
				if rule == "minimum" {
					d.exec(t, `UPDATE products SET minimum_quantity=5 WHERE id='p01'`)
				} else {
					d.exec(t, `UPDATE products SET quantity_step=2 WHERE id='p01'`)
				}
				before := d.cart(t, "usr-demo")
				gate.release()
				select {
				case response := <-result:
					if response.Code != http.StatusUnprocessableEntity || response.body["error"].(map[string]any)["code"] != "validation_error" {
						t.Errorf("stale rules authorized write: status=%d", response.Code)
					}
				case <-d.ctx.Done():
					t.Fatal("quantity mutation did not finish")
				}
				if !reflect.DeepEqual(before, d.cart(t, "usr-demo")) {
					t.Fatal("rejected quantity mutation changed cart")
				}
			})
		}
	}
}

// Exercise a real product-row lock wait as well as the pre-owner SQL gate above.
// MVCC reads allow POST's early validation to finish against the old rules;
// pg_blocking_pids proves the writer is waiting before the staff update commits.
func TestCartQuantityRulesChangedDuringProductLockWait(t *testing.T) {
	for _, patch := range []bool{false, true} {
		method := "POST"
		if patch {
			method = "PATCH"
		}
		for _, rule := range []string{"minimum", "step"} {
			t.Run(method+"/"+rule, func(t *testing.T) {
				d := newDatabase(t, true)
				before := d.cartRows(t)
				blocker := d.lockProduct(t, "p01")
				result := make(chan error, 1)
				go func() {
					var err error
					q := 4
					if patch {
						_, err = d.service.PatchCartItem(d.ctx, "usr-demo", "p01", domain.CartPatch{Quantity: &q})
					} else {
						_, err = d.service.SetCartItem(d.ctx, "usr-demo", "p01", q, true)
					}
					result <- err
				}()
				d.waitForBlocked(t, 1)
				sql := `UPDATE products SET minimum_quantity=5 WHERE id='p01'`
				if rule == "step" {
					sql = `UPDATE products SET quantity_step=2 WHERE id='p01'`
				}
				if _, err := blocker.Exec(d.ctx, sql); err != nil {
					t.Fatal(err)
				}
				if err := blocker.Commit(d.ctx); err != nil {
					t.Fatal(err)
				}
				if err := receiveError(t, d.ctx, result); !errors.Is(err, domain.ErrValidation) {
					t.Fatalf("locked quantity rules: %v", err)
				}
				if before != d.cartRows(t) {
					t.Fatal("rejected mutation changed persisted cart, including timestamps")
				}
			})
		}
	}
}

func (d *database) cartRows(t *testing.T) string {
	t.Helper()
	var rows string
	if err := d.pool.QueryRow(d.ctx, `SELECT coalesce(jsonb_agg(to_jsonb(ci) ORDER BY product_id),'[]'::jsonb)::text FROM cart_items ci WHERE user_id='usr-demo'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestCartLockedQuantityValidation(t *testing.T) {
	for _, patch := range []bool{false, true} {
		method := "POST"
		if patch {
			method = "PATCH"
		}
		t.Run(method, func(t *testing.T) {
			d := newDatabase(t, true)
			for _, tc := range []struct {
				name     string
				quantity int
				archived bool
				want     error
			}{
				{"zero", 0, false, domain.ErrValidation},
				{"negative", -1, false, domain.ErrValidation},
				{"below minimum", 2, false, domain.ErrValidation},
				{"step mismatch", 4, false, domain.ErrValidation},
				{"stock", 11, false, domain.ErrOutOfStock},
				{"overflow", math.MaxInt, false, domain.ErrOutOfStock},
				{"archived", 5, true, domain.ErrNotFound},
				{"locked valid", 7, false, nil},
			} {
				t.Run(tc.name, func(t *testing.T) {
					d.exec(t, `UPDATE products SET stock=10,minimum_quantity=3,quantity_step=2,archived_at=CASE WHEN $1 THEN now() ELSE NULL END WHERE id='p01'`, tc.archived)
					before := d.cartRows(t)
					var err error
					if patch {
						_, err = d.store.PatchCartItem(d.ctx, "usr-demo", "cha-701", domain.CartPatch{Quantity: &tc.quantity})
					} else {
						_, err = d.store.SetCartItem(d.ctx, "usr-demo", "p01", tc.quantity, true)
					}
					if !errors.Is(err, tc.want) {
						t.Fatalf("got %v, want %v", err, tc.want)
					}
					if tc.want != nil && before != d.cartRows(t) {
						t.Fatal("rejected mutation changed persisted cart")
					}
					if tc.want == nil && quantity(d.cart(t, "usr-demo"), "p01") != tc.quantity {
						t.Fatal("valid quantity was not persisted")
					}
				})
			}
			// Selection-only PATCH must bypass all quantity and availability rules.
			d.exec(t, `UPDATE products SET stock=0,minimum_quantity=9,quantity_step=3,archived_at=now() WHERE id='p01'`)
			selected := false
			cart, err := d.service.PatchCartItem(d.ctx, "usr-demo", "dark-chocolate-sea-salt", domain.CartPatch{Selected: &selected})
			if err != nil || quantity(cart, "p01") != 7 {
				t.Fatalf("deselect archived/invalid line: %v", err)
			}
			for _, line := range cart.Items {
				if line.Product.ID == "p01" && line.Selected {
					t.Fatal("archived/invalid line remained selected")
				}
			}
		})
	}
}
