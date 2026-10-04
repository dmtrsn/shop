package application

import (
	"sweetshop/internal/domain"
	"testing"
)

func TestRepeatDigestBindsEveryReviewedField(t *testing.T) {
	pos, existing, price := 1, 3, int64(12900)
	base := domain.RepeatV2Selection{SourcePosition: &pos, ProductID: "p01", Quantity: 2, ExpectedExistingCartQuantity: &existing, ExpectedUnitPriceMinor: &price, ExpectedCurrency: "RUB", ExpectedMinimumQuantity: 1, ExpectedQuantityStep: 1}
	digest := repeatRequestDigest("order", []domain.RepeatV2Selection{base})
	changes := map[string]func(*domain.RepeatV2Selection){
		"position": func(i *domain.RepeatV2Selection) { v := 5; i.SourcePosition = &v },
		"product":  func(i *domain.RepeatV2Selection) { i.ProductID = "p02" },
		"addition": func(i *domain.RepeatV2Selection) { i.Quantity++ },
		"existing": func(i *domain.RepeatV2Selection) { v := 4; i.ExpectedExistingCartQuantity = &v },
		"price":    func(i *domain.RepeatV2Selection) { v := int64(12901); i.ExpectedUnitPriceMinor = &v },
		"currency": func(i *domain.RepeatV2Selection) { i.ExpectedCurrency = "USD" },
		"minimum":  func(i *domain.RepeatV2Selection) { i.ExpectedMinimumQuantity++ },
		"step":     func(i *domain.RepeatV2Selection) { i.ExpectedQuantityStep++ },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			item := base
			change(&item)
			if repeatRequestDigest("order", []domain.RepeatV2Selection{item}) == digest {
				t.Fatal("field not bound")
			}
		})
	}
	if repeatRequestDigest("another-order", []domain.RepeatV2Selection{base}) == digest {
		t.Fatal("order not bound")
	}
	second := base
	p := 9
	second.SourcePosition = &p
	if repeatRequestDigest("order", []domain.RepeatV2Selection{base, second}) != repeatRequestDigest("order", []domain.RepeatV2Selection{second, base}) {
		t.Fatal("item order is not canonical")
	}
}
