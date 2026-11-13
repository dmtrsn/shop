//go:build integration

package integration_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"sweetshop/internal/domain"
	"sweetshop/internal/repository/postgres"
)

// Frozen P3.1 predicate: keep this independent of the production query builder.
// In particular, dictionary archives, description tokenization and LIKE wildcards
// deliberately retain their existing customer-search behavior.
const oldSearchFrom = ` FROM products p
 JOIN categories c ON c.id=p.category_id
 JOIN manufacturers m ON m.id=p.manufacturer_id
 JOIN brands b ON b.id=p.brand_id
 WHERE p.archived_at IS NULL AND (
 p.search_vector @@ plainto_tsquery('simple',$1) OR p.tags @> $3::text[]
 OR lower(p.name) LIKE $2 OR lower(p.short_name) LIKE $2
 OR lower(p.sku) LIKE $2 OR lower(p.series) LIKE $2
 OR p.manufacturer_id IN (SELECT sm.id FROM manufacturers sm WHERE lower(sm.name) LIKE $2)
 OR p.brand_id IN (SELECT sb.id FROM brands sb WHERE lower(sb.name) LIKE $2)
 OR p.category_id IN (SELECT sc.id FROM categories sc WHERE lower(sc.name) LIKE $2))`

func oldSearchArgs(query string) []any {
	return []any{query, "%" + strings.ToLower(query) + "%", []string{strings.ToLower(query)}}
}

