package domain

import "time"

// Request is the HTTP call the job makes.
type Request struct {
	Method    string
	URL       string
	Headers   map[string]string
	Body      []byte
	SecretRef string
}

// Result of the last attempt.
type Result struct {
	StartedAt    *time.Time
	FinishedAt   *time.Time
	HTTPStatus   *int
	Error        string // "timeout", "lease expired", connection error...
	ResponseBody []byte // truncated by the executor
}
