package ports

import (
	"context"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
	"github.com/google/uuid"
)

type JobService interface {
	Get(ctx context.Context, tenantID, jobID uuid.UUID) (*domain.Job, error)
	History(ctx context.Context, query HistoryQuery) (JobPage, error)
	ListDead(ctx context.Context, query DeadQuery) (JobPage, error)
	Retry(ctx context.Context, tenantID, jobID uuid.UUID) error
}

type HistoryQuery struct {
	TenantID uuid.UUID
	TaskID   uuid.UUID
	Before   *time.Time
	Limit    int
}

type DeadQuery struct {
	TenantID uuid.UUID
	After    *uuid.UUID
	Limit    int
}

type JobPage struct {
	Jobs       []*domain.Job
	NextCursor string
}
