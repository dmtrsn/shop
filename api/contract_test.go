package api_test

import (
	"io"
	"log/slog"
	"sweetshop/internal/application"
	"sweetshop/internal/delivery/httpapi"
	"sweetshop/internal/repository/memory"
	"sweetshop/tests/contract"
	"testing"
)

func TestCurrentHTTPContract(t *testing.T) {
	handler := httpapi.New(application.New(memory.NewDemo(), application.Options{AllowDemoAccount: true}), slog.New(slog.NewTextHandler(io.Discard, nil)), "../web", "memory", contract.SessionConfig())
	contract.Run(t, handler)
}

// The admin contract is exercised against PostgreSQL in tests/integration;
// here it is validated and matched against the registered routes.
func TestAdminContractInventory(t *testing.T) {
	contract.AdminDocument(t)
}
