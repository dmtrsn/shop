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

// testProductStore records what reached it and answers with err when set.
type testProductStore struct {
	mu        sync.Mutex
	err       error
	mutations []domain.StaffMutation
	inputs    []domain.ProductInput
	revisions []int64
}

func (s *testProductStore) record(mutation domain.StaffMutation, revision int64, input domain.ProductInput) (domain.AdminProduct, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return domain.AdminProduct{}, s.err
	}
	s.mutations = append(s.mutations, mutation)
	s.inputs = append(s.inputs, input)
	s.revisions = append(s.revisions, revision)
	return domain.AdminProduct{ID: "prd_x", Revision: revision + 1, Gallery: []string{}, Tags: []string{}, Badges: []string{}, Collections: []domain.Reference{}}, nil
}

func (s *testProductStore) AdminProducts(context.Context, domain.AdminProductFilter) (domain.AdminProductPage, error) {
	return domain.AdminProductPage{Items: []domain.AdminProduct{}, Page: 1, Limit: 50}, s.err
}

func (s *testProductStore) AdminProduct(context.Context, string) (domain.AdminProduct, error) {
	return domain.AdminProduct{}, domain.ErrNotFound
}

func (s *testProductStore) CreateProduct(_ context.Context, mutation domain.StaffMutation, product domain.NewProduct) (domain.AdminProduct, error) {
	return s.record(mutation, 0, product.Input)
}

func (s *testProductStore) UpdateProduct(_ context.Context, mutation domain.StaffMutation, _ string, revision int64, input domain.ProductInput) (domain.AdminProduct, error) {
	return s.record(mutation, revision, input)
}

func (s *testProductStore) SetProductArchived(_ context.Context, mutation domain.StaffMutation, _ string, revision int64, _ bool) (domain.AdminProduct, error) {
	return s.record(mutation, revision, domain.ProductInput{})
}

func productHandler(t *testing.T, products *testProductStore) (http.Handler, *testStaffStore) {
	t.Helper()
	staff := newTestStaffStore(t)
	s := newServer(customerService(), discard(), "", "memory", SessionConfig{Secure: true, TrustedOrigins: []string{shopOrigin}},
		WithStaff(application.NewStaffAuth(staff), StaffConfig{TrustedOrigins: []string{adminOrigin}}),
		WithProductAdmin(application.NewProductAdmin(products)))
	if !s.staffEnabled() {
		t.Fatal("staff not enabled")
	}
	mux := http.NewServeMux()
	s.registerStaffRoutes(mux)
	mux.HandleFunc("POST /api/auth/session", s.startSession)
	return s.middleware(mux), staff
}

func productJSON(extra string) string {
	body := `{"slug":"dark-bar","sku":"DRK-1","name":"Тёмный","shortName":"","description":"","priceMinor":48900,"oldPriceMinor":0,` +
		`"unit":"плитка","weightGrams":90,"categoryId":"cat-bars","manufacturerId":"m-choco","brandId":"b-atelier-noir","collectionIds":[],` +
		`"series":"","tags":[],"badges":[],"ingredients":"","nutrition":"","shelfLifeDays":0,"stock":5,"minimumQuantity":1,"quantityStep":1,"featured":false`
	if extra != "" {
		body += "," + extra
	}
	return body + "}"
}

func errorBody(t *testing.T, body string) (string, []string) {
	t.Helper()
	var result struct {
		Error struct {
			Code   string
			Fields []domain.FieldError
		}
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatal(err)
	}
	fields := []string{}
	for _, field := range result.Error.Fields {
		fields = append(fields, field.Field)
	}
	return result.Error.Code, fields
}

func TestProductRoutesNeedTheProductService(t *testing.T) {
	handler := staffHandler(t, newTestStaffStore(t), true)
	staffCookie, csrf := staffLogin(t, handler, true)
	for _, method := range []string{"GET", "POST"} {
		if rec := call(handler, method, "/api/admin/v1/products", productJSON(""), map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}); rec.Code != 404 {
			t.Fatalf("%s: %d", method, rec.Code)
		}
	}
}

