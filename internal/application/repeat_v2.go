package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"sweetshop/internal/domain"
)

// Preview is stateless. Suggestions describe each source line in isolation;
// when duplicate products are selected, commit validates their aggregate.
func (s *Service) RepeatPreviewV2(ctx context.Context, userID, orderID string) (domain.RepeatV2Preview, error) {
	order, err := s.repo.Order(ctx, userID, orderID)
	if err != nil {
		return domain.RepeatV2Preview{}, err
	}
	cart, err := s.repo.Cart(ctx, userID)
	if err != nil {
		return domain.RepeatV2Preview{}, err
	}
	ids := []string{}
	for _, line := range order.Items {
		ids = append(ids, line.ProductID, line.ReplacementProductID)
	}
	products, err := s.repo.Products(ctx, ids)
	if err != nil {
		return domain.RepeatV2Preview{}, err
	}
	existing := map[string]domain.CartItem{}
	for _, item := range cart.Items {
		existing[item.Product.ID] = item
	}
	preview := domain.RepeatV2Preview{OrderID: order.ID, OrderNumber: order.Number, Items: []domain.RepeatV2Line{}}
	for _, old := range order.Items {
		row := existing[old.ProductID]
		line := domain.RepeatV2Line{SourcePosition: old.SourcePosition, ProductID: old.ProductID, SKU: old.SKU, Name: old.Name, Image: old.Image, RequestedQuantity: old.Quantity, OriginalUnitPriceMinor: old.UnitPriceMinor, ExistingCartQuantity: row.Quantity, ExistingCartSelected: row.Selected, Status: "discontinued"}
		if p, ok := products[old.ProductID]; ok {
			line.Product = &p
			// Historical orders have no currency field; keep the existing RUB
			// assumption explicit and never subtract amounts in different currencies.
			line.PriceChanged = p.Currency != "RUB" || p.PriceMinor != old.UnitPriceMinor
			if p.Currency == "RUB" {
				delta := p.PriceMinor - old.UnitPriceMinor
				line.PriceDeltaMinor = &delta
			}
			addable := max(0, p.Stock-row.Quantity)
			line.SuggestedQuantity = repeatAdditionAtMost(p, row.Quantity, old.Quantity, addable)
			line.ProposedQuantity = line.SuggestedQuantity
			if line.ProposedQuantity == 0 {
				line.ProposedQuantity = smallestRepeatAddition(p, row.Quantity, addable)
			}
			switch {
			case line.ProposedQuantity == 0:
				line.Status = "unavailable"
			case line.SuggestedQuantity != old.Quantity:
				line.Status = "limited"
			default:
				line.Status = "available"
			}
		} else if replacement, ok := products[old.ReplacementProductID]; ok {
			line.Replacement = &replacement
		}
		preview.Items = append(preview.Items, line)
	}
	return preview, nil
}

var repeatCurrencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func (s *Service) CommitRepeatV2(ctx context.Context, userID, orderID string, input domain.RepeatV2Input) (domain.RepeatV2Result, error) {
	fields := []domain.FieldError{}
	if !idempotencyKeyPattern.MatchString(input.IdempotencyKey) {
		fields = append(fields, domain.FieldError{Field: "Idempotency-Key", Message: "Передайте ключ из 16–128 символов A-Z, a-z, 0-9, _ или -"})
	}
	if len(input.Items) == 0 {
		fields = append(fields, domain.FieldError{Field: "items", Message: "Явно выберите хотя бы одну позицию"})
	}
	seen := map[int]bool{}
	for i, item := range input.Items {
		invalid := item.SourcePosition == nil || item.ProductID == "" || strings.TrimSpace(item.ProductID) != item.ProductID || item.Quantity < 1 || item.Quantity > math.MaxInt32 || item.ExpectedExistingCartQuantity == nil || item.ExpectedUnitPriceMinor == nil || !repeatCurrencyPattern.MatchString(item.ExpectedCurrency) || item.ExpectedMinimumQuantity < 1 || item.ExpectedMinimumQuantity > math.MaxInt32 || item.ExpectedQuantityStep < 1 || item.ExpectedQuantityStep > math.MaxInt32
		if item.SourcePosition != nil {
			invalid = invalid || *item.SourcePosition < math.MinInt32 || *item.SourcePosition > math.MaxInt32 || seen[*item.SourcePosition]
			seen[*item.SourcePosition] = true
		}
		if item.ExpectedExistingCartQuantity != nil {
			invalid = invalid || *item.ExpectedExistingCartQuantity < 0 || *item.ExpectedExistingCartQuantity > math.MaxInt32
		}
		if item.ExpectedUnitPriceMinor != nil {
			invalid = invalid || *item.ExpectedUnitPriceMinor < 0 || *item.ExpectedUnitPriceMinor > 9007199254740991
		}
		if invalid {
			fields = append(fields, domain.FieldError{Field: fmt.Sprintf("items.%d", i), Message: "Некорректная позиция, количество или ожидаемые условия"})
		}
	}
	if len(fields) != 0 {
		return domain.RepeatV2Result{}, &domain.ValidationError{Fields: fields}
	}
	// Canonicalize the owned order alias only. Product aliases are never resolved.
	order, err := s.repo.Order(ctx, userID, orderID)
	if err != nil {
		return domain.RepeatV2Result{}, err
	}
	input.RequestDigest = repeatRequestDigest(order.ID, input.Items)
	return s.repo.CommitRepeatV2(ctx, userID, order.ID, input)
}

func repeatRequestDigest(orderID string, selections []domain.RepeatV2Selection) string {
	items := append([]domain.RepeatV2Selection(nil), selections...)
	sort.Slice(items, func(i, j int) bool { return *items[i].SourcePosition < *items[j].SourcePosition })
	form := struct {
		Version string                     `json:"v"`
		OrderID string                     `json:"orderId"`
		Items   []domain.RepeatV2Selection `json:"items"`
	}{"repeat/v2", orderID, items}
	encoded, err := json.Marshal(form)
	if err != nil {
		panic("encode repeat digest")
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
