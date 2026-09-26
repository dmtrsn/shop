// Package contract contains the shared HTTP contract suite for both adapters.
package contract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
)

func document(t *testing.T) *openapi3.T {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "../..")
	doc, err := openapi3.NewLoader().LoadFromFile(filepath.Join(root, "api/openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Fail if a route is added/removed without updating the contract (and vice versa).
	source, err := os.ReadFile(filepath.Join(root, "internal/delivery/httpapi/server.go"))
	if err != nil {
		t.Fatal(err)
	}
	routes := regexp.MustCompile(`mux\.Handle(?:Func)?\("(GET|POST|PATCH|PUT|DELETE) ([^"]+)"`).FindAllStringSubmatch(string(source), -1)
	actual := map[string]bool{}
	for _, route := range routes {
		actual[route[1]+" "+route[2]] = true
	}
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			key := method + " " + path
			if !actual[key] {
				t.Errorf("documented route not registered: %s", key)
			}
			delete(actual, key)
		}
	}
	for route := range actual {
		t.Errorf("registered route missing from OpenAPI: %s", route)
	}
	return doc
}

// Run requires a fresh demo fixture. Requests execute in order, with assertions
// on meaningful legacy behaviors as well as all operation response schemas.
func Run(t *testing.T, handler http.Handler) {
	t.Helper()
	doc := document(t)
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	token := ""
	request := func(method, path, body string, status int, auth bool) map[string]any {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", "contract-test")
		if auth {
			req.Header.Set("Authorization", "bEaReR "+token)
		}
		route, params, err := router.FindRoute(req)
		if err != nil {
			t.Fatal(err)
		}
		if status < 300 {
			// Validate the canonical success requests too. Keep decoder defaults
			// from changing what the real server receives.
			input := &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route,
				Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, SkipSettingDefaults: true}}
			if err := openapi3filter.ValidateRequest(context.Background(), input); err != nil {
				t.Fatalf("%s %s request contract: %v", method, path, err)
			}
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s status=%d want=%d", method, path, rec.Code, status)
		}
		if rec.Header().Get("X-Request-ID") != "contract-test" {
			t.Fatal("missing request ID")
		}
		if auth || path == "/api/auth/login" {
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private response missing no-store")
			}
		}
		validation := &openapi3filter.ResponseValidationInput{RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route}, Status: rec.Code, Header: rec.Header(), Options: &openapi3filter.Options{IncludeResponseStatus: true}}
		if err := openapi3filter.ValidateResponse(context.Background(), validation.SetBodyBytes(rec.Body.Bytes())); err != nil {
			t.Fatalf("%s %s response contract: %v", method, path, err)
		}
		if status < 300 {
			covered[route.Operation.OperationID] = true
		}
		var result map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if status >= 400 {
			e := result["error"].(map[string]any)
			if e["requestId"] != "contract-test" {
				t.Fatal("error request ID")
			}
		}
		return result
	}
	request("GET", "/api/health", "", 200, false)
	request("GET", "/api/cart", "", 401, false)
	request("POST", "/api/auth/login", `{"email":"demo@sugar.ru","password":"wrong"}`, 401, false)
	login := request("POST", "/api/auth/login", `{"email":" DEMO@SUGAR.RU ","password":"Sweet2026!"}`, 200, false)
	token = login["token"].(string)
	request("GET", "/api/me", "", 200, true)
	for _, path := range []string{"categories", "manufacturers", "brands", "collections", "catalog", "catalog/facets", "products/cha-701", "search/suggest?q=choco", "cart", "favorites", "orders", "orders/%2310482"} {
		request("GET", "/api/"+path, "", 200, path == "cart" || path == "favorites" || strings.HasPrefix(path, "orders"))
	}
	empty := request("GET", "/api/catalog?q=missing-product-xyz", "", 200, false)
	if empty["totalPages"] != float64(0) || len(empty["items"].([]any)) != 0 {
		t.Fatal("empty catalog envelope changed")
	}
	facets := request("GET", "/api/catalog/facets?q=missing-product-xyz&sort=ignored&page=1000001", "", 200, false)
	for _, key := range []string{"categories", "manufacturers", "brands", "tags", "series"} {
		if len(facets[key].([]any)) != 0 {
			t.Fatal("empty facets contract")
		}
	}
	short := request("GET", "/api/search/suggest?q=x", "", 200, false)
	if len(short["items"].([]any)) != 0 {
		t.Fatal("short suggestions")
	}
	page := request("GET", "/api/catalog?minPrice=48900&maxPrice=48900&size=2&page=-1", "", 200, false)
	if page["total"] != float64(1) || page["limit"] != float64(2) || page["page"] != float64(1) {
		t.Fatal("price/size aliases must use minor units")
	}
	page = request("GET", "/api/catalog?limit=101&size=2&minPriceMinor=0&minPrice=999999&maxPriceMinor=0", "", 200, false)
	if page["limit"] != float64(100) || page["total"] != float64(24) {
		t.Fatal("canonical alias precedence or cap changed")
	}
	canonical := request("GET", "/api/catalog?newOnly=false&new=true", "", 200, false)
	if canonical["total"] != float64(24) {
		t.Fatal("newOnly precedence")
	}
	newer := request("GET", "/api/catalog?new=1", "", 200, false)
	if newer["total"] != float64(4) {
		t.Fatal("new alias")
	}
	for _, query := range []string{"sort=priceAsc", "page=1000001", "page=abc", "limit=abc", "minPrice=-1", "inStock=invalid", "sale=invalid", "new=invalid", "q=" + strings.Repeat("a", 201)} {
		request("GET", "/api/catalog?"+query, "", 422, false)
	}
	request("GET", "/api/search/suggest?q="+strings.Repeat("a", 201), "", 422, false)
	request("GET", "/api/search/suggest?q="+strings.Repeat("a", 200), "", 200, false)
	request("GET", "/api/products/missing", "", 404, false)
	request("GET", "/api/orders/missing", "", 404, true)
	request("POST", "/api/orders/ord-2026-002/repeat/preview", "not JSON", 200, true)
	request("POST", "/api/orders/ord-2026-002/repeat/commit", `{"items":[{"productId":"p01","quantity":1,"selected":true},{"productId":"p07","quantity":1,"selected":false}]}`, 200, true)
	request("POST", "/api/orders/ord-2026-002/repeat/commit", `{"items":[{"productId":"p18","quantity":1,"selected":true}]}`, 422, true)
	request("POST", "/api/orders/ord-2026-002/repeat/commit", `{"items":[{"productId":"p01","selected":false},{"productId":"p01","selected":false}]}`, 422, true)
	// Missing/null/empty items all retain default-addition behavior.
	for _, body := range []string{`{}`, `null`, `{"items":[]}`, `{"items":null}`} {
		r := request("POST", "/api/orders/ord-2026-002/repeat/commit", body, 200, true)
		if r["addedCount"].(float64) < 1 {
			t.Fatal("legacy default repeat additions disappeared")
		}
	}
	request("POST", "/api/cart/items", `{"productId":"p01","quantity":2}`, 201, true)
	cart := request("POST", "/api/cart/items", `{"productId":"p01","quantity":3,"selected":null}`, 201, true)
	assertLine(t, cart, "p01", 3, true)
	request("POST", "/api/cart/items", `{"productId":"p18","quantity":1}`, 409, true)
	request("POST", "/api/cart/items", `{"productId":"p01","quantity":0}`, 422, true)
	request("POST", "/api/cart/items", `{"productId":"missing","quantity":1}`, 404, true)
	request("PATCH", "/api/cart/items/cha-701", `{"quantity":2,"selected":false}`, 200, true)
	request("PATCH", "/api/cart/items/p01", `{}`, 422, true)
	request("PATCH", "/api/cart/items/missing", `{"selected":false}`, 404, true)
	request("PATCH", "/api/cart/selection", `{"productIds":["p18"],"selected":true}`, 200, true)
	request("POST", "/api/orders", `{"deliveryAddress":"Test"}`, 409, true)
	request("PATCH", "/api/cart/items/p18", `{"selected":false}`, 200, true)
	request("DELETE", "/api/cart/items/cha-701", "", 200, true)
	request("PUT", "/api/favorites/cha-701", "ignored body", 200, true)
	request("DELETE", "/api/favorites/cha-701", "", 200, true)
	request("DELETE", "/api/favorites/missing", "", 404, true)
	for _, body := range []string{"", `{`, `{} {}`, `{"unexpected":true}`, `{"productId":"` + strings.Repeat("a", 1<<20) + `","quantity":1}`} {
		request("POST", "/api/cart/items", body, 422, true)
	}
	request("POST", "/api/orders", `{"deliveryAddress":"  "}`, 422, true)
	// The malformed boolean is intentionally destructive in the CURRENT contract.
	cart = request("DELETE", "/api/cart?selectedOnly=invalid", "", 200, true)
	if len(cart["items"].([]any)) != 0 {
		t.Fatal("legacy malformed selectedOnly behavior changed")
	}
	request("POST", "/api/orders", `{"deliveryAddress":"Test"}`, 409, true)
	request("POST", "/api/cart/items", `{"productId":"p01","quantity":2}`, 201, true)
	request("POST", "/api/cart/items", `{"productId":"p02","quantity":1,"selected":false}`, 201, true)
	order := request("POST", "/api/orders", `{"deliveryAddress":" Test "}`, 201, true)
	if order["itemCount"] != float64(2) || order["totalMinor"] != float64(126800) {
		t.Fatal("checkout monetary contract")
	}
	cart = request("GET", "/api/cart", "", 200, true)
	assertLine(t, cart, "p02", 1, false)
	if len(cart["items"].([]any)) != 1 {
		t.Fatal("purchased lines remain")
	}
	request("PATCH", "/api/cart/selection", `{"productIds":[],"selected":true}`, 200, true)
	cart = request("DELETE", "/api/cart?selectedOnly=true", "", 200, true)
	if len(cart["items"].([]any)) != 0 || len(cart["groups"].([]any)) != 0 {
		t.Fatal("empty cart arrays")
	}
	for path, item := range doc.Paths.Map() {
		for method, operation := range item.Operations() {
			if !covered[operation.OperationID] {
				t.Errorf("no success response checked for %s %s", method, path)
			}
		}
	}
}

func assertLine(t *testing.T, cart map[string]any, id string, quantity int, selected bool) {
	t.Helper()
	for _, v := range cart["items"].([]any) {
		line := v.(map[string]any)
		if line["product"].(map[string]any)["id"] == id {
			if line["quantity"] != float64(quantity) || line["selected"] != selected {
				t.Fatalf("unexpected cart line %s", id)
			}
			return
		}
	}
	t.Fatalf("missing cart line %s", id)
}