func TestProductRoutesAcceptOnlyTheStaffSession(t *testing.T) {
	products := &testProductStore{}
	handler, _ := productHandler(t, products)
	staffCookie, csrf := staffLogin(t, handler, true)
	customer := call(handler, "POST", "/api/auth/session", `{"email":"demo@sugar.ru","password":"Sweet2026!"}`, map[string]string{"Origin": shopOrigin})
	customerCookie := "__Host-sweetshop_session=" + cookieNamed(customer, "__Host-sweetshop_session").Value
	var customerBody struct{ CSRFToken string }
	_ = json.Unmarshal(customer.Body.Bytes(), &customerBody)
	for name, tc := range map[string]struct {
		method, path string
		headers      map[string]string
		status       int
	}{
		"anonymous list":           {"GET", "/api/admin/v1/products", nil, 401},
		"customer list":            {"GET", "/api/admin/v1/products", map[string]string{"Cookie": customerCookie}, 401},
		"customer create":          {"POST", "/api/admin/v1/products", map[string]string{"Cookie": customerCookie, "Origin": adminOrigin, "X-CSRF-Token": customerBody.CSRFToken}, 401},
		"customer bearer":          {"GET", "/api/admin/v1/products", map[string]string{"Authorization": "Bearer " + strings.SplitN(customerCookie, "=", 2)[1]}, 401},
		"bearer beside staff":      {"GET", "/api/admin/v1/products", map[string]string{"Cookie": staffCookie, "Authorization": "Bearer x"}, 401},
		"create without CSRF":      {"POST", "/api/admin/v1/products", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin}, 403},
		"create from shop origin":  {"POST", "/api/admin/v1/products", map[string]string{"Cookie": staffCookie, "Origin": shopOrigin, "X-CSRF-Token": csrf}, 403},
		"archive without CSRF":     {"POST", "/api/admin/v1/products/p01/archive", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin}, 403},
		"update for another staff": {"PUT", "/api/admin/v1/products/p01", map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf, expectedStaffHeader: "stf_" + strings.Repeat("0", 32)}, 409},
		"staff list":               {"GET", "/api/admin/v1/products", map[string]string{"Cookie": staffCookie}, 200},
	} {
		t.Run(name, func(t *testing.T) {
			if rec := call(handler, tc.method, tc.path, productJSON(""), tc.headers); rec.Code != tc.status {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
		})
	}
	if len(products.mutations) != 0 {
		t.Fatal("a rejected request reached the store")
	}
}

func TestProductWritesTakeTheActorFromTheSession(t *testing.T) {
	products := &testProductStore{}
	handler, staff := productHandler(t, products)
	staffCookie, csrf := staffLogin(t, handler, true)
	headers := map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf, "X-Request-ID": "req-7"}
	if rec := call(handler, "POST", "/api/admin/v1/products", productJSON(""), headers); rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(handler, "PUT", "/api/admin/v1/products/p01", productJSON(`"expectedRevision":7`), headers); rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	for _, path := range []string{"/api/admin/v1/products/p01/archive", "/api/admin/v1/products/p01/restore"} {
		if rec := call(handler, "POST", path, `{"expectedRevision":8}`, headers); rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	want := []domain.AuditAction{application.ProductCreate, application.ProductUpdate, application.ProductArchive, application.ProductRestore}
	for i, mutation := range products.mutations {
		if mutation.Actor() != staff.credential.Staff.ID || mutation.Action() != want[i] || mutation.RequestID() != "req-7" {
			t.Fatalf("mutation %d: %s %s %s", i, mutation.Actor(), mutation.Action(), mutation.RequestID())
		}
	}
	if products.revisions[1] != 7 || products.inputs[1].Stock != 5 || products.revisions[2] != 8 {
		t.Fatalf("revisions %v", products.revisions)
	}
}

