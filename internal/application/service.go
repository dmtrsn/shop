package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sweetshop/internal/domain"
)

const sessionLifetime = 30 * 24 * time.Hour

type Service struct {
	repo domain.Repository
	now  func() time.Time
}

type readinessRepository interface {
	Ping(context.Context) error
}

func New(repo domain.Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

func (s *Service) Ready(ctx context.Context) error {
	if repository, ok := s.repo.(readinessRepository); ok {
		return repository.Ping(ctx)
	}
	return nil
}

type LoginResult struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expiresAt"`
	User      domain.User `json:"user"`
}

func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return LoginResult{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "email", Message: "Укажите email"}, {Field: "password", Message: "Укажите пароль"}}}
	}
	credential, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return LoginResult{}, domain.ErrUnauthorized
		}
		return LoginResult{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(credential.PasswordHash), []byte(password)) != nil {
		return LoginResult{}, domain.ErrUnauthorized
	}
	token, err := randomToken(32)
	if err != nil {
		return LoginResult{}, fmt.Errorf("create session token: %w", err)
	}
	expires := s.now().UTC().Add(sessionLifetime)
	if err := s.repo.CreateSession(ctx, credential.User.ID, tokenHash(token), expires); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, ExpiresAt: expires, User: credential.User}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if token == "" {
		return domain.User{}, domain.ErrUnauthorized
	}
	return s.repo.UserBySession(ctx, tokenHash(token), s.now().UTC())
}

func (s *Service) Categories(ctx context.Context) ([]domain.Category, error) {
	return s.repo.ListCategories(ctx)
}

func (s *Service) Manufacturers(ctx context.Context) ([]domain.Manufacturer, error) {
	return s.repo.ListManufacturers(ctx)
}

func (s *Service) Brands(ctx context.Context) ([]domain.Brand, error) {
	return s.repo.ListBrands(ctx)
}

func (s *Service) Collections(ctx context.Context) ([]domain.Collection, error) {
	return s.repo.ListCollections(ctx)
}

func (s *Service) Catalog(ctx context.Context, filter domain.ProductFilter) (domain.ProductPage, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	if len([]rune(filter.Query)) > 200 {
		return domain.ProductPage{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "q", Message: "Поисковый запрос не должен быть длиннее 200 символов"}}}
	}
	filter.Category = strings.TrimSpace(filter.Category)
	filter.Manufacturer = strings.TrimSpace(filter.Manufacturer)
	filter.Brand = strings.TrimSpace(filter.Brand)
	filter.Series = strings.TrimSpace(filter.Series)
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Page > 1_000_000 {
		return domain.ProductPage{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "page", Message: "Номер страницы слишком большой"}}}
	}
	if filter.Limit < 1 {
		filter.Limit = 24
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	validSort := map[string]bool{"": true, "popular": true, "new": true, "price_asc": true, "price_desc": true, "rating": true, "name": true}
	if !validSort[filter.Sort] {
		return domain.ProductPage{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "sort", Message: "Неизвестный способ сортировки"}}}
	}
	return s.repo.ListProducts(ctx, filter)
}

func (s *Service) Facets(ctx context.Context, filter domain.ProductFilter) (domain.CatalogFacets, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	if len([]rune(filter.Query)) > 200 {
		return domain.CatalogFacets{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "q", Message: "Поисковый запрос не должен быть длиннее 200 символов"}}}
	}
	filter.Category = strings.TrimSpace(filter.Category)
	filter.Manufacturer = strings.TrimSpace(filter.Manufacturer)
	filter.Brand = strings.TrimSpace(filter.Brand)
	filter.Series = strings.TrimSpace(filter.Series)
	return s.repo.CatalogFacets(ctx, filter)
}

func (s *Service) Product(ctx context.Context, idOrSlug string) (domain.Product, error) {
	return s.repo.Product(ctx, strings.TrimSpace(idOrSlug))
}

