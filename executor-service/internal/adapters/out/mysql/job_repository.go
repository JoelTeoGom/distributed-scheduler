package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

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
