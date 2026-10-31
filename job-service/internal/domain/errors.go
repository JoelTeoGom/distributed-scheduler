package domain

import "errors"

var (
	ErrNotFound     = errors.New("job not found")
	ErrNotRetryable = errors.New("only dead jobs can be retried")
	ErrInvalidInput = errors.New("invalid input")
	ErrUnauthorized = errors.New("unauthorized")
)
