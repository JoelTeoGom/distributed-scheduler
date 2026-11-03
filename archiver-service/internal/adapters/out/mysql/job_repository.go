package mysql

import (
	"context"
	"database/sql"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/ports"
)

type JobRepository struct {
	db *sql.DB
}

var _ ports.JobRepository = (*JobRepository)(nil)

func NewJobRepository(db *sql.DB) *JobRepository {
	return &JobRepository{db: db}
}

func (r *JobRepository) ListArchivableJobs(ctx context.Context, succeededBefore, deadBefore time.Time, jobLimit int) ([]*domain.Job, error) {
	return nil, nil
}

func (r *JobRepository) DeleteJobs(ctx context.Context, jobs []*domain.Job) (int64, error) {
	return 0, nil
}
