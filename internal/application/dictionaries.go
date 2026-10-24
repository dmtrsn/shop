package application

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"sweetshop/internal/domain"
)

// Staff dictionary actions (ADR 018). Reads are authorized as
// dictionary.read; each mutation is authorized and audited under its own
// action, with the dictionary row as the audit target.
const DictionaryRead = "dictionary.read"

const (
	CategoryCreate      = domain.AuditAction("category.create")
	CategoryUpdate      = domain.AuditAction("category.update")
	CategoryArchive     = domain.AuditAction("category.archive")
	CategoryRestore     = domain.AuditAction("category.restore")
	ManufacturerCreate  = domain.AuditAction("manufacturer.create")
	ManufacturerUpdate  = domain.AuditAction("manufacturer.update")
	ManufacturerArchive = domain.AuditAction("manufacturer.archive")
	ManufacturerRestore = domain.AuditAction("manufacturer.restore")
	BrandCreate         = domain.AuditAction("brand.create")
	BrandUpdate         = domain.AuditAction("brand.update")
	BrandArchive        = domain.AuditAction("brand.archive")
	BrandRestore        = domain.AuditAction("brand.restore")
	CollectionCreate    = domain.AuditAction("collection.create")
	CollectionUpdate    = domain.AuditAction("collection.update")
	CollectionArchive   = domain.AuditAction("collection.archive")
	CollectionRestore   = domain.AuditAction("collection.restore")
)

// CollectionAccents are the tints the storefront knows; others render neutral.
var CollectionAccents = []string{"lime", "cocoa", "coral", "sky"}

// DictionaryStore is the staff dictionary port; only PostgreSQL implements
// it. Every mutation is one audited staff transaction that locks the row,
// requires the expected revision (ErrStaleRevision), and increments it once;
// archive refusals are *ReferenceConflictError, tree refusals
// ErrInvalidHierarchy, unknown references and used slugs ValidationError, a
// repeated archive or restore ErrConflict.
type DictionaryStore interface {
	AdminCategories(ctx context.Context, status domain.DictionaryStatus) ([]domain.AdminCategory, error)
	AdminCategory(ctx context.Context, id string) (domain.AdminCategory, error)
	CreateCategory(ctx context.Context, mutation domain.StaffMutation, id string, input domain.CategoryInput) (domain.AdminCategory, error)
	UpdateCategory(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, input domain.CategoryInput) (domain.AdminCategory, error)
	SetCategoryArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminCategory, error)

	AdminManufacturers(ctx context.Context, status domain.DictionaryStatus) ([]domain.AdminManufacturer, error)
	AdminManufacturer(ctx context.Context, id string) (domain.AdminManufacturer, error)
	CreateManufacturer(ctx context.Context, mutation domain.StaffMutation, id string, input domain.ManufacturerInput) (domain.AdminManufacturer, error)
	UpdateManufacturer(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, input domain.ManufacturerInput) (domain.AdminManufacturer, error)
	SetManufacturerArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminManufacturer, error)

	AdminBrands(ctx context.Context, status domain.DictionaryStatus) ([]domain.AdminBrand, error)
	AdminBrand(ctx context.Context, id string) (domain.AdminBrand, error)
	CreateBrand(ctx context.Context, mutation domain.StaffMutation, id string, input domain.BrandInput) (domain.AdminBrand, error)
	UpdateBrand(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, input domain.BrandInput) (domain.AdminBrand, error)
	SetBrandArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminBrand, error)

	AdminCollections(ctx context.Context, status domain.DictionaryStatus) ([]domain.AdminCollection, error)
	AdminCollection(ctx context.Context, id string) (domain.AdminCollection, error)
	CreateCollection(ctx context.Context, mutation domain.StaffMutation, id string, input domain.CollectionInput) (domain.AdminCollection, error)
	UpdateCollection(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, input domain.CollectionInput) (domain.AdminCollection, error)
	SetCollectionArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminCollection, error)
}

// DictionaryAdmin is the staff dictionary use case. Each entity keeps its own
// rules; only the revision and archive plumbing is shared.
type DictionaryAdmin struct {
	store DictionaryStore
}

func NewDictionaryAdmin(store DictionaryStore) *DictionaryAdmin {
	return &DictionaryAdmin{store: store}
}

