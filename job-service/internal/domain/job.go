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
	Config         Config
	Status         Status
	Attempt        int       // incremented on claim
	NextAttemptAt  time.Time // = ScheduledAt at first, then now + backoff
	AssignedWorker *Worker   // nil = nobody holds it
	Result         Result
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
