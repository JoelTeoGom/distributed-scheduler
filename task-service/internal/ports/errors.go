package ports

import "errors"

var (
	ErrNotFound            = errors.New("task not found")
	ErrDuplicateKey        = errors.New("duplicate key")
	ErrVersionConflict     = errors.New("version conflict")
	ErrIdempotencyMismatch = errors.New("idempotency key reused with a different body")
)
