package application

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"sweetshop/internal/domain"
)

type fakeProductStore struct {
	created  []domain.NewProduct
	updated  []domain.ProductInput
	archived []bool
	filter   domain.AdminProductFilter
}

func (f *fakeProductStore) AdminProducts(_ context.Context, filter domain.AdminProductFilter) (domain.AdminProductPage, error) {
	f.filter = filter
	return domain.AdminProductPage{}, nil
}

func (f *fakeProductStore) AdminProduct(context.Context, string) (domain.AdminProduct, error) {
	return domain.AdminProduct{}, domain.ErrNotFound
}

func (f *fakeProductStore) CreateProduct(_ context.Context, _ domain.StaffMutation, product domain.NewProduct) (domain.AdminProduct, error) {
	f.created = append(f.created, product)
	return domain.AdminProduct{ID: product.ID, Revision: 1}, nil
}

func (f *fakeProductStore) UpdateProduct(_ context.Context, _ domain.StaffMutation, _ string, _ int64, input domain.ProductInput) (domain.AdminProduct, error) {
	f.updated = append(f.updated, input)
	return domain.AdminProduct{}, nil
}

func (f *fakeProductStore) SetProductArchived(_ context.Context, _ domain.StaffMutation, _ string, _ int64, archived bool) (domain.AdminProduct, error) {
	f.archived = append(f.archived, archived)
	return domain.AdminProduct{}, nil
}

func validProduct() domain.ProductInput {
	return domain.ProductInput{
		Slug: "dark-bar", SKU: "DRK-1", Name: "Тёмный шоколад", Unit: "плитка 90 г", PriceMinor: 48900, OldPriceMinor: 54900,
		CategoryID: "cat-bars", ManufacturerID: "m-choco", BrandID: "brand-choco", CollectionIDs: []string{}, Tags: []string{"веган"},
		Badges: []string{"Хит"}, Stock: 10, MinimumQuantity: 2, QuantityStep: 2,
	}
}

func staffMutationFor(t *testing.T, action domain.AuditAction) domain.StaffMutation {
	t.Helper()
	mutation, err := NewStaffAuth(&fakeStaffStore{}).AuthorizeMutation(domain.StaffUser{ID: "stf_" + strings.Repeat("a", 32)}, action, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	return mutation
}

func fieldNames(err error) []string {
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		return nil
	}
	names := []string{}
	for _, field := range validation.Fields {
		names = append(names, field.Field)
	}
	return names
}

func TestValidateProductInputAcceptsAndTrims(t *testing.T) {
	input := validProduct()
	input.Name, input.Tags = "  Тёмный шоколад  ", []string{" веган ", "постное"}
	got, err := ValidateProductInput(input)
	if err != nil || got.Name != "Тёмный шоколад" || strings.Join(got.Tags, ",") != "веган,постное" {
		t.Fatalf("%+v %v", got, err)
	}
	input.OldPriceMinor = 0
	if _, err := ValidateProductInput(input); err != nil {
		t.Fatalf("no old price: %v", err)
	}
}

func TestValidateProductInputRejectsEveryInvariant(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(*domain.ProductInput)
		field  string
	}{
		"zero price":              {func(p *domain.ProductInput) { p.PriceMinor = 0 }, "priceMinor"},
		"negative price":          {func(p *domain.ProductInput) { p.PriceMinor = -1 }, "priceMinor"},
		"huge price":              {func(p *domain.ProductInput) { p.PriceMinor, p.OldPriceMinor = MaxPriceMinor+1, 0 }, "priceMinor"},
		"old price equal":         {func(p *domain.ProductInput) { p.OldPriceMinor = p.PriceMinor }, "oldPriceMinor"},
		"old price lower":         {func(p *domain.ProductInput) { p.OldPriceMinor = p.PriceMinor - 1 }, "oldPriceMinor"},
		"negative old price":      {func(p *domain.ProductInput) { p.OldPriceMinor = -5 }, "oldPriceMinor"},
		"negative stock":          {func(p *domain.ProductInput) { p.Stock = -1 }, "stock"},
		"huge stock":              {func(p *domain.ProductInput) { p.Stock = MaxStock + 1 }, "stock"},
		"zero minimum":            {func(p *domain.ProductInput) { p.MinimumQuantity = 0 }, "minimumQuantity"},
		"negative minimum":        {func(p *domain.ProductInput) { p.MinimumQuantity = -2 }, "minimumQuantity"},
		"huge minimum":            {func(p *domain.ProductInput) { p.MinimumQuantity = MaxQuantityRule + 1 }, "minimumQuantity"},
		"zero step":               {func(p *domain.ProductInput) { p.QuantityStep = 0 }, "quantityStep"},
		"negative step":           {func(p *domain.ProductInput) { p.QuantityStep = -1 }, "quantityStep"},
		"bad slug":                {func(p *domain.ProductInput) { p.Slug = "Dark Bar" }, "slug"},
		"slug with underscore":    {func(p *domain.ProductInput) { p.Slug = "dark_bar" }, "slug"},
		"bad sku":                 {func(p *domain.ProductInput) { p.SKU = "-x" }, "sku"},
		"empty name":              {func(p *domain.ProductInput) { p.Name = "   " }, "name"},
		"multiline name":          {func(p *domain.ProductInput) { p.Name = "a\nb" }, "name"},
		"long name":               {func(p *domain.ProductInput) { p.Name = strings.Repeat("я", 201) }, "name"},
		"empty unit":              {func(p *domain.ProductInput) { p.Unit = "" }, "unit"},
		"control in description":  {func(p *domain.ProductInput) { p.Description = "a\x00b" }, "description"},
		"negative weight":         {func(p *domain.ProductInput) { p.WeightGrams = -1 }, "weightGrams"},
		"negative shelf life":     {func(p *domain.ProductInput) { p.ShelfLifeDays = -1 }, "shelfLifeDays"},
		"empty category":          {func(p *domain.ProductInput) { p.CategoryID = "" }, "categoryId"},
		"path-like manufacturer":  {func(p *domain.ProductInput) { p.ManufacturerID = "../m" }, "manufacturerId"},
		"empty brand":             {func(p *domain.ProductInput) { p.BrandID = " " }, "brandId"},
		"repeated collection":     {func(p *domain.ProductInput) { p.CollectionIDs = []string{"c1", "c1"} }, "collectionIds"},
		"empty tag":               {func(p *domain.ProductInput) { p.Tags = []string{""} }, "tags"},
		"too many badges":         {func(p *domain.ProductInput) { p.Badges = make([]string, 21) }, "badges"},
		"long tag":                {func(p *domain.ProductInput) { p.Tags = []string{strings.Repeat("t", 51)} }, "tags"},
		"invalid UTF-8 in series": {func(p *domain.ProductInput) { p.Series = "\xff" }, "series"},
	} {
		t.Run(name, func(t *testing.T) {
			input := validProduct()
			tc.change(&input)
			_, err := ValidateProductInput(input)
			if names := fieldNames(err); len(names) != 1 || names[0] != tc.field {
				t.Fatalf("fields %v (%v)", names, err)
			}
		})
	}
}

