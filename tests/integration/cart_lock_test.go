//go:build integration

package integration_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	postgres "sweetshop/internal/repository/postgres"
)

// Pause real repository SQL immediately before commit/rollback. No production
// hooks, synthetic Cart writes, sleeps or assumed goroutine scheduling.
type transactionGate struct {
	statement              string
	entered, resume        chan struct{}
	enterOnce, releaseOnce sync.Once
}

func (g *transactionGate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.EqualFold(strings.TrimSpace(data.SQL), g.statement) {
		g.enterOnce.Do(func() {
			close(g.entered)
			select {
			case <-g.resume:
			case <-ctx.Done():
			}
		})
	}
	return ctx
}
func (*transactionGate) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func (g *transactionGate) release()                                                      { g.releaseOnce.Do(func() { close(g.resume) }) }
func (g *transactionGate) wait(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-g.entered:
	case <-ctx.Done():
		t.Fatal("transaction did not reach gate")
	}
}

func (d *database) gatedStore(t *testing.T, statement string) (*postgres.Store, *transactionGate) {
	t.Helper()
	g := &transactionGate{statement: statement, entered: make(chan struct{}), resume: make(chan struct{})}
	config := d.pool.Config()
	config.ConnConfig.Tracer = g
	pool, err := pgxpool.NewWithConfig(d.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	t.Cleanup(g.release)
	return postgres.New(pool), g
}

func checkoutReviewP01(t *testing.T, d *database) domain.CheckoutInput {
	t.Helper()
	if _, err := d.store.ClearCart(d.ctx, "usr-demo", false); err != nil {
		t.Fatal(err)
	}
	d.set(t, "usr-demo", "p01", 1, true)
	d.set(t, "usr-demo", "p02", 1, false)
	items, total := d.reviewOf(t, "usr-demo")
	return domain.CheckoutInput{DeliveryAddress: "Test", ExpectedItems: items, ExpectedTotalMinor: total, IdempotencyKey: keyRef(idempotencyKey)}
}

func cartWriter(ctx context.Context, store *postgres.Store, kind string) error {
	var err error
	switch kind {
	case "insert":
		_, err = store.SetCartItem(ctx, "usr-demo", "p02", 1, true)
	case "select":
		_, err = store.SetCartSelection(ctx, "usr-demo", []string{"p02"}, true)
	case "quantity":
		_, err = store.SetCartItem(ctx, "usr-demo", "p01", 3, true)
	case "deselect":
		_, err = store.SetCartSelection(ctx, "usr-demo", []string{"p01"}, false)
	case "remove":
		_, err = store.RemoveCartItem(ctx, "usr-demo", "p01")
	case "clear":
		_, err = store.ClearCart(ctx, "usr-demo", false)
	}
	return err
}

func TestCheckoutCartWriterCommitsBeforeLock(t *testing.T) {
	for _, writer := range []string{"insert", "select", "quantity"} {
		t.Run(writer, func(t *testing.T) {
			d := newDatabase(t, true)
			input := checkoutReviewP01(t, d)
			if writer == "insert" {
				if _, err := d.store.RemoveCartItem(d.ctx, "usr-demo", "p02"); err != nil {
					t.Fatal(err)
				}
			}
			before := d.purchaseState(t, "usr-demo")
			store, gate := d.gatedStore(t, "commit")
			written := make(chan error, 1)
			go func() { written <- cartWriter(d.ctx, store, writer) }()
			gate.wait(t, d.ctx)
			checked := make(chan error, 1)
			go func() { _, err := d.service.Checkout(d.ctx, "usr-demo", input); checked <- err }()
			d.waitForBlocked(t, 1) // Checkout waits on the writer's user-row lock.
			gate.release()
			if err := receiveError(t, d.ctx, written); err != nil {
				t.Fatal(err)
			}
			if err := receiveError(t, d.ctx, checked); !errors.Is(err, domain.ErrCheckoutChanged) {
				t.Fatal(err)
			}
			after := d.purchaseState(t, "usr-demo")
			cart := after.cart
			before.cart = cart
			if !reflect.DeepEqual(before, after) {
				t.Fatal("stale checkout wrote order/stock/receipt/sequence")
			}
			want := 1
			if writer == "quantity" {
				want = 3
			}
			if quantity(cart, "p01") != want || quantity(cart, "p02") != 1 {
				t.Fatal("checkout consumed Cart")
			}
			if writer != "quantity" {
				for _, line := range cart.Items {
					if line.Product.ID == "p02" && !line.Selected {
						t.Fatal("p02 not selected")
					}
				}
			}
		})
	}
}

func TestCheckoutHoldsCartLockThroughFinish(t *testing.T) {
	for _, finish := range []string{"commit", "rollback"} {
		for _, writer := range []string{"insert", "select", "quantity", "deselect", "remove", "clear"} {
			t.Run(finish+"/"+writer, func(t *testing.T) {
				d := newDatabase(t, true)
				input := checkoutReviewP01(t, d)
				if writer == "insert" {
					if _, err := d.store.RemoveCartItem(d.ctx, "usr-demo", "p02"); err != nil {
						t.Fatal(err)
					}
				}
				before := d.purchaseState(t, "usr-demo")
				if finish == "rollback" {
					wrong := *input.ExpectedTotalMinor + 1
					input.ExpectedTotalMinor = &wrong
				}
				store, gate := d.gatedStore(t, finish)
				service := application.New(store, application.Options{AllowDemoAccount: true})
				checked := make(chan error, 1)
				go func() { _, err := service.Checkout(d.ctx, "usr-demo", input); checked <- err }()
				gate.wait(t, d.ctx)
				written := make(chan error, 1)
				go func() { written <- cartWriter(d.ctx, d.store, writer) }()
				d.waitForBlocked(t, 1)
				if !reflect.DeepEqual(before, d.purchaseState(t, "usr-demo")) && finish == "rollback" {
					t.Fatal("rollback path wrote state")
				}
				select {
				case err := <-written:
					t.Fatalf("writer finished before checkout: %v", err)
				default:
				}
				gate.release()
				err := receiveError(t, d.ctx, checked)
				if finish == "rollback" {
					if !errors.Is(err, domain.ErrCheckoutChanged) {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if err := receiveError(t, d.ctx, written); err != nil {
					t.Fatal(err)
				}
				after := d.purchaseState(t, "usr-demo")
				if finish == "commit" {
					if after.orders != before.orders+1 || after.stockP01 != before.stockP01-1 {
						t.Fatal("checkout did not commit once")
					}
				} else {
					cart := after.cart
					before.cart = cart
					if !reflect.DeepEqual(before, after) {
						t.Fatal("failed checkout wrote")
					}
				}
				cart := after.cart
				if writer == "insert" || writer == "select" {
					if quantity(cart, "p02") != 1 {
						t.Fatal("p02 writer lost")
					}
					for _, line := range cart.Items {
						if line.Product.ID == "p02" && !line.Selected {
							t.Fatal("selection lost")
						}
					}
				}
				if writer == "quantity" && quantity(cart, "p01") != 3 {
					t.Fatal("absolute quantity writer lost")
				}
				if writer == "remove" && quantity(cart, "p01") != 0 {
					t.Fatal("remove lost")
				}
				if writer == "clear" && len(cart.Items) != 0 {
					t.Fatal("clear lost")
				}
			})
		}
	}
}

func TestRepeatCartQuantityLockOrder(t *testing.T) {
	for _, first := range []string{"quantity", "repeat"} {
		t.Run(first, func(t *testing.T) {
			d := newDatabase(t, true)
			input := repeatReview(t, d)
			store, gate := d.gatedStore(t, "commit")
			service := application.New(store, application.Options{AllowDemoAccount: true})
			repeated, written := make(chan error, 1), make(chan error, 1)
			if first == "quantity" {
				go func() { written <- cartWriter(d.ctx, store, "quantity") }()
				gate.wait(t, d.ctx)
				go func() { _, err := d.service.CommitRepeatV2(d.ctx, "usr-demo", "ord-2026-002", input); repeated <- err }()
			} else {
				go func() { _, err := service.CommitRepeatV2(d.ctx, "usr-demo", "ord-2026-002", input); repeated <- err }()
				gate.wait(t, d.ctx)
				go func() { written <- cartWriter(d.ctx, d.store, "quantity") }()
			}
			d.waitForBlocked(t, 1)
			gate.release()
			if err := receiveError(t, d.ctx, written); err != nil {
				t.Fatal(err)
			}
			err := receiveError(t, d.ctx, repeated)
			want := int64(1)
			if first == "quantity" {
				want = 0
				if !errors.Is(err, domain.ErrRepeatChanged) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if quantity(d.cart(t, "usr-demo"), "p01") != 3 || d.number(t, `SELECT count(*) FROM repeat_commits`) != want {
				t.Fatal("bad serialized Repeat result")
			}
		})
	}
}

func TestCartLockIndependentUsersAndForeignKeys(t *testing.T) {
	d := newDatabase(t, true)
	d.customer(t, "other")
	tx, err := d.pool.Begin(d.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(d.ctx, `SELECT id FROM users WHERE id='usr-demo' FOR NO KEY UPDATE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(d.ctx, 3*time.Second)
	defer cancel()
	if _, err = d.store.SetCartItem(ctx, "other", "p02", 1, true); err != nil {
		t.Fatal("different user blocked", err)
	}
	if _, err = d.store.SetFavorite(ctx, "usr-demo", "p03", true); err != nil {
		t.Fatal("foreign-key reader blocked", err)
	}
	if _, err = d.store.Cart(ctx, "usr-demo"); err != nil {
		t.Fatal("Cart GET blocked", err)
	}
}

func TestCheckoutBeforeRepeatCartLock(t *testing.T) {
	for _, version := range []string{"legacy", "v2"} {
		t.Run(version, func(t *testing.T) {
			d := newDatabase(t, true)
			input := checkoutReviewP01(t, d)
			repeat := repeatReview(t, d)
			store, gate := d.gatedStore(t, "commit")
			service := application.New(store, application.Options{AllowDemoAccount: true})
			checked, repeated := make(chan error, 1), make(chan error, 1)
			go func() { _, err := service.Checkout(d.ctx, "usr-demo", input); checked <- err }()
			gate.wait(t, d.ctx)
			go func() {
				var err error
				if version == "legacy" {
					_, err = d.service.CommitRepeat(d.ctx, "usr-demo", "ord-2026-002", []domain.RepeatSelection{{ProductID: "p01", Quantity: 1, Selected: true}})
				} else {
					_, err = d.service.CommitRepeatV2(d.ctx, "usr-demo", "ord-2026-002", repeat)
				}
				repeated <- err
			}()
			d.waitForBlocked(t, 1)
			gate.release()
			if err := receiveError(t, d.ctx, checked); err != nil {
				t.Fatal(err)
			}
			err := receiveError(t, d.ctx, repeated)
			if version == "legacy" {
				if err != nil || quantity(d.cart(t, "usr-demo"), "p01") != 1 {
					t.Fatal("legacy addition changed", err)
				}
			} else if !errors.Is(err, domain.ErrRepeatChanged) || quantity(d.cart(t, "usr-demo"), "p01") != 0 || d.number(t, `SELECT count(*) FROM repeat_commits`) != 0 {
				t.Fatal("stale Repeat committed", err)
			}
		})
	}
}

func TestCartLockAllowsProductThenUserForeignKey(t *testing.T) {
	d := newDatabase(t, true)
	input := checkoutReviewP01(t, d)
	tx, err := d.pool.Begin(d.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(d.ctx, `SELECT id FROM products WHERE id='p01' FOR KEY SHARE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := d.service.Checkout(d.ctx, "usr-demo", input); done <- err }()
	d.waitForBlocked(t, 1)
	// Checkout owns the user lock and waits for this product. User FK KEY SHARE
	// must remain compatible, otherwise these two transactions form a cycle.
	ctx, cancel := context.WithTimeout(d.ctx, 3*time.Second)
	defer cancel()
	if _, err = tx.Exec(ctx, `INSERT INTO favorites(user_id,product_id) VALUES('usr-demo','p01')`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(d.ctx); err != nil {
		t.Fatal(err)
	}
	if err = receiveError(t, d.ctx, done); err != nil {
		t.Fatal(err)
	}
}

func TestCommittedReplaysDoNotWaitForCartLock(t *testing.T) {
	d := newDatabase(t, true)
	repeat := repeatReview(t, d)
	if _, err := d.service.CommitRepeatV2(d.ctx, "usr-demo", "ord-2026-002", repeat); err != nil {
		t.Fatal(err)
	}
	checkout := domain.CheckoutInput{DeliveryAddress: "Test", IdempotencyKey: keyRef(idempotencyKey)}
	if _, err := d.service.Checkout(d.ctx, "usr-demo", checkout); err != nil {
		t.Fatal(err)
	}
	tx, err := d.pool.Begin(d.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(d.ctx, `SELECT id FROM users WHERE id='usr-demo' FOR NO KEY UPDATE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(d.ctx, 3*time.Second)
	defer cancel()
	if replay, err := d.service.Checkout(ctx, "usr-demo", checkout); err != nil || !replay.Replayed {
		t.Fatal("Checkout replay blocked", err)
	}
	if replay, err := d.service.CommitRepeatV2(ctx, "usr-demo", "ord-2026-002", repeat); err != nil || !replay.Replayed {
		t.Fatal("Repeat replay blocked", err)
	}
}
