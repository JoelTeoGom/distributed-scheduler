package domain

import "time"

// Result of the last attempt.
type Result struct {
	StartedAt    *time.Time
	FinishedAt   *time.Time
	HTTPStatus   *int
	Error        string // "timeout", "lease expired", connection error...
	ResponseBody []byte // truncated by the executor
}
