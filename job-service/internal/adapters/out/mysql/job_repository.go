package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/ports"
)

type JobRepository struct {
	db *sql.DB
}

var _ ports.HotJobRepository = (*JobRepository)(nil)

func NewJobRepository(db *sql.DB) *JobRepository {
	return &JobRepository{db: db}
}

func (r *JobRepository) Get(ctx context.Context, tenantID, jobID uuid.UUID) (*domain.Job, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+jobColumns()+`
		FROM jobs
		WHERE id = ? AND tenant_id = ?`,
		jobID[:], tenantID[:])

	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mysql: select job: %w", err)
	}
	return job, nil
}

func (r *JobRepository) History(ctx context.Context, tenantID, taskID uuid.UUID, before *time.Time, limit int) ([]*domain.Job, error) {
	query := `
		SELECT ` + jobColumns() + `
		FROM jobs
		WHERE task_id = ? AND tenant_id = ?
		ORDER BY scheduled_at DESC
		LIMIT ?`
	values := []any{taskID[:], tenantID[:]}

	if before != nil {
		query = `
			SELECT ` + jobColumns() + `
			FROM jobs
			WHERE task_id = ? AND tenant_id = ? AND scheduled_at < ?
			ORDER BY scheduled_at DESC
			LIMIT ?`
		values = append(values, *before)
	}

	return r.listJobs(ctx, query, append(values, limit))
}

func (r *JobRepository) ListDead(ctx context.Context, tenantID uuid.UUID, after *uuid.UUID, limit int) ([]*domain.Job, error) {
	// Without the hint MySQL intersects two indexes on later pages and sorts the result.
	query := `
		SELECT ` + jobColumns() + `
		FROM jobs FORCE INDEX (idx_jobs_tenant_status)
		WHERE tenant_id = ? AND status = 'dead'
		ORDER BY id
		LIMIT ?`
	values := []any{tenantID[:]}

	if after != nil {
		query = `
			SELECT ` + jobColumns() + `
			FROM jobs FORCE INDEX (idx_jobs_tenant_status)
			WHERE tenant_id = ? AND status = 'dead' AND id > ?
			ORDER BY id
			LIMIT ?`
		values = append(values, after[:])
	}

	return r.listJobs(ctx, query, append(values, limit))
}

func (r *JobRepository) Retry(ctx context.Context, tenantID, jobID uuid.UUID, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET status           = 'pending',
		    attempt          = 0,
		    next_attempt_at  = ?,
		    worker_id        = NULL,
		    lease_expires_at = NULL,
		    started_at       = NULL,
		    finished_at      = NULL,
		    http_status      = NULL,
		    error            = NULL,
		    response_body    = NULL,
		    updated_at       = ?
		WHERE id = ? AND tenant_id = ? AND status = 'dead'`,
		now, now, jobID[:], tenantID[:])
	if err != nil {
		return fmt.Errorf("mysql: retry job: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql: rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrNotRetryable
	}
	return nil
}

func (r *JobRepository) listJobs(ctx context.Context, query string, values []any) ([]*domain.Job, error) {
	rows, err := r.db.QueryContext(ctx, query, values...)
	if err != nil {
		return nil, fmt.Errorf("mysql: list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*domain.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("mysql: scan job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql: list jobs: %w", err)
	}
	return jobs, nil
}