func (d *database) oldSearchIDs(t *testing.T, query, order string) []string {
	t.Helper()
	rows, err := d.pool.Query(d.ctx, "SELECT p.id"+oldSearchFrom+" ORDER BY "+order, oldSearchArgs(query)...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func (d *database) oldSearchFacets(t *testing.T, query string) domain.CatalogFacets {
	t.Helper()
	f := domain.CatalogFacets{}
	args := oldSearchArgs(query)
	if err := d.pool.QueryRow(d.ctx, "SELECT COALESCE(min(p.price_minor),0),COALESCE(max(p.price_minor),0)"+oldSearchFrom, args...).Scan(&f.MinPriceMinor, &f.MaxPriceMinor); err != nil {
		t.Fatal(err)
	}
	for _, facet := range []struct {
		selectSQL, tail string
		target          *[]domain.FacetValue
	}{
		{"c.id,c.slug,c.name", " GROUP BY c.id,c.slug,c.name ORDER BY c.name", &f.Categories},
		{"m.id,m.slug,m.name", " GROUP BY m.id,m.slug,m.name ORDER BY m.name", &f.Manufacturers},
		{"b.id,b.slug,b.name", " GROUP BY b.id,b.slug,b.name ORDER BY b.name", &f.Brands},
		{"p.series,p.series,p.series", " AND p.series<>'' GROUP BY p.series ORDER BY p.series", &f.Series},
		{"tag.name,tag.name,tag.name", " GROUP BY tag.name ORDER BY tag.name", &f.Tags},
	} {
		from := oldSearchFrom
		if facet.target == &f.Tags {
			from = strings.Replace(from, " WHERE p.archived_at", " CROSS JOIN LATERAL unnest(p.tags) AS tag(name) WHERE p.archived_at", 1)
		}
		rows, err := d.pool.Query(d.ctx, "SELECT "+facet.selectSQL+",count(*)"+from+facet.tail, args...)
		if err != nil {
			t.Fatal(err)
		}
		*facet.target = []domain.FacetValue{}
		for rows.Next() {
			var v domain.FacetValue
			if err := rows.Scan(&v.ID, &v.Slug, &v.Name, &v.Count); err != nil {
				t.Fatal(err)
			}
			*facet.target = append(*facet.target, v)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (d *database) oldSearchSuggestions(t *testing.T, query string, limit int, products map[string]domain.Product) []domain.SearchSuggestion {
	t.Helper()
	ids := d.oldSearchIDs(t, query, "p.featured DESC,p.reviews_count DESC,p.rating DESC,p.id ASC")
	result := []domain.SearchSuggestion{}
	for _, id := range ids[:min(len(ids), limit)] {
		p := products[id]
		s := domain.SearchSuggestion{Type: "product", ID: id, Slug: p.Slug, Title: p.Name, Subtitle: p.Manufacturer.Name + " · " + p.Unit, Image: p.Image}
		if p.ManagedMedia != nil && p.ManagedMedia.Primary != nil {
			s.Thumbnail = p.ManagedMedia.Primary.ThumbnailURL
		}
		result = append(result, s)
	}
	if len(result) == limit {
		return result
	}
	rows, err := d.pool.Query(d.ctx, `SELECT kind,id,slug,name,subtitle FROM (
 SELECT 'manufacturer' kind,id,slug,name,country subtitle FROM manufacturers WHERE archived_at IS NULL
 UNION ALL SELECT 'brand',b.id,b.slug,b.name,m.name FROM brands b JOIN manufacturers m ON m.id=b.manufacturer_id WHERE b.archived_at IS NULL
 UNION ALL SELECT 'category',id,slug,name,'' FROM categories WHERE archived_at IS NULL) x
 WHERE lower(name) LIKE $1 ORDER BY name LIMIT $2`, "%"+strings.ToLower(query)+"%", limit-len(result))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s domain.SearchSuggestion
		if err := rows.Scan(&s.Type, &s.ID, &s.Slug, &s.Title, &s.Subtitle); err != nil {
			t.Fatal(err)
		}
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCustomerSearchSemanticEquivalence(t *testing.T) {
	d := newDatabase(t, false)
	if err := (postgres.Migrator{Directory: "../../migrations", Fixtures: postgres.FixtureSkip}).Migrate(d.ctx, d.pool); err != nil {
		t.Fatal(err)
	}
	d.exec(t, `
 INSERT INTO categories(id,slug,name,archived_at) VALUES
 ('c0','plain','Plain category',NULL),('c1','category','Categoryneedle',NULL),
 ('c2','hidden','Hiddenneedle',now()),('c3','shared','Sharedneedle',NULL),('c4','duplicate','Sharedneedle',NULL),
 ('c5','dictionary-only','Dictionaryonly',NULL);
 INSERT INTO manufacturers(id,slug,name,archived_at) VALUES
 ('m0','plain','Plain maker',NULL),('m1','maker','Makerneedle',NULL),
 ('m2','hidden','Hiddenneedle',now()),('m3','shared','Sharedneedle',NULL),('m4','duplicate','Sharedneedle',NULL);
 INSERT INTO brands(id,slug,name,manufacturer_id,archived_at) VALUES
 ('b0','plain','Plain brand','m0',NULL),('b1','brand','Brandneedle','m0',NULL),
 ('bm1','maker-only','Neutral maker brand','m1',NULL),
 ('b2','hidden','Hiddenneedle','m2',now()),('b3','shared','Sharedneedle','m3',NULL),('b4','duplicate','Sharedneedle','m4',NULL);
 INSERT INTO products(id,slug,sku,name,price_minor,unit,category_id,manufacturer_id,brand_id,image,gallery,created_at)
 SELECT 's'||lpad(i::text,2,'0'),'search-'||i,'BASE-'||i,'Neutral '||i,100+(i%3),'piece','c0','m0','b0',
 '/assets/legacy.png',ARRAY['/assets/gallery.png'],'2025-01-01'::timestamptz FROM generate_series(1,22) i;
 UPDATE products SET name='NameNeedle' WHERE id IN ('s01','s07');
 UPDATE products SET description='Desconly' WHERE id='s02';
 UPDATE products SET manufacturer_id='m1',brand_id='bm1' WHERE id='s03';
 UPDATE products SET brand_id='b1' WHERE id='s04';
 UPDATE products SET category_id='c1' WHERE id='s05';
 UPDATE products SET name='Sharedneedle',manufacturer_id='m3',brand_id='b3',category_id='c3',tags=ARRAY['sharedneedle'] WHERE id='s06';
 UPDATE products SET archived_at=now() WHERE id='s07';
 UPDATE products SET manufacturer_id='m2',brand_id='b2',category_id='c2' WHERE id='s08';
 UPDATE products SET name='',sku='',short_name='',description='',series='',tags='{}',legacy_media_retired_at=now() WHERE id='s09';
 UPDATE products SET name='MiXeDcAsE tart' WHERE id='s10';
 UPDATE products SET name='Partialcomponent' WHERE id='s11';
 UPDATE products SET name='Literal% mark' WHERE id='s12';
 UPDATE products SET name='Literal_ mark' WHERE id='s13';
 UPDATE products SET name='Sharedneedle',manufacturer_id='m4',brand_id='b4',category_id='c4',price_minor=100 WHERE id='s14';
 UPDATE products SET short_name='Shortneedle' WHERE id='s15';
 UPDATE products SET sku='SKU-needle' WHERE id='s16';
 UPDATE products SET series='Seriesneedle' WHERE id='s17';
 UPDATE products SET tags=ARRAY['tagneedle','CaseTag'] WHERE id='s18';
 UPDATE products SET description='tokenblue tokensky' WHERE id='s19';
 UPDATE products SET description='Desconlyish' WHERE id='s21';
 UPDATE products SET name='Шоколад' WHERE id='s22';
 INSERT INTO collections(id,slug,name) VALUES('co','collection','Collectionneedle');
 INSERT INTO collection_products(collection_id,product_id) VALUES('co','s20');`)
	staff := d.auditStaff(t, "search-equivalence@example.com")
	large, small := d.uploadLarge(t, staff), d.uploadMedia(t, staff)
	d.attachPrimary(t, staff, "s06", large.ID, small.ID)
	d.attachPrimary(t, staff, "s14", small.ID)

	products := map[string]domain.Product{}
	allIDs := []string{}
	for i := 1; i <= 22; i++ {
		if i == 7 {
			continue
		}
		id := fmt.Sprintf("s%02d", i)
		p, err := d.store.Product(d.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		products[id] = p
		allIDs = append(allIDs, id)
	}
	// These IDs were asserted against P3.1 before the production rewrite.
	cases := []struct {
		query string
		ids   []string
	}{
		{"nameneedle", []string{"s01"}}, {"desconly", []string{"s02"}}, {"descon", []string{}},
		{"makerneedle", []string{"s03"}}, {"brandneedle", []string{"s04"}}, {"categoryneedle", []string{"s05"}},
		{"sharedneedle", []string{"s06", "s14"}}, {"hiddenneedle", []string{"s08"}},
		{"MIXEDcase", []string{"s10"}}, {"component", []string{"s11"}},
		{"shortneedle", []string{"s15"}}, {"SKU-needle", []string{"s16"}}, {"seriesneedle", []string{"s17"}},
		{"tagneedle", []string{"s18"}}, {"tagnee", []string{}}, {"casetag", []string{}},
		{"tokenblue tokensky", []string{"s19"}}, {"collectionneedle", []string{}},
		{"шОкОлАд", []string{"s22"}}, {"dictionaryonly", []string{}}, {"missingneedle", []string{}},
		{"%", allIDs}, {"_", allIDs}, {"%needle%", []string{"s01", "s03", "s04", "s05", "s06", "s08", "s14", "s15", "s16", "s17"}},
	}
	sorts := []struct{ name, order string }{
		{"", "p.featured DESC,p.reviews_count DESC,p.rating DESC,p.id ASC"},
		{"popular", "p.featured DESC,p.reviews_count DESC,p.rating DESC,p.id ASC"},
		{"name", "p.name ASC,p.id ASC"}, {"new", "p.created_at DESC,p.id ASC"},
		{"price_asc", "p.price_minor ASC,p.name ASC,p.id ASC"}, {"price_desc", "p.price_minor DESC,p.name ASC,p.id ASC"},
		{"rating", "p.rating DESC,p.reviews_count DESC,p.id ASC"},
	}
	store, counter := d.tracedCatalog(t)
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			if ids := d.oldSearchIDs(t, tc.query, "p.id ASC"); !reflect.DeepEqual(ids, tc.ids) {
				t.Fatalf("P3.1 oracle IDs=%v, want %v", ids, tc.ids)
			}
			for _, sort := range sorts {
				ids := d.oldSearchIDs(t, tc.query, sort.order)
				for pageNumber := 1; pageNumber <= (len(ids)+1)/2+1; pageNumber++ {
					start := min((pageNumber-1)*2, len(ids))
					want := []domain.Product{}
					for _, id := range ids[start:min(start+2, len(ids))] {
						want = append(want, products[id])
					}
					counter.queries.Store(0)
					page, err := store.ListProducts(d.ctx, domain.ProductFilter{Query: tc.query, Sort: sort.name, Page: pageNumber, Limit: 2})
					queries := int64(1)
					if len(want) > 0 {
						queries = 2
					}
					if err != nil || page.Total != len(ids) || page.TotalPages != (len(ids)+1)/2 || page.Page != pageNumber || page.Limit != 2 || !reflect.DeepEqual(page.Items, want) || counter.queries.Load() != queries {
						t.Fatalf("sort=%q page=%d got=%+v want=%+v err=%v queries=%d", sort.name, pageNumber, page, want, err, counter.queries.Load())
					}
				}
			}
			wantFacets := d.oldSearchFacets(t, tc.query)
			counter.queries.Store(0)
			facets, err := store.CatalogFacets(d.ctx, domain.ProductFilter{Query: tc.query})
			if err != nil || !reflect.DeepEqual(facets, wantFacets) || counter.queries.Load() != 6 {
				t.Fatalf("facets=%+v want=%+v err=%v queries=%d", facets, wantFacets, err, counter.queries.Load())
			}
			for _, limit := range []int{1, 8, 30} {
				want := d.oldSearchSuggestions(t, tc.query, limit, products)
				counter.queries.Store(0)
				got, err := store.Suggestions(d.ctx, tc.query, limit)
				queries := int64(1)
				if len(tc.ids) < limit {
					queries = 2
				}
				if err != nil || !reflect.DeepEqual(got, want) || counter.queries.Load() != queries {
					t.Fatalf("limit=%d suggestions=%+v want=%+v err=%v queries=%d", limit, got, want, err, counter.queries.Load())
				}
			}
		})
	}
}
