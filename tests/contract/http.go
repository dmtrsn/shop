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
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"

	"sweetshop/internal/delivery/httpapi"
)

// TrustedOrigin is the only browser origin the contract handler trusts.
const TrustedOrigin = "https://shop.example"

// SessionConfig is the production-shaped session policy used by the suite:
// Secure (__Host-) cookie and a single https origin.
func SessionConfig() httpapi.SessionConfig {
	return httpapi.SessionConfig{Secure: true, TrustedOrigins: []string{TrustedOrigin}}
}

const sessionCookieName = "__Host-sweetshop_session"

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
	send := func(method, path, body string, status int, headers map[string]string) (map[string]any, *httptest.ResponseRecorder) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", "contract-test")
		for key, value := range headers {
			req.Header.Set(key, value)
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
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, rec.Code, status, rec.Body.String())
		}
		if rec.Header().Get("X-Request-ID") != "contract-test" {
			t.Fatal("missing request ID")
		}
		if headers["Authorization"] != "" || headers["Cookie"] != "" || path == "/api/auth/login" || strings.HasPrefix(path, "/api/auth/session") {
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private response missing no-store")
			}
		}
		protected := path == "/api/me" || strings.HasPrefix(path, "/api/cart") || strings.HasPrefix(path, "/api/favorites") || strings.HasPrefix(path, "/api/orders")
		if protected {
			owner := rec.Header().Get("X-Authenticated-User-ID")
			if status == 401 || status == 403 {
				if owner != "" {
					t.Fatal("rejected request exposes owner")
				}
			} else if owner != "usr-demo" {
				t.Fatalf("%s %s owner=%q", method, path, owner)
			}
		}
		validation := &openapi3filter.ResponseValidationInput{RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route}, Status: rec.Code, Header: rec.Header(), Options: &openapi3filter.Options{IncludeResponseStatus: true}}
		if err := openapi3filter.ValidateResponse(context.Background(), validation.SetBodyBytes(rec.Body.Bytes())); err != nil {
			t.Fatalf("%s %s response contract: %v", method, path, err)
		}
		if status < 300 {
			covered[route.Operation.OperationID] = true
		}
		if rec.Code == http.StatusNoContent {
			if rec.Body.Len() != 0 {
				t.Fatal("204 response has a body")
			}
			return nil, rec
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
		return result, rec
	}
	request := func(method, path, body string, status int, auth bool) map[string]any {
		t.Helper()
		headers := map[string]string{}
		if auth {
			headers["Authorization"] = "bEaReR " + token
		}
		result, _ := send(method, path, body, status, headers)
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
	legacyPreview := request("POST", "/api/orders/ord-2026-002/repeat/preview", "not JSON", 200, true)
	legacyLines := legacyPreview["items"].([]any)
	if legacyLines[3].(map[string]any)["replacement"].(map[string]any)["id"] != "p06" {
		t.Fatal("legacy informational replacement disappeared")
	}
	legacyResult := request("POST", "/api/orders/ord-2026-002/repeat/commit", `{"items":[{"productId":"p01","quantity":1,"selected":true},{"productId":"p07","quantity":1,"selected":false}]}`, 200, true)
	assertLine(t, legacyResult["cart"].(map[string]any), "p01", 3, true)
	if legacyResult["addedQuantity"] != float64(1) || legacyResult["skippedCount"] != float64(1) {
		t.Fatal("legacy addition/selected semantics changed")
	}
	request("POST", "/api/orders/ord-2026-002/repeat/commit", `{"items":[{"productId":"p06","quantity":1,"selected":true}]}`, 422, true)
	request("POST", "/api/orders/ord-2026-002/repeat/commit", `{"items":[{"productId":"p18","quantity":1,"selected":true}]}`, 422, true)
	request("POST", "/api/orders/ord-2026-002/repeat/commit", `{"items":[{"productId":"p01","selected":false},{"productId":"p01","selected":false}]}`, 422, true)
	// Missing/null/empty items all retain default-addition behavior.
	for _, body := range []string{`{}`, `null`, `{"items":[]}`, `{"items":null}`} {
		r := request("POST", "/api/orders/ord-2026-002/repeat/commit", body, 200, true)
		if r["addedCount"].(float64) < 1 {
			t.Fatal("legacy default repeat additions disappeared")
		}
	}
	// Strict v2 is additive: explicit selections, receipt and current Cart replay.
	v2Preview := request("POST", "/api/v2/orders/ord-2026-002/repeat/preview", "", 200, true)
	line := v2Preview["items"].([]any)[0].(map[string]any)
	product := line["product"].(map[string]any)
	v2Body, _ := json.Marshal(map[string]any{"items": []any{map[string]any{
		"sourcePosition": line["sourcePosition"], "productId": line["productId"], "quantity": 1,
		"expectedExistingCartQuantity": line["existingCartQuantity"], "expectedUnitPriceMinor": product["priceMinor"],
		"expectedCurrency": product["currency"], "expectedMinimumQuantity": product["minimumQuantity"], "expectedQuantityStep": product["quantityStep"],
	}}})
	v2Headers := map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "contract-repeat-v2-0001"}
	for _, body := range []string{"", "null", "{}", `{"items":null}`, `{"items":[]}`} {
		send("POST", "/api/v2/orders/ord-2026-002/repeat/commit", body, 422, v2Headers)
	}
	send("POST", "/api/v2/orders/ord-2026-002/repeat/commit", string(v2Body), 200, v2Headers)
	_, replay := send("POST", "/api/v2/orders/ord-2026-002/repeat/commit", string(v2Body), 200, v2Headers)
	if replay.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatal("v2 receipt did not replay")
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
	favorites := request("PUT", "/api/favorites/cha-701", "ignored body", 200, true)
	repeatedFavorite := request("PUT", "/api/favorites/cha-701", "", 200, true)
	if favorites["total"] != repeatedFavorite["total"] {
		t.Fatal("repeated favorite PUT changed membership count")
	}
	favorites = request("DELETE", "/api/favorites/cha-701", "", 200, true)
	repeatedFavorite = request("DELETE", "/api/favorites/cha-701", "", 200, true)
	if favorites["total"] != repeatedFavorite["total"] {
		t.Fatal("repeated favorite DELETE changed membership count")
	}
	request("PUT", "/api/favorites/missing", "", 404, true)
	request("DELETE", "/api/favorites/missing", "", 404, true)
	// p18 is out of stock (the cart request above returns 409), but remains
	// eligible for favorites. Both adapters must return it as a full Product.
	favorites = request("PUT", "/api/favorites/p18", "", 200, true)
	foundUnavailable := false
	for _, item := range favorites["items"].([]any) {
		product := item.(map[string]any)
		if product["id"] == "p18" && product["stock"] == float64(0) {
			foundUnavailable = true
		}
	}
	if !foundUnavailable {
		t.Fatal("out-of-stock favorite missing from full product response")
	}
	request("DELETE", "/api/favorites/p18", "", 200, true)
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
	runCheckoutIdempotency(t, send, token)
	runCookieSessions(t, send)
	runGuardedLogout(t, send)
	// Bearer logout revokes only the presented bearer session.
	request("DELETE", "/api/auth/session", "", 204, true)
	request("GET", "/api/cart", "", 401, true)
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

