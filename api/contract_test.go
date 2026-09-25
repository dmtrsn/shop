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
	handler := httpapi.New(application.New(memory.NewDemo()), slog.New(slog.NewTextHandler(io.Discard, nil)), "../web", "memory")
	contract.Run(t, handler)
}
