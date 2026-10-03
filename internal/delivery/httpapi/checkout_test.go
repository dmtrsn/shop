package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/internal/repository/memory"
)

const checkoutKey = "0f8b2f7e-6c1d-4a51-9a39-2c0f8a1e5b77"

func checkoutRequest(h *harness, token, body string, keys ...string) *httptest.ResponseRecorder {
	h.t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/orders", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	for _, key := range keys {
		request.Header.Add("Idempotency-Key", key)
	}
	response := httptest.NewRecorder()
	h.handler.ServeHTTP(response, request)
	return response
}

// reviewBody builds the expectation from the current authoritative cart.
func reviewBody(t *testing.T, store *memory.Store, address string) string {
	t.Helper()
	cart, err := store.Cart(context.Background(), domain.DemoUserID)
	if err != nil {
		t.Fatal(err)
	}
	items := []domain.ExpectedCheckoutItem{}
	for _, line := range cart.Items {
		if line.Selected {
			items = append(items, domain.ExpectedCheckoutItem{ProductID: line.Product.ID, Quantity: line.Quantity, UnitPriceMinor: line.Product.PriceMinor})
		}
	}
	body, err := json.Marshal(map[string]any{"deliveryAddress": address, "expectedItems": items, "expectedTotalMinor": cart.Summary.TotalMinor})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func fieldNames(t *testing.T, response *httptest.ResponseRecorder) []string {
	t.Helper()
	var body struct {
		Error struct {
			Fields []domain.FieldError `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, field := range body.Error.Fields {
		names = append(names, field.Field)
	}
	return names
}

func TestCheckoutRequestValidationWritesNothing(t *testing.T) {
	store := memory.NewDemo()
	h := newHarness(t, store, application.Options{AllowDemoAccount: true}, testSessions)
	token := loginHTTP(t, h.handler)
	before := snapshot(t, store)
	valid := reviewBody(t, store, "Test")
	for name, tc := range map[string]struct {
		body  string
		keys  []string
		field string
	}{
		"short key":                {valid, []string{"too-short"}, "Idempotency-Key"},
		"empty key":                {valid, []string{""}, "Idempotency-Key"},
		"long key":                 {valid, []string{strings.Repeat("k", 129)}, "Idempotency-Key"},
		"key with comma":           {valid, []string{checkoutKey + "," + checkoutKey}, "Idempotency-Key"},
		"repeated key":             {valid, []string{checkoutKey, checkoutKey}, "Idempotency-Key"},
		"client digest":            {`{"deliveryAddress":"Test","requestDigest":"abc"}`, []string{checkoutKey}, "body"},
		"client key in body":       {`{"deliveryAddress":"Test","idempotencyKey":"` + checkoutKey + `"}`, nil, "body"},
		"items without total":      {`{"deliveryAddress":"Test","expectedItems":[{"productId":"p01","quantity":2,"unitPriceMinor":48900}]}`, nil, "expectedItems"},
		"total without items":      {`{"deliveryAddress":"Test","expectedTotalMinor":1}`, nil, "expectedItems"},
		"null items, total":        {`{"deliveryAddress":"Test","expectedItems":null,"expectedTotalMinor":1}`, nil, "expectedItems"},
		"empty items":              {`{"deliveryAddress":"Test","expectedItems":[],"expectedTotalMinor":1}`, nil, "expectedItems"},
		"duplicate product":        {`{"deliveryAddress":"Test","expectedItems":[{"productId":"p01","quantity":2,"unitPriceMinor":1},{"productId":"p01","quantity":2,"unitPriceMinor":1}],"expectedTotalMinor":1}`, nil, "expectedItems.1.productId"},
		"empty product":            {`{"deliveryAddress":"Test","expectedItems":[{"productId":"","quantity":2,"unitPriceMinor":1}],"expectedTotalMinor":1}`, nil, "expectedItems.0.productId"},
		"zero quantity":            {`{"deliveryAddress":"Test","expectedItems":[{"productId":"p01","quantity":0,"unitPriceMinor":1}],"expectedTotalMinor":1}`, nil, "expectedItems.0.quantity"},
		"negative price":           {`{"deliveryAddress":"Test","expectedItems":[{"productId":"p01","quantity":1,"unitPriceMinor":-1}],"expectedTotalMinor":1}`, nil, "expectedItems.0.unitPriceMinor"},
		"negative total":           {`{"deliveryAddress":"Test","expectedItems":[{"productId":"p01","quantity":1,"unitPriceMinor":1}],"expectedTotalMinor":-1}`, nil, "expectedTotalMinor"},
		"unknown item field":       {`{"deliveryAddress":"Test","expectedItems":[{"productId":"p01","quantity":1,"unitPriceMinor":1,"name":"x"}],"expectedTotalMinor":1}`, nil, "body"},
		"blank address, valid key": {`{"deliveryAddress":"  "}`, []string{checkoutKey}, "deliveryAddress"},
	} {
		t.Run(name, func(t *testing.T) {
			response := checkoutRequest(h, token, tc.body, tc.keys...)
			expectStatus(t, response, http.StatusUnprocessableEntity)
			if errorCode(t, response) != "validation_error" {
				t.Fatal("expected validation_error")
			}
			if fields := fieldNames(t, response); len(fields) == 0 || fields[0] != tc.field {
				t.Fatalf("fields=%v want %s", fields, tc.field)
			}
		})
	}
	if !reflect.DeepEqual(before, snapshot(t, store)) {
		t.Fatal("rejected checkout changed state")
	}
	// A key that only failed validation was never recorded and stays usable.
	response := checkoutRequest(h, token, valid, checkoutKey)
	expectStatus(t, response, http.StatusCreated)
}

func TestCheckoutIdempotentReplayOverHTTP(t *testing.T) {
	store := memory.NewDemo()
	h := newHarness(t, store, application.Options{AllowDemoAccount: true}, testSessions)
	token := loginHTTP(t, h.handler)
	body := reviewBody(t, store, "Test")
	first := checkoutRequest(h, token, body, checkoutKey)
	expectStatus(t, first, http.StatusCreated)
	if first.Header().Get("Idempotent-Replayed") != "" {
		t.Fatal("first checkout marked as replay")
	}
	afterFirst := snapshot(t, store)
	// Same key and logical request: the cart is now empty of selected lines,
	// but the committed order is returned again instead of a conflict.
	replay := checkoutRequest(h, token, body, checkoutKey)
	expectStatus(t, replay, http.StatusCreated)
	if replay.Header().Get("Idempotent-Replayed") != "true" || replay.Header().Get("X-Authenticated-User-ID") != domain.DemoUserID {
		t.Fatal("replay headers")
	}
	if replay.Body.String() != first.Body.String() {
		t.Fatalf("replay body differs:\n%s\n%s", first.Body.String(), replay.Body.String())
	}
	if !reflect.DeepEqual(afterFirst, snapshot(t, store)) {
		t.Fatal("replay consumed cart or stock again")
	}
	if strings.Contains(strings.ToLower(replay.Body.String()), "digest") {
		t.Fatal("request digest leaked")
	}
	reused := checkoutRequest(h, token, strings.Replace(body, `"deliveryAddress":"Test"`, `"deliveryAddress":"Other"`, 1), checkoutKey)
	expectStatus(t, reused, http.StatusConflict)
	if errorCode(t, reused) != "idempotency_key_reused" {
		t.Fatal("expected idempotency_key_reused")
	}
	// A new key after the cart was consumed is an ordinary conflict.
	fresh := checkoutRequest(h, token, `{"deliveryAddress":"Test"}`, "another-key-0000000001")
	expectStatus(t, fresh, http.StatusConflict)
	if errorCode(t, fresh) != "conflict" {
		t.Fatal("expected conflict")
	}
	if !reflect.DeepEqual(afterFirst, snapshot(t, store)) {
		t.Fatal("rejected requests changed state")
	}
}

func TestCheckoutChangedOverHTTP(t *testing.T) {
	store := memory.NewDemo()
	h := newHarness(t, store, application.Options{AllowDemoAccount: true}, testSessions)
	token := loginHTTP(t, h.handler)
	body := strings.Replace(reviewBody(t, store, "Test"), `"unitPriceMinor":48900`, `"unitPriceMinor":48800`, 1)
	if !strings.Contains(body, "48800") {
		t.Fatalf("fixture price changed: %s", body)
	}
	before := snapshot(t, store)
	response := checkoutRequest(h, token, body, checkoutKey)
	expectStatus(t, response, http.StatusConflict)
	if errorCode(t, response) != "checkout_changed" || response.Header().Get("X-Authenticated-User-ID") != domain.DemoUserID {
		t.Fatal("expected owned checkout_changed")
	}
	if !reflect.DeepEqual(before, snapshot(t, store)) {
		t.Fatal("checkout_changed wrote")
	}
}
