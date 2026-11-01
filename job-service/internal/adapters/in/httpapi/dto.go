package httpapi

import "time"

type HTTPRequestDTO struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      string            `json:"body,omitempty"`
	SecretRef string            `json:"secret_ref,omitempty"`
}

type RetryDTO struct {
	MaxAttempts        int    `json:"max_attempts"`
	BackoffBaseSeconds uint32 `json:"backoff_base_seconds"`
}

type ResultDTO struct {
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	HTTPStatus   *int       `json:"http_status,omitempty"`
	Error        string     `json:"error,omitempty"`
	ResponseBody string     `json:"response_body,omitempty"`
}

type JobResponse struct {
	ID             string          `json:"id"`
	TaskID         string          `json:"task_id"`
	ScheduledAt    time.Time       `json:"scheduled_at"`
	Priority       string          `json:"priority"`
	Request        *HTTPRequestDTO `json:"request,omitempty"`
	TimeoutSeconds *uint32         `json:"timeout_seconds,omitempty"`
	Retry          *RetryDTO       `json:"retry,omitempty"`
	Status         string          `json:"status"`
	Attempt        int             `json:"attempt"`
	NextAttemptAt  *time.Time      `json:"next_attempt_at,omitempty"`
	Result         ResultDTO       `json:"result"`
	CreatedAt      *time.Time      `json:"created_at,omitempty"`
	UpdatedAt      *time.Time      `json:"updated_at,omitempty"`
}

type JobPageResponse struct {
	Jobs       []JobResponse `json:"jobs"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
