package ports

import (
	"context"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
	"github.com/google/uuid"
)

// HotJobRepository is backed by the MySQL hot table.
type HotJobRepository interface {
	Get(ctx context.Context, tenantID, jobID uuid.UUID) (*domain.Job, error)
	History(ctx context.Context, tenantID, taskID uuid.UUID, before *time.Time, limit int) ([]*domain.Job, error)
	ListDead(ctx context.Context, tenantID uuid.UUID, after *uuid.UUID, limit int) ([]*domain.Job, error)
	// Retry affects 0 rows when the job is not dead or does not exist.
	Retry(ctx context.Context, tenantID, jobID uuid.UUID, now time.Time) error
}

// JobHistoryRepository is backed by Cassandra.
type JobHistoryRepository interface {
	Get(ctx context.Context, tenantID, jobID uuid.UUID) (*domain.Job, error)
	History(ctx context.Context, tenantID, taskID uuid.UUID, before *time.Time, limit int) ([]*domain.Job, error)
}

type TokenVerifier interface {
	Verify(token string) (tenantID uuid.UUID, err error)
}
