//go:build integration

package integration_test

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/internal/repository/postgres"
)

func (d *database) tracedCatalog(t *testing.T) (*postgres.Store, *queryCounter) {
	t.Helper()
	config := d.pool.Config()
	counter := &queryCounter{}
	config.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(d.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return postgres.New(pool), counter
}

func TestCatalogQueryGuards(t *testing.T) {
	d := newDatabase(t, true)
	store, counter := d.tracedCatalog(t)
	service := application.New(store, application.Options{})
	for _, tc := range []struct {
		name                      string
		filter                    domain.ProductFilter
		total, page, limit, pages int
		items                     int
		queries                   int64
	}{
		{"defaults", domain.ProductFilter{}, 24, 1, 24, 1, 24, 2},
		{"first", domain.ProductFilter{Page: 1, Limit: 10}, 24, 1, 10, 3, 10, 2},
		{"last partial", domain.ProductFilter{Page: 3, Limit: 10}, 24, 3, 10, 3, 4, 2},
		{"past partial", domain.ProductFilter{Page: 4, Limit: 10}, 24, 4, 10, 3, 0, 1},
		{"last full", domain.ProductFilter{Page: 2, Limit: 12}, 24, 2, 12, 2, 12, 2},
		{"past full", domain.ProductFilter{Page: 3, Limit: 12}, 24, 3, 12, 2, 0, 1},
		{"public maximum page", domain.ProductFilter{Page: 1_000_000, Limit: 100}, 24, 1_000_000, 100, 1, 0, 1},
		{"no search matches", domain.ProductFilter{Query: "p31-no-such-product", Page: 7, Limit: 10}, 0, 7, 10, 0, 0, 1},
		{"no filter matches", domain.ProductFilter{MaxPriceMinor: 1, Page: 2, Limit: 10}, 0, 2, 10, 0, 0, 1},
		{"missing category", domain.ProductFilter{Category: "p31-missing-category"}, 0, 1, 24, 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			counter.queries.Store(0)
			page, err := service.Catalog(d.ctx, tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != tc.total || page.Page != tc.page || page.Limit != tc.limit || page.TotalPages != tc.pages || page.Items == nil || len(page.Items) != tc.items {
				t.Fatalf("page=%+v", page)
			}
			// A populated page needs one count and one page statement. An empty
			// page needs just the count, with no page/media query at all.
			if got := counter.queries.Load(); got != tc.queries {
				t.Fatalf("queries=%d, want %d", got, tc.queries)
			}
		})
	}
	// Direct repository callers must also avoid overflowing (page-1)*limit.
	counter.queries.Store(0)
	page, err := store.ListProducts(d.ctx, domain.ProductFilter{Page: math.MaxInt, Limit: 100})
	if err != nil || page.Page != math.MaxInt || page.Total != 24 || page.TotalPages != 1 || page.Items == nil || len(page.Items) != 0 || counter.queries.Load() != 1 {
		t.Fatalf("huge repository page=%+v err=%v queries=%d", page, err, counter.queries.Load())
	}
	// Facets have their own semantics and query path, including empty arrays.
	counter.queries.Store(0)
	facets, err := store.CatalogFacets(d.ctx, domain.ProductFilter{Query: "p31-no-such-product"})
	want := domain.CatalogFacets{Categories: []domain.FacetValue{}, Manufacturers: []domain.FacetValue{}, Brands: []domain.FacetValue{}, Series: []domain.FacetValue{}, Tags: []domain.FacetValue{}}
	if err != nil || !reflect.DeepEqual(facets, want) || counter.queries.Load() != 6 {
		t.Fatalf("facets=%+v err=%v queries=%d", facets, err, counter.queries.Load())
	}
}

func TestSuggestionsSkipCountAndPreserveOrder(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "suggestions@example.com")
	large, small := d.uploadLarge(t, staff), d.uploadMedia(t, staff)
	d.attachPrimary(t, staff, "p20", large.ID, small.ID)
	d.attachPrimary(t, staff, "p06", small.ID)
	products := []domain.SearchSuggestion{}
	for _, id := range []string{"p20", "p01", "p06", "p08"} {
		p, err := d.store.Product(d.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		item := domain.SearchSuggestion{Type: "product", ID: p.ID, Slug: p.Slug, Title: p.Name, Subtitle: p.Manufacturer.Name + " · " + p.Unit, Image: p.Image}
		if id == "p20" {
			item.Thumbnail = large.ThumbnailURL
		}
		products = append(products, item)
	}
	all := append(append([]domain.SearchSuggestion{}, products...),
		domain.SearchSuggestion{Type: "brand", ID: "b-atelier-maison", Slug: "atelier-maison", Title: "Atelier Maison", Subtitle: "Choco Atelier"},
		domain.SearchSuggestion{Type: "brand", ID: "b-atelier-noir", Slug: "atelier-noir", Title: "Atelier Noir", Subtitle: "Choco Atelier"},
		domain.SearchSuggestion{Type: "manufacturer", ID: "m-choco", Slug: "chocoatelier", Title: "Choco Atelier", Subtitle: "Россия"})
	d.exec(t, `INSERT INTO categories(id,slug,name) VALUES('p31-dictionary','p31-dictionary','P31 dictionary')`)
	store, counter := d.tracedCatalog(t)
	for _, tc := range []struct {
		name, query string
		limit       int
		want        []domain.SearchSuggestion
		queries     int64
	}{
		{"full product limit", "Atelier", 3, products[:3], 1},
		{"products then dictionaries", "Atelier", 8, all, 2},
		{"dictionary only", "P31 dictionary", 8, []domain.SearchSuggestion{{Type: "category", ID: "p31-dictionary", Slug: "p31-dictionary", Title: "P31 dictionary"}}, 2},
		{"no matches", "p31-no-such-product", 8, []domain.SearchSuggestion{}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			counter.queries.Store(0)
			got, err := store.Suggestions(d.ctx, tc.query, tc.limit)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("suggestions=%+v want=%+v err=%v", got, tc.want, err)
			}
			// Only the product page and, when needed, dictionary statement.
			// An unused count would add a third (or second) statement.
			if got := counter.queries.Load(); got != tc.queries {
				t.Fatalf("queries=%d, want %d", got, tc.queries)
			}
		})
	}
}