func (a *DictionaryAdmin) Categories(ctx context.Context, status domain.DictionaryStatus) ([]domain.AdminCategory, error) {
	return a.store.AdminCategories(ctx, status)
}

func (a *DictionaryAdmin) Category(ctx context.Context, id string) (domain.AdminCategory, error) {
	return a.store.AdminCategory(ctx, id)
}

func (a *DictionaryAdmin) Manufacturers(ctx context.Context, status domain.DictionaryStatus) ([]domain.AdminManufacturer, error) {
	return a.store.AdminManufacturers(ctx, status)
}

func (a *DictionaryAdmin) Manufacturer(ctx context.Context, id string) (domain.AdminManufacturer, error) {
	return a.store.AdminManufacturer(ctx, id)
}

func (a *DictionaryAdmin) Brands(ctx context.Context, status domain.DictionaryStatus) ([]domain.AdminBrand, error) {
	return a.store.AdminBrands(ctx, status)
}

func (a *DictionaryAdmin) Brand(ctx context.Context, id string) (domain.AdminBrand, error) {
	return a.store.AdminBrand(ctx, id)
}

func (a *DictionaryAdmin) Collections(ctx context.Context, status domain.DictionaryStatus) ([]domain.AdminCollection, error) {
	return a.store.AdminCollections(ctx, status)
}

func (a *DictionaryAdmin) Collection(ctx context.Context, id string) (domain.AdminCollection, error) {
	return a.store.AdminCollection(ctx, id)
}

func ParseDictionaryStatus(raw string) (domain.DictionaryStatus, error) {
	switch status := domain.DictionaryStatus(raw); status {
	case "":
		return domain.DictionaryActive, nil
	case domain.DictionaryActive, domain.DictionaryArchived, domain.DictionaryAll:
		return status, nil
	}
	return "", &domain.ValidationError{Fields: []domain.FieldError{{Field: "status", Message: "Допустимо: active, archived, all"}}}
}

// --- shared checks --------------------------------------------------------

type checker struct{ fields []domain.FieldError }

func (c *checker) fail(field, message string) {
	c.fields = append(c.fields, domain.FieldError{Field: field, Message: message})
}

func (c *checker) slug(value string) {
	if !productSlugPattern.MatchString(value) || len(value) > 120 {
		c.fail("slug", "Slug: до 120 символов a–z, 0–9 и дефисов между ними")
	}
}

func (c *checker) line(field, value string, required bool, limit int) {
	switch {
	case required && value == "":
		c.fail(field, "Обязательное поле")
	case utf8.RuneCountInString(value) > limit || !singleLine(value):
		c.fail(field, fmt.Sprintf("До %d символов в одну строку", limit))
	}
}

func (c *checker) text(field, value string, limit int) {
	if utf8.RuneCountInString(value) > limit || !multiLine(value) {
		c.fail(field, fmt.Sprintf("До %d символов без управляющих символов", limit))
	}
}

func (c *checker) sortOrder(value int) {
	if value < 0 || value > 100_000 {
		c.fail("sortOrder", "Порядок: от 0 до 100000")
	}
}

func (c *checker) reference(field, value string, required bool) {
	if value == "" {
		if required {
			c.fail(field, "Обязательное поле")
		}
		return
	}
	if !referencePattern.MatchString(value) || len(value) > 100 {
		c.fail(field, "Укажите существующую запись")
	}
}

func (c *checker) err() error {
	if len(c.fields) > 0 {
		return &domain.ValidationError{Fields: c.fields}
	}
	return nil
}

func trim(values ...*string) {
	for _, value := range values {
		*value = strings.TrimSpace(*value)
	}
}

func ValidateCategory(in domain.CategoryInput) (domain.CategoryInput, error) {
	trim(&in.Slug, &in.Name, &in.Description, &in.ParentID, &in.Icon)
	c := checker{}
	c.slug(in.Slug)
	c.line("name", in.Name, true, 120)
	c.text("description", in.Description, 2000)
	c.reference("parentId", in.ParentID, false)
	if in.Icon != "" && (!productSlugPattern.MatchString(in.Icon) || len(in.Icon) > 40) {
		c.fail("icon", "Иконка: до 40 символов a–z, 0–9 и дефисов")
	}
	c.sortOrder(in.SortOrder)
	return in, c.err()
}

