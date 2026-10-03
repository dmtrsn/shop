package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/internal/repository/memory"
)

// dataCalls counts every repository call a protected handler can make. Login
// and session lookups are deliberately not counted: they run before handlers.
type dataCalls struct {
	*memory.Store
	calls *atomic.Int64
}

func (r dataCalls) Product(ctx context.Context, id string) (domain.Product, error) {
	r.calls.Add(1)
	return r.Store.Product(ctx, id)
}
func (r dataCalls) Products(ctx context.Context, ids []string) (map[string]domain.Product, error) {
	r.calls.Add(1)
	return r.Store.Products(ctx, ids)
}
func (r dataCalls) Cart(ctx context.Context, user string) (domain.Cart, error) {
	r.calls.Add(1)
	return r.Store.Cart(ctx, user)
}
func (r dataCalls) SetCartItem(ctx context.Context, user, product string, quantity int, selected bool) (domain.Cart, error) {
	r.calls.Add(1)
	return r.Store.SetCartItem(ctx, user, product, quantity, selected)
}
func (r dataCalls) RemoveCartItem(ctx context.Context, user, product string) (domain.Cart, error) {
	r.calls.Add(1)
	return r.Store.RemoveCartItem(ctx, user, product)
}
func (r dataCalls) ClearCart(ctx context.Context, user string, selectedOnly bool) (domain.Cart, error) {
	r.calls.Add(1)
	return r.Store.ClearCart(ctx, user, selectedOnly)
}
func (r dataCalls) SetCartSelection(ctx context.Context, user string, ids []string, selected bool) (domain.Cart, error) {
	r.calls.Add(1)
	return r.Store.SetCartSelection(ctx, user, ids, selected)
}
func (r dataCalls) AddCartItems(ctx context.Context, user string, mutations []domain.CartMutation) (domain.Cart, error) {
	r.calls.Add(1)
	return r.Store.AddCartItems(ctx, user, mutations)
}
func (r dataCalls) Favorites(ctx context.Context, user string) (domain.Favorites, error) {
	r.calls.Add(1)
	return r.Store.Favorites(ctx, user)
}
func (r dataCalls) SetFavorite(ctx context.Context, user, product string, enabled bool) (domain.Favorites, error) {
	r.calls.Add(1)
	return r.Store.SetFavorite(ctx, user, product, enabled)
}
func (r dataCalls) ListOrders(ctx context.Context, user string) ([]domain.Order, error) {
	r.calls.Add(1)
	return r.Store.ListOrders(ctx, user)
}
func (r dataCalls) Order(ctx context.Context, user, order string) (domain.Order, error) {
	r.calls.Add(1)
	return r.Store.Order(ctx, user, order)
}
func (r dataCalls) CreateOrder(ctx context.Context, user string, input domain.CheckoutInput) (domain.Order, bool, error) {
	r.calls.Add(1)
	return r.Store.CreateOrder(ctx, user, input)
}

type privateState struct {
	cart      domain.Cart
	favorites domain.Favorites
	orders    []domain.Order
	stock     map[string]int
}

func snapshot(t *testing.T, store *memory.Store) privateState {
	t.Helper()
	ctx := context.Background()
	cart, err := store.Cart(ctx, domain.DemoUserID)
	if err != nil {
		t.Fatal(err)
	}
	favorites, err := store.Favorites(ctx, domain.DemoUserID)
	if err != nil {
		t.Fatal(err)
	}
	orders, err := store.ListOrders(ctx, domain.DemoUserID)
	if err != nil {
		t.Fatal(err)
	}
	products, err := store.Products(ctx, []string{"p01", "p02", "p03", "p10", "p16", "p18"})
	if err != nil {
		t.Fatal(err)
	}
	stock := map[string]int{}
	for id, product := range products {
		stock[id] = product.Stock
	}
	return privateState{cart, favorites, orders, stock}
}