func TestCategoryCountsAggregatePreservesHierarchy(t *testing.T) {
	d := newDatabase(t, true)
	d.exec(t, `UPDATE products SET archived_at=now()`)
	d.exec(t, `INSERT INTO categories(id,slug,name,parent_id,sort_order,archived_at) VALUES
		('p31-root','p31-root','Root',NULL,101,NULL),
		('p31-child','p31-child','Child','p31-root',102,NULL),
		('p31-hidden','p31-hidden','Hidden','p31-root',103,now()),
		('p31-grandchild','p31-grandchild','Grandchild','p31-child',104,NULL),
		('p31-empty','p31-empty','Empty',NULL,105,NULL),
		('p31-other','p31-other','Other',NULL,106,NULL),
		('p31-self','p31-self','Self','p31-self',107,NULL)`)
	for id, category := range map[string]string{"p01": "p31-root", "p02": "p31-child", "p03": "p31-hidden", "p04": "p31-child", "p05": "p31-grandchild", "p06": "p31-other", "p07": "p31-self"} {
		d.exec(t, `UPDATE products SET category_id=$2,archived_at=NULL WHERE id=$1`, id, category)
	}
	d.exec(t, `UPDATE products SET archived_at=now() WHERE id='p04'`)
	// Dictionary archives do not withdraw existing live products from counts.
	d.exec(t, `UPDATE manufacturers SET archived_at=now() WHERE id='m-choco'`)
	d.exec(t, `UPDATE brands SET archived_at=now() WHERE id='b-atelier-noir'`)
	store, counter := d.tracedCatalog(t)
	categories, err := store.ListCategories(d.ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"p31-root": 3, "p31-child": 2, "p31-grandchild": 1, "p31-empty": 0, "p31-other": 1, "p31-self": 1}
	ids := []string{}
	for _, c := range categories {
		if c.ID == "p31-hidden" {
			t.Fatal("archived category returned")
		}
		if count, ok := want[c.ID]; ok {
			if c.ProductCount != count {
				t.Fatalf("%s count=%d, want %d", c.ID, c.ProductCount, count)
			}
			ids = append(ids, c.ID)
		} else if c.ProductCount != 0 {
			t.Fatalf("empty fixture category %s count=%d", c.ID, c.ProductCount)
		}
	}
	if !reflect.DeepEqual(ids, []string{"p31-root", "p31-child", "p31-grandchild", "p31-empty", "p31-other", "p31-self"}) || counter.queries.Load() != 1 {
		t.Fatalf("categories=%v queries=%d", ids, counter.queries.Load())
	}
}