func ValidateManufacturer(in domain.ManufacturerInput) (domain.ManufacturerInput, error) {
	trim(&in.Slug, &in.Name, &in.Country, &in.Description)
	c := checker{}
	c.slug(in.Slug)
	c.line("name", in.Name, true, 120)
	c.line("country", in.Country, false, 60)
	c.text("description", in.Description, 2000)
	c.sortOrder(in.SortOrder)
	return in, c.err()
}

func ValidateBrand(in domain.BrandInput) (domain.BrandInput, error) {
	trim(&in.Slug, &in.Name, &in.ManufacturerID, &in.Description)
	c := checker{}
	c.slug(in.Slug)
	c.line("name", in.Name, true, 120)
	c.reference("manufacturerId", in.ManufacturerID, true)
	c.text("description", in.Description, 2000)
	c.sortOrder(in.SortOrder)
	return in, c.err()
}

func ValidateCollection(in domain.CollectionInput) (domain.CollectionInput, error) {
	trim(&in.Slug, &in.Name, &in.Description, &in.Accent)
	c := checker{}
	c.slug(in.Slug)
	c.line("name", in.Name, true, 120)
	c.text("description", in.Description, 2000)
	known := false
	for _, accent := range CollectionAccents {
		known = known || in.Accent == accent
	}
	if !known {
		c.fail("accent", "Акцент: lime, cocoa, coral или sky")
	}
	if in.StartsAt != nil && in.EndsAt != nil && in.EndsAt.Before(*in.StartsAt) {
		c.fail("endsAt", "Окончание не раньше начала")
	}
	c.sortOrder(in.SortOrder)
	return in, c.err()
}

func checkRevision(expectedRevision int64) error {
	if fields := revisionFields(expectedRevision); len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}
	return nil
}

// --- categories -----------------------------------------------------------

func (a *DictionaryAdmin) CreateCategory(ctx context.Context, mutation domain.StaffMutation, in domain.CategoryInput) (domain.AdminCategory, error) {
	if err := requireAction(mutation, CategoryCreate); err != nil {
		return domain.AdminCategory{}, err
	}
	in, err := ValidateCategory(in)
	if err != nil {
		return domain.AdminCategory{}, err
	}
	id, err := newAccountID("cat_")
	if err != nil {
		return domain.AdminCategory{}, err
	}
	return a.store.CreateCategory(ctx, mutation, id, in)
}

func (a *DictionaryAdmin) UpdateCategory(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, in domain.CategoryInput) (domain.AdminCategory, error) {
	if err := requireAction(mutation, CategoryUpdate); err != nil {
		return domain.AdminCategory{}, err
	}
	if err := checkRevision(expectedRevision); err != nil {
		return domain.AdminCategory{}, err
	}
	in, err := ValidateCategory(in)
	if err != nil {
		return domain.AdminCategory{}, err
	}
	if in.ParentID == id {
		return domain.AdminCategory{}, domain.ErrInvalidHierarchy
	}
	return a.store.UpdateCategory(ctx, mutation, id, expectedRevision, in)
}

func (a *DictionaryAdmin) SetCategoryArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminCategory, error) {
	if err := requireAction(mutation, archiveAction(archived, CategoryArchive, CategoryRestore)); err != nil {
		return domain.AdminCategory{}, err
	}
	if err := checkRevision(expectedRevision); err != nil {
		return domain.AdminCategory{}, err
	}
	return a.store.SetCategoryArchived(ctx, mutation, id, expectedRevision, archived)
}

// --- manufacturers --------------------------------------------------------

func (a *DictionaryAdmin) CreateManufacturer(ctx context.Context, mutation domain.StaffMutation, in domain.ManufacturerInput) (domain.AdminManufacturer, error) {
	if err := requireAction(mutation, ManufacturerCreate); err != nil {
		return domain.AdminManufacturer{}, err
	}
	in, err := ValidateManufacturer(in)
	if err != nil {
		return domain.AdminManufacturer{}, err
	}
	id, err := newAccountID("mfr_")
	if err != nil {
		return domain.AdminManufacturer{}, err
	}
	return a.store.CreateManufacturer(ctx, mutation, id, in)
}

