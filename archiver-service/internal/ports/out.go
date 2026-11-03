package ports

import (
	"context"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/domain"
)

type JobRepository interface {
	// ListArchivableJobs returns up to jobLimit succeeded jobs finished before succeededBefore
	// and dead jobs finished before deadBefore.
	ListArchivableJobs(ctx context.Context, succeededBefore, deadBefore time.Time, jobLimit int) ([]*domain.Job, error)
	// DeleteJobs deletes the jobs that still have the status and attempt they were read with,
	// and returns how many it deleted.
	DeleteJobs(ctx context.Context, jobs []*domain.Job) (int64, error)
}

// JobHistoryRepository is backed by Cassandra.
type JobHistoryRepository interface {
	// CreateJob writes the job to job_history and job_by_id. Writing the same job twice overwrites the same rows.
	CreateJob(ctx context.Context, job *domain.Job) error
}

type ArchiverService interface {
	StartArchiver(ctx context.Context) error
}
