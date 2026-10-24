package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"sweetshop/internal/domain"
)

// fakeDictionaryStore embeds the port, so a call this test does not expect panics.
type fakeDictionaryStore struct {
	DictionaryStore
	categories []domain.CategoryInput
	archived   []bool
}

func (f *fakeDictionaryStore) CreateCategory(_ context.Context, _ domain.StaffMutation, id string, in domain.CategoryInput) (domain.AdminCategory, error) {
	f.categories = append(f.categories, in)
	return domain.AdminCategory{DictionaryState: domain.DictionaryState{ID: id}}, nil
}

func (f *fakeDictionaryStore) UpdateCategory(_ context.Context, _ domain.StaffMutation, id string, _ int64, in domain.CategoryInput) (domain.AdminCategory, error) {
	f.categories = append(f.categories, in)
	return domain.AdminCategory{DictionaryState: domain.DictionaryState{ID: id}}, nil
}

func (f *fakeDictionaryStore) SetBrandArchived(_ context.Context, _ domain.StaffMutation, id string, _ int64, archived bool) (domain.AdminBrand, error) {
	f.archived = append(f.archived, archived)
	return domain.AdminBrand{DictionaryState: domain.DictionaryState{ID: id, Archived: archived}}, nil
}

func TestDictionaryAdminMutationsNeedTheirOwnAction(t *testing.T) {
	store := &fakeDictionaryStore{}
	admin := NewDictionaryAdmin(store)
	ctx := context.Background()
	category := domain.CategoryInput{Slug: "jams", Name: "Джемы"}
	if _, err := admin.CreateCategory(ctx, staffMutationFor(t, CategoryUpdate), category); err == nil {
		t.Fatal("an update mutation created a category")
	}
	if _, err := admin.SetBrandArchived(ctx, staffMutationFor(t, BrandRestore), "brd-1", 1, true); err == nil {
		t.Fatal("a restore mutation archived a brand")
	}
	if _, err := admin.SetBrandArchived(ctx, staffMutationFor(t, BrandArchive), "brd-1", 1, false); err == nil {
		t.Fatal("an archive mutation restored a brand")
	}
	if _, err := admin.SetBrandArchived(ctx, domain.StaffMutation{}, "brd-1", 1, true); err == nil {
		t.Fatal("an unauthorized mutation reached the store")
	}
	if len(store.categories)+len(store.archived) != 0 {
		t.Fatal("a mismatched mutation reached the store")
	}
	if _, err := admin.SetBrandArchived(ctx, staffMutationFor(t, BrandArchive), "brd-1", 0, true); fieldNames(err) == nil {
		t.Fatal("archive without a revision")
	}
	if brand, err := admin.SetBrandArchived(ctx, staffMutationFor(t, BrandArchive), "brd-1", 2, true); err != nil || !brand.Archived {
		t.Fatalf("archive: %+v %v", brand, err)
	}
}

func TestDictionaryAdminCreatesPrefixedIDsAndTrimmedInput(t *testing.T) {
	store := &fakeDictionaryStore{}
	admin := NewDictionaryAdmin(store)
	created, err := admin.CreateCategory(context.Background(), staffMutationFor(t, CategoryCreate),
		domain.CategoryInput{Slug: " jams ", Name: "  Джемы ", Description: " Ягоды\nи фрукты ", ParentID: " cat-sweets ", Icon: " jar "})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.ID, "cat_") || len(created.ID) != 36 {
		t.Fatalf("id %q", created.ID)
	}
	got := store.categories[0]
	if got.Slug != "jams" || got.Name != "Джемы" || got.Description != "Ягоды\nи фрукты" || got.ParentID != "cat-sweets" || got.Icon != "jar" {
		t.Fatalf("%+v", got)
	}
	// A category cannot be its own parent; that is a tree rule, not a field.
	if _, err := admin.UpdateCategory(context.Background(), staffMutationFor(t, CategoryUpdate), "cat-1", 1,
		domain.CategoryInput{Slug: "a", Name: "A", ParentID: "cat-1"}); !errors.Is(err, domain.ErrInvalidHierarchy) {
		t.Fatalf("self parent: %v", err)
	}
}

func TestDictionaryValidation(t *testing.T) {
	long := strings.Repeat("я", 121)
	from := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(-time.Hour)
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"category ok":         {second(ValidateCategory(domain.CategoryInput{Slug: "a-1", Name: "A"})), ""},
		"category slug":       {second(ValidateCategory(domain.CategoryInput{Slug: "A_1", Name: "A"})), "slug"},
		"category name":       {second(ValidateCategory(domain.CategoryInput{Slug: "a", Name: long})), "name"},
		"category newline":    {second(ValidateCategory(domain.CategoryInput{Slug: "a", Name: "A\nB"})), "name"},
		"category icon":       {second(ValidateCategory(domain.CategoryInput{Slug: "a", Name: "A", Icon: "<svg>"})), "icon"},
		"category parent":     {second(ValidateCategory(domain.CategoryInput{Slug: "a", Name: "A", ParentID: "cat 1"})), "parentId"},
		"category sort":       {second(ValidateCategory(domain.CategoryInput{Slug: "a", Name: "A", SortOrder: 100_001})), "sortOrder"},
		"manufacturer empty":  {second(ValidateManufacturer(domain.ManufacturerInput{})), "slug,name"},
		"manufacturer ok":     {second(ValidateManufacturer(domain.ManufacturerInput{Slug: "a", Name: "A", Country: "Россия"})), ""},
		"brand maker":         {second(ValidateBrand(domain.BrandInput{Slug: "a", Name: "A"})), "manufacturerId"},
		"collection accent":   {second(ValidateCollection(domain.CollectionInput{Slug: "a", Name: "A", Accent: "red"})), "accent"},
		"collection reversed": {second(ValidateCollection(domain.CollectionInput{Slug: "a", Name: "A", Accent: "lime", StartsAt: &from, EndsAt: &to})), "endsAt"},
		"collection open":     {second(ValidateCollection(domain.CollectionInput{Slug: "a", Name: "A", Accent: "coral", EndsAt: &to})), ""},
	} {
		if got := strings.Join(fieldNames(tc.err), ","); got != tc.want {
			t.Errorf("%s: fields %q, want %q (%v)", name, got, tc.want, tc.err)
		}
	}
	if _, err := ParseDictionaryStatus("deleted"); fieldNames(err) == nil {
		t.Fatal("unknown status accepted")
	}
	if status, err := ParseDictionaryStatus(""); err != nil || status != domain.DictionaryActive {
		t.Fatalf("default status %q %v", status, err)
	}
}

func second[T any](_ T, err error) error { return err }
