package postgres

import (
	"strings"
	"testing"

	"sweetshop/internal/domain"
)

func TestProductWhereUsesArgumentsForUserInput(t *testing.T) {
	attack := "x' OR true --"
	where, args := productWhere(domain.ProductFilter{Query: attack, Category: attack, Manufacturer: attack, Series: attack, Tags: []string{attack}, MinPriceMinor: 100, MaxPriceMinor: 200})
	if strings.Contains(where, attack) {
		t.Fatalf("user input interpolated into SQL: %s", where)
	}
	if len(args) != 9 {
		t.Fatalf("args = %d (%#v)", len(args), args)
	}
	if !strings.Contains(where, "p.tags @>") {
		t.Fatalf("catalog query does not use the indexed tag search: %s", where)
	}
}

func TestProductWhereAppliesInclusiveCollectionSchedule(t *testing.T) {
	where, args := productWhere(domain.ProductFilter{Collection: "seasonal"})
	if len(args) != 1 || args[0] != "seasonal" {
		t.Fatalf("args=%#v", args)
	}
	for _, predicate := range []string{
		"co.starts_at IS NULL OR co.starts_at<=now()",
		"co.ends_at IS NULL OR co.ends_at>=now()",
	} {
		if !strings.Contains(where, predicate) {
			t.Fatalf("missing collection schedule predicate %q in %s", predicate, where)
		}
	}
}

func TestSortSQLIsAllowlisted(t *testing.T) {
	if got := sortSQL("price_asc; DROP TABLE products"); strings.Contains(got, "DROP") {
		t.Fatalf("unsafe sort SQL: %s", got)
	}
}

func TestSortSQLEndsWithUniqueTieBreaker(t *testing.T) {
	for _, sortBy := range []string{"", "popular", "new", "price_asc", "price_desc", "rating", "name", "unknown"} {
		if got := sortSQL(sortBy); !strings.HasSuffix(got, ",p.id ASC") {
			t.Fatalf("sort %q lacks a unique tie-breaker: %s", sortBy, got)
		}
	}
}
