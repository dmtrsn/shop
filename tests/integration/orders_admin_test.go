//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

// Staff order management on PostgreSQL (ADR 016). Orders are placed through
// the real checkout, so stock, revisions and snapshots are what customers get.

type line struct {
	product string
	qty     int
}

// placeOrder replaces the customer's cart with lines and checks out.
func (d *database) placeOrder(t *testing.T, user string, lines ...line) domain.Order {
	t.Helper()
	if _, err := d.service.ClearCart(d.ctx, user, false); err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		d.set(t, user, l.product, l.qty, true)
	}
	result, err := d.service.Checkout(d.ctx, user, domain.CheckoutInput{DeliveryAddress: "Москва, ул. Тестовая, 1, кв. 2"})
	if err != nil {
		t.Fatal(err)
	}
	return result.Order
}

func (d *database) orderRevision(t *testing.T, id string) int64 {
	return d.number(t, "SELECT revision FROM orders WHERE id=$1", id)
}

func (d *database) productRevision(t *testing.T, id string) int64 {
	return d.number(t, "SELECT revision FROM products WHERE id=$1", id)
}

func orderAudits(trail []auditRow) []auditRow {
	result := []auditRow{}
	for _, row := range trail {
		if row.entityType == "order" {
			result = append(result, row)
		}
	}
	return result
}

func TestStaffReadsOrdersAndCustomersCannot(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	order := d.placeOrder(t, "usr-demo", line{"p03", 2}, line{"p05", 1})
	customerCookie := cookieLogin(t, handler, "demo@sugar.ru", "Sweet2026!")
	for _, headers := range []map[string]string{nil, {"Cookie": shopCookie + "=" + customerCookie}, {"Authorization": "Bearer " + customerCookie}} {
		for _, path := range []string{"/api/admin/v1/orders", "/api/admin/v1/orders/" + order.ID} {
			if ex := serve(handler, "GET", path, "", headers); ex.Code != 401 {
				t.Fatalf("non-staff %s: %d", path, ex.Code)
			}
		}
		if ex := serve(handler, "POST", "/api/admin/v1/orders/"+order.ID+"/cancel", `{"expectedRevision":1}`, headers); ex.Code != 401 {
			t.Fatalf("non-staff cancel: %d", ex.Code)
		}
	}
	login := d.signedInStaff(t, handler)
	// A substring of the customer's name. Case folding of non-ASCII letters
	// follows the database locale (this test cluster is C), so the substring
	// skips the capital first letter; email matching is checked case-insensitively.
	first := []rune(strings.Fields(d.text(t, "SELECT name FROM users WHERE id='usr-demo'"))[0])
	name := string(first[1:])
	for _, q := range []string{order.ID, order.Number, strings.TrimPrefix(order.Number, "#"), "SUGAR.RU", name} {
		ex := adminRequest(handler, login, "GET", "/api/admin/v1/orders?q="+url.QueryEscape(q), "")
		var page domain.AdminOrderPage
		if err := json.Unmarshal(ex.Body.Bytes(), &page); err != nil || ex.Code != 200 || page.Total < 1 || (q != "SUGAR.RU" && q != name && page.Items[0].ID != order.ID) {
			t.Fatalf("q=%s: %d %+v", q, ex.Code, page)
		}
	}
	ex := adminRequest(handler, login, "GET", "/api/admin/v1/orders?status=delivered", "")
	var delivered domain.AdminOrderPage
	_ = json.Unmarshal(ex.Body.Bytes(), &delivered)
	if delivered.Total != 1 || delivered.Items[0].ID != "ord-2026-002" || delivered.Items[0].Items != nil {
		t.Fatalf("status filter: %+v", delivered)
	}
	ex = adminRequest(handler, login, "GET", "/api/admin/v1/orders/"+order.ID, "")
	var detail domain.AdminOrder
	if err := json.Unmarshal(ex.Body.Bytes(), &detail); err != nil || ex.Code != 200 {
		t.Fatalf("detail: %d", ex.Code)
	}
	if detail.Revision != 1 || detail.Status != domain.OrderProcessing || detail.Customer.Email != "demo@sugar.ru" || !reflect.DeepEqual(detail.AllowedTransitions, []domain.OrderStatus{domain.OrderTransit, domain.OrderCancelled}) ||
		len(detail.Items) != 2 || detail.TotalMinor != order.TotalMinor || detail.DeliveryAddress != order.DeliveryAddress {
		t.Fatalf("detail: %+v", detail)
	}
	for i, item := range order.Items {
		got := detail.Items[i]
		if got.ProductID != item.ProductID || got.Name != item.Name || got.Quantity != item.Quantity || got.UnitPriceMinor != item.UnitPriceMinor || got.LineTotalMinor != item.LineTotalMinor {
			t.Fatalf("line %d: %+v vs %+v", i, got, item)
		}
	}
	if strings.Contains(ex.Body.String(), "password") || strings.Contains(ex.Body.String(), "token") {
		t.Fatal("credential material in the order view")
	}
	if ex := adminRequest(handler, login, "GET", "/api/admin/v1/orders/missing", ""); ex.Code != 404 {
		t.Fatalf("missing order: %d", ex.Code)
	}
	if len(d.auditTrail(t)) != 0 {
		t.Fatal("a read wrote an audit event")
	}
}