func TestProductBodiesMustBeComplete(t *testing.T) {
	products := &testProductStore{}
	handler, _ := productHandler(t, products)
	staffCookie, csrf := staffLogin(t, handler, true)
	headers := map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}
	complete := productJSON("")
	for name, tc := range map[string]struct {
		method, path, body string
		fields             string
	}{
		"missing stock":           {"POST", "/api/admin/v1/products", strings.Replace(complete, `"stock":5,`, "", 1), "stock"},
		"null price":              {"POST", "/api/admin/v1/products", strings.Replace(complete, `"priceMinor":48900`, `"priceMinor":null`, 1), "priceMinor"},
		"actor in body":           {"POST", "/api/admin/v1/products", productJSON(`"staffUserId":"stf_x"`), "staffUserId"},
		"revision on create":      {"POST", "/api/admin/v1/products", productJSON(`"revision":3`), "revision"},
		"fractional price":        {"POST", "/api/admin/v1/products", strings.Replace(complete, "48900", "489.5", 1), "body"},
		"overflowing price":       {"POST", "/api/admin/v1/products", strings.Replace(complete, "48900", "1e30", 1), "body"},
		"string stock":            {"POST", "/api/admin/v1/products", strings.Replace(complete, `"stock":5`, `"stock":"5"`, 1), "body"},
		"negative stock":          {"POST", "/api/admin/v1/products", strings.Replace(complete, `"stock":5`, `"stock":-1`, 1), "stock"},
		"zero step":               {"POST", "/api/admin/v1/products", strings.Replace(complete, `"quantityStep":1`, `"quantityStep":0`, 1), "quantityStep"},
		"update without revision": {"PUT", "/api/admin/v1/products/p01", complete, "expectedRevision"},
		"archive without body":    {"POST", "/api/admin/v1/products/p01/archive", `{}`, "expectedRevision"},
		"archive extra member":    {"POST", "/api/admin/v1/products/p01/archive", `{"expectedRevision":1,"archived":true}`, "archived"},
		"array body":              {"POST", "/api/admin/v1/products", `[]`, "body"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := call(handler, tc.method, tc.path, tc.body, headers)
			code, fields := errorBody(t, rec.Body.String())
			if rec.Code != 422 || code != "validation_error" || strings.Join(fields, ",") != tc.fields {
				t.Fatalf("%d %s %v", rec.Code, code, fields)
			}
		})
	}
	if len(products.mutations) != 0 {
		t.Fatal("an invalid body reached the store")
	}
}

func TestProductErrorMapping(t *testing.T) {
	products := &testProductStore{}
	handler, _ := productHandler(t, products)
	staffCookie, csrf := staffLogin(t, handler, true)
	headers := map[string]string{"Cookie": staffCookie, "Origin": adminOrigin, "X-CSRF-Token": csrf}
	for err, want := range map[error]struct {
		status int
		code   string
	}{
		domain.ErrStaleRevision:      {409, "stale_revision"},
		domain.ErrConflict:           {409, "conflict"},
		domain.ErrNotFound:           {404, "not_found"},
		domain.ErrAuditActorInactive: {401, "unauthorized"},
		&domain.ValidationError{}:    {422, "validation_error"},
	} {
		products.err = err
		rec := call(handler, "PUT", "/api/admin/v1/products/p01", productJSON(`"expectedRevision":1`), headers)
		if code, _ := errorBody(t, rec.Body.String()); rec.Code != want.status || code != want.code {
			t.Errorf("%v: %d %s", err, rec.Code, code)
		}
	}
	products.err = nil
	if rec := call(handler, "GET", "/api/admin/v1/products/missing", "", map[string]string{"Cookie": staffCookie}); rec.Code != 404 {
		t.Fatalf("missing product: %d", rec.Code)
	}
	for _, query := range []string{"?page=0", "?limit=x", "?status=deleted", "?limit=101"} {
		if rec := call(handler, "GET", "/api/admin/v1/products"+query, "", map[string]string{"Cookie": staffCookie}); rec.Code != 422 {
			t.Errorf("%s: %d", query, rec.Code)
		}
	}
}