type sender func(method, path, body string, status int, headers map[string]string) (map[string]any, *httptest.ResponseRecorder)

// runCookieSessions exercises the browser session contract: login CSRF, cookie
// attributes, safe/unsafe requests, CSRF rejection and server-side logout.
func runCookieSessions(t *testing.T, send sender) {
	t.Helper()
	const credentials = `{"email":"demo@sugar.ru","password":"Sweet2026!"}`
	anonymous, _ := send("GET", "/api/auth/session", "", 200, nil)
	if anonymous["authenticated"] != false || anonymous["demoLogin"] != true || anonymous["csrfToken"] != nil {
		t.Fatal("anonymous session state")
	}
	send("POST", "/api/auth/session", credentials, 403, nil)
	send("POST", "/api/auth/session", credentials, 403, map[string]string{"Origin": "https://evil.example"})
	send("POST", "/api/auth/session", `{"email":"demo@sugar.ru","password":"wrong"}`, 401, map[string]string{"Origin": TrustedOrigin})
	login, rec := send("POST", "/api/auth/session", credentials, 200, map[string]string{"Origin": TrustedOrigin})
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value == "" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge <= 0 {
		t.Fatalf("session cookie attributes: %#v", cookie)
	}
	if login["token"] != nil || strings.Contains(rec.Body.String(), cookie.Value) {
		t.Fatal("cookie login exposed the session token")
	}
	csrf, _ := login["csrfToken"].(string)
	if csrf == "" {
		t.Fatal("missing csrf token")
	}
	jar := map[string]string{"Cookie": sessionCookieName + "=" + cookie.Value}
	with := func(extra map[string]string) map[string]string {
		headers := map[string]string{}
		for k, v := range jar {
			headers[k] = v
		}
		for k, v := range extra {
			headers[k] = v
		}
		return headers
	}
	state, _ := send("GET", "/api/auth/session", "", 200, jar)
	if state["authenticated"] != true || state["csrfToken"] != csrf {
		t.Fatal("cookie session state")
	}
	send("GET", "/api/me", "", 200, jar)
	send("GET", "/api/cart", "", 200, jar)
	send("PUT", "/api/favorites/p02", "", 403, jar)
	send("PUT", "/api/favorites/p02", "", 403, with(map[string]string{"Origin": TrustedOrigin}))
	send("PUT", "/api/favorites/p02", "", 403, with(map[string]string{"Origin": "https://evil.example", "X-CSRF-Token": csrf}))
	send("PUT", "/api/favorites/p02", "", 200, with(map[string]string{"Origin": TrustedOrigin, "X-CSRF-Token": csrf}))
	send("DELETE", "/api/auth/session", "", 403, jar)
	_, rec = send("DELETE", "/api/auth/session", "", 204, with(map[string]string{"Origin": TrustedOrigin, "X-CSRF-Token": csrf}))
	cleared := rec.Result().Cookies()
	if len(cleared) != 1 || cleared[0].Name != sessionCookieName || cleared[0].MaxAge >= 0 || cleared[0].Value != "" {
		t.Fatal("logout did not clear the session cookie")
	}
	send("GET", "/api/cart", "", 401, jar)
	gone, _ := send("GET", "/api/auth/session", "", 200, jar)
	if gone["authenticated"] != false {
		t.Fatal("revoked cookie still authenticated")
	}
}

