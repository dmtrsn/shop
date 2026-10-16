package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"sweetshop/internal/domain"
)

const sessionLifetime = 30 * 24 * time.Hour

// dummyPasswordHash is a bcrypt hash at CustomerPasswordCost of random bytes
// that were discarded when it was generated. It verifies nothing: comparing
// unknown-user logins against it keeps their timing close to verifying a real
// customer password without generating a hash on the request path.
const dummyPasswordHash = "$2a$12$t859E7d1TQ8J0QX8qUT/j.1xp81JsrOhXJzyBFH2fmCjaBoZg5QTm"

// Options holds deployment policy for the service.
type Options struct {
	// AllowDemoAccount permits the seeded demo principal to sign in and use its
	// sessions. It must be false in production.
	AllowDemoAccount bool
}

type Service struct {
	repo    domain.Repository
	now     func() time.Time
	options Options
}

type readinessRepository interface {
	Ping(context.Context) error
}

func New(repo domain.Repository, options Options) *Service {
	return &Service{repo: repo, now: time.Now, options: options}
}

func (s *Service) Ready(ctx context.Context) error {
	if repository, ok := s.repo.(readinessRepository); ok {
		return repository.Ping(ctx)
	}
	return nil
}

// DemoLoginAllowed reports whether the seeded demo principal may sign in.
func (s *Service) DemoLoginAllowed() bool { return s.options.AllowDemoAccount }

type LoginResult struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expiresAt"`
	User      domain.User `json:"user"`
}

// Login is the legacy bearer login; its contract is unchanged.
func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	return s.login(ctx, email, password, domain.SessionBearer, "")
}

// StartCookieSession creates a cookie-transport session. When previousToken is
// the raw value of a cookie the client presented, that cookie session is
// replaced atomically with the new one.
func (s *Service) StartCookieSession(ctx context.Context, email, password, previousToken string) (LoginResult, error) {
	return s.login(ctx, email, password, domain.SessionCookie, previousToken)
}

func (s *Service) login(ctx context.Context, email, password string, transport domain.SessionTransport, previousToken string) (LoginResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return LoginResult{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "email", Message: "Укажите email"}, {Field: "password", Message: "Укажите пароль"}}}
	}
	credential, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			_ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(password))
			return LoginResult{}, domain.ErrUnauthorized
		}
		return LoginResult{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(credential.PasswordHash), []byte(password)) != nil {
		return LoginResult{}, domain.ErrUnauthorized
	}
	// Checked after the password so a disabled account costs the same and
	// answers the same as a wrong password.
	if credential.Disabled {
		return LoginResult{}, domain.ErrAccountDisabled
	}
	if err := s.allowPrincipal(credential.User.ID); err != nil {
		return LoginResult{}, err
	}
	token, err := randomToken(32)
	if err != nil {
		return LoginResult{}, fmt.Errorf("create session token: %w", err)
	}
	now := s.now().UTC()
	// PostgreSQL stores microseconds; report exactly the instant that is enforced.
	expires := now.Add(sessionLifetime).Truncate(time.Microsecond)
	session := domain.NewSession{UserID: credential.User.ID, TokenHash: tokenHash(token), Transport: transport, ExpiresAt: expires, PasswordHash: credential.PasswordHash}
	if transport == domain.SessionCookie && previousToken != "" {
		session.ReplacesTokenHash = tokenHash(previousToken)
	}
	if err := s.repo.CreateSession(ctx, session, now); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, ExpiresAt: session.ExpiresAt, User: credential.User}, nil
}

// Authenticate validates a legacy bearer token.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, error) {
	record, err := s.AuthenticateSession(ctx, token, domain.SessionBearer)
	return record.User, err
}

// AuthenticateSession validates a raw token presented through transport.
func (s *Service) AuthenticateSession(ctx context.Context, token string, transport domain.SessionTransport) (domain.SessionRecord, error) {
	if token == "" || !transport.Valid() {
		return domain.SessionRecord{}, domain.ErrUnauthorized
	}
	record, err := s.repo.SessionByTokenHash(ctx, tokenHash(token), transport, s.now().UTC())
	if err != nil {
		return domain.SessionRecord{}, err
	}
	if err := s.allowPrincipal(record.User.ID); err != nil {
		return domain.SessionRecord{}, err
	}
	return record, nil
}

// Logout revokes the session identified by the raw token and transport. It is
// idempotent.
func (s *Service) Logout(ctx context.Context, token string, transport domain.SessionTransport) error {
	if token == "" || !transport.Valid() {
		return nil
	}
	return s.repo.DeleteSession(ctx, tokenHash(token), transport)
}

