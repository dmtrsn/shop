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

func TestSortSQLIsAllowlisted(t *testing.T) {
	if got := sortSQL("price_asc; DROP TABLE products"); strings.Contains(got, "DROP") {
		t.Fatalf("unsafe sort SQL: %s", got)
	}
}
