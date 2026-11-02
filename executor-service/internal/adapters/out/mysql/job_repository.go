package mysql

import (
	"context"
	"database/sql"

	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/ports"
)

type JobRepository struct {
	db *sql.DB
}

var _ ports.JobRepository = (*JobRepository)(nil)

func NewJobRepository(db *sql.DB) *JobRepository {
	return &JobRepository{db: db}
}

func (r *JobRepository) ObtainJobs(ctx context.Context, timeLimit, jobLimit int) ([]*domain.Job, error) {
	// TODO: poll the jobs table for up to jobLimit jobs within timeLimit.
	return nil, nil
}