func TestProductAdminCreatesWithANewProductID(t *testing.T) {
	store := &fakeProductStore{}
	admin := NewProductAdmin(store)
	if _, err := admin.Create(context.Background(), staffMutationFor(t, ProductCreate), validProduct()); err != nil {
		t.Fatal(err)
	}
	if len(store.created) != 1 || !regexp.MustCompile(`^prd_[0-9a-f]{32}$`).MatchString(store.created[0].ID) {
		t.Fatalf("%+v", store.created)
	}
	bad := validProduct()
	bad.Stock = -1
	if _, err := admin.Create(context.Background(), staffMutationFor(t, ProductCreate), bad); !errors.Is(err, domain.ErrValidation) || len(store.created) != 1 {
		t.Fatal("invalid product reached the store")
	}
}

func TestProductAdminMutationsNeedTheirOwnAction(t *testing.T) {
	store := &fakeProductStore{}
	admin := NewProductAdmin(store)
	ctx := context.Background()
	update := staffMutationFor(t, ProductUpdate)
	if _, err := admin.Create(ctx, update, validProduct()); err == nil {
		t.Fatal("an update mutation created a product")
	}
	if _, err := admin.Archive(ctx, update, "p01", 1); err == nil {
		t.Fatal("an update mutation archived a product")
	}
	if _, err := admin.Restore(ctx, staffMutationFor(t, ProductArchive), "p01", 1); err == nil {
		t.Fatal("an archive mutation restored a product")
	}
	if _, err := admin.Update(ctx, domain.StaffMutation{}, "p01", 1, validProduct()); err == nil {
		t.Fatal("an unauthorized update ran")
	}
	if len(store.created)+len(store.updated)+len(store.archived) != 0 {
		t.Fatal("store reached")
	}
}

func TestProductAdminRequiresAnExpectedRevision(t *testing.T) {
	store := &fakeProductStore{}
	admin := NewProductAdmin(store)
	ctx := context.Background()
	bad := validProduct()
	bad.QuantityStep = 0
	_, err := admin.Update(ctx, staffMutationFor(t, ProductUpdate), "p01", 0, bad)
	if names := fieldNames(err); strings.Join(names, ",") != "expectedRevision,quantityStep" {
		t.Fatalf("%v", names)
	}
	if _, err := admin.Archive(ctx, staffMutationFor(t, ProductArchive), "p01", -1); fieldNames(err) == nil {
		t.Fatal("archive without a revision")
	}
	if _, err := admin.Archive(ctx, staffMutationFor(t, ProductArchive), "p01", 3); err != nil || len(store.archived) != 1 || !store.archived[0] {
		t.Fatal("archive")
	}
	if _, err := admin.Restore(ctx, staffMutationFor(t, ProductRestore), "p01", 4); err != nil || store.archived[1] {
		t.Fatal("restore")
	}
}

func TestProductAdminListFilter(t *testing.T) {
	store := &fakeProductStore{}
	admin := NewProductAdmin(store)
	if _, err := admin.List(context.Background(), domain.AdminProductFilter{Query: "  шоколад "}); err != nil {
		t.Fatal(err)
	}
	if store.filter != (domain.AdminProductFilter{Status: domain.AdminProductsActive, Query: "шоколад", Page: 1, Limit: 50}) {
		t.Fatalf("%+v", store.filter)
	}
	for _, filter := range []domain.AdminProductFilter{{Status: "deleted"}, {Limit: MaxAdminPageLimit + 1}, {Page: -1}, {Query: strings.Repeat("q", 101)}, {Query: "a\nb"}} {
		if _, err := admin.List(context.Background(), filter); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%+v accepted", filter)
		}
	}
}

func TestProductActionsAreValidAuditActions(t *testing.T) {
	for _, action := range []domain.AuditAction{ProductCreate, ProductUpdate, ProductArchive, ProductRestore} {
		if !action.Valid() || !strings.HasPrefix(string(action), "product.") {
			t.Errorf("%q", action)
		}
	}
}