// runCheckoutIdempotency starts from an empty cart. It covers the optional
// expected-user precondition, the review guard and the Idempotency-Key replay.
func runCheckoutIdempotency(t *testing.T, send sender, token string) {
	t.Helper()
	auth := func(extra map[string]string) map[string]string {
		headers := map[string]string{"Authorization": "Bearer " + token}
		for k, v := range extra {
			headers[k] = v
		}
		return headers
	}
	// The precondition passes for the authenticated user and stops anyone else
	// before the handler: the cart stays empty.
	send("GET", "/api/cart", "", 200, auth(map[string]string{"X-Expected-Authenticated-User-ID": "usr-demo"}))
	expectCode(t, "authenticated_user_changed")(send("POST", "/api/cart/items", `{"productId":"p01","quantity":2}`, 409, auth(map[string]string{"X-Expected-Authenticated-User-ID": "usr-other"})))
	expectCode(t, "authenticated_user_changed")(send("GET", "/api/cart", "", 409, auth(map[string]string{"X-Expected-Authenticated-User-ID": "usr-other"})))
	if cart, _ := send("GET", "/api/cart", "", 200, auth(nil)); len(cart["items"].([]any)) != 0 {
		t.Fatal("precondition failure changed the cart")
	}
	send("POST", "/api/cart/items", `{"productId":"p01","quantity":2}`, 201, auth(nil))
	send("POST", "/api/cart/items", `{"productId":"p10","quantity":1}`, 201, auth(nil))
	send("POST", "/api/cart/items", `{"productId":"p02","quantity":1,"selected":false}`, 201, auth(nil))
	cart, _ := send("GET", "/api/cart", "", 200, auth(nil))
	items := []map[string]any{}
	for _, v := range cart["items"].([]any) {
		line := v.(map[string]any)
		if line["selected"] == true {
			product := line["product"].(map[string]any)
			items = append(items, map[string]any{"productId": product["id"], "quantity": line["quantity"], "unitPriceMinor": product["priceMinor"]})
		}
	}
	total := cart["summary"].(map[string]any)["totalMinor"].(float64)
	body := func(address string, items []map[string]any, total float64) string {
		encoded, err := json.Marshal(map[string]any{"deliveryAddress": address, "comment": nil, "expectedItems": items, "expectedTotalMinor": total})
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	stock := func() float64 {
		product, _ := send("GET", "/api/products/p01", "", 200, nil)
		return product["stock"].(float64)
	}
	const key = "contract-checkout-key-0001"
	keyed := auth(map[string]string{"Idempotency-Key": key, "X-Expected-Authenticated-User-ID": "usr-demo"})
	before := stock()
	expectCode(t, "checkout_changed")(send("POST", "/api/orders", body("Test", items, total+1), 409, keyed))
	expectCode(t, "checkout_changed")(send("POST", "/api/orders", body("Test", items[:1], total), 409, keyed))
	for _, invalid := range []string{
		`{"deliveryAddress":"Test","expectedTotalMinor":1}`,
		`{"deliveryAddress":"Test","expectedItems":[],"expectedTotalMinor":1}`,
		`{"deliveryAddress":"Test","requestDigest":"0000"}`,
	} {
		send("POST", "/api/orders", invalid, 422, keyed)
	}
	send("POST", "/api/orders", body("Test", items, total), 422, auth(map[string]string{"Idempotency-Key": "short"}))
	if after, _ := send("GET", "/api/cart", "", 200, auth(nil)); len(after["items"].([]any)) != 3 || stock() != before {
		t.Fatal("rejected checkout wrote")
	}
	first, rec := send("POST", "/api/orders", body(" Test ", items, total), 201, keyed)
	if rec.Header().Get("Idempotent-Replayed") != "" || first["totalMinor"] != total || first["deliveryAddress"] != "Test" {
		t.Fatal("keyed checkout")
	}
	// The retry finds an emptied selection but replays the committed order;
	// omitted and null comment are the same logical request.
	retry := `{"deliveryAddress":"Test","expectedItems":` + string(must(json.Marshal(items))) + `,"expectedTotalMinor":` + strconv.FormatFloat(total, 'f', -1, 64) + `}`
	replay, rec := send("POST", "/api/orders", retry, 201, keyed)
	if rec.Header().Get("Idempotent-Replayed") != "true" || replay["id"] != first["id"] || replay["totalMinor"] != first["totalMinor"] {
		t.Fatal("idempotent replay")
	}
	if stock() != before-2 {
		t.Fatal("replay decremented stock again")
	}
	if remaining, _ := send("GET", "/api/cart", "", 200, auth(nil)); len(remaining["items"].([]any)) != 1 {
		t.Fatal("replay changed the remaining cart")
	}
	expectCode(t, "idempotency_key_reused")(send("POST", "/api/orders", body("Other", items, total), 409, keyed))
	expectCode(t, "conflict")(send("POST", "/api/orders", body("Test", items, total), 409, auth(map[string]string{"Idempotency-Key": "contract-checkout-key-0002"})))
	send("DELETE", "/api/cart/items/p02", "", 200, auth(nil))
}

func expectCode(t *testing.T, code string) func(map[string]any, *httptest.ResponseRecorder) {
	t.Helper()
	return func(result map[string]any, _ *httptest.ResponseRecorder) {
		t.Helper()
		if got := result["error"].(map[string]any)["code"]; got != code {
			t.Fatalf("error code=%v want %s", got, code)
		}
	}
}

func must(value []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return value
}
