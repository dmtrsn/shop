package memory

import (
	"context"
	"testing"
	"time"

	"sweetshop/internal/domain"
)

func TestCollectionScheduleAppliesToListingAndCatalogFiltering(t *testing.T) {
	store := NewDemo()
	now := time.Now()
	store.collections = []domain.Collection{
		{ID: "open", Slug: "open", ProductIDs: []string{"p01"}, ProductCount: 1},
		{ID: "started", Slug: "started", StartsAt: now.Add(-time.Hour), ProductIDs: []string{"p02"}, ProductCount: 1},
		{ID: "future", Slug: "future", StartsAt: now.Add(time.Hour), ProductIDs: []string{"p03"}, ProductCount: 1},
		{ID: "expired", Slug: "expired", EndsAt: now.Add(-time.Hour), ProductIDs: []string{"p04"}, ProductCount: 1},
	}

	collections, err := store.ListCollections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(collections) != 2 || collections[0].ID != "open" || collections[1].ID != "started" {
		t.Fatalf("visible collections=%+v", collections)
	}

	for _, test := range []struct {
		identity string
		wantID   string
	}{
		{identity: "open", wantID: "p01"},
		{identity: "started", wantID: "p02"},
		{identity: "future"},
		{identity: "expired"},
	} {
		page, err := store.ListProducts(context.Background(), domain.ProductFilter{Collection: test.identity})
		if err != nil {
			t.Fatalf("collection %s: %v", test.identity, err)
		}
		if test.wantID == "" {
			if page.Total != 0 || len(page.Items) != 0 {
				t.Fatalf("inactive collection %s exposed %+v", test.identity, page.Items)
			}
			continue
		}
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != test.wantID {
			t.Fatalf("active collection %s page=%+v", test.identity, page)
		}
	}
}

// The storefront shows productCount next to links into /catalog?category=…,
// so both must use the same category semantics.
func TestCategoryProductCountMatchesUnfilteredCatalogTotal(t *testing.T) {
	store := NewDemo()
	ctx := context.Background()
	categories, err := store.ListCategories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(categories) == 0 {
		t.Fatal("demo categories are missing")
	}
	for _, category := range categories {
		for _, identity := range []string{category.ID, category.Slug} {
			page, err := store.ListProducts(ctx, domain.ProductFilter{Category: identity})
			if err != nil {
				t.Fatalf("category %s: %v", identity, err)
			}
			if page.Total != category.ProductCount {
				t.Fatalf("category %s: productCount=%d catalog total=%d", identity, category.ProductCount, page.Total)
			}
		}
	}
}
