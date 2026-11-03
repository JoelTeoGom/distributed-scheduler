package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/JoelTeoGom/distributed-scheduler/reconciler-service/internal/ports"
)

type JobRepository struct {
	db *sql.DB
}

var _ ports.JobRepository = (*JobRepository)(nil)

func NewJobRepository(db *sql.DB) *JobRepository {
	return &JobRepository{db: db}
}

// The claim already counted the lost attempt, so a job on its last attempt goes to dead instead of running once more.
func (r *JobRepository) ReconcileJobs(ctx context.Context, jobLimit int) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET status           = IF(attempt >= max_attempts, 'dead', 'pending'),
		    next_attempt_at  = NOW(3),
		    http_status      = NULL,
		    error            = 'lease expired',
		    response_body    = NULL,
		    finished_at      = IF(attempt >= max_attempts, NOW(3), NULL),
		    worker_id        = NULL,
		    lease_expires_at = NULL,
		    updated_at       = NOW(3)
		WHERE status = 'running' AND lease_expires_at < NOW(3)
		LIMIT ?`,
		jobLimit)
	if err != nil {
		return 0, fmt.Errorf("mysql: reconcile jobs: %w", err)
	}

	reconciledJobs, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mysql: read affected rows: %w", err)
	}
	return reconciledJobs, nil
}
