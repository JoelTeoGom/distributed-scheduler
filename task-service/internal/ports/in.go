package ports

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"
	"github.com/google/uuid"
)

type TaskService interface {
	Create(ctx context.Context, tenantID uuid.UUID, idempotency *IdempotencyKey, input CreateTaskInput) (domain.Task, error)
	Get(ctx context.Context, tenantID, taskID uuid.UUID) (domain.Task, error)
	List(ctx context.Context, tenantID uuid.UUID, page PageRequest) (TaskPage, error)
	Update(ctx context.Context, tenantID, taskID uuid.UUID, expectedVersion uint32, input UpdateTaskInput) (domain.Task, error)
	Pause(ctx context.Context, tenantID, taskID uuid.UUID) (domain.Task, error)
	Resume(ctx context.Context, tenantID, taskID uuid.UUID) (domain.Task, error)
	Delete(ctx context.Context, tenantID, taskID uuid.UUID) error
}

type CreateTaskInput struct {
	Name     string
	Schedule domain.Schedule
	Priority domain.Priority
	Request  domain.Request
	Timeout  time.Duration
	Retry    domain.RetryPolicy
}

// UpdateTaskInput has PATCH semantics: a nil field stays unchanged.
type UpdateTaskInput struct {
	Name     *string
	Schedule *domain.Schedule
	Priority *domain.Priority
	Request  *domain.Request
	Timeout  *time.Duration
	Retry    *domain.RetryPolicy
}

type IdempotencyKey struct {
	Key         string
	RequestHash [sha256.Size]byte
}

type PageRequest struct {
	Cursor string
	Limit  int
}

type TaskPage struct {
	Tasks      []domain.Task
	NextCursor string
}
