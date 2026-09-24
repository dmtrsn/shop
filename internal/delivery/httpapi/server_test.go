package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"sweetshop/internal/application"
	"sweetshop/internal/delivery/httpapi"
	"sweetshop/internal/domain"
	"sweetshop/internal/repository/memory"
)

type unhealthyRepository struct{ domain.Repository }

func (unhealthyRepository) Ping(context.Context) error { return errors.New("database unavailable") }

func testServer() http.Handler {
	return httpapi.New(application.New(memory.NewDemo()), slog.New(slog.NewTextHandler(io.Discard, nil)), "", "memory")
}

func TestCatalogAndProtectedCart(t *testing.T) {
	handler := testServer()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/catalog?manufacturer=chocoatelier&sort=price_asc", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("catalog status=%d body=%s", response.Code, response.Body.String())
	}
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total == 0 || len(page.Items) == 0 {
		t.Fatal("catalog is empty")
	}
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/cart", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("cart status=%d", unauthorized.Code)
	}
	token := loginHTTP(t, handler)
	request := httptest.NewRequest(http.MethodGet, "/api/cart", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	cart := httptest.NewRecorder()
	handler.ServeHTTP(cart, request)
	if cart.Code != http.StatusOK {
		t.Fatalf("authorized cart status=%d body=%s", cart.Code, cart.Body.String())
	}
}

func TestHealthReportsUnavailableRepository(t *testing.T) {
	handler := httpapi.New(application.New(unhealthyRepository{}), slog.New(slog.NewTextHandler(io.Discard, nil)), "", "postgres")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("health status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRepeatPreviewEndpoint(t *testing.T) {
	handler := testServer()
	token := loginHTTP(t, handler)
	request := httptest.NewRequest(http.MethodPost, "/api/orders/ord-2026-002/repeat/preview", bytes.NewBufferString(`{}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var preview struct {
		Items []struct {
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	statuses := map[string]bool{}
	for _, item := range preview.Items {
		statuses[item.Status] = true
	}
	for _, wanted := range []string{"available", "limited", "unavailable", "discontinued"} {
		if !statuses[wanted] {
			t.Errorf("missing status %s", wanted)
		}
	}
}

func TestCanDeselectCartItemThatIsNowUnavailable(t *testing.T) {
	handler := testServer()
	token := loginHTTP(t, handler)

	selectRequest := httptest.NewRequest(http.MethodPatch, "/api/cart/selection", bytes.NewBufferString(`{"productIds":["p18"],"selected":true}`))
	selectRequest.Header.Set("Authorization", "Bearer "+token)
	selectRequest.Header.Set("Content-Type", "application/json")
	selectResponse := httptest.NewRecorder()
	handler.ServeHTTP(selectResponse, selectRequest)
	if selectResponse.Code != http.StatusOK {
		t.Fatalf("select unavailable status=%d body=%s", selectResponse.Code, selectResponse.Body.String())
	}

	request := httptest.NewRequest(http.MethodPatch, "/api/cart/items/p18", bytes.NewBufferString(`{"selected":false}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("deselect unavailable status=%d body=%s", response.Code, response.Body.String())
	}
	var cart struct {
		Items []struct {
			Product struct {
				ID string `json:"id"`
			} `json:"product"`
			Selected bool `json:"selected"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &cart); err != nil {
		t.Fatal(err)
	}
	for _, item := range cart.Items {
		if item.Product.ID == "p18" && item.Selected {
			t.Fatal("unavailable cart item remained selected")
		}
	}
}

func TestUnknownBulkSelectionIDDoesNotChangeWholeCart(t *testing.T) {
	handler := testServer()
	token := loginHTTP(t, handler)

	request := httptest.NewRequest(http.MethodPatch, "/api/cart/selection", bytes.NewBufferString(`{"productIds":["missing-product"],"selected":false}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("selection status=%d body=%s", response.Code, response.Body.String())
	}
	var cart struct {
		Items []struct {
			Product struct {
				ID string `json:"id"`
			} `json:"product"`
			Selected bool `json:"selected"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &cart); err != nil {
		t.Fatal(err)
	}
	selected := 0
	for _, item := range cart.Items {
		if item.Selected {
			selected++
		}
	}
	if selected == 0 {
		t.Fatal("unknown product ID deselected the entire cart")
	}
}

func loginHTTP(t *testing.T, handler http.Handler) string {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"demo@sugar.ru","password":"Sweet2026!"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Token
}
