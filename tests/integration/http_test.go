//go:build integration

package integration_test

import (
	"io"
	"log/slog"
	"sweetshop/internal/delivery/httpapi"
	"sweetshop/tests/contract"
	"testing"
)

func TestPostgresHTTPContract(t *testing.T) {
	d := newDatabase(t, true)
	handler := httpapi.New(d.service, slog.New(slog.NewTextHandler(io.Discard, nil)), "../../web", "postgres")
	contract.Run(t, handler)
}