// send allows repeated header values, which harness.do cannot express.
func send(h *harness, method, path, body string, headers map[string]string, expected ...string) *httptest.ResponseRecorder {
	h.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	for _, value := range expected {
		request.Header.Add("X-Expected-Authenticated-User-ID", value)
	}
	response := httptest.NewRecorder()
	h.handler.ServeHTTP(response, request)
	return response
}

var protectedRoutes = []struct{ method, path, body string }{
	{"GET", "/api/me", ""},
	{"GET", "/api/cart", ""},
	{"HEAD", "/api/cart", ""},
	{"GET", "/api/favorites", ""},
	{"GET", "/api/orders", ""},
	{"GET", "/api/orders/ord-2026-002", ""},
	{"POST", "/api/cart/items", `{"productId":"p03","quantity":1}`},
	{"PATCH", "/api/cart/items/p01", `{"quantity":1}`},
	{"PATCH", "/api/cart/items/p01", `{"selected":false}`},
	{"PATCH", "/api/cart/selection", `{"productIds":["p16"],"selected":true}`},
	{"DELETE", "/api/cart/items/p01", ""},
	{"DELETE", "/api/cart", ""},
	{"DELETE", "/api/cart?selectedOnly=true", ""},
	{"PUT", "/api/favorites/p03", ""},
	{"DELETE", "/api/favorites/p02", ""},
	{"POST", "/api/orders/ord-2026-002/repeat/preview", ""},
	{"POST", "/api/orders/ord-2026-002/repeat/commit", `{"items":[{"productId":"p01","quantity":1,"selected":true}]}`},
	{"POST", "/api/orders", `{"deliveryAddress":"Test"}`},
}

