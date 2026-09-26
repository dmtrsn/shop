package domain

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound     = errors.New("resource not found")
	ErrUnauthorized = errors.New("authentication required")
	ErrForbidden    = errors.New("operation is forbidden")
	ErrConflict     = errors.New("resource conflict")
	ErrValidation   = errors.New("validation failed")
	ErrOutOfStock   = errors.New("requested quantity is not available")

	// ErrDemoAccountDisabled is an ErrUnauthorized, so clients see the generic
	// failure; only server logs distinguish it.
	ErrDemoAccountDisabled = fmt.Errorf("demo account is disabled: %w", ErrUnauthorized)
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationError struct {
	Fields []FieldError `json:"fields"`
}

func (e *ValidationError) Error() string { return ErrValidation.Error() }
func (e *ValidationError) Unwrap() error { return ErrValidation }
