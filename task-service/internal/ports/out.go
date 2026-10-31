package ports

import (
	"context"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"
	"github.com/google/uuid"
)

type TaskRepository interface {
	Create(ctx context.Context, task domain.Task, idempotency *IdempotencyKey) error
	GetByID(ctx context.Context, tenantID, taskID uuid.UUID) (domain.Task, error)
	// GetByIdempotencyKey also finds deleted tasks, so a retried POST replays the original result.
	GetByIdempotencyKey(ctx context.Context, tenantID uuid.UUID, key string) (domain.Task, IdempotencyKey, error)
	List(ctx context.Context, tenantID uuid.UUID, page PageRequest) (TaskPage, error)
	// Update never writes next_run_at, which the scheduler advances concurrently.
	Update(ctx context.Context, task domain.Task, expectedVersion uint32) error
	// Reschedule is Update plus next_run_at, for resumes and schedule changes.
	Reschedule(ctx context.Context, task domain.Task, expectedVersion uint32) error
}

type NextRunCalculator interface {
	Next(cronExpr, timezone string, after time.Time) (time.Time, error)
}