func TestCatalogPagesPreserveOrderAndMedia(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "catalog-pages@example.com")
	a, b, c := d.uploadLarge(t, staff), d.uploadMedia(t, staff), d.uploadMedia(t, staff)
	gallery := d.uploadLarge(t, staff)
	d.attachPrimary(t, staff, "p01", a.ID, c.ID, gallery.ID, b.ID)
	d.attachPrimary(t, staff, "p02", b.ID)
	d.attachPrimary(t, staff, "p04", "", c.ID, b.ID)
	d.exec(t, `UPDATE products SET legacy_media_retired_at=now() WHERE id='p05'`)
	d.exec(t, `UPDATE products SET archived_at=now() WHERE id='p24'`)
	// Ties exercise the ID tie-breaker across page boundaries in every sort.
	d.exec(t, `UPDATE products SET name='P31 tied',price_minor=49000,rating=4.7,reviews_count=90,featured=false,created_at='2025-01-01' WHERE id<='p06'`)
	expected := map[string]domain.Product{}
	for i := 1; i <= 23; i++ {
		id := fmt.Sprintf("p%02d", i)
		p, err := d.store.Product(d.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		expected[id] = p
	}
	if p := expected["p01"]; p.Image != a.URL || !reflect.DeepEqual(p.Gallery, []string{c.URL, gallery.URL, b.URL}) || p.ManagedMedia.Primary.ThumbnailURL != a.ThumbnailURL || p.ManagedMedia.Gallery[1].ThumbnailURL != gallery.ThumbnailURL {
		t.Fatalf("managed ordered gallery=%+v", p)
	}
	if p := expected["p02"]; p.Image != b.URL || len(p.Gallery) != 0 || p.ManagedMedia.Primary.ThumbnailURL != "" {
		t.Fatalf("small managed primary=%+v", p)
	}
	if p := expected["p03"]; p.ManagedMedia != nil || p.Image == "" || len(p.Gallery) == 0 {
		t.Fatalf("legacy fallback=%+v", p)
	}
	if p := expected["p05"]; p.Image != "" || len(p.Gallery) != 0 {
		t.Fatalf("retired legacy=%+v", p)
	}
	store, counter := d.tracedCatalog(t)
	for sortBy, order := range map[string]string{
		"popular": "featured DESC,reviews_count DESC,rating DESC,id ASC",
		"new":     "created_at DESC,id ASC", "name": "name ASC,id ASC",
		"price_asc": "price_minor ASC,name ASC,id ASC", "price_desc": "price_minor DESC,name ASC,id ASC",
		"rating": "rating DESC,reviews_count DESC,id ASC",
	} {
		t.Run(sortBy, func(t *testing.T) {
			// This reference selects IDs without catalog/media projection.
			rows, err := d.pool.Query(d.ctx, "SELECT id FROM products WHERE archived_at IS NULL ORDER BY "+order)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, id)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			for pageNumber := 1; pageNumber <= 5; pageNumber++ {
				counter.queries.Store(0)
				page, err := store.ListProducts(d.ctx, domain.ProductFilter{Page: pageNumber, Limit: 5, Sort: sortBy})
				if err != nil || page.Total != 23 || page.TotalPages != 5 || counter.queries.Load() != 2 {
					t.Fatalf("page=%+v err=%v queries=%d", page, err, counter.queries.Load())
				}
				start := (pageNumber - 1) * 5
				end := min(start+5, len(ids))
				want := []domain.Product{}
				for _, id := range ids[start:end] {
					want = append(want, expected[id])
				}
				if !reflect.DeepEqual(page.Items, want) {
					t.Fatalf("page %d changed product order or fields: got=%+v want=%+v", pageNumber, page.Items, want)
				}
			}
		})
	}
}
