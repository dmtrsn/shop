package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

type testOrderStore struct {
	mu        sync.Mutex
	err       error
	mutations []domain.StaffMutation
	statuses  []domain.OrderStatus
	revisions []int64
}

func (s *testOrderStore) AdminOrders(context.Context, domain.AdminOrderFilter) (domain.AdminOrderPage, error) {
	return domain.AdminOrderPage{Items: []domain.AdminOrder{}, Page: 1, Limit: 50}, s.err
}

func (s *testOrderStore) AdminOrder(context.Context, string) (domain.AdminOrder, error) {
	return domain.AdminOrder{}, domain.ErrNotFound
}

func (s *testOrderStore) SetOrderStatus(_ context.Context, mutation domain.StaffMutation, id string, revision int64, to domain.OrderStatus) (domain.AdminOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return domain.AdminOrder{}, s.err
	}
	s.mutations, s.statuses, s.revisions = append(s.mutations, mutation), append(s.statuses, to), append(s.revisions, revision)
	return domain.AdminOrder{ID: id, Revision: revision + 1, Status: to, AllowedTransitions: domain.NextOrderStatuses(to)}, nil
}

func orderHandler(t *testing.T, orders *testOrderStore) (http.Handler, *testStaffStore) {
	t.Helper()
	staff := newTestStaffStore(t)
	s := newServer(customerService(), discard(), "", "memory", SessionConfig{Secure: true, TrustedOrigins: []string{shopOrigin}},
		WithStaff(application.NewStaffAuth(staff), StaffConfig{TrustedOrigins: []string{adminOrigin}}),
		WithOrderAdmin(application.NewOrderAdmin(orders)))
	if !s.staffEnabled() {
		t.Fatal("staff not enabled")
	}
	mux := http.NewServeMux()
	s.registerStaffRoutes(mux)
	mux.HandleFunc("POST /api/auth/session", s.startSession)
	return s.middleware(mux), staff
}

func TestOrderRoutesAcceptOnlyTheStaffSession(t *testing.T) {
	orders := &testOrderStore{}
	handler, _ := orderHandler(t, orders)
	staffCookie, csrf := staffLogin(t, handler, true)
	customer := call(handler, "POST", "/api/auth/session", `{"email":"demo@sugar.ru","password":"Sweet2026!"}`, map[string]string{"Origin": shopOrigin})
	customerCookie := "__Host-sweetshop_session=" + cookieNamed(customer, "__Host-sweetshop_session").Value
	for name, tc := range map[string]struct {
		method, path string
		headers      map[string]string
		status       int
	}{
		"anonymous list":         {"GET", "/api/admin/v1/orders", nil, 401},
		"customer list":          {"GET", "/api/admin/v1/orders", map[string]string{"Cookie": customerCookie}, 401},
		"customer detail":        {"GET", "/api/admin/v1/orders/ord-1", map[string]string{"Cookie": customerCookie}, 401},
		"customer cancel":        {"POST", "/api/admin/v1/orders/ord-1/cancel", map[string]string{"Cookie": customerCookie, "Origin": adminOrigin}, 401},
		"bearer":                 {"GET", "/api/admin/v1/orders", map[string]string{"Cookie": staffCookie, "Authorization": "Bearer x"}, 401},
		"cancel without CSRF":    {"POST", "/api/admin/v1/orders/ord-1/cancel", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin}, 403},
		"status from shop":       {"POST", "/api/admin/v1/orders/ord-1/status", map[string]string{"Cookie": staffCookie, "Origin": shopOrigin, "X-CSRF-Token": csrf}, 403},
		"other expected staff":   {"POST", "/api/admin/v1/orders/ord-1/cancel", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf, expectedStaffHeader: "stf_" + strings.Repeat("0", 32)}, 409},
		"no delete route":        {"DELETE", "/api/admin/v1/orders/ord-1", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}, 405},
		"staff list":             {"GET", "/api/admin/v1/orders", map[string]string{"Cookie": staffCookie}, 200},
		"no arbitrary PUT/PATCH": {"PATCH", "/api/admin/v1/orders/ord-1", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}, 405},
	} {
		t.Run(name, func(t *testing.T) {
			if rec := call(handler, tc.method, tc.path, `{"expectedRevision":1}`, tc.headers); rec.Code != tc.status {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
		})
	}
	if len(orders.mutations) != 0 {
		t.Fatal("a rejected request reached the store")
	}
}

