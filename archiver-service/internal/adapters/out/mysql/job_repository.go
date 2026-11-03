package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
	// idx_jobs_archive (status, finished_at) turns each condition into one range of the index.
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+jobColumns()+`
		FROM jobs
		WHERE (status = 'succeeded' AND finished_at < ?)
		   OR (status = 'dead' AND finished_at < ?)
		LIMIT ?`,
		succeededBefore, deadBefore, jobLimit)
	if err != nil {
		return nil, fmt.Errorf("mysql: list archivable jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*domain.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("mysql: scan archivable job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql: list archivable jobs: %w", err)
	}
	return jobs, nil
}

func (r *JobRepository) DeleteJobs(ctx context.Context, jobs []*domain.Job) (int64, error) {
	if len(jobs) == 0 {
		return 0, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("(?, ?, ?), ", len(jobs)), ", ")
	values := make([]any, 0, 3*len(jobs))
	for _, job := range jobs {
		values = append(values, job.ID[:], job.Status, job.Attempt)
	}

	// A dead job retried by hand after it was read no longer matches its status and attempt, so it stays.
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM jobs
		WHERE (id, status, attempt) IN (`+placeholders+`)`,
		values...)
	if err != nil {
		return 0, fmt.Errorf("mysql: delete jobs: %w", err)
	}

	deletedJobs, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mysql: read affected rows: %w", err)
	}
	return deletedJobs, nil
}
