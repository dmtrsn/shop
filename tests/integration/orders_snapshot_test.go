//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

// Order-time customer contact (ADR 017). The application offers no way to
// change a customer's name, email or phone, so these tests change the users
// row directly in their disposable database, the way a future profile
// feature or an operator would.

type contact struct{ name, email, phone string }

var (
	contactA = contact{"Анна Первая", "anna.a@example.com", "+7 900 111-11-11"}
	contactB = contact{"Борис Второй", "boris.b@example.com", "+7 900 222-22-22"}
)

func (d *database) setAccount(t *testing.T, user string, c contact) {
	t.Helper()
	d.exec(t, "UPDATE users SET name=$2,email=$3,phone=$4 WHERE id=$1", user, c.name, c.email, c.phone)
}

// orderContact reads the stored columns; nil means none was recorded.
func (d *database) orderContact(t *testing.T, id string) *contact {
	t.Helper()
	var name, email, phone *string
	if err := d.pool.QueryRow(d.ctx, "SELECT customer_name,customer_email,customer_phone FROM orders WHERE id=$1", id).Scan(&name, &email, &phone); err != nil {
		t.Fatal(err)
	}
	if email == nil {
		if name != nil || phone != nil {
			t.Fatal("partial contact")
		}
		return nil
	}
	return &contact{*name, *email, *phone}
}

func (d *database) staffOrder(t *testing.T, id string) domain.AdminOrder {
	t.Helper()
	order, err := application.NewOrderAdmin(d.store).Get(d.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return order
}

func sameContact(got *domain.OrderContact, want contact) bool {
	return got != nil && *got == domain.OrderContact{Name: want.name, Email: want.email, Phone: want.phone}
}

// Name A, contact A, order #1; the account becomes B; #1 still shows A, the
// account shows B, and order #2 records B.
func TestOrderKeepsTheContactOfItsCheckout(t *testing.T) {
	d := newDatabase(t, true)
	d.setAccount(t, "usr-demo", contactA)
	first := d.placeOrder(t, "usr-demo", line{"p03", 2})
	if got := d.orderContact(t, first.ID); got == nil || *got != contactA {
		t.Fatalf("recorded contact: %+v", got)
	}
	if first.Contact == nil || first.Contact.Email != contactA.email {
		t.Fatal("checkout did not return the recorded contact")
	}
	d.setAccount(t, "usr-demo", contactB)

	view := d.staffOrder(t, first.ID)
	if !sameContact(view.Contact, contactA) || view.CurrentAccount != nil || view.CustomerID != "usr-demo" {
		t.Fatalf("staff view of #1: %+v / %+v", view.Contact, view.CurrentAccount)
	}
	encoded, _ := json.Marshal(view)
	for _, value := range []string{contactB.name, contactB.email, contactB.phone} {
		if strings.Contains(string(encoded), value) {
			t.Fatalf("staff view of #1 shows today's account (%s)", value)
		}
	}
	// The account itself is B now.
	var account contact
	if err := d.pool.QueryRow(d.ctx, "SELECT name,email,phone FROM users WHERE id='usr-demo'").Scan(&account.name, &account.email, &account.phone); err != nil || account != contactB {
		t.Fatalf("account: %+v %v", account, err)
	}
	second := d.placeOrder(t, "usr-demo", line{"p05", 1})
	if got := d.orderContact(t, second.ID); got == nil || *got != contactB {
		t.Fatalf("contact of #2: %+v", got)
	}
	if got := d.orderContact(t, first.ID); got == nil || *got != contactA {
		t.Fatalf("contact of #1 changed: %+v", got)
	}
	// The customer's own history is unchanged and does not expose the contact.
	history, err := d.service.Order(d.ctx, "usr-demo", first.ID)
	if err != nil || history.DeliveryAddress != first.DeliveryAddress || history.TotalMinor != first.TotalMinor || history.Items[0].LineTotalMinor != first.Items[0].LineTotalMinor {
		t.Fatalf("customer history: %+v %v", history, err)
	}
	if body, _ := json.Marshal(history); strings.Contains(string(body), contactA.email) || strings.Contains(string(body), `"contact"`) {
		t.Fatal("the customer order DTO exposes the contact")
	}
}

func TestStaffChangesNeverTouchTheContact(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	admin := application.NewOrderAdmin(d.store)
	d.setAccount(t, "usr-demo", contactA)
	delivered := d.placeOrder(t, "usr-demo", line{"p03", 1})
	cancelled := d.placeOrder(t, "usr-demo", line{"p05", 2})
	before := map[string]*contact{delivered.ID: d.orderContact(t, delivered.ID), cancelled.ID: d.orderContact(t, cancelled.ID)}
	if _, err := admin.ChangeStatus(d.ctx, mutationAs(t, staff, application.OrderStatusChange), delivered.ID, 1, domain.OrderTransit); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ChangeStatus(d.ctx, mutationAs(t, staff, application.OrderStatusChange), delivered.ID, 2, domain.OrderDelivered); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Cancel(d.ctx, mutationAs(t, staff, application.OrderCancel), cancelled.ID, 1); err != nil {
		t.Fatal(err)
	}
	for id, want := range before {
		if got := d.orderContact(t, id); got == nil || *got != *want {
			t.Fatalf("%s contact changed: %+v", id, got)
		}
		if view := d.staffOrder(t, id); !sameContact(view.Contact, contactA) {
			t.Fatalf("%s staff view: %+v", id, view.Contact)
		}
	}
	// Audit rows identify the change, never the order's contents.
	rows, err := d.pool.Query(d.ctx, "SELECT row_to_json(e)::text FROM staff_audit_events e")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatal(err)
		}
		count++
		for _, value := range []string{contactA.name, contactA.email, contactA.phone, "Тестовая"} {
			if strings.Contains(row, value) {
				t.Fatalf("audit row carries order contents: %s", row)
			}
		}
	}
	if count != 3 {
		t.Fatalf("%d audit rows", count)
	}
}

