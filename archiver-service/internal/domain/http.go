package domain

import "time"

// Result of the last attempt.
type Result struct {
	StartedAt    *time.Time
	FinishedAt   *time.Time
	HTTPStatus   *int
	Error        string
	ResponseBody []byte
}
