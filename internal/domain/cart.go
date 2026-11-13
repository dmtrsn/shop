package domain

import "fmt"

// CartPatch carries only the fields supplied by the caller. Repositories merge
// it with an existing line while holding their cart mutation lock.
type CartPatch struct {
	Quantity *int  `json:"quantity"`
	Selected *bool `json:"selected"`
}

// ValidateCartQuantity uses the same minimum/step rule as cart and checkout.
// Writers must call it with the product state protected by their mutation lock.
func ValidateCartQuantity(product Product, quantity int) error {
	if product.Archived {
		return ErrNotFound
	}
	if quantity < 1 {
		return &ValidationError{Fields: []FieldError{{Field: "quantity", Message: "Количество должно быть больше нуля"}}}
	}
	if quantity > product.Stock {
		return ErrOutOfStock
	}
	if !QuantityAllowed(quantity, product.MinimumQuantity, product.QuantityStep) {
		return &ValidationError{Fields: []FieldError{{Field: "quantity", Message: fmt.Sprintf("Минимум %d, шаг %d", product.MinimumQuantity, product.QuantityStep)}}}
	}
	return nil
}
