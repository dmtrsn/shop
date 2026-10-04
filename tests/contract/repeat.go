package contract

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
)

// RepeatFixture runs the same adversarial service contract against both stores.
// Mutators are test-only fixture operations, never application hooks.
type RepeatFixture struct {
	Repo            domain.Repository
	ChangeProduct   func(func(*domain.Product))
	DeleteProduct   func()
	DuplicateSource func()
	ReceiptCount    func() int
}

func RepeatInput(line domain.RepeatV2Line, quantity int) domain.RepeatV2Input {
	position, existing, price := line.SourcePosition, line.ExistingCartQuantity, line.Product.PriceMinor
	return domain.RepeatV2Input{IdempotencyKey: "repeat-contract-key-0001", Items: []domain.RepeatV2Selection{{SourcePosition: &position, ProductID: line.ProductID, Quantity: quantity, ExpectedExistingCartQuantity: &existing, ExpectedUnitPriceMinor: &price, ExpectedCurrency: line.Product.Currency, ExpectedMinimumQuantity: line.Product.MinimumQuantity, ExpectedQuantityStep: line.Product.QuantityStep}}}
}

func RunRepeatV2(t *testing.T, fresh func(*testing.T) RepeatFixture) {
	t.Helper()
	ctx := context.Background()
	const user, order = "usr-demo", "ord-2026-002"
	setup := func(t *testing.T) (RepeatFixture, *application.Service, domain.RepeatV2Input) {
		f := fresh(t)
		s := application.New(f.Repo, application.Options{AllowDemoAccount: true})
		p, err := s.RepeatPreviewV2(ctx, user, order)
		if err != nil {
			t.Fatal(err)
		}
		return f, s, RepeatInput(p.Items[0], 2)
	}
	quantity := func(c domain.Cart, id string) int {
		for _, item := range c.Items {
			if item.Product.ID == id {
				return item.Quantity
			}
		}
		return 0
	}
	t.Run("preview pure ordered historical lines", func(t *testing.T) {
		f, s, _ := setup(t)
		before, _ := f.Repo.Cart(ctx, user)
		p, err := s.RepeatPreviewV2(ctx, user, order)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Items) != 4 {
			t.Fatal(p)
		}
		for i, line := range p.Items {
			if line.SourcePosition != i+1 || line.ProductID == "" || line.Name == "" || line.SKU == "" {
				t.Fatal(line)
			}
		}
		first := p.Items[0]
		if first.Product == nil || first.ExistingCartQuantity != 2 || !first.ExistingCartSelected || !first.PriceChanged || first.PriceDeltaMinor == nil || first.Product.MinimumQuantity < 1 || first.Product.QuantityStep < 1 || first.Status != "available" {
			t.Fatal(first)
		}
		if p.Items[1].Status != "limited" || p.Items[2].Status != "unavailable" || p.Items[3].Status != "discontinued" || p.Items[3].Product != nil || p.Items[3].Replacement == nil {
			t.Fatal(p.Items)
		}
		for _, pair := range [][2]string{{"foreign", order}, {user, "missing"}} {
			if _, err := s.RepeatPreviewV2(ctx, pair[0], pair[1]); !errors.Is(err, domain.ErrNotFound) {
				t.Fatal(err)
			}
		}
		after, _ := f.Repo.Cart(ctx, user)
		if !reflect.DeepEqual(before, after) || f.ReceiptCount() != 0 {
			t.Fatal("preview wrote state")
		}
	})
	t.Run("exact subset lost response and replay current cart", func(t *testing.T) {
		f, s, input := setup(t)
		result, err := s.CommitRepeatV2(ctx, user, order, input)
		if err != nil || result.Replayed || result.Receipt.AddedCount != 1 || result.Receipt.AddedQuantity != 2 || quantity(result.Cart, "p01") != 4 {
			t.Fatalf("%+v %v", result, err)
		}
		if quantity(result.Cart, "p07") != 0 {
			t.Fatal("omitted line added")
		}
		// Treat the first result as lost. Alter both affected/unrelated Cart lines
		// and product price before explicitly recovering with the exact request.
		if _, err = f.Repo.SetCartItem(ctx, user, "p01", 7, false); err != nil {
			t.Fatal(err)
		}
		if _, err = f.Repo.SetCartItem(ctx, user, "p02", 1, true); err != nil {
			t.Fatal(err)
		}
		f.ChangeProduct(func(p *domain.Product) { p.PriceMinor++ })
		replay, err := s.CommitRepeatV2(ctx, user, "#10482", input)
		if err != nil || !replay.Replayed || !reflect.DeepEqual(result.Receipt, replay.Receipt) || quantity(replay.Cart, "p01") != 7 || quantity(replay.Cart, "p02") != 1 || f.ReceiptCount() != 1 {
			t.Fatalf("%+v %v", replay, err)
		}
		changed := input
		changed.Items = append([]domain.RepeatV2Selection(nil), input.Items...)
		changed.Items[0].Quantity++
		if _, err = s.CommitRepeatV2(ctx, user, order, changed); !errors.Is(err, domain.ErrIdempotencyKeyReused) {
			t.Fatal(err)
		}
		input.IdempotencyKey = "repeat-contract-key-0002"
		if _, err = s.CommitRepeatV2(ctx, user, order, input); !errors.Is(err, domain.ErrRepeatChanged) {
			t.Fatal(err)
		}
		if f.ReceiptCount() != 1 {
			t.Fatal("failed commit persisted")
		}
	})
	changes := map[string]func(RepeatFixture){
		"price":    func(f RepeatFixture) { f.ChangeProduct(func(p *domain.Product) { p.PriceMinor++ }) },
		"currency": func(f RepeatFixture) { f.ChangeProduct(func(p *domain.Product) { p.Currency = "USD" }) },
		"minimum":  func(f RepeatFixture) { f.ChangeProduct(func(p *domain.Product) { p.MinimumQuantity++ }) },
		"step":     func(f RepeatFixture) { f.ChangeProduct(func(p *domain.Product) { p.QuantityStep++ }) },
		"deleted":  func(f RepeatFixture) { f.DeleteProduct() },
		"cart": func(f RepeatFixture) {
			if _, err := f.Repo.SetCartItem(ctx, user, "p01", 3, false); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range changes {
		t.Run("stale "+name, func(t *testing.T) {
			f, s, input := setup(t)
			change(f)
			before, _ := f.Repo.Cart(ctx, user)
			if _, err := s.CommitRepeatV2(ctx, user, order, input); !errors.Is(err, domain.ErrRepeatChanged) {
				t.Fatal(err)
			}
			after, _ := f.Repo.Cart(ctx, user)
			if !reflect.DeepEqual(before, after) || f.ReceiptCount() != 0 {
				t.Fatal("stale review wrote state")
			}
		})
	}
	for _, stock := range []int{3, 4, 99} {
		t.Run(fmt.Sprintf("stock %d", stock), func(t *testing.T) {
			f, s, input := setup(t)
			f.ChangeProduct(func(p *domain.Product) { p.Stock = stock })
			before, _ := f.Repo.Cart(ctx, user)
			result, err := s.CommitRepeatV2(ctx, user, order, input)
			if stock < 4 {
				after, _ := f.Repo.Cart(ctx, user)
				if !errors.Is(err, domain.ErrOutOfStock) || !reflect.DeepEqual(before, after) || f.ReceiptCount() != 0 {
					t.Fatal(err)
				}
			} else if err != nil || quantity(result.Cart, "p01") != 4 {
				t.Fatal(err)
			}
			p, _ := f.Repo.Product(ctx, "p01")
			if p.Stock != stock {
				t.Fatal("repeat reserved stock")
			}
		})
	}
	invalid := map[string]func(*domain.RepeatV2Input){
		"empty":              func(i *domain.RepeatV2Input) { i.Items = nil },
		"zero":               func(i *domain.RepeatV2Input) { i.Items[0].Quantity = 0 },
		"negative":           func(i *domain.RepeatV2Input) { i.Items[0].Quantity = -1 },
		"overflow":           func(i *domain.RepeatV2Input) { i.Items[0].Quantity = 1 << 32 },
		"duplicate position": func(i *domain.RepeatV2Input) { i.Items = append(i.Items, i.Items[0]) },
		"missing position":   func(i *domain.RepeatV2Input) { i.Items[0].SourcePosition = nil },
		"unknown position":   func(i *domain.RepeatV2Input) { v := 99; i.Items[0].SourcePosition = &v },
		"wrong product":      func(i *domain.RepeatV2Input) { i.Items[0].ProductID = "p02" },
		"alias":              func(i *domain.RepeatV2Input) { i.Items[0].ProductID = "CHA-701" },
		"replacement":        func(i *domain.RepeatV2Input) { v := 4; i.Items[0].SourcePosition = &v; i.Items[0].ProductID = "p06" },
		"missing price":      func(i *domain.RepeatV2Input) { i.Items[0].ExpectedUnitPriceMinor = nil },
		"negative price":     func(i *domain.RepeatV2Input) { v := int64(-1); i.Items[0].ExpectedUnitPriceMinor = &v },
		"missing existing":   func(i *domain.RepeatV2Input) { i.Items[0].ExpectedExistingCartQuantity = nil },
		"negative existing":  func(i *domain.RepeatV2Input) { v := -1; i.Items[0].ExpectedExistingCartQuantity = &v },
		"currency":           func(i *domain.RepeatV2Input) { i.Items[0].ExpectedCurrency = "rub" },
		"minimum":            func(i *domain.RepeatV2Input) { i.Items[0].ExpectedMinimumQuantity = 0 },
		"step":               func(i *domain.RepeatV2Input) { i.Items[0].ExpectedQuantityStep = 0 },
		"key":                func(i *domain.RepeatV2Input) { i.IdempotencyKey = "" },
	}
	for name, change := range invalid {
		t.Run("invalid "+name, func(t *testing.T) {
			f, s, input := setup(t)
			before, _ := f.Repo.Cart(ctx, user)
			change(&input)
			want := domain.ErrValidation
			if name == "unknown position" || name == "wrong product" || name == "alias" || name == "replacement" {
				want = domain.ErrRepeatChanged
			}
			if _, err := s.CommitRepeatV2(ctx, user, order, input); !errors.Is(err, want) {
				t.Fatal(err)
			}
			after, _ := f.Repo.Cart(ctx, user)
			if !reflect.DeepEqual(before, after) || f.ReceiptCount() != 0 {
				t.Fatal("invalid request wrote state")
			}
		})
	}
	t.Run("discontinued original", func(t *testing.T) {
		f, s, input := setup(t)
		position := 4
		input.Items[0].SourcePosition = &position
		input.Items[0].ProductID = "legacy-01"
		if _, err := s.CommitRepeatV2(ctx, user, order, input); !errors.Is(err, domain.ErrRepeatChanged) || f.ReceiptCount() != 0 {
			t.Fatal(err)
		}
	})
	t.Run("final minimum and step reject without a receipt", func(t *testing.T) {
		f, s, _ := setup(t)
		f.ChangeProduct(func(p *domain.Product) { p.MinimumQuantity = 3; p.QuantityStep = 2 })
		if _, err := f.Repo.RemoveCartItem(ctx, user, "p01"); err != nil {
			t.Fatal(err)
		}
		preview, err := s.RepeatPreviewV2(ctx, user, order)
		if err != nil {
			t.Fatal(err)
		}
		for _, quantity := range []int{2, 4} {
			input := RepeatInput(preview.Items[0], quantity)
			if _, err := s.CommitRepeatV2(ctx, user, order, input); !errors.Is(err, domain.ErrValidation) {
				t.Fatal(err)
			}
		}
		if f.ReceiptCount() != 0 {
			t.Fatal("invalid final quantity persisted")
		}
	})
	t.Run("non RUB price delta is not fabricated", func(t *testing.T) {
		f, s, _ := setup(t)
		f.ChangeProduct(func(p *domain.Product) { p.Currency = "USD" })
		preview, err := s.RepeatPreviewV2(ctx, user, order)
		if err != nil {
			t.Fatal(err)
		}
		if preview.Items[0].PriceDeltaMinor != nil || !preview.Items[0].PriceChanged {
			t.Fatal(preview.Items[0])
		}
	})
	for _, stock := range []int{8, 9} {
		t.Run("duplicate sources aggregate", func(t *testing.T) {
			f, s, _ := setup(t)
			f.DuplicateSource()
			f.ChangeProduct(func(p *domain.Product) { p.MinimumQuantity = 3; p.QuantityStep = 2; p.Stock = stock })
			if _, err := f.Repo.SetCartItem(ctx, user, "p01", 3, false); err != nil {
				t.Fatal(err)
			}
			p, err := s.RepeatPreviewV2(ctx, user, order)
			if err != nil {
				t.Fatal(err)
			}
			last := p.Items[len(p.Items)-1]
			if last.SourcePosition != 9 || last.ProductID != "p01" {
				t.Fatal(last)
			}
			input := RepeatInput(p.Items[0], 1)
			second := RepeatInput(last, 5)
			input.Items = append(input.Items, second.Items[0])
			result, err := s.CommitRepeatV2(ctx, user, order, input)
			if stock == 8 {
				if !errors.Is(err, domain.ErrOutOfStock) || f.ReceiptCount() != 0 {
					t.Fatal(err)
				}
				return
			}
			if err != nil || quantity(result.Cart, "p01") != 9 || result.Receipt.AddedCount != 1 || result.Receipt.AddedQuantity != 6 || len(result.Receipt.Additions) != 2 {
				t.Fatalf("%+v %v", result, err)
			}
			for _, line := range result.Cart.Items {
				if line.Product.ID == "p01" && !line.Selected {
					t.Fatal("repeated line not selected")
				}
			}
			input.Items[0], input.Items[1] = input.Items[1], input.Items[0]
			if replay, err := s.CommitRepeatV2(ctx, user, order, input); err != nil || !replay.Replayed {
				t.Fatal(err)
			}
		})
	}
	for _, different := range []bool{false, true} {
		t.Run("concurrent keyed commits", func(t *testing.T) {
			f, s, input := setup(t)
			second := input
			second.Items = append([]domain.RepeatV2Selection(nil), input.Items...)
			if different {
				second.Items[0].Quantity = 1
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for _, request := range []domain.RepeatV2Input{input, second} {
				wg.Add(1)
				go func(request domain.RepeatV2Input) {
					defer wg.Done()
					<-start
					_, err := s.CommitRepeatV2(ctx, user, order, request)
					results <- err
				}(request)
			}
			close(start)
			wg.Wait()
			close(results)
			reused := 0
			for err := range results {
				if errors.Is(err, domain.ErrIdempotencyKeyReused) {
					reused++
				} else if err != nil {
					t.Fatal(err)
				}
			}
			if (different && reused != 1) || (!different && reused != 0) || f.ReceiptCount() != 1 {
				t.Fatal("bad keyed race")
			}
			cart, _ := f.Repo.Cart(ctx, user)
			q := quantity(cart, "p01")
			if q < 3 || q > 4 {
				t.Fatal("double increment", q)
			}
		})
	}
}
