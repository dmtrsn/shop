package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/internal/repository/memory"
)

func newService() *application.Service { return application.New(memory.NewDemo()) }

func login(t *testing.T, service *application.Service) application.LoginResult {
	t.Helper()
	result, err := service.Login(context.Background(), "demo@sugar.ru", "Sweet2026!")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.Token == "" || result.User.ID != "usr-demo" {
		t.Fatalf("unexpected login result: %#v", result)
	}
	return result
}

func TestLoginAndAuthentication(t *testing.T) {
	service := newService()
	result := login(t, service)
	user, err := service.Authenticate(context.Background(), result.Token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if user.Email != "demo@sugar.ru" {
		t.Fatalf("email = %q", user.Email)
	}
	if _, err := service.Login(context.Background(), "demo@sugar.ru", "wrong"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("wrong password error = %v", err)
	}
}

func TestRepeatPreviewShowsEveryAvailabilityCase(t *testing.T) {
	service := newService()
	preview, err := service.RepeatPreview(context.Background(), "usr-demo", "ord-2026-002")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) != 4 {
		t.Fatalf("items = %d", len(preview.Items))
	}
	byID := map[string]domain.RepeatItem{}
	for _, item := range preview.Items {
		byID[item.Product.ID] = item
	}
	if item := byID["p01"]; item.Status != "available" || item.AlreadyInCartQuantity != 2 || !item.PriceChanged || item.PriceDeltaMinor != 3000 {
		t.Errorf("p01 preview = %#v", item)
	}
	if item := byID["p07"]; item.Status != "limited" || item.SuggestedQuantity != 4 || item.RequestedQuantity != 12 {
		t.Errorf("p07 preview = %#v", item)
	}
	if item := byID["p18"]; item.Status != "unavailable" || item.AvailableQuantity != 0 {
		t.Errorf("p18 preview = %#v", item)
	}
	if item := byID["legacy-01"]; item.Status != "discontinued" || item.Replacement == nil || item.Replacement.ID != "p06" {
		t.Errorf("legacy preview = %#v", item)
	}
}

func TestRepeatCommitAddsWithoutSilentlyReducing(t *testing.T) {
	service := newService()
	ctx := context.Background()
	_, err := service.CommitRepeat(ctx, "usr-demo", "ord-2026-002", []domain.RepeatSelection{{ProductID: "p07", Quantity: 12, Selected: true}})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	before, _ := service.Cart(ctx, "usr-demo")
	if findCartQuantity(before, "p07") != 0 {
		t.Fatal("invalid commit partially changed cart")
	}
	result, err := service.CommitRepeat(ctx, "usr-demo", "ord-2026-002", []domain.RepeatSelection{
		{ProductID: "p01", Quantity: 2, Selected: true}, {ProductID: "p07", Quantity: 4, Selected: true}, {ProductID: "p18", Selected: false}, {ProductID: "legacy-01", Selected: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AddedCount != 2 || result.AddedQuantity != 6 || result.AdjustedCount != 1 || result.SkippedCount != 2 {
		t.Fatalf("summary = %#v", result)
	}
	if got := findCartQuantity(result.Cart, "p01"); got != 4 {
		t.Errorf("p01 cart quantity = %d", got)
	}
	if got := findCartQuantity(result.Cart, "p07"); got != 4 {
		t.Errorf("p07 cart quantity = %d", got)
	}
}

func TestRepeatCommitRejectsSelectedDiscontinuedItemWithoutPanic(t *testing.T) {
	service := newService()
	_, err := service.CommitRepeat(context.Background(), "usr-demo", "ord-2026-002", []domain.RepeatSelection{{ProductID: "legacy-01", Quantity: 0, Selected: true}})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestCatalogFilterAndPagination(t *testing.T) {
	service := newService()
	inStock := true
	page, err := service.Catalog(context.Background(), domain.ProductFilter{Category: "chocolate", InStock: &inStock, Sort: "price_asc", Page: 1, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total < 3 || len(page.Items) != 3 {
		t.Fatalf("page = %#v", page)
	}
	for i, p := range page.Items {
		if p.Stock <= 0 {
			t.Errorf("out-of-stock item returned")
		}
		if i > 0 && page.Items[i-1].PriceMinor > p.PriceMinor {
			t.Errorf("not sorted by price")
		}
	}
}

func TestSuggestionsApplySharedQueryBounds(t *testing.T) {
	service := newService()
	short, err := service.Suggestions(context.Background(), " ш ")
	if err != nil || short == nil || len(short) != 0 {
		t.Fatalf("short suggestions = %#v, %v", short, err)
	}
	items, err := service.Suggestions(context.Background(), "CHA-701")
	if err != nil || len(items) == 0 {
		t.Fatalf("two-or-more rune query must reach the repository: %#v, %v", items, err)
	}
	if _, err := service.Suggestions(context.Background(), strings.Repeat("ш", 200)); err != nil {
		t.Fatalf("200 runes must be accepted: %v", err)
	}
	_, err = service.Suggestions(context.Background(), strings.Repeat("ш", 201))
	var validation *domain.ValidationError
	if !errors.Is(err, domain.ErrValidation) || !errors.As(err, &validation) || len(validation.Fields) != 1 || validation.Fields[0].Field != "q" {
		t.Fatalf("201 runes error = %v", err)
	}
}

func TestCatalogSaleAndNewFilters(t *testing.T) {
	service := newService()
	sale, err := service.Catalog(context.Background(), domain.ProductFilter{OnSale: true, Page: 1, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(sale.Items) == 0 {
		t.Fatal("sale catalog is empty")
	}
	for _, product := range sale.Items {
		if product.OldPriceMinor <= product.PriceMinor {
			t.Fatalf("non-sale product returned: %s", product.ID)
		}
	}

	newProducts, err := service.Catalog(context.Background(), domain.ProductFilter{NewOnly: true, Page: 1, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(newProducts.Items) == 0 {
		t.Fatal("new catalog is empty")
	}
	for _, product := range newProducts.Items {
		if !containsStringFold(product.Badges, "Новинка") {
			t.Fatalf("non-new product returned: %s", product.ID)
		}
	}
}

func TestCheckoutTotalMatchesCartIncludingDelivery(t *testing.T) {
	service := newService()
	ctx := context.Background()
	cart, err := service.Cart(ctx, "usr-demo")
	if err != nil {
		t.Fatal(err)
	}
	if cart.Summary.DeliveryMinor != domain.StandardDeliveryMinor {
		t.Fatalf("delivery = %d", cart.Summary.DeliveryMinor)
	}
	if cart.Summary.DiscountMinor <= 0 {
		t.Fatalf("expected demo cart discount, got %d", cart.Summary.DiscountMinor)
	}
	order, err := service.Checkout(ctx, "usr-demo", domain.CheckoutInput{DeliveryAddress: "Москва, Тестовая, 1"})
	if err != nil {
		t.Fatal(err)
	}
	if order.TotalMinor != cart.Summary.TotalMinor {
		t.Fatalf("order total = %d, cart total = %d", order.TotalMinor, cart.Summary.TotalMinor)
	}
}

func findCartQuantity(cart domain.Cart, productID string) int {
	for _, item := range cart.Items {
		if item.Product.ID == productID {
			return item.Quantity
		}
	}
	return 0
}

func containsStringFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}
