package domain

import (
	"time"

	"github.com/google/uuid"
)

// Job is one execution of a task. Its ID is also the Idempotency-Key sent to the endpoint.
type Job struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	TaskID         uuid.UUID
	ScheduledAt    time.Time // logical fire time; StartedAt - ScheduledAt = scheduling lag
	Config         JobConfig
	Status         Status
	Attempt        int       // incremented on claim
	NextAttemptAt  time.Time // = ScheduledAt at first, then now + backoff
	AssignedWorker *Worker   // nil = nobody holds it
	Result         Result
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// JobConfig is copied from the task when the job is created, so editing the task does not change existing jobs.
type JobConfig struct {
	Priority Priority
	Request  Request
	Timeout  time.Duration
	Retry    RetryPolicy
}

// There is no failed status: a failed attempt goes back to pending (retry) or to dead (retries exhausted).
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusDead      Status = "dead"
)

// Priority is numeric so the claim query can ORDER BY it: lower = more urgent.
type Priority uint8

const (
	PriorityCritical Priority = iota
	PriorityHigh
	PriorityNormal
	PriorityLow
)

type RetryPolicy struct {
	MaxAttempts int
	BackoffBase time.Duration
}
