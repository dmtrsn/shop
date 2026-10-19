package domain

import "testing"

func TestQuantityAllowed(t *testing.T) {
	for _, tc := range []struct {
		quantity, minimum, step int
		allowed                 bool
	}{
		{1, 1, 1, true}, {5, 1, 1, true}, {0, 1, 1, false},
		{6, 6, 6, true}, {12, 6, 6, true}, {5, 6, 6, false}, {9, 6, 6, false},
		{3, 3, 2, true}, {5, 3, 2, true}, {4, 3, 2, false},
		// Legacy non-positive rules behave as 1, never divide by zero.
		{1, 0, 0, true},
	} {
		if got := QuantityAllowed(tc.quantity, tc.minimum, tc.step); got != tc.allowed {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}

func TestCartLineIssue(t *testing.T) {
	product := Product{Stock: 10, MinimumQuantity: 2, QuantityStep: 2}
	archived := product
	archived.Archived = true
	for _, tc := range []struct {
		product  Product
		quantity int
		issue    string
	}{
		{product, 4, ""},
		{product, 3, "Минимум 2, шаг 2"},
		{product, 1, "Минимум 2, шаг 2"},
		{product, 12, "Доступно только 10"},
		{Product{MinimumQuantity: 1, QuantityStep: 1}, 1, "Нет в наличии"},
		{archived, 4, "Товар больше не продаётся"},
	} {
		if got := CartLineIssue(tc.product, tc.quantity); got != tc.issue {
			t.Errorf("%+v x%d: %q", tc.product, tc.quantity, got)
		}
	}
}
