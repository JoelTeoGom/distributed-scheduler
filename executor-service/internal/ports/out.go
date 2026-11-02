package ports

import (
	"context"

	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/domain"
)

type JobRepository interface {
	ObtainJobs(ctx context.Context, timeLimit, jobLimit int) ([]*domain.Job, error)
}