// processing → transit → delivered, each one revision and one audit event;
// delivered is terminal, and stale or invalid changes write nothing.
func TestOrderLifecycleRevisionsAndAudit(t *testing.T) {
	d := newDatabase(t, true)
	handler := d.staffServer(t, nil)
	login := d.signedInStaff(t, handler)
	order := d.placeOrder(t, "usr-demo", line{"p03", 2})
	stock := d.stock(t, "p03")
	ex := adminRequest(handler, login, "POST", "/api/admin/v1/orders/"+order.ID+"/status", `{"status":"transit","expectedRevision":1}`)
	if ex.Code != 200 || ex.body["status"] != "transit" || ex.body["revision"].(float64) != 2 {
		t.Fatalf("transit: %d %s", ex.Code, ex.Body.String())
	}
	if trail := orderAudits(d.auditTrail(t)); len(trail) != 1 || trail[0] != (auditRow{login.id, "order.status_change", "order", order.ID, "req-b1"}) {
		t.Fatalf("audit: %+v", trail)
	}
	for name, tc := range map[string]struct{ path, body, code string }{
		"stale":                  {"/status", `{"status":"delivered","expectedRevision":1}`, "stale_revision"},
		"cancel a shipped order": {"/cancel", `{"expectedRevision":2}`, "invalid_transition"},
		"back to transit":        {"/status", `{"status":"transit","expectedRevision":2}`, "invalid_transition"},
	} {
		if ex := adminRequest(handler, login, "POST", "/api/admin/v1/orders/"+order.ID+tc.path, tc.body); ex.Code != 409 || !strings.HasPrefix(ex.errorCode(), tc.code+"|") {
			t.Fatalf("%s: %d %s", name, ex.Code, ex.errorCode())
		}
	}
	if d.orderRevision(t, order.ID) != 2 || len(orderAudits(d.auditTrail(t))) != 1 {
		t.Fatal("a refused change wrote something")
	}
	staff := domain.StaffUser{ID: login.id}
	admin := application.NewOrderAdmin(d.store)
	delivered, err := admin.ChangeStatus(d.ctx, mutationAs(t, staff, application.OrderStatusChange), order.ID, 2, domain.OrderDelivered)
	if err != nil || delivered.Status != domain.OrderDelivered || delivered.Revision != 3 || delivered.DeliveredAt == nil || len(delivered.AllowedTransitions) != 0 {
		t.Fatalf("delivered: %+v %v", delivered, err)
	}
	for _, attempt := range []func() error{
		func() error {
			_, err := admin.Cancel(d.ctx, mutationAs(t, staff, application.OrderCancel), order.ID, 3)
			return err
		},
		func() error {
			_, err := admin.ChangeStatus(d.ctx, mutationAs(t, staff, application.OrderStatusChange), order.ID, 3, domain.OrderTransit)
			return err
		},
	} {
		if err := attempt(); !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatalf("terminal order changed: %v", err)
		}
	}
	if d.stock(t, "p03") != stock || len(orderAudits(d.auditTrail(t))) != 2 {
		t.Fatal("delivery touched stock or the audit count is wrong")
	}
	// The customer sees the label; lines and totals are the checkout snapshot.
	history, err := d.service.Order(d.ctx, "usr-demo", order.ID)
	if err != nil || history.Status != "delivered" || history.StatusLabel != "Доставлен" || history.DeliveredAt.IsZero() ||
		history.TotalMinor != order.TotalMinor || !reflect.DeepEqual(history.Items[0].LineTotalMinor, order.Items[0].LineTotalMinor) {
		t.Fatalf("customer history: %+v %v", history, err)
	}
}

