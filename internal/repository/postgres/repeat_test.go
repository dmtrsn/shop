package postgres

import (
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
)

func TestRepeatUniqueConstraintRecognition(t *testing.T) {
	for _, tc := range []struct {
		code, constraint string
		want             bool
	}{{"23505", repeatIdempotencyConstraint, true}, {"23505", orderIdempotencyIndex, false}, {"23505", "unrelated", false}, {"23514", repeatIdempotencyConstraint, false}} {
		err := fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: tc.code, ConstraintName: tc.constraint})
		if isRepeatIdempotencyViolation(err) != tc.want {
			t.Fatal(tc)
		}
	}
}