// A stale client that expects user A while the request authenticates as B must
// be stopped before any handler runs. B is the demo user; its cookie requests
// carry B's own valid CSRF token, so only the new precondition can reject them.
func TestExpectedUserMismatchStopsEveryProtectedHandler(t *testing.T) {
	store := memory.NewDemo()
	calls := &atomic.Int64{}
	h := newHarness(t, dataCalls{Store: store, calls: calls}, application.Options{AllowDemoAccount: true}, testSessions)
	cookie, csrf := h.cookieLogin(nil)
	bearer := loginHTTP(t, h.handler)
	before := snapshot(t, store)
	for name, credentials := range map[string]map[string]string{
		"bearer B":               {"Authorization": "Bearer " + bearer},
		"cookie B with B's CSRF": h.mutation(cookie, csrf),
	} {
		for expectation, values := range map[string][]string{
			"user A":           {"usr-a"},
			"empty":            {""},
			"repeated":         {domain.DemoUserID, domain.DemoUserID},
			"repeated with A":  {domain.DemoUserID, "usr-a"},
			"case-changed B":   {strings.ToUpper(domain.DemoUserID)},
			"spoofed owner, A": {"usr-a"},
		} {
			headers := map[string]string{}
			for key, value := range credentials {
				headers[key] = value
			}
			if expectation == "spoofed owner, A" {
				// An incoming owner header is never an input.
				headers["X-Authenticated-User-ID"] = "usr-a"
			}
			for _, route := range protectedRoutes {
				t.Run(name+"/"+expectation+"/"+route.method+" "+route.path, func(t *testing.T) {
					response := send(h, route.method, route.path, route.body, headers, values...)
					expectStatus(t, response, http.StatusConflict)
					if response.Header().Get("X-Authenticated-User-ID") != domain.DemoUserID {
						t.Fatalf("owner=%q, want the actual authenticated user", response.Header().Get("X-Authenticated-User-ID"))
					}
					if response.Header().Get("Cache-Control") != "no-store" {
						t.Fatal("missing no-store")
					}
					if route.method != http.MethodHead {
						if code := errorCode(t, response); code != "authenticated_user_changed" {
							t.Fatalf("code=%q", code)
						}
						// Only the error envelope: no private handler output.
						if strings.Contains(response.Body.String(), `"items"`) || strings.Contains(response.Body.String(), `"user"`) {
							t.Fatalf("private body leaked: %s", response.Body.String())
						}
					}
				})
			}
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("protected handlers reached the repository %d times", got)
	}
	if after := snapshot(t, store); !reflect.DeepEqual(before, after) {
		t.Fatal("rejected requests changed cart, favorites, orders or stock")
	}
	if !strings.Contains(h.logs.String(), `"msg":"expected user precondition failed"`) {
		t.Fatal("precondition rejection was not logged")
	}
}

func TestExpectedUserMatchAndOmissionReachHandlers(t *testing.T) {
	store := memory.NewDemo()
	calls := &atomic.Int64{}
	h := newHarness(t, dataCalls{Store: store, calls: calls}, application.Options{AllowDemoAccount: true}, testSessions)
	cookie, csrf := h.cookieLogin(nil)
	bearer := loginHTTP(t, h.handler)
	for name, credentials := range map[string]map[string]string{
		"bearer": {"Authorization": "Bearer " + bearer},
		"cookie": h.mutation(cookie, csrf),
		"cookie, spoof owner": func() map[string]string {
			m := h.mutation(cookie, csrf)
			m["X-Authenticated-User-ID"] = "usr-a"
			return m
		}(),
	} {
		for _, expected := range [][]string{{domain.DemoUserID}, nil} {
			t.Run(name+"/"+strings.Join(expected, ","), func(t *testing.T) {
				before := calls.Load()
				response := send(h, "GET", "/api/cart", "", credentials, expected...)
				expectStatus(t, response, http.StatusOK)
				response = send(h, "PUT", "/api/favorites/p03", "", credentials, expected...)
				expectStatus(t, response, http.StatusOK)
				if response.Header().Get("X-Authenticated-User-ID") != domain.DemoUserID {
					t.Fatal("owner header")
				}
				if calls.Load() == before {
					t.Fatal("matching or omitted expectation did not reach the handler")
				}
			})
		}
	}
	favorites, err := store.Favorites(context.Background(), domain.DemoUserID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, product := range favorites.Items {
		found = found || product.ID == "p03"
	}
	if !found {
		t.Fatal("matching expectation did not apply the mutation")
	}
	// Matching expectation reaches checkout and creates exactly one order.
	existing, err := store.ListOrders(context.Background(), domain.DemoUserID)
	if err != nil {
		t.Fatal(err)
	}
	response := send(h, "POST", "/api/orders", `{"deliveryAddress":"Test"}`, h.mutation(cookie, csrf), domain.DemoUserID)
	expectStatus(t, response, http.StatusCreated)
	orders, err := store.ListOrders(context.Background(), domain.DemoUserID)
	if err != nil || len(orders) != len(existing)+1 {
		t.Fatalf("orders=%d err=%v", len(orders), err)
	}
}

// CSRF stays the first gate for cookie mutations: a wrong token is rejected as
// csrf_failed without an owner, whatever the expectation says.
func TestCSRFPrecedesExpectedUserPrecondition(t *testing.T) {
	h := demoHarness(t)
	cookie, _ := h.cookieLogin(nil)
	response := send(h, "POST", "/api/orders", `{"deliveryAddress":"Test"}`, h.mutation(cookie, "wrong-csrf"), "usr-a")
	expectStatus(t, response, http.StatusForbidden)
	if errorCode(t, response) != "csrf_failed" || response.Header().Get("X-Authenticated-User-ID") != "" {
		t.Fatal("csrf rejection must precede the expected-user precondition")
	}
	response = send(h, "GET", "/api/cart", "", map[string]string{"Authorization": "Bearer invalid"}, "usr-a")
	expectStatus(t, response, http.StatusUnauthorized)
	if response.Header().Get("X-Authenticated-User-ID") != "" {
		t.Fatal("unauthenticated request has owner")
	}
}
