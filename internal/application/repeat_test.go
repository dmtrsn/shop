package application

import (
	"testing"

	"sweetshop/internal/domain"
)

func TestRepeatAdditionRespectsFinalCartMinimumAndStep(t *testing.T) {
	product := domain.Product{MinimumQuantity: 3, QuantityStep: 2}

	tests := []struct {
		name      string
		existing  int
		requested int
		addable   int
		want      int
	}{
		{name: "empty cart keeps valid requested quantity", existing: 0, requested: 3, addable: 10, want: 3},
		{name: "existing quantity changes valid increment", existing: 3, requested: 3, addable: 10, want: 2},
		{name: "rounds down to valid lattice", existing: 0, requested: 4, addable: 10, want: 3},
		{name: "does not silently exceed requested quantity", existing: 0, requested: 1, addable: 10, want: 0},
		{name: "insufficient stock has no suggestion", existing: 0, requested: 3, addable: 2, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := repeatAdditionAtMost(product, tt.existing, tt.requested, tt.addable); got != tt.want {
				t.Fatalf("suggestion = %d, want %d", got, tt.want)
			}
			if got := smallestRepeatAddition(product, tt.existing, tt.addable); got > 0 && !validRepeatAddition(product, tt.existing, got, tt.addable) {
				t.Fatalf("smallest suggestion %d is invalid", got)
			}
		})
	}
}

func TestRepeatAdditionDefaultsInvalidProductRulesSafely(t *testing.T) {
	product := domain.Product{}
	if !validRepeatAddition(product, 0, 1, 1) {
		t.Fatal("zero-value rules should be normalized to minimum 1 and step 1")
	}
}
