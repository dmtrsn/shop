//go:build integration

package integration_test

import (
	"errors"
	"sweetshop/internal/domain"
	"testing"
)

func TestPostgresCatalog(t *testing.T) {
	d := newDatabase(t, true)
	for _, alias := range []string{"p01", "dark-chocolate-sea-salt", "cha-701"} {
		p, err := d.service.Product(d.ctx, alias)
		if err != nil || p.ID != "p01" || p.PriceMinor != 48900 {
			t.Fatalf("product alias %s: %+v %v", alias, p, err)
		}
	}
	yes, no := true, false
	for _, tc := range []struct {
		name  string
		f     domain.ProductFilter
		total int
	}{
		{"all", domain.ProductFilter{}, 24},
		{"parent and children", domain.ProductFilter{Category: "chocolate"}, 6},
		{"manufacturer slug", domain.ProductFilter{Manufacturer: "chocoatelier"}, 4},
		{"brand", domain.ProductFilter{Brand: "atelier-noir"}, 2},
		{"series", domain.ProductFilter{Series: "atelier origin"}, 2},
		{"collection", domain.ProductFilter{Collection: "new"}, 4},
		{"all tags", domain.ProductFilter{Tags: []string{"без сахара", "веган"}}, 1},
		{"tag case sensitive", domain.ProductFilter{Tags: []string{"ВЕГАН"}}, 0},
		{"available", domain.ProductFilter{InStock: &yes}, 23},
		{"unavailable", domain.ProductFilter{InStock: &no}, 1},
		{"sale", domain.ProductFilter{OnSale: true}, 6},
		{"new", domain.ProductFilter{NewOnly: true}, 4},
		{"sku search", domain.ProductFilter{Query: "CHA-701"}, 1},
		{"brand search", domain.ProductFilter{Query: "Atelier Noir"}, 2},
		{"exact minor price", domain.ProductFilter{MinPriceMinor: 48900, MaxPriceMinor: 48900}, 1},
		{"empty", domain.ProductFilter{Query: "no-such-product-xyz"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := d.service.Catalog(d.ctx, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if p.Total != tc.total || p.Items == nil || p.Page != 1 || p.Limit != 24 {
				t.Fatalf("page=%+v", p)
			}
		})
	}
	for _, sort := range []string{"popular", "new", "price_asc", "price_desc", "rating", "name"} {
		p, err := d.service.Catalog(d.ctx, domain.ProductFilter{Sort: sort, Limit: 3, Page: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Items) != 3 || p.TotalPages != 8 || p.Page != 2 {
			t.Fatalf("pagination: %+v", p)
		}
		if sort == "price_asc" && p.Items[0].PriceMinor > p.Items[1].PriceMinor {
			t.Fatal("ascending sort")
		}
	}
	facets, err := d.service.Facets(d.ctx, domain.ProductFilter{Brand: "atelier-noir"})
	if err != nil {
		t.Fatal(err)
	}
	if len(facets.Brands) != 1 || facets.Brands[0].Count != 2 || facets.MinPriceMinor != 48900 || facets.MaxPriceMinor != 84900 || len(facets.Series) != 1 {
		t.Fatalf("self-filtered facets: %+v", facets)
	}
	categories, err := d.service.Categories(d.ctx)
	if err != nil || len(categories) != 10 {
		t.Fatalf("categories: %v", err)
	}
	manufacturers, err := d.service.Manufacturers(d.ctx)
	if err != nil || len(manufacturers) != 8 {
		t.Fatalf("manufacturers: %v", err)
	}
	brands, err := d.service.Brands(d.ctx)
	if err != nil || len(brands) != 11 {
		t.Fatalf("zero-product brand must be hidden: %v", err)
	}
	collections, err := d.service.Collections(d.ctx)
	if err != nil || len(collections) != 4 {
		t.Fatalf("collections: %v", err)
	}
	d.exec(t, "UPDATE collections SET ends_at='2000-01-01' WHERE id='col-new'")
	collections, err = d.service.Collections(d.ctx)
	if err != nil || len(collections) != 3 {
		t.Fatalf("collection schedule: %v", err)
	}
	page, err := d.service.Catalog(d.ctx, domain.ProductFilter{Collection: "new"})
	if err != nil || page.Total != 4 {
		t.Fatalf("direct collection filter currently ignores schedule: %v", err)
	}
	suggestions, err := d.service.Suggestions(d.ctx, "CHA-701")
	if err != nil || len(suggestions) != 1 || suggestions[0].ID != "p01" {
		t.Fatalf("suggestions: %v", err)
	}
	empty, err := d.service.Suggestions(d.ctx, "x")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("short query must return empty array")
	}
}

func TestCartAndFavoritesMutations(t *testing.T) {
	d := newDatabase(t, true)
	d.customer(t, "other")
	if c := d.cart(t, "other"); c.Items == nil || c.Groups == nil || c.Summary.PositionCount != 0 || !c.UpdatedAt.IsZero() {
		t.Fatal("empty cart contract")
	}
	d.set(t, "other", "cha-701", 2, true)
	c := d.set(t, "other", "dark-chocolate-sea-salt", 3, false)
	if quantity(c, "p01") != 3 || c.Summary.SelectedCount != 0 {
		t.Fatal("POST is absolute assignment")
	}
	if _, err := d.service.SetCartItem(d.ctx, "other", "p01", 25, true); !errors.Is(err, domain.ErrOutOfStock) {
		t.Fatalf("stock: %v", err)
	}
	// PostgreSQL bulk selection accepts canonical IDs only; memory also resolves aliases.
	c, err := d.service.SetCartSelection(d.ctx, "other", []string{"cha-701"}, true)
	if err != nil || c.Summary.SelectedCount != 0 {
		t.Fatalf("canonical-only selection: %v", err)
	}
	c, err = d.service.SetCartSelection(d.ctx, "other", nil, true)
	if err != nil || c.Summary.SelectedCount != 3 {
		t.Fatalf("empty IDs select all: %v", err)
	}
	for i := 0; i < 2; i++ {
		f, err := d.service.SetFavorite(d.ctx, "other", "cha-701", true)
		if err != nil || f.Total != 1 {
			t.Fatalf("favorite idempotence: %v", err)
		}
	}
	f, err := d.service.SetFavorite(d.ctx, "other", "p01", false)
	if err != nil || f.Total != 0 || f.Items == nil || f.Groups == nil {
		t.Fatalf("empty favorites: %v", err)
	}
	c, err = d.service.RemoveCartItem(d.ctx, "other", "cha-701")
	if err != nil || len(c.Items) != 0 {
		t.Fatalf("remove alias: %v", err)
	}
	d.set(t, "other", "p01", 1, true)
	d.set(t, "other", "p02", 1, false)
	c, err = d.service.ClearCart(d.ctx, "other", true)
	if err != nil || len(c.Items) != 1 || quantity(c, "p02") != 1 {
		t.Fatalf("selected deletion: %v", err)
	}
	if quantity(d.cart(t, "usr-demo"), "p01") != 2 {
		t.Fatal("cross-user cart change")
	}
}