// Repeating an order fills the cart; the next checkout records the contact
// the account has then, not the old order's.
func TestRepeatedOrderRecordsAFreshContact(t *testing.T) {
	d := newDatabase(t, true)
	d.setAccount(t, "usr-demo", contactA)
	original := d.placeOrder(t, "usr-demo", line{"p03", 2})
	d.setAccount(t, "usr-demo", contactB)
	if _, err := d.service.ClearCart(d.ctx, "usr-demo", false); err != nil {
		t.Fatal(err)
	}
	if _, err := d.service.CommitRepeat(d.ctx, "usr-demo", original.ID, []domain.RepeatSelection{{ProductID: "p03", Quantity: 2, Selected: true}}); err != nil {
		t.Fatal(err)
	}
	result, err := d.service.Checkout(d.ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "Новый адрес, 5"})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.orderContact(t, result.Order.ID); got == nil || *got != contactB {
		t.Fatalf("repeated order contact: %+v", got)
	}
	if got := d.orderContact(t, original.ID); got == nil || *got != contactA {
		t.Fatalf("original contact changed: %+v", got)
	}
	if result.Order.DeliveryAddress != "Новый адрес, 5" {
		t.Fatal("repeat reused the old delivery address")
	}
}

// Orders without a recorded contact stay without one: no backfill, and the
// staff view shows the account only as the current account.
func TestOrdersWithoutContactAreNotBackfilled(t *testing.T) {
	d := newDatabase(t, true)
	if d.orderContact(t, "ord-2026-002") != nil {
		t.Fatal("the migration backfilled a pre-existing order")
	}
	// What an older application version writes during a rolling deploy.
	d.exec(t, `INSERT INTO orders(id,number,user_id,status,status_label,item_count,total_minor,delivery_address)
		VALUES ('ord-old-replica','#99001','usr-demo','processing','Собираем заказ',1,100,'Адрес')`)
	d.setAccount(t, "usr-demo", contactB)
	for _, id := range []string{"ord-2026-002", "ord-old-replica"} {
		view := d.staffOrder(t, id)
		if view.Contact != nil || !sameContact(view.CurrentAccount, contactB) {
			t.Fatalf("%s: contact %+v, current %+v", id, view.Contact, view.CurrentAccount)
		}
		if d.orderContact(t, id) != nil {
			t.Fatalf("%s gained a contact by being read", id)
		}
	}
	// Half a contact is impossible.
	_, err := d.pool.Exec(d.ctx, "UPDATE orders SET customer_name='x' WHERE id='ord-2026-002'")
	if pgCode(err) != "23514" {
		t.Fatalf("partial contact: %v", err)
	}
	// Search still finds legacy orders by the account and new ones by their contact.
	d.setAccount(t, "usr-demo", contactA)
	recorded := d.placeOrder(t, "usr-demo", line{"p03", 1})
	d.setAccount(t, "usr-demo", contactB)
	page, err := application.NewOrderAdmin(d.store).List(d.ctx, domain.AdminOrderFilter{Query: "anna.a@"})
	if err != nil || page.Total != 1 || page.Items[0].ID != recorded.ID {
		t.Fatalf("search by order-time email: %+v %v", page, err)
	}
}

