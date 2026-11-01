package domain

import (
	"time"

	"github.com/google/uuid"
)

// Status is the lifecycle of the task, not the execution status (that lives in the job).
type Status string

// Task is the configuration a tenant creates. Each execution is a job.
type Task struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Name      string
	Schedule  Schedule
	Priority  Priority
	Request   Request
	Timeout   time.Duration
	Retry     RetryPolicy
	Status    Status
	NextRunAt *time.Time // nil = nothing left to run
	Version   uint32     // optimistic locking
	CreatedAt time.Time
	UpdatedAt time.Time
}
