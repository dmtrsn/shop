package contract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"

	"sweetshop/internal/delivery/httpapi"
)

// AdminTrustedOrigin is the staff origin of the contract handler; it is
// distinct from TrustedOrigin.
const AdminTrustedOrigin = "https://admin.shop.example"

const staffCookieName = "__Host-sweetshop_staff"

// StaffConfig is the staff policy matching SessionConfig.
func StaffConfig() httpapi.StaffConfig {
	return httpapi.StaffConfig{TrustedOrigins: []string{AdminTrustedOrigin}}
}

// RunAdmin exercises every staff operation against api/admin-openapi.yaml.
// The handler must have the staff API and product management enabled with
// StaffConfig, a staff account with the given credentials, and the demo
// catalog dictionaries (cat-bars, m-choco, b-atelier-noir, col-gifts).
func RunAdmin(t *testing.T, handler http.Handler, email, password string) {
	t.Helper()
	doc := AdminDocument(t)
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	send := func(method, path, body string, status int, headers map[string]string) (map[string]any, *httptest.ResponseRecorder) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", "admin-contract")
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		route, params, err := router.FindRoute(req)
		if err != nil {
			t.Fatal(err)
		}
		if status < 300 {
			input := &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route,
				Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, SkipSettingDefaults: true}}
			if err := openapi3filter.ValidateRequest(context.Background(), input); err != nil {
				t.Fatalf("%s request contract: %v", method, err)
			}
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s status=%d want=%d body=%s", method, rec.Code, status, rec.Body.String())
		}
		if rec.Header().Get("X-Request-ID") != "admin-contract" || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: request ID or no-store missing", method)
		}
		validation := &openapi3filter.ResponseValidationInput{RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route}, Status: rec.Code, Header: rec.Header(), Options: &openapi3filter.Options{IncludeResponseStatus: true}}
		if err := openapi3filter.ValidateResponse(context.Background(), validation.SetBodyBytes(rec.Body.Bytes())); err != nil {
			t.Fatalf("%s response contract: %v", method, err)
		}
		if status < 300 {
			covered[route.Operation.OperationID] = true
		}
		if rec.Code == http.StatusNoContent {
			return nil, rec
		}
		var result map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result, rec
	}
	errorCode := func(result map[string]any) string {
		return result["error"].(map[string]any)["code"].(string)
	}
	const sessionPath = "/api/admin/v1/auth/session"
	credentials := `{"email":"` + email + `","password":"` + password + `"}`
	origin := map[string]string{"Origin": AdminTrustedOrigin}

	if state, _ := send("GET", sessionPath, "", 200, nil); state["authenticated"] != false {
		t.Fatal("anonymous staff state")
	}
	if result, _ := send("POST", sessionPath, credentials, 403, map[string]string{"Origin": TrustedOrigin}); errorCode(result) != "csrf_failed" {
		t.Fatal("customer origin accepted for staff login")
	}
	if result, _ := send("POST", sessionPath, `{"email":"`+email+`","password":"wrong password 1"}`, 401, origin); errorCode(result) != "unauthorized" {
		t.Fatal("wrong password")
	}
	if result, _ := send("POST", sessionPath, `{"email":"x"`, 422, origin); errorCode(result) != "validation_error" {
		t.Fatal("malformed body")
	}
	if result, _ := send("POST", sessionPath, credentials, 401, map[string]string{"Origin": AdminTrustedOrigin, "Authorization": "Bearer customer-token"}); errorCode(result) != "unauthorized" {
		t.Fatal("bearer accepted on staff login")
	}
	session, rec := send("POST", sessionPath, credentials, 200, origin)
	cookie := ""
	for _, c := range rec.Result().Cookies() {
		if c.Name == staffCookieName {
			cookie = c.Name + "=" + c.Value
			if strings.Contains(rec.Body.String(), c.Value) {
				t.Fatal("staff session token in the body")
			}
		}
	}
	if cookie == "" {
		t.Fatal("no staff cookie")
	}
	staff := session["staff"].(map[string]any)
	id, csrf := staff["id"].(string), session["csrfToken"].(string)

	if state, _ := send("GET", sessionPath, "", 200, map[string]string{"Cookie": cookie}); state["authenticated"] != true || state["csrfToken"] != csrf {
		t.Fatal("authenticated staff state")
	}
	if result, _ := send("GET", sessionPath, "", 401, map[string]string{"Cookie": cookie, "Authorization": "Bearer x"}); errorCode(result) != "unauthorized" {
		t.Fatal("bearer accepted on staff state")
	}
	if result, _ := send("DELETE", sessionPath, "", 403, map[string]string{"Cookie": cookie, "Origin": AdminTrustedOrigin}); errorCode(result) != "csrf_failed" {
		t.Fatal("staff logout without CSRF")
	}
	if result, _ := send("DELETE", sessionPath, "", 409, map[string]string{"Cookie": cookie, "Origin": AdminTrustedOrigin, "X-CSRF-Token": csrf, "X-Expected-Staff-ID": "stf_00000000000000000000000000000000"}); errorCode(result) != "authenticated_staff_changed" {
		t.Fatal("expected staff precondition")
	}
	runAdminProducts(t, send, errorCode, cookie, csrf)
	send("DELETE", sessionPath, "", 204, map[string]string{"Cookie": cookie, "Origin": AdminTrustedOrigin, "X-CSRF-Token": csrf, "X-Expected-Staff-ID": id})
	if state, _ := send("GET", sessionPath, "", 200, map[string]string{"Cookie": cookie}); state["authenticated"] != false {
		t.Fatal("staff session survived logout")
	}
	for _, operation := range []string{"staffSessionState", "startStaffSession", "endStaffSession",
		"listProducts", "createProduct", "getProduct", "updateProduct", "archiveProduct", "restoreProduct", "getCatalogOptions"} {
		if !covered[operation] {
			t.Errorf("operation %s not covered", operation)
		}
	}
}

