//go:build integration

package integration_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

// Synchronize on actual PostgreSQL lock waits, never an assumed scheduling delay.
func (d *database) waitForBlocked(t *testing.T, count int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(d.ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var got int
		err := d.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND cardinality(pg_blocking_pids(pid))>0`).Scan(&got)
		if err != nil {
			t.Fatal(err)
		}
		if got >= count {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wanted %d blocked transactions, saw %d", count, got)
		case <-ticker.C:
		}
	}
}
func (d *database) lockProduct(t *testing.T, id string) pgx.Tx {
	t.Helper()
	tx, err := d.pool.Begin(d.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(d.ctx, "SELECT id FROM products WHERE id=$1 FOR UPDATE", id); err != nil {
		t.Fatal(err)
	}
	return tx
}
func receiveError(t *testing.T, ctx context.Context, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		t.Fatal("transaction did not finish")
		return ctx.Err()
	}
}

func TestConcurrentCheckouts(t *testing.T) {
	for _, sameCustomer := range []bool{false, true} {
		name := "two customers"
		if sameCustomer {
			name = "same customer"
		}
		t.Run(name, func(t *testing.T) {
			d := newDatabase(t, true)
			d.customer(t, "buyer-a")
			d.customer(t, "buyer-b")
			d.exec(t, "UPDATE products SET stock=3 WHERE id='p01'")
			d.set(t, "buyer-a", "p01", 2, true)
			userB := "buyer-b"
			if sameCustomer {
				userB = "buyer-a"
			} else {
				d.set(t, userB, "p01", 2, true)
			}
			blocker := d.lockProduct(t, "p01")
			results := make(chan error, 2)
			for _, user := range []string{"buyer-a", userB} {
				go func(user string) {
					_, err := d.service.Checkout(d.ctx, user, domain.CheckoutInput{DeliveryAddress: "Test"})
					results <- err
				}(user)
			}
			d.waitForBlocked(t, 2)
			if err := blocker.Commit(d.ctx); err != nil {
				t.Fatal(err)
			}
			success, failure := 0, 0
			for i := 0; i < 2; i++ {
				err := receiveError(t, d.ctx, results)
				if err == nil {
					success++
				} else {
					want := domain.ErrOutOfStock
					if sameCustomer {
						want = domain.ErrConflict
					}
					if !errors.Is(err, want) {
						t.Fatalf("unexpected concurrent failure: %v", err)
					}
					failure++
				}
			}
			if success != 1 || failure != 1 || d.number(t, "SELECT stock FROM products WHERE id='p01'") != 1 || d.number(t, "SELECT count(*) FROM orders WHERE user_id IN ('buyer-a','buyer-b')") != 1 {
				t.Fatal("oversell or duplicate checkout")
			}
			remaining := d.number(t, "SELECT COALESCE(sum(quantity),0) FROM cart_items WHERE user_id IN ('buyer-a','buyer-b')")
			want := int64(2)
			if sameCustomer {
				want = 0
			}
			if remaining != want {
				t.Fatal("losing customer's cart changed")
			}
		})
	}
}

func TestRepeatConcurrentStockUpdate(t *testing.T) {
	d := newDatabase(t, true)
	// Uncommitted stock change is invisible to preview, but commit must wait and recheck it.
	blocker := d.lockProduct(t, "p07")
	if _, err := blocker.Exec(d.ctx, "UPDATE products SET stock=0 WHERE id='p07'"); err != nil {
		t.Fatal(err)
	}
	before := d.cart(t, "usr-demo")
	done := make(chan error, 1)
	go func() {
		_, err := d.service.CommitRepeat(d.ctx, "usr-demo", "ord-2026-002", []domain.RepeatSelection{{ProductID: "p01", Quantity: 1, Selected: true}, {ProductID: "p07", Quantity: 4, Selected: true}})
		done <- err
	}()
	d.waitForBlocked(t, 1)
	if err := blocker.Commit(d.ctx); err != nil {
		t.Fatal(err)
	}
	if err := receiveError(t, d.ctx, done); !errors.Is(err, domain.ErrOutOfStock) {
		t.Fatalf("stale preview should fail at transaction boundary: %v", err)
	}
	after := d.cart(t, "usr-demo")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("repeat partially changed cart after stock race")
	}
	if d.number(t, "SELECT stock FROM products WHERE id='p01'") != 24 {
		t.Fatal("repeat reserved inventory")
	}
}

// Pause only between service validation and the real repository transaction.
// No fake database operation or production synchronization hook is involved.
type gatedRepeat struct {
	domain.Repository
	entered chan struct{}
	resume  chan struct{}
}

func (g *gatedRepeat) AddCartItems(ctx context.Context, user string, m []domain.CartMutation) (domain.Cart, error) {
	close(g.entered)
	select {
	case <-g.resume:
	case <-ctx.Done():
		return domain.Cart{}, ctx.Err()
	}
	return g.Repository.AddCartItems(ctx, user, m)
}
func TestRepeatConcurrentCheckout(t *testing.T) {
	d := newDatabase(t, true)
	d.customer(t, "buyer")
	d.set(t, "buyer", "p07", 4, true)
	before := d.cart(t, "usr-demo")
	gate := &gatedRepeat{Repository: d.store, entered: make(chan struct{}), resume: make(chan struct{})}
	repeat := application.New(gate)
	repeated := make(chan error, 1)
	go func() {
		_, err := repeat.CommitRepeat(d.ctx, "usr-demo", "ord-2026-002", []domain.RepeatSelection{{ProductID: "p01", Quantity: 1, Selected: true}, {ProductID: "p07", Quantity: 4, Selected: true}})
		repeated <- err
	}()
	select {
	case <-gate.entered:
	case <-d.ctx.Done():
		t.Fatal("preview did not complete")
	}
	blocker := d.lockProduct(t, "p07")
	checked := make(chan error, 1)
	go func() {
		_, err := d.service.Checkout(d.ctx, "buyer", domain.CheckoutInput{DeliveryAddress: "Test"})
		checked <- err
	}()
	d.waitForBlocked(t, 1)
	close(gate.resume)
	d.waitForBlocked(t, 2)
	if err := blocker.Commit(d.ctx); err != nil {
		t.Fatal(err)
	}
	if err := receiveError(t, d.ctx, checked); err != nil {
		t.Fatal(err)
	}
	if err := receiveError(t, d.ctx, repeated); !errors.Is(err, domain.ErrOutOfStock) {
		t.Fatalf("repeat after checkout: %v", err)
	}
	if !reflect.DeepEqual(before, d.cart(t, "usr-demo")) {
		t.Fatal("repeat cart partially changed")
	}
	if d.number(t, "SELECT stock FROM products WHERE id='p07'") != 0 || len(d.cart(t, "buyer").Items) != 0 {
		t.Fatal("checkout did not commit consistently")
	}
}
