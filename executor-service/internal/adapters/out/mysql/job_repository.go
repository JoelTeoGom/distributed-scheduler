package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/ports"
)

type JobRepository struct {
	db         *sql.DB
	executorID string
}

var _ ports.JobRepository = (*JobRepository)(nil)

// executorID identifies this executor instance in worker_id, such as the pod name.
func NewJobRepository(db *sql.DB, executorID string) *JobRepository {
	return &JobRepository{db: db, executorID: executorID}
}

// The transaction ends before any HTTP call, so the row locks last milliseconds.
func (r *JobRepository) ClaimJobs(ctx context.Context, timeLimit, jobLimit int) ([]*domain.Job, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mysql: begin transaction: %w", err)
	}
	defer tx.Rollback()

	jobIDs, err := selectClaimableJobIDs(ctx, tx, timeLimit, jobLimit)
	if err != nil {
		return nil, err
	}
	if len(jobIDs) == 0 {
		return nil, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(jobIDs)), ", ")
	idValues := make([]any, len(jobIDs))
	for i, jobID := range jobIDs {
		idValues[i] = jobID[:]
	}

	// The lease covers the wait until next_attempt_at, the request timeout and a 30 second margin.
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status           = 'running',
		    worker_id        = ?,
		    attempt          = attempt + 1,
		    started_at       = NOW(3),
		    lease_expires_at = GREATEST(next_attempt_at, NOW(3)) + INTERVAL (timeout_seconds + 30) SECOND,
		    updated_at       = NOW(3)
		WHERE id IN (`+placeholders+`)`,
		append([]any{r.executorID}, idValues...)...); err != nil {
		return nil, fmt.Errorf("mysql: claim jobs: %w", err)
	}

	jobs, err := listJobs(ctx, tx, `
		SELECT `+jobColumns()+`
		FROM jobs
		WHERE id IN (`+placeholders+`)
		ORDER BY priority, next_attempt_at`,
		idValues)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("mysql: commit claim: %w", err)
	}
	return jobs, nil
}

// The guard on attempt fences out writes from an older claim, even one made by this same executor.
func (r *JobRepository) SaveSucceeded(ctx context.Context, job *domain.Job) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET status           = 'succeeded',
		    http_status      = ?,
		    response_body    = ?,
		    error            = NULL,
		    finished_at      = NOW(3),
		    lease_expires_at = NULL,
		    updated_at       = NOW(3)
		WHERE id = ? AND status = 'running' AND worker_id = ? AND attempt = ?`,
		nullableInt(job.Result.HTTPStatus), job.Result.ResponseBody,
		job.ID[:], r.executorID, job.Attempt)
	if err != nil {
		return fmt.Errorf("mysql: save succeeded job: %w", err)
	}
	return checkLeaseHeld(result)
}

func (r *JobRepository) SaveRetry(ctx context.Context, job *domain.Job, nextAttemptAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET status           = 'pending',
		    next_attempt_at  = ?,
		    http_status      = ?,
		    error            = ?,
		    response_body    = ?,
		    worker_id        = NULL,
		    lease_expires_at = NULL,
		    updated_at       = NOW(3)
		WHERE id = ? AND status = 'running' AND worker_id = ? AND attempt = ?`,
		nextAttemptAt, nullableInt(job.Result.HTTPStatus), nullableError(job.Result.Error), job.Result.ResponseBody,
		job.ID[:], r.executorID, job.Attempt)
	if err != nil {
		return fmt.Errorf("mysql: save retry job: %w", err)
	}
	return checkLeaseHeld(result)
}

// worker_id is kept so the job detail can show which executor ran it.
func (r *JobRepository) SaveDead(ctx context.Context, job *domain.Job) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET status           = 'dead',
		    http_status      = ?,
		    error            = ?,
		    response_body    = ?,
		    finished_at      = NOW(3),
		    lease_expires_at = NULL,
		    updated_at       = NOW(3)
		WHERE id = ? AND status = 'running' AND worker_id = ? AND attempt = ?`,
		nullableInt(job.Result.HTTPStatus), nullableError(job.Result.Error), job.Result.ResponseBody,
		job.ID[:], r.executorID, job.Attempt)
	if err != nil {
		return fmt.Errorf("mysql: save dead job: %w", err)
	}
	return checkLeaseHeld(result)
}

// The attempt never happened, so the claim gives it back. Reusing that attempt number is safe:
// this is the last write of this executor for the job, so no late result can come after it.
func (r *JobRepository) ReleaseJob(ctx context.Context, job *domain.Job) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET status           = 'pending',
		    attempt          = attempt - 1,
		    worker_id        = NULL,
		    started_at       = NULL,
		    lease_expires_at = NULL,
		    updated_at       = NOW(3)
		WHERE id = ? AND status = 'running' AND worker_id = ? AND attempt = ?`,
		job.ID[:], r.executorID, job.Attempt)
	if err != nil {
		return fmt.Errorf("mysql: release job: %w", err)
	}
	return checkLeaseHeld(result)
}

// SKIP LOCKED lets several executors claim at the same time without waiting for each other's rows.
func selectClaimableJobIDs(ctx context.Context, tx *sql.Tx, timeLimit, jobLimit int) ([]uuid.UUID, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM jobs
		WHERE status = 'pending'
		  AND next_attempt_at <= NOW(3) + INTERVAL ? SECOND
		ORDER BY priority, next_attempt_at
		LIMIT ?
		FOR UPDATE SKIP LOCKED`,
		timeLimit, jobLimit)
	if err != nil {
		return nil, fmt.Errorf("mysql: select claimable jobs: %w", err)
	}
	defer rows.Close()

	var jobIDs []uuid.UUID
	for rows.Next() {
		var jobID uuid.UUID
		if err := rows.Scan(&jobID); err != nil {
			return nil, fmt.Errorf("mysql: scan claimable job: %w", err)
		}
		jobIDs = append(jobIDs, jobID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql: read claimable jobs: %w", err)
	}
	return jobIDs, nil
}

func listJobs(ctx context.Context, tx *sql.Tx, query string, values []any) ([]*domain.Job, error) {
	rows, err := tx.QueryContext(ctx, query, values...)
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

// No affected rows means the reconciler took the job back after its lease expired.
func checkLeaseHeld(result sql.Result) error {
	affectedRows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql: read affected rows: %w", err)
	}
	if affectedRows == 0 {
		return domain.ErrLeaseLost
	}
	return nil
}