func (s *Service) Suggestions(ctx context.Context, query string) ([]domain.SearchSuggestion, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return []domain.SearchSuggestion{}, nil
	}
	return s.repo.Suggestions(ctx, query, 8)
}

func (s *Service) Cart(ctx context.Context, userID string) (domain.Cart, error) {
	return s.repo.Cart(ctx, userID)
}

func (s *Service) SetCartItem(ctx context.Context, userID, productID string, quantity int, selected bool) (domain.Cart, error) {
	if quantity < 1 {
		return domain.Cart{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "quantity", Message: "Количество должно быть больше нуля"}}}
	}
	product, err := s.repo.Product(ctx, productID)
	if err != nil {
		return domain.Cart{}, err
	}
	if quantity > product.Stock {
		return domain.Cart{}, domain.ErrOutOfStock
	}
	if quantity < product.MinimumQuantity || (quantity-product.MinimumQuantity)%product.QuantityStep != 0 {
		return domain.Cart{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "quantity", Message: fmt.Sprintf("Минимум %d, шаг %d", product.MinimumQuantity, product.QuantityStep)}}}
	}
	return s.repo.SetCartItem(ctx, userID, product.ID, quantity, selected)
}

func (s *Service) RemoveCartItem(ctx context.Context, userID, productID string) (domain.Cart, error) {
	return s.repo.RemoveCartItem(ctx, userID, productID)
}

func (s *Service) ClearCart(ctx context.Context, userID string, selectedOnly bool) (domain.Cart, error) {
	return s.repo.ClearCart(ctx, userID, selectedOnly)
}

func (s *Service) SetCartSelection(ctx context.Context, userID string, productIDs []string, selected bool) (domain.Cart, error) {
	return s.repo.SetCartSelection(ctx, userID, productIDs, selected)
}

func (s *Service) Favorites(ctx context.Context, userID string) (domain.Favorites, error) {
	return s.repo.Favorites(ctx, userID)
}

func (s *Service) SetFavorite(ctx context.Context, userID, productID string, enabled bool) (domain.Favorites, error) {
	if _, err := s.repo.Product(ctx, productID); err != nil {
		return domain.Favorites{}, err
	}
	return s.repo.SetFavorite(ctx, userID, productID, enabled)
}

func (s *Service) Orders(ctx context.Context, userID string) ([]domain.Order, error) {
	return s.repo.ListOrders(ctx, userID)
}

func (s *Service) Order(ctx context.Context, userID, orderID string) (domain.Order, error) {
	return s.repo.Order(ctx, userID, orderID)
}

func (s *Service) Checkout(ctx context.Context, userID string, input domain.CheckoutInput) (domain.Order, error) {
	input.DeliveryAddress = strings.TrimSpace(input.DeliveryAddress)
	if input.DeliveryAddress == "" {
		return domain.Order{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "deliveryAddress", Message: "Укажите адрес доставки"}}}
	}
	return s.repo.CreateOrder(ctx, userID, input)
}

