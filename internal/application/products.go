package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"sweetshop/internal/domain"
)

// Staff product actions. Each mutation is authorized and audited under its
// own action (ADR 012, ADR 013).
const (
	ProductRead    = "product.read"
	ProductCreate  = domain.AuditAction("product.create")
	ProductUpdate  = domain.AuditAction("product.update")
	ProductArchive = domain.AuditAction("product.archive")
	ProductRestore = domain.AuditAction("product.restore")
)

// Bounds keep every order total inside int64: at most MaxStock units at
// MaxPriceMinor each.
const (
	MaxPriceMinor      int64 = 1_000_000_000
	MaxStock                 = 1_000_000
	MaxQuantityRule          = 10_000
	MaxAdminPageLimit        = 100
	defaultAdminLimit        = 50
	maxProductListSize       = 20
)

// ProductAdminStore is the staff product port; only PostgreSQL implements
// it. Every mutation runs inside one audited staff transaction, rechecks
// the expected revision under the product row lock and increments it.
type ProductAdminStore interface {
	AdminProducts(ctx context.Context, filter domain.AdminProductFilter) (domain.AdminProductPage, error)
	// AdminProduct returns archived products too; a missing one is ErrNotFound.
	AdminProduct(ctx context.Context, id string) (domain.AdminProduct, error)
	// CreateProduct returns a ValidationError for unknown references or a
	// used slug or SKU.
	CreateProduct(ctx context.Context, mutation domain.StaffMutation, product domain.NewProduct) (domain.AdminProduct, error)
	// UpdateProduct and SetProductArchived return ErrNotFound, ErrStaleRevision,
	// a ValidationError, or (archive of an archived product, restore of an
	// active one) ErrConflict, changing nothing.
	UpdateProduct(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, input domain.ProductInput) (domain.AdminProduct, error)
	SetProductArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminProduct, error)
	AdminCatalogOptions(ctx context.Context) (domain.CatalogOptions, error)
}

// ProductAdmin is the staff product use case.
type ProductAdmin struct {
	store ProductAdminStore
}

func NewProductAdmin(store ProductAdminStore) *ProductAdmin {
	return &ProductAdmin{store: store}
}

