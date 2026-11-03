package ports

import (
	"context"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/domain"
)

// The Save methods return domain.ErrLeaseLost when the job no longer belongs to this claim.
type JobRepository interface {
	ClaimJobs(ctx context.Context, timeLimit, jobLimit int) ([]*domain.Job, error)
	SaveSucceeded(ctx context.Context, job *domain.Job) error
	SaveRetry(ctx context.Context, job *domain.Job, nextAttemptAt time.Time) error
	SaveDead(ctx context.Context, job *domain.Job) error
}

// RequestExecutor sends the job request. Endpoint failures go in the Result; the error is
// domain.ErrDestinationBlocked, when the request was never sent.
type RequestExecutor interface {
	Execute(ctx context.Context, job *domain.Job) (domain.Result, error)
}

type WorkerService interface {
	StartWorkerPool(ctx context.Context, workerCount int) error
}
