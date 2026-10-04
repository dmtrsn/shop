package domain

import "sort"

type RepeatV2Line struct {
	SourcePosition         int      `json:"sourcePosition"`
	ProductID              string   `json:"productId"`
	SKU                    string   `json:"sku"`
	Name                   string   `json:"name"`
	Image                  string   `json:"image"`
	RequestedQuantity      int      `json:"requestedQuantity"`
	OriginalUnitPriceMinor int64    `json:"originalUnitPriceMinor"`
	Product                *Product `json:"product,omitempty"`
	ExistingCartQuantity   int      `json:"existingCartQuantity"`
	ExistingCartSelected   bool     `json:"existingCartSelected"`
	SuggestedQuantity      int      `json:"suggestedQuantity"`
	ProposedQuantity       int      `json:"proposedQuantity"`
	Status                 string   `json:"status"`
	PriceChanged           bool     `json:"priceChanged"`
	PriceDeltaMinor        *int64   `json:"priceDeltaMinor,omitempty"`
	Replacement            *Product `json:"replacement,omitempty"`
}

type RepeatV2Preview struct {
	OrderID     string         `json:"orderId"`
	OrderNumber string         `json:"orderNumber"`
	Items       []RepeatV2Line `json:"items"`
}

// Pointers distinguish omitted/null expectations from a reviewed zero.
type RepeatV2Selection struct {
	SourcePosition               *int   `json:"sourcePosition"`
	ProductID                    string `json:"productId"`
	Quantity                     int    `json:"quantity"`
	ExpectedExistingCartQuantity *int   `json:"expectedExistingCartQuantity"`
	ExpectedUnitPriceMinor       *int64 `json:"expectedUnitPriceMinor"`
	ExpectedCurrency             string `json:"expectedCurrency"`
	ExpectedMinimumQuantity      int    `json:"expectedMinimumQuantity"`
	ExpectedQuantityStep         int    `json:"expectedQuantityStep"`
}

type RepeatV2Input struct {
	Items          []RepeatV2Selection `json:"items"`
	IdempotencyKey string              `json:"-"`
	RequestDigest  string              `json:"-"`
}

type RepeatAddition struct {
	SourcePosition int    `json:"sourcePosition"`
	ProductID      string `json:"productId"`
	Quantity       int    `json:"quantity"`
}

type RepeatReceipt struct {
	SourceOrderID     string           `json:"sourceOrderId"`
	SourceOrderNumber string           `json:"sourceOrderNumber"`
	AddedCount        int              `json:"addedCount"` // Distinct target products.
	AddedQuantity     int              `json:"addedQuantity"`
	Additions         []RepeatAddition `json:"additions"`
}

type RepeatV2Result struct {
	Receipt  RepeatReceipt `json:"receipt"`
	Cart     Cart          `json:"cart"` // Current authoritative Cart, including on replay.
	Replayed bool          `json:"-"`
}

// PlanRepeatV2 runs against authoritative values under the adapter's locks.
// It performs no writes; every line is checked before aggregate mutations exist.
func PlanRepeatV2(order Order, input RepeatV2Input, products map[string]Product, existing map[string]int) (RepeatReceipt, []CartMutation, error) {
	receipt := RepeatReceipt{SourceOrderID: order.ID, SourceOrderNumber: order.Number, Additions: []RepeatAddition{}}
	sources := make(map[int]OrderItem, len(order.Items))
	for _, line := range order.Items {
		sources[line.SourcePosition] = line
	}
	additions := map[string]int64{}
	for _, item := range input.Items {
		source, ok := sources[*item.SourcePosition]
		if !ok || source.ProductID != item.ProductID {
			return RepeatReceipt{}, nil, ErrRepeatChanged
		}
		p, ok := products[source.ProductID]
		if !ok || p.ID != item.ProductID || p.PriceMinor != *item.ExpectedUnitPriceMinor || p.Currency != item.ExpectedCurrency || p.MinimumQuantity != item.ExpectedMinimumQuantity || p.QuantityStep != item.ExpectedQuantityStep || existing[p.ID] != *item.ExpectedExistingCartQuantity {
			return RepeatReceipt{}, nil, ErrRepeatChanged
		}
		additions[p.ID] += int64(item.Quantity)
		receipt.Additions = append(receipt.Additions, RepeatAddition{*item.SourcePosition, p.ID, item.Quantity})
	}
	ids := make([]string, 0, len(additions))
	for id := range additions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	mutations := make([]CartMutation, 0, len(ids))
	for _, id := range ids {
		p := products[id]
		final := int64(existing[id]) + additions[id]
		if final > int64(p.Stock) {
			return RepeatReceipt{}, nil, ErrOutOfStock
		}
		if final < int64(p.MinimumQuantity) || (final-int64(p.MinimumQuantity))%int64(p.QuantityStep) != 0 {
			return RepeatReceipt{}, nil, &ValidationError{Fields: []FieldError{{Field: "items", Message: "Итоговое количество не соответствует минимальной партии или шагу"}}}
		}
		mutations = append(mutations, CartMutation{ProductID: id, Quantity: int(additions[id]), Selected: true})
		receipt.AddedQuantity += int(additions[id])
	}
	sort.Slice(receipt.Additions, func(i, j int) bool { return receipt.Additions[i].SourcePosition < receipt.Additions[j].SourcePosition })
	receipt.AddedCount = len(mutations)
	return receipt, mutations, nil
}
