package domain

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound            = errors.New("task not found")
	ErrDuplicateKey        = errors.New("duplicate key")
	ErrVersionConflict     = errors.New("version conflict")
	ErrIdempotencyMismatch = errors.New("idempotency key reused with a different body")
	ErrInvalidInput        = errors.New("invalid input")
)

func invalid(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, message)
}