func (s *Service) allowPrincipal(userID string) error {
	if userID == domain.DemoUserID && !s.options.AllowDemoAccount {
		return domain.ErrDemoAccountDisabled
	}
	return nil
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
	if err := validateSearchQuery(filter.Query); err != nil {
		return domain.ProductPage{}, err
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
	if err := validateSearchQuery(filter.Query); err != nil {
		return domain.CatalogFacets{}, err
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
	if err := validateSearchQuery(query); err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(query) < minSuggestionQueryRunes {
		return []domain.SearchSuggestion{}, nil
	}
	return s.repo.Suggestions(ctx, query, 8)
}

const (
	maxSearchQueryRunes     = 200
	minSuggestionQueryRunes = 2
)

// validateSearchQuery applies the shared length bound for catalog, facets and suggestions.
func validateSearchQuery(query string) error {
	if utf8.RuneCountInString(query) > maxSearchQueryRunes {
		return &domain.ValidationError{Fields: []domain.FieldError{{Field: "q", Message: "Поисковый запрос не должен быть длиннее 200 символов"}}}
	}
	return nil
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

// idempotencyKeyPattern is the accepted Idempotency-Key syntax; a browser
// crypto.randomUUID() value matches it.
var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

// Checkout validates the request and computes the idempotency digest before the
// repository consumes the selected cart. Address and comment rules are the
// legacy ones: a trimmed nonempty address and an untouched optional comment.
func (s *Service) Checkout(ctx context.Context, userID string, input domain.CheckoutInput) (domain.CheckoutResult, error) {
	input.DeliveryAddress = strings.TrimSpace(input.DeliveryAddress)
	fields := []domain.FieldError{}
	if input.DeliveryAddress == "" {
		fields = append(fields, domain.FieldError{Field: "deliveryAddress", Message: "Укажите адрес доставки"})
	}
	if input.IdempotencyKey != nil && !idempotencyKeyPattern.MatchString(*input.IdempotencyKey) {
		fields = append(fields, domain.FieldError{Field: "Idempotency-Key", Message: "Ключ идемпотентности: от 16 до 128 символов A-Z, a-z, 0-9, _ или -"})
	}
	fields = append(fields, validateCheckoutExpectation(input)...)
	if len(fields) > 0 {
		return domain.CheckoutResult{}, &domain.ValidationError{Fields: fields}
	}
	input.RequestDigest = ""
	if input.IdempotencyKey != nil {
		input.RequestDigest = checkoutRequestDigest(input)
	}
	order, replayed, err := s.repo.CreateOrder(ctx, userID, input)
	if err != nil {
		return domain.CheckoutResult{}, err
	}
	return domain.CheckoutResult{Order: order, Replayed: replayed}, nil
}

func validateCheckoutExpectation(input domain.CheckoutInput) []domain.FieldError {
	if (input.ExpectedItems == nil) != (input.ExpectedTotalMinor == nil) {
		return []domain.FieldError{{Field: "expectedItems", Message: "Передайте expectedItems и expectedTotalMinor вместе"}}
	}
	if input.ExpectedItems == nil {
		return nil
	}
	fields := []domain.FieldError{}
	items := *input.ExpectedItems
	if len(items) == 0 {
		fields = append(fields, domain.FieldError{Field: "expectedItems", Message: "Список ожидаемых позиций не может быть пустым"})
	}
	seen := make(map[string]bool, len(items))
	for i, item := range items {
		switch {
		case item.ProductID == "":
			fields = append(fields, domain.FieldError{Field: fmt.Sprintf("expectedItems.%d.productId", i), Message: "Укажите товар"})
		case seen[item.ProductID]:
			fields = append(fields, domain.FieldError{Field: fmt.Sprintf("expectedItems.%d.productId", i), Message: "Товар указан повторно"})
		}
		seen[item.ProductID] = true
		if item.Quantity < 1 {
			fields = append(fields, domain.FieldError{Field: fmt.Sprintf("expectedItems.%d.quantity", i), Message: "Количество должно быть больше нуля"})
		}
		if item.UnitPriceMinor < 0 {
			fields = append(fields, domain.FieldError{Field: fmt.Sprintf("expectedItems.%d.unitPriceMinor", i), Message: "Цена не может быть отрицательной"})
		}
	}
	if *input.ExpectedTotalMinor < 0 {
		fields = append(fields, domain.FieldError{Field: "expectedTotalMinor", Message: "Сумма не может быть отрицательной"})
	}
	return fields
}

// checkoutDigestForm is the versioned canonical logical request. Field order
// is fixed by the struct, so encoding/json output is deterministic. Strings
// decoded from HTTP JSON are already valid UTF-8, so encoding is lossless.
type checkoutDigestForm struct {
	Version         string                      `json:"v"`
	DeliveryAddress string                      `json:"deliveryAddress"`
	Comment         string                      `json:"comment"`
	Expectations    *checkoutDigestExpectations `json:"expectations"`
}

type checkoutDigestExpectations struct {
	Items      []domain.ExpectedCheckoutItem `json:"items"`
	TotalMinor int64                         `json:"totalMinor"`
}

// checkoutRequestDigest identifies the logical request bound to an
// idempotency key: the normalized address, the comment exactly as persisted
// (omitted, null and "" are the same empty string), and the expectation or
// its absence with items sorted by product ID. Internal only; never returned.
func checkoutRequestDigest(input domain.CheckoutInput) string {
	form := checkoutDigestForm{Version: "checkout/v1", DeliveryAddress: input.DeliveryAddress, Comment: input.Comment}
	if input.ExpectedItems != nil && input.ExpectedTotalMinor != nil {
		items := append([]domain.ExpectedCheckoutItem(nil), (*input.ExpectedItems)...)
		sort.Slice(items, func(i, j int) bool { return items[i].ProductID < items[j].ProductID })
		form.Expectations = &checkoutDigestExpectations{Items: items, TotalMinor: *input.ExpectedTotalMinor}
	}
	encoded, err := json.Marshal(form)
	if err != nil {
		// Plain strings and integers cannot fail to encode.
		panic(fmt.Sprintf("encode checkout digest: %v", err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
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