func (a *ProductAdmin) List(ctx context.Context, filter domain.AdminProductFilter) (domain.AdminProductPage, error) {
	fields := []domain.FieldError{}
	switch filter.Status {
	case "":
		filter.Status = domain.AdminProductsActive
	case domain.AdminProductsActive, domain.AdminProductsArchived, domain.AdminProductsAll:
	default:
		fields = append(fields, domain.FieldError{Field: "status", Message: "Допустимо: active, archived, all"})
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if utf8.RuneCountInString(filter.Query) > 100 || !singleLine(filter.Query) {
		fields = append(fields, domain.FieldError{Field: "q", Message: "Запрос: до 100 символов в одну строку"})
	}
	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.Limit == 0 {
		filter.Limit = defaultAdminLimit
	}
	if filter.Page < 1 || filter.Page > 10_000 {
		fields = append(fields, domain.FieldError{Field: "page", Message: "Страница: от 1 до 10000"})
	}
	if filter.Limit < 1 || filter.Limit > MaxAdminPageLimit {
		fields = append(fields, domain.FieldError{Field: "limit", Message: fmt.Sprintf("Размер страницы: от 1 до %d", MaxAdminPageLimit)})
	}
	if len(fields) > 0 {
		return domain.AdminProductPage{}, &domain.ValidationError{Fields: fields}
	}
	return a.store.AdminProducts(ctx, filter)
}

// CatalogOptions lists the references a product may use.
func (a *ProductAdmin) CatalogOptions(ctx context.Context) (domain.CatalogOptions, error) {
	return a.store.AdminCatalogOptions(ctx)
}

func (a *ProductAdmin) Get(ctx context.Context, id string) (domain.AdminProduct, error) {
	return a.store.AdminProduct(ctx, id)
}

func (a *ProductAdmin) Create(ctx context.Context, mutation domain.StaffMutation, input domain.ProductInput) (domain.AdminProduct, error) {
	if err := requireAction(mutation, ProductCreate); err != nil {
		return domain.AdminProduct{}, err
	}
	input, err := ValidateProductInput(input)
	if err != nil {
		return domain.AdminProduct{}, err
	}
	id, err := newAccountID("prd_")
	if err != nil {
		return domain.AdminProduct{}, err
	}
	return a.store.CreateProduct(ctx, mutation, domain.NewProduct{ID: id, Input: input})
}

func (a *ProductAdmin) Update(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, input domain.ProductInput) (domain.AdminProduct, error) {
	if err := requireAction(mutation, ProductUpdate); err != nil {
		return domain.AdminProduct{}, err
	}
	fields := revisionFields(expectedRevision)
	input, err := ValidateProductInput(input)
	if validation, ok := err.(*domain.ValidationError); ok {
		fields = append(fields, validation.Fields...)
	} else if err != nil {
		return domain.AdminProduct{}, err
	}
	if len(fields) > 0 {
		return domain.AdminProduct{}, &domain.ValidationError{Fields: fields}
	}
	return a.store.UpdateProduct(ctx, mutation, id, expectedRevision, input)
}

// Archive withdraws the product from sale; Restore returns it. Neither
// deletes anything.
func (a *ProductAdmin) Archive(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64) (domain.AdminProduct, error) {
	return a.setArchived(ctx, mutation, ProductArchive, id, expectedRevision, true)
}

func (a *ProductAdmin) Restore(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64) (domain.AdminProduct, error) {
	return a.setArchived(ctx, mutation, ProductRestore, id, expectedRevision, false)
}

func (a *ProductAdmin) setArchived(ctx context.Context, mutation domain.StaffMutation, action domain.AuditAction, id string, expectedRevision int64, archived bool) (domain.AdminProduct, error) {
	if err := requireAction(mutation, action); err != nil {
		return domain.AdminProduct{}, err
	}
	if fields := revisionFields(expectedRevision); len(fields) > 0 {
		return domain.AdminProduct{}, &domain.ValidationError{Fields: fields}
	}
	return a.store.SetProductArchived(ctx, mutation, id, expectedRevision, archived)
}

// requireAction keeps a mutation authorized for one action out of another.
func requireAction(mutation domain.StaffMutation, action domain.AuditAction) error {
	if !mutation.Valid() || mutation.Action() != action {
		return fmt.Errorf("staff mutation for %q used for %q", mutation.Action(), action)
	}
	return nil
}

func revisionFields(expectedRevision int64) []domain.FieldError {
	if expectedRevision < 1 {
		return []domain.FieldError{{Field: "expectedRevision", Message: "Укажите ревизию, на основе которой сделано изменение"}}
	}
	return nil
}

var (
	productSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	productSKUPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	referencePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// ValidateProductInput trims text and checks every staff product rule. It
// reports all violations at once; references are checked again by the store
// for existence.
func ValidateProductInput(input domain.ProductInput) (domain.ProductInput, error) {
	fields := []domain.FieldError{}
	fail := func(field, message string) {
		fields = append(fields, domain.FieldError{Field: field, Message: message})
	}
	for _, text := range []*string{&input.Slug, &input.SKU, &input.Name, &input.ShortName, &input.Unit, &input.Series,
		&input.CategoryID, &input.ManufacturerID, &input.BrandID, &input.Description, &input.Ingredients, &input.Nutrition} {
		*text = strings.TrimSpace(*text)
	}
	if !productSlugPattern.MatchString(input.Slug) || len(input.Slug) > 120 {
		fail("slug", "Slug: до 120 символов a–z, 0–9 и дефисов между ними")
	}
	if !productSKUPattern.MatchString(input.SKU) || len(input.SKU) > 64 {
		fail("sku", "Артикул: до 64 символов A–Z, a–z, 0–9, «.», «_», «-»")
	}
	checkLine := func(field, value string, required bool, limit int) {
		switch {
		case required && value == "":
			fail(field, "Обязательное поле")
		case utf8.RuneCountInString(value) > limit || !singleLine(value):
			fail(field, fmt.Sprintf("До %d символов в одну строку", limit))
		}
	}
	checkLine("name", input.Name, true, 200)
	checkLine("shortName", input.ShortName, false, 100)
	checkLine("unit", input.Unit, true, 50)
	checkLine("series", input.Series, false, 100)
	for _, text := range []struct{ field, value string }{{"description", input.Description}, {"ingredients", input.Ingredients}, {"nutrition", input.Nutrition}} {
		if utf8.RuneCountInString(text.value) > 5000 || !multiLine(text.value) {
			fail(text.field, "До 5000 символов без управляющих символов")
		}
	}
	if input.PriceMinor < 1 || input.PriceMinor > MaxPriceMinor {
		fail("priceMinor", fmt.Sprintf("Цена в копейках: от 1 до %d", MaxPriceMinor))
	}
	// An old price is either absent (0) or a real discount.
	if input.OldPriceMinor != 0 && (input.OldPriceMinor <= input.PriceMinor || input.OldPriceMinor > MaxPriceMinor) {
		fail("oldPriceMinor", "Старая цена: 0 или больше текущей цены")
	}
	checkRange := func(field string, value, low, high int) {
		if value < low || value > high {
			fail(field, fmt.Sprintf("От %d до %d", low, high))
		}
	}
	checkRange("weightGrams", input.WeightGrams, 0, 1_000_000)
	checkRange("shelfLifeDays", input.ShelfLifeDays, 0, 3650)
	checkRange("stock", input.Stock, 0, MaxStock)
	checkRange("minimumQuantity", input.MinimumQuantity, 1, MaxQuantityRule)
	checkRange("quantityStep", input.QuantityStep, 1, MaxQuantityRule)
	for _, reference := range []struct{ field, value string }{{"categoryId", input.CategoryID}, {"manufacturerId", input.ManufacturerID}, {"brandId", input.BrandID}} {
		if !referencePattern.MatchString(reference.value) || len(reference.value) > 100 {
			fail(reference.field, "Укажите существующий справочник")
		}
	}
	var message string
	if input.CollectionIDs, message = normalizeList(input.CollectionIDs, func(id string) bool { return referencePattern.MatchString(id) && len(id) <= 100 }); message != "" {
		fail("collectionIds", message)
	}
	label := func(value string) bool { return utf8.RuneCountInString(value) <= 50 && singleLine(value) }
	if input.Tags, message = normalizeList(input.Tags, label); message != "" {
		fail("tags", message)
	}
	if input.Badges, message = normalizeList(input.Badges, label); message != "" {
		fail("badges", message)
	}
	if len(fields) > 0 {
		return domain.ProductInput{}, &domain.ValidationError{Fields: fields}
	}
	return input, nil
}

// normalizeList trims, rejects empty, invalid and repeated entries, and
// bounds the length; the order is kept.
func normalizeList(values []string, valid func(string) bool) ([]string, string) {
	if len(values) > maxProductListSize {
		return nil, fmt.Sprintf("Не больше %d значений", maxProductListSize)
	}
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || !valid(value) {
			return nil, "Пустое или недопустимое значение"
		}
		if seen[value] {
			return nil, "Значения не должны повторяться"
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, ""
}

func singleLine(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

func multiLine(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t'
	})
}