type adminSend func(method, path, body string, status int, headers map[string]string) (map[string]any, *httptest.ResponseRecorder)

// AdminProductBody is a complete staff product document using demo
// dictionaries; expectedRevision > 0 adds the update precondition.
func AdminProductBody(slug string, stock int, expectedRevision int64) string {
	body := `{"slug":"` + slug + `","sku":"` + strings.ToUpper(slug) + `","name":"Контрактный батончик","shortName":"Батончик",` +
		`"description":"Описание\nв две строки","priceMinor":15900,"oldPriceMinor":19900,"unit":"шт.","weightGrams":40,` +
		`"categoryId":"cat-bars","manufacturerId":"m-choco","brandId":"b-atelier-noir","collectionIds":["col-gifts"],` +
		`"series":"","tags":["подарок"],"badges":["Новинка"],"ingredients":"","nutrition":"","shelfLifeDays":90,` +
		`"stock":` + strconv.Itoa(stock) + `,"minimumQuantity":2,"quantityStep":2,"featured":false`
	if expectedRevision > 0 {
		body += `,"expectedRevision":` + strconv.FormatInt(expectedRevision, 10)
	}
	return body + "}"
}

func runAdminProducts(t *testing.T, send adminSend, errorCode func(map[string]any) string, cookie, csrf string) {
	t.Helper()
	read := map[string]string{"Cookie": cookie}
	write := map[string]string{"Cookie": cookie, "Origin": AdminTrustedOrigin, "X-CSRF-Token": csrf}
	revision := func(product map[string]any) int64 { return int64(product["revision"].(float64)) }

	if options, _ := send("GET", "/api/admin/v1/catalog-options", "", 200, read); len(options["brands"].([]any)) == 0 || len(options["collections"].([]any)) == 0 {
		t.Fatal("catalog options")
	}
	if result, _ := send("GET", "/api/admin/v1/catalog-options", "", 401, nil); errorCode(result) != "unauthorized" {
		t.Fatal("anonymous catalog options")
	}
	if page, _ := send("GET", "/api/admin/v1/products?status=all&limit=5", "", 200, read); len(page["items"].([]any)) != 5 {
		t.Fatal("admin product page")
	}
	if result, _ := send("GET", "/api/admin/v1/products/missing", "", 404, read); errorCode(result) != "not_found" {
		t.Fatal("missing product")
	}
	if result, _ := send("POST", "/api/admin/v1/products", AdminProductBody("contract-bar", 10, 0), 403, map[string]string{"Cookie": cookie, "Origin": AdminTrustedOrigin}); errorCode(result) != "csrf_failed" {
		t.Fatal("product create without CSRF")
	}
	if result, _ := send("POST", "/api/admin/v1/products", AdminProductBody("contract-bar", -1, 0), 422, write); errorCode(result) != "validation_error" {
		t.Fatal("negative stock accepted")
	}
	created, _ := send("POST", "/api/admin/v1/products", AdminProductBody("contract-bar", 10, 0), 201, write)
	id := created["id"].(string)
	if revision(created) != 1 || created["archived"] != false || len(created["collections"].([]any)) != 1 {
		t.Fatalf("created product: %v", created)
	}
	path := "/api/admin/v1/products/" + id
	if got, _ := send("GET", path, "", 200, read); revision(got) != 1 {
		t.Fatal("product read")
	}
	updated, _ := send("PUT", path, AdminProductBody("contract-bar", 9, 1), 200, write)
	if revision(updated) != 2 || updated["stock"].(float64) != 9 {
		t.Fatalf("updated product: %v", updated)
	}
	if result, _ := send("PUT", path, AdminProductBody("contract-bar", 8, 1), 409, write); errorCode(result) != "stale_revision" {
		t.Fatal("stale update accepted")
	}
	archived, _ := send("POST", path+"/archive", `{"expectedRevision":2}`, 200, write)
	if revision(archived) != 3 || archived["archived"] != true || archived["archivedAt"] == nil {
		t.Fatalf("archived product: %v", archived)
	}
	if result, _ := send("POST", path+"/archive", `{"expectedRevision":3}`, 409, write); errorCode(result) != "conflict" {
		t.Fatal("archived twice")
	}
	if restored, _ := send("POST", path+"/restore", `{"expectedRevision":3}`, 200, write); revision(restored) != 4 || restored["archived"] != false {
		t.Fatalf("restored product: %v", restored)
	}
}