func (s *Service) RepeatPreview(ctx context.Context, userID, orderID string) (domain.RepeatPreview, error) {
	order, err := s.repo.Order(ctx, userID, orderID)
	if err != nil {
		return domain.RepeatPreview{}, err
	}
	preview := domain.RepeatPreview{OrderID: order.ID, OrderNumber: order.Number, Items: make([]domain.RepeatItem, 0, len(order.Items))}
	cart, err := s.repo.Cart(ctx, userID)
	if err != nil {
		return domain.RepeatPreview{}, err
	}
	inCart := make(map[string]int, len(cart.Items))
	for _, item := range cart.Items {
		inCart[item.Product.ID] = item.Quantity
	}
	productIDs := make([]string, 0, len(order.Items)*2)
	for _, item := range order.Items {
		productIDs = append(productIDs, item.ProductID)
		if item.ReplacementProductID != "" {
			productIDs = append(productIDs, item.ReplacementProductID)
		}
	}
	products, err := s.repo.Products(ctx, productIDs)
	if err != nil {
		return domain.RepeatPreview{}, err
	}
	for _, oldItem := range order.Items {
		preview.RequestedCount += oldItem.Quantity
		product, exists := products[oldItem.ProductID]
		if !exists {
			repeat := domain.RepeatItem{
				Product:           domain.Product{ID: oldItem.ProductID, SKU: oldItem.SKU, Name: oldItem.Name, Image: oldItem.Image},
				RequestedQuantity: oldItem.Quantity, OriginalUnitPriceMinor: oldItem.UnitPriceMinor,
				Status: "discontinued", Message: "Товар больше не продаётся",
			}
			if replacement, ok := products[oldItem.ReplacementProductID]; ok {
				replacementCopy := replacement
				repeat.Replacement = &replacementCopy
			}
			preview.Items = append(preview.Items, repeat)
			preview.UnavailableCount++
			continue
		}
		already := inCart[product.ID]
		addable := product.Stock - already
		if addable < 0 {
			addable = 0
		}
		item := domain.RepeatItem{
			Product: product, RequestedQuantity: oldItem.Quantity, AvailableQuantity: product.Stock,
			AddableQuantity: addable, AlreadyInCartQuantity: already,
			OriginalUnitPriceMinor: oldItem.UnitPriceMinor, CurrentUnitPriceMinor: product.PriceMinor,
			PriceDeltaMinor: product.PriceMinor - oldItem.UnitPriceMinor, PriceChanged: product.PriceMinor != oldItem.UnitPriceMinor,
		}
		suggested := repeatAdditionAtMost(product, already, oldItem.Quantity, addable)
		smallest := smallestRepeatAddition(product, already, addable)
		switch {
		case addable <= 0:
			item.Status = "unavailable"
			if already > 0 && product.Stock > 0 {
				item.Message = "Весь доступный остаток уже в корзине"
			} else {
				item.Message = "Нет в наличии"
			}
			preview.UnavailableCount++
		case smallest == 0:
			item.Status = "unavailable"
			item.Message = "Недостаточный остаток для минимальной партии"
			preview.UnavailableCount++
		case suggested == 0:
			item.Status = "limited"
			item.ProposedQuantity = smallest
			item.Message = fmt.Sprintf("Минимально можно добавить %d", smallest)
			preview.LimitedCount++
		case suggested < oldItem.Quantity:
			item.Status = "limited"
			item.ProposedQuantity = suggested
			item.SuggestedQuantity = suggested
			item.Selected = true
			item.Message = fmt.Sprintf("Можно добавить только %d", suggested)
			preview.LimitedCount++
			preview.EstimatedTotalMinor += int64(suggested) * product.PriceMinor
		case true:
			item.Status = "available"
			item.ProposedQuantity = suggested
			item.SuggestedQuantity = suggested
			item.Selected = true
			preview.AvailableCount++
			preview.EstimatedTotalMinor += int64(suggested) * product.PriceMinor
		}
		preview.Items = append(preview.Items, item)
	}
	return preview, nil
}