func TestOrderMutationsTakeTheActorFromTheSession(t *testing.T) {
	orders := &testOrderStore{}
	handler, staff := orderHandler(t, orders)
	staffCookie, csrf := staffLogin(t, handler, true)
	headers := map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf, "X-Request-ID": "req-9"}
	if rec := call(handler, "POST", "/api/admin/v1/orders/ord-1/status", `{"status":"transit","expectedRevision":3}`, headers); rec.Code != 200 {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	rec := call(handler, "POST", "/api/admin/v1/orders/ord-1/cancel", `{"expectedRevision":4}`, headers)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != 200 || body["status"] != "cancelled" || body["revision"].(float64) != 5 {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body.String())
	}
	want := []domain.AuditAction{application.OrderStatusChange, application.OrderCancel}
	for i, mutation := range orders.mutations {
		if mutation.Actor() != staff.credential.Staff.ID || mutation.Action() != want[i] || mutation.RequestID() != "req-9" {
			t.Fatalf("mutation %d: %s %s %s", i, mutation.Actor(), mutation.Action(), mutation.RequestID())
		}
	}
	if orders.statuses[0] != domain.OrderTransit || orders.statuses[1] != domain.OrderCancelled || orders.revisions[0] != 3 || orders.revisions[1] != 4 {
		t.Fatalf("%v %v", orders.statuses, orders.revisions)
	}
}

func TestOrderBodiesAndErrors(t *testing.T) {
	orders := &testOrderStore{}
	handler, _ := orderHandler(t, orders)
	staffCookie, csrf := staffLogin(t, handler, true)
	headers := map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}
	for name, tc := range map[string]struct{ path, body, fields string }{
		"missing revision":    {"/api/admin/v1/orders/ord-1/status", `{"status":"transit"}`, "expectedRevision"},
		"cancel via status":   {"/api/admin/v1/orders/ord-1/status", `{"status":"cancelled","expectedRevision":1}`, "status"},
		"unknown status":      {"/api/admin/v1/orders/ord-1/status", `{"status":"refunded","expectedRevision":1}`, "status"},
		"actor in body":       {"/api/admin/v1/orders/ord-1/cancel", `{"expectedRevision":1,"staffUserId":"stf_x"}`, "staffUserId"},
		"fractional revision": {"/api/admin/v1/orders/ord-1/cancel", `{"expectedRevision":1.5}`, "body"},
		"zero revision":       {"/api/admin/v1/orders/ord-1/cancel", `{"expectedRevision":0}`, "expectedRevision"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := call(handler, "POST", tc.path, tc.body, headers)
			code, fields := errorBody(t, rec.Body.String())
			if rec.Code != 422 || code != "validation_error" || strings.Join(fields, ",") != tc.fields {
				t.Fatalf("%d %s %v", rec.Code, code, fields)
			}
		})
	}
	for err, want := range map[error]struct {
		status int
		code   string
	}{
		domain.ErrInvalidTransition: {409, "invalid_transition"},
		domain.ErrStaleRevision:     {409, "stale_revision"},
		domain.ErrNotFound:          {404, "not_found"},
	} {
		orders.err = err
		rec := call(handler, "POST", "/api/admin/v1/orders/ord-1/cancel", `{"expectedRevision":1}`, headers)
		if code, _ := errorBody(t, rec.Body.String()); rec.Code != want.status || code != want.code {
			t.Errorf("%v: %d %s", err, rec.Code, code)
		}
	}
	if len(orders.mutations) != 0 {
		t.Fatal("an invalid request reached the store")
	}
}
