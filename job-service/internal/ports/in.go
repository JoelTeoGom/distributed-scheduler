package ports

import (
	"context"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
	"github.com/google/uuid"
)

type JobService interface {
	Get(ctx context.Context, tenantID, jobID uuid.UUID) (domain.Job, error)
	ListByTask(ctx context.Context, tenantID, taskID uuid.UUID, page PageRequest) (JobPage, error)
	ListDead(ctx context.Context, tenantID uuid.UUID, page PageRequest) (JobPage, error)
	Retry(ctx context.Context, tenantID, jobID uuid.UUID) (domain.Job, error)
}

type PageRequest struct {
	Cursor string
	Limit  int
}

type JobPage struct {
	Jobs       []domain.Job
	NextCursor string
}