func (s *Service) CommitRepeat(ctx context.Context, userID, orderID string, selections []domain.RepeatSelection) (domain.RepeatCommitResult, error) {
	preview, err := s.RepeatPreview(ctx, userID, orderID)
	if err != nil {
		return domain.RepeatCommitResult{}, err
	}
	byProduct := make(map[string]domain.RepeatItem, len(preview.Items))
	for _, item := range preview.Items {
		byProduct[item.Product.ID] = item
	}
	mutations := make([]domain.CartMutation, 0, len(selections))
	skipped := 0
	adjusted := 0
	addedQuantity := 0
	fields := make([]domain.FieldError, 0)
	seen := make(map[string]bool, len(selections))
	for i, selection := range selections {
		item, exists := byProduct[selection.ProductID]
		if !exists || seen[selection.ProductID] {
			fields = append(fields, domain.FieldError{Field: fmt.Sprintf("items.%d.productId", i), Message: "Позиция не относится к этому заказу"})
			continue
		}
		seen[selection.ProductID] = true
		if !selection.Selected {
			skipped++
			continue
		}
		minimum := max(1, item.Product.MinimumQuantity)
		step := max(1, item.Product.QuantityStep)
		if item.Status == "unavailable" || item.Status == "discontinued" || !validRepeatAddition(item.Product, item.AlreadyInCartQuantity, selection.Quantity, item.AddableQuantity) {
			if item.Status != "unavailable" && item.Status != "discontinued" && selection.Quantity > 0 && selection.Quantity <= item.AddableQuantity {
				fields = append(fields, domain.FieldError{Field: fmt.Sprintf("items.%d.quantity", i), Message: fmt.Sprintf("Итоговое количество должно быть не меньше %d, шаг %d", minimum, step)})
				continue
			}
			fields = append(fields, domain.FieldError{Field: fmt.Sprintf("items.%d.selected", i), Message: "Товар сейчас нельзя добавить в корзину"})
			continue
		}
		mutations = append(mutations, domain.CartMutation{ProductID: selection.ProductID, Quantity: selection.Quantity, Selected: true})
		addedQuantity += selection.Quantity
		if selection.Quantity != item.RequestedQuantity {
			adjusted++
		}
	}
	if len(fields) > 0 {
		return domain.RepeatCommitResult{}, &domain.ValidationError{Fields: fields}
	}
	if len(selections) == 0 {
		for _, item := range preview.Items {
			if item.Selected && validRepeatAddition(item.Product, item.AlreadyInCartQuantity, item.SuggestedQuantity, item.AddableQuantity) {
				mutations = append(mutations, domain.CartMutation{ProductID: item.Product.ID, Quantity: item.SuggestedQuantity, Selected: true})
				addedQuantity += item.SuggestedQuantity
				if item.SuggestedQuantity != item.RequestedQuantity {
					adjusted++
				}
			} else {
				skipped++
			}
		}
	}
	cart, err := s.repo.AddCartItems(ctx, userID, mutations)
	if err != nil {
		return domain.RepeatCommitResult{}, err
	}
	return domain.RepeatCommitResult{Cart: cart, AddedCount: len(mutations), AddedQuantity: addedQuantity, AdjustedCount: adjusted, SkippedCount: skipped}, nil
}

func validRepeatAddition(product domain.Product, existing, addition, addable int) bool {
	if addition < 1 || addition > addable {
		return false
	}
	minimum := max(1, product.MinimumQuantity)
	step := max(1, product.QuantityStep)
	finalQuantity := existing + addition
	return finalQuantity >= minimum && (finalQuantity-minimum)%step == 0
}

func repeatAdditionAtMost(product domain.Product, existing, requested, addable int) int {
	limit := min(requested, addable)
	if limit < 1 {
		return 0
	}
	minimum := max(1, product.MinimumQuantity)
	step := max(1, product.QuantityStep)
	residue := positiveMod(minimum-existing, step)
	candidate := limit - positiveMod(limit-residue, step)
	if candidate < 1 || existing+candidate < minimum {
		return 0
	}
	return candidate
}

func smallestRepeatAddition(product domain.Product, existing, addable int) int {
	if addable < 1 {
		return 0
	}
	minimum := max(1, product.MinimumQuantity)
	step := max(1, product.QuantityStep)
	lower := max(1, minimum-existing)
	residue := positiveMod(minimum-existing, step)
	candidate := lower + positiveMod(residue-lower, step)
	if candidate > addable {
		return 0
	}
	return candidate
}

func positiveMod(value, divisor int) int {
	result := value % divisor
	if result < 0 {
		result += divisor
	}
	return result
}

func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