func TestCancelReturnsStockExactlyOnce(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	admin := application.NewOrderAdmin(d.store)
	before := map[string]int64{"p03": d.stock(t, "p03"), "p05": d.stock(t, "p05")}
	order := d.placeOrder(t, "usr-demo", line{"p03", 2}, line{"p05", 3})
	if d.stock(t, "p03") != before["p03"]-2 || d.stock(t, "p05") != before["p05"]-3 {
		t.Fatal("checkout did not decrement stock")
	}
	revisions := map[string]int64{"p03": d.productRevision(t, "p03"), "p05": d.productRevision(t, "p05")}
	cancelled, err := admin.Cancel(d.ctx, mutationAs(t, staff, application.OrderCancel), order.ID, 1)
	if err != nil || cancelled.Status != domain.OrderCancelled || cancelled.StatusLabel != "Отменён" || cancelled.Revision != 2 {
		t.Fatalf("cancel: %+v %v", cancelled, err)
	}
	for product, stock := range before {
		if d.stock(t, product) != stock || d.productRevision(t, product) != revisions[product]+1 {
			t.Fatalf("%s: stock %d (want %d), revision %d", product, d.stock(t, product), stock, d.productRevision(t, product))
		}
	}
	// Again, with the current revision: refused, and no second restock.
	if _, err := admin.Cancel(d.ctx, mutationAs(t, staff, application.OrderCancel), order.ID, 2); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("second cancel: %v", err)
	}
	if d.stock(t, "p03") != before["p03"] || d.stock(t, "p05") != before["p05"] {
		t.Fatal("stock returned twice")
	}
	if trail := orderAudits(d.auditTrail(t)); len(trail) != 1 || trail[0].action != "order.cancel" || trail[0].actor != staff.ID || trail[0].entityID != order.ID {
		t.Fatalf("audit: %+v", trail)
	}
	// History keeps the snapshot; repeat still offers the products.
	history, err := d.service.Orders(d.ctx, "usr-demo")
	if err != nil || history[0].ID != order.ID || history[0].StatusLabel != "Отменён" || history[0].TotalMinor != order.TotalMinor || len(history[0].Items) != 2 {
		t.Fatalf("history: %+v %v", history, err)
	}
	preview, err := d.service.RepeatPreview(d.ctx, "usr-demo", order.ID)
	if err != nil || len(preview.Items) != 2 || preview.Items[0].Status == "discontinued" {
		t.Fatalf("repeat of a cancelled order: %+v %v", preview, err)
	}
}

func TestConcurrentCancelsHaveOneWinner(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	admin := application.NewOrderAdmin(d.store)
	before := d.stock(t, "p03")
	order := d.placeOrder(t, "usr-demo", line{"p03", 3})
	const n = 8
	mutations := make([]domain.StaffMutation, n)
	for i := range mutations {
		mutations[i] = mutationAs(t, staff, application.OrderCancel)
	}
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = admin.Cancel(d.ctx, mutations[i], order.ID, 1)
		}(i)
	}
	close(start)
	wg.Wait()
	winners := 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, domain.ErrStaleRevision):
		default:
			t.Fatalf("cancel %d: %v", i, err)
		}
	}
	if winners != 1 || d.stock(t, "p03") != before || d.orderRevision(t, order.ID) != 2 || len(orderAudits(d.auditTrail(t))) != 1 {
		t.Fatalf("winners %d, stock %d (want %d)", winners, d.stock(t, "p03"), before)
	}
}