func TestContactSurvivesADisabledAccount(t *testing.T) {
	d := newDatabase(t, true)
	address, password := "kate@example.com", "original password 1"
	account := d.operatorCustomer(t, address, password)
	d.set(t, account.ID, "p03", 1, true)
	result, err := d.service.Checkout(d.ctx, account.ID, domain.CheckoutInput{DeliveryAddress: "Адрес Кати"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.SetCustomerDisabled(d.ctx, address, true); err != nil {
		t.Fatal(err)
	}
	view := d.staffOrder(t, result.Order.ID)
	if view.Contact == nil || view.Contact.Email != address || view.Contact.Name != account.Name || view.CurrentAccount != nil {
		t.Fatalf("disabled customer's order: %+v", view.Contact)
	}
}

// Only name, email and phone are recorded; nothing from the credential or
// session side reaches the order or its staff view.
func TestContactHoldsNoCredentials(t *testing.T) {
	d := newDatabase(t, true)
	order := d.placeOrder(t, "usr-demo", line{"p03", 1})
	rows, err := d.pool.Query(d.ctx, `SELECT column_name FROM information_schema.columns WHERE table_name='orders' AND column_name LIKE 'customer%' ORDER BY column_name`)
	if err != nil {
		t.Fatal(err)
	}
	var columns []string
	for rows.Next() {
		var column string
		_ = rows.Scan(&column)
		columns = append(columns, column)
	}
	rows.Close()
	if strings.Join(columns, ",") != "customer_email,customer_name,customer_phone" {
		t.Fatalf("order contact columns: %v", columns)
	}
	hash := d.text(t, "SELECT password_hash FROM users WHERE id='usr-demo'")
	encoded, _ := json.Marshal(d.staffOrder(t, order.ID))
	if strings.Contains(string(encoded), hash) || strings.Contains(strings.ToLower(string(encoded)), "password") || strings.Contains(strings.ToLower(string(encoded)), "token") {
		t.Fatal("credential material in the staff order view")
	}
}

// A checkout racing a change of the account records either the whole old or
// the whole new contact, never a mix: it reads them under the users row lock.
func TestContactIsCoherentUnderConcurrentAccountChange(t *testing.T) {
	d := newDatabase(t, true)
	for round := 0; round < 8; round++ {
		before := contact{fmt.Sprintf("Имя %d", round), fmt.Sprintf("before%d@example.com", round), fmt.Sprintf("+7 900 000-00-%02d", round)}
		after := contact{fmt.Sprintf("Новое имя %d", round), fmt.Sprintf("after%d@example.com", round), fmt.Sprintf("+7 911 000-00-%02d", round)}
		d.setAccount(t, "usr-demo", before)
		if _, err := d.service.ClearCart(d.ctx, "usr-demo", false); err != nil {
			t.Fatal(err)
		}
		d.set(t, "usr-demo", "p03", 1, true)
		var order domain.Order
		var checkoutErr, updateErr error
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			var result domain.CheckoutResult
			result, checkoutErr = d.service.Checkout(d.ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "Адрес"})
			order = result.Order
		}()
		go func() {
			defer wg.Done()
			<-start
			_, updateErr = d.pool.Exec(d.ctx, "UPDATE users SET name=$2,email=$3,phone=$4 WHERE id=$1", "usr-demo", after.name, after.email, after.phone)
		}()
		close(start)
		wg.Wait()
		if checkoutErr != nil || updateErr != nil {
			t.Fatalf("round %d: checkout %v, update %v", round, checkoutErr, updateErr)
		}
		if got := d.orderContact(t, order.ID); got == nil || (*got != before && *got != after) {
			t.Fatalf("round %d: incoherent contact %+v", round, got)
		}
	}
}

// The B4 image (migrations 001–014) accepts the contact columns; its
// checkout, which does not know them, still works and leaves them NULL.
func TestPreviousBinaryToleratesContactSchema(t *testing.T) {
	d := newDatabase(t, true)
	previous := migrator(migrationDir(t, []string{"001_", "002_", "003_", "004_", "005_", "006_", "007_", "008_", "009_", "010_", "011_", "012_", "013_", "014_"}, nil), &lockedBuffer{})
	if err := previous.Verify(d.ctx, d.pool); err != nil {
		t.Fatalf("B4 schema check on the B5 schema: %v", err)
	}
	if err := previous.Migrate(d.ctx, d.pool); err != nil {
		t.Fatalf("B4 migrate on the B5 schema: %v", err)
	}
	// The B4 insert statement, verbatim in shape: no contact columns.
	d.exec(t, `INSERT INTO orders(id,number,user_id,status,status_label,item_count,total_minor,delivery_address,comment,created_at,idempotency_key,request_digest)
		VALUES ('ord-b4','#99002','usr-demo','processing','Собираем заказ',1,100,'Адрес','',now(),NULL,NULL)`)
	if d.orderContact(t, "ord-b4") != nil || d.orderRevision(t, "ord-b4") != 1 {
		t.Fatal("B4-style order")
	}
	handler := d.staffServer(t, nil)
	cookie := cookieLogin(t, handler, "demo@sugar.ru", "Sweet2026!")
	if ex := serve(handler, "GET", "/api/orders", "", map[string]string{"Cookie": shopCookie + "=" + cookie}); ex.Code != 200 || !strings.Contains(ex.Body.String(), "ord-b4") {
		t.Fatalf("customer history: %d", ex.Code)
	}
}
