package httpapi

import "time"

type ScheduleDTO struct {
	Type     string     `json:"type"`
	RunAt    *time.Time `json:"run_at,omitempty"`
	CronExpr string     `json:"cron_expr,omitempty"`
	Timezone string     `json:"timezone,omitempty"`
}

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

type CreateTaskRequest struct {
	Name           string         `json:"name"`
	Schedule       ScheduleDTO    `json:"schedule"`
	Priority       string         `json:"priority"`
	Request        HTTPRequestDTO `json:"request"`
	TimeoutSeconds uint32         `json:"timeout_seconds"`
	Retry          RetryDTO       `json:"retry"`
}

type UpdateTaskRequest struct {
	Name           *string         `json:"name"`
	Schedule       *ScheduleDTO    `json:"schedule"`
	Priority       *string         `json:"priority"`
	Request        *HTTPRequestDTO `json:"request"`
	TimeoutSeconds *uint32         `json:"timeout_seconds"`
	Retry          *RetryDTO       `json:"retry"`
}

type TaskResponse struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Schedule       ScheduleDTO    `json:"schedule"`
	Priority       string         `json:"priority"`
	Request        HTTPRequestDTO `json:"request"`
	TimeoutSeconds uint32         `json:"timeout_seconds"`
	Retry          RetryDTO       `json:"retry"`
	Status         string         `json:"status"`
	NextRunAt      *time.Time     `json:"next_run_at,omitempty"`
	Version        uint32         `json:"version"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type TaskPageResponse struct {
	Tasks      []TaskResponse `json:"tasks"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