func (a *DictionaryAdmin) UpdateManufacturer(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, in domain.ManufacturerInput) (domain.AdminManufacturer, error) {
	if err := requireAction(mutation, ManufacturerUpdate); err != nil {
		return domain.AdminManufacturer{}, err
	}
	if err := checkRevision(expectedRevision); err != nil {
		return domain.AdminManufacturer{}, err
	}
	in, err := ValidateManufacturer(in)
	if err != nil {
		return domain.AdminManufacturer{}, err
	}
	return a.store.UpdateManufacturer(ctx, mutation, id, expectedRevision, in)
}

func (a *DictionaryAdmin) SetManufacturerArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminManufacturer, error) {
	if err := requireAction(mutation, archiveAction(archived, ManufacturerArchive, ManufacturerRestore)); err != nil {
		return domain.AdminManufacturer{}, err
	}
	if err := checkRevision(expectedRevision); err != nil {
		return domain.AdminManufacturer{}, err
	}
	return a.store.SetManufacturerArchived(ctx, mutation, id, expectedRevision, archived)
}

// --- brands ---------------------------------------------------------------

func (a *DictionaryAdmin) CreateBrand(ctx context.Context, mutation domain.StaffMutation, in domain.BrandInput) (domain.AdminBrand, error) {
	if err := requireAction(mutation, BrandCreate); err != nil {
		return domain.AdminBrand{}, err
	}
	in, err := ValidateBrand(in)
	if err != nil {
		return domain.AdminBrand{}, err
	}
	id, err := newAccountID("brd_")
	if err != nil {
		return domain.AdminBrand{}, err
	}
	return a.store.CreateBrand(ctx, mutation, id, in)
}

func (a *DictionaryAdmin) UpdateBrand(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, in domain.BrandInput) (domain.AdminBrand, error) {
	if err := requireAction(mutation, BrandUpdate); err != nil {
		return domain.AdminBrand{}, err
	}
	if err := checkRevision(expectedRevision); err != nil {
		return domain.AdminBrand{}, err
	}
	in, err := ValidateBrand(in)
	if err != nil {
		return domain.AdminBrand{}, err
	}
	return a.store.UpdateBrand(ctx, mutation, id, expectedRevision, in)
}

func (a *DictionaryAdmin) SetBrandArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminBrand, error) {
	if err := requireAction(mutation, archiveAction(archived, BrandArchive, BrandRestore)); err != nil {
		return domain.AdminBrand{}, err
	}
	if err := checkRevision(expectedRevision); err != nil {
		return domain.AdminBrand{}, err
	}
	return a.store.SetBrandArchived(ctx, mutation, id, expectedRevision, archived)
}

// --- collections ----------------------------------------------------------

func (a *DictionaryAdmin) CreateCollection(ctx context.Context, mutation domain.StaffMutation, in domain.CollectionInput) (domain.AdminCollection, error) {
	if err := requireAction(mutation, CollectionCreate); err != nil {
		return domain.AdminCollection{}, err
	}
	in, err := ValidateCollection(in)
	if err != nil {
		return domain.AdminCollection{}, err
	}
	id, err := newAccountID("col_")
	if err != nil {
		return domain.AdminCollection{}, err
	}
	return a.store.CreateCollection(ctx, mutation, id, in)
}

func (a *DictionaryAdmin) UpdateCollection(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, in domain.CollectionInput) (domain.AdminCollection, error) {
	if err := requireAction(mutation, CollectionUpdate); err != nil {
		return domain.AdminCollection{}, err
	}
	if err := checkRevision(expectedRevision); err != nil {
		return domain.AdminCollection{}, err
	}
	in, err := ValidateCollection(in)
	if err != nil {
		return domain.AdminCollection{}, err
	}
	return a.store.UpdateCollection(ctx, mutation, id, expectedRevision, in)
}

func (a *DictionaryAdmin) SetCollectionArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminCollection, error) {
	if err := requireAction(mutation, archiveAction(archived, CollectionArchive, CollectionRestore)); err != nil {
		return domain.AdminCollection{}, err
	}
	if err := checkRevision(expectedRevision); err != nil {
		return domain.AdminCollection{}, err
	}
	return a.store.SetCollectionArchived(ctx, mutation, id, expectedRevision, archived)
}

func archiveAction(archived bool, archive, restore domain.AuditAction) domain.AuditAction {
	if archived {
		return archive
	}
	return restore
}
