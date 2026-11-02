package ports

import (
	"context"

	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/domain"
)

type JobRepository interface {
	ClaimJobs(ctx context.Context, timeLimit, jobLimit int) ([]*domain.Job, error)
}

// RequestExecutor sends the job request. Endpoint failures go in the Result, not in an error.
type RequestExecutor interface {
	Execute(ctx context.Context, job *domain.Job) domain.Result
}

type WorkerService interface {
	StartWorkerPool(ctx context.Context, workerCount int) error
}