// Archiving keeps the product row, so a cancelled order's stock returns to
// it; the product stays archived and the order snapshot is untouched.
func TestCancelReturnsStockToAnArchivedProduct(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	before := d.stock(t, "p03")
	order := d.placeOrder(t, "usr-demo", line{"p03", 2})
	product := d.adminProduct(t, "p03")
	if _, err := application.NewProductAdmin(d.store).Archive(d.ctx, mutationAs(t, staff, application.ProductArchive), "p03", product.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := application.NewOrderAdmin(d.store).Cancel(d.ctx, mutationAs(t, staff, application.OrderCancel), order.ID, 1); err != nil {
		t.Fatal(err)
	}
	after := d.adminProduct(t, "p03")
	if after.Stock != int(before) || !after.Archived {
		t.Fatalf("archived product after cancel: stock %d archived %v", after.Stock, after.Archived)
	}
	if stored, _ := d.service.Order(d.ctx, "usr-demo", order.ID); stored.Items[0].Name != order.Items[0].Name || stored.Items[0].UnitPriceMinor != order.Items[0].UnitPriceMinor {
		t.Fatal("order snapshot changed")
	}
}

func TestOrderChangeRollsBackWhenItsAuditFails(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	order := d.placeOrder(t, "usr-demo", line{"p03", 2})
	stock, productRevision := d.stock(t, "p03"), d.productRevision(t, "p03")
	d.exec(t, `CREATE FUNCTION fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure'; END $$`)
	d.exec(t, `CREATE TRIGGER fail_order_audit BEFORE INSERT ON staff_audit_events FOR EACH ROW WHEN (NEW.entity_type='order') EXECUTE FUNCTION fail_audit()`)
	admin := application.NewOrderAdmin(d.store)
	if _, err := admin.Cancel(d.ctx, mutationAs(t, staff, application.OrderCancel), order.ID, 1); err == nil {
		t.Fatal("cancel committed without its audit event")
	}
	if _, err := admin.ChangeStatus(d.ctx, mutationAs(t, staff, application.OrderStatusChange), order.ID, 1, domain.OrderTransit); err == nil {
		t.Fatal("status change committed without its audit event")
	}
	if d.text(t, "SELECT status FROM orders WHERE id=$1", order.ID) != "processing" || d.orderRevision(t, order.ID) != 1 ||
		d.stock(t, "p03") != stock || d.productRevision(t, "p03") != productRevision {
		t.Fatal("an order change survived its failed audit")
	}
}

// A staff stock edit racing a cancellation loses nothing: either the edit
// commits first and the cancelled quantity comes back on top of it, or the
// cancellation commits first and the edit, now stale, is refused. Checkout of
// the same product runs alongside without deadlock.
func TestCancelRacingStaffStockEditAndCheckout(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	products := application.NewProductAdmin(d.store)
	orders := application.NewOrderAdmin(d.store)
	productAudits := 0
	for round := 0; round < 6; round++ {
		order := d.placeOrder(t, "usr-demo", line{"p03", 2})
		buyer := "usr_race_" + string(rune('a'+round))
		d.customer(t, buyer)
		d.set(t, buyer, "p03", 1, true)
		read := d.adminProduct(t, "p03")
		edit := inputOf(read)
		edit.Stock = 500 + round
		productMutation := mutationAs(t, staff, application.ProductUpdate)
		cancelMutation := mutationAs(t, staff, application.OrderCancel)
		var editErr, cancelErr, checkoutErr error
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(3)
		go func() {
			defer wg.Done()
			<-start
			_, editErr = products.Update(d.ctx, productMutation, "p03", read.Revision, edit)
		}()
		go func() { defer wg.Done(); <-start; _, cancelErr = orders.Cancel(d.ctx, cancelMutation, order.ID, 1) }()
		go func() {
			defer wg.Done()
			<-start
			_, checkoutErr = d.service.Checkout(context.Background(), buyer, domain.CheckoutInput{DeliveryAddress: "Адрес"})
		}()
		close(start)
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("cancel, stock edit and checkout deadlocked")
		}
		if cancelErr != nil || checkoutErr != nil {
			t.Fatalf("round %d: cancel %v, checkout %v", round, cancelErr, checkoutErr)
		}
		after := d.adminProduct(t, "p03")
		switch {
		case editErr == nil:
			productAudits++
			// The edit set 500+round at some point; cancel added 2 and the
			// checkout took 1, both after it or the edit would be stale.
			if after.Stock != 500+round+2-1 {
				t.Fatalf("round %d: edit won, stock %d", round, after.Stock)
			}
		case errors.Is(editErr, domain.ErrStaleRevision):
			if after.Stock != read.Stock+2-1 {
				t.Fatalf("round %d: edit refused, stock %d (read %d)", round, after.Stock, read.Stock)
			}
		default:
			t.Fatalf("round %d edit: %v", round, editErr)
		}
	}
	updates := 0
	for _, row := range d.auditTrail(t) {
		if row.action == "product.update" {
			updates++
		}
	}
	if updates != productAudits || len(orderAudits(d.auditTrail(t))) != 6 {
		t.Fatal("audit events do not match committed changes")
	}
}

// The B3 image (migrations 001–013) accepts the order revision column and
// still shows orders whose status staff changed, with their stored labels.
func TestPreviousBinaryToleratesOrderRevisionSchema(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	order := d.placeOrder(t, "usr-demo", line{"p03", 1})
	if _, err := application.NewOrderAdmin(d.store).Cancel(d.ctx, mutationAs(t, staff, application.OrderCancel), order.ID, 1); err != nil {
		t.Fatal(err)
	}
	previous := migrator(migrationDir(t, []string{"001_", "002_", "003_", "004_", "005_", "006_", "007_", "008_", "009_", "010_", "011_", "012_", "013_"}, nil), &lockedBuffer{})
	if err := previous.Verify(d.ctx, d.pool); err != nil {
		t.Fatalf("B3 schema check on the B4 schema: %v", err)
	}
	if err := previous.Migrate(d.ctx, d.pool); err != nil {
		t.Fatalf("B3 migrate on the B4 schema: %v", err)
	}
	handler := d.staffServer(t, nil)
	cookie := cookieLogin(t, handler, "demo@sugar.ru", "Sweet2026!")
	ex := serve(handler, "GET", "/api/orders", "", map[string]string{"Cookie": shopCookie + "=" + cookie})
	if ex.Code != 200 || !strings.Contains(ex.Body.String(), `"statusLabel":"Отменён"`) {
		t.Fatalf("customer history: %d", ex.Code)
	}
	// A checkout written without the column still gets revision 1.
	next := d.placeOrder(t, "usr-demo", line{"p05", 1})
	if d.orderRevision(t, next.ID) != 1 {
		t.Fatal("new order revision")
	}
}
