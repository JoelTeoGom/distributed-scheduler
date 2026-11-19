package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/ports"
)

type SchedulerRepository struct {
	db        *sql.DB
	lookahead time.Duration
}

var _ ports.SchedulerRepository = (*SchedulerRepository)(nil)

func NewSchedulerRepository(db *sql.DB, lookahead time.Duration) *SchedulerRepository {
	return &SchedulerRepository{db: db, lookahead: lookahead}
}

func (r *SchedulerRepository) ScheduleJobs(ctx context.Context, now time.Time, nextRuns ports.NextRunCalculator) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	tasks, err := selectScheduledTasks(ctx, tx, now.Add(r.lookahead))
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}

	//I have decided that it is not worth parallelizing: each calculation takes microseconds, close to the cost of starting a goroutine.
	nextRunTimes := make([]sql.NullTime, len(tasks))
	for i, task := range tasks {
		nextRunTimes[i], err = nextRunAfterFiring(task, nextRuns)
		if err != nil {
			return fmt.Errorf("calculate next run of task %s: %w", task.ID, err)
		}
	}

	if err := insertJobs(ctx, tx, tasks, now); err != nil {
		return err
	}

	if err := updateNextRuns(ctx, tx, tasks, nextRunTimes); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func nextRunAfterFiring(task domain.Task, nextRuns ports.NextRunCalculator) (sql.NullTime, error) {
	if task.Schedule.Type == domain.ScheduleAt {
		return sql.NullTime{}, nil
	}

	next, err := nextRuns.Next(task.Schedule.CronExpr, task.Schedule.Timezone, *task.NextRunAt)
	if err != nil {
		return sql.NullTime{}, err
	}
	return sql.NullTime{Time: next, Valid: true}, nil
}

func selectScheduledTasks(ctx context.Context, tx *sql.Tx, horizon time.Time) ([]domain.Task, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT `+taskColumns()+`
		FROM tasks
		WHERE status = 'active' AND next_run_at <= ?
		ORDER BY next_run_at
		LIMIT 1000
		FOR UPDATE`,
		horizon)
	if err != nil {
		return nil, fmt.Errorf("select scheduled tasks: %w", err)
	}
	defer rows.Close()

	var tasks []domain.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan scheduled task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read scheduled tasks: %w", err)
	}
	return tasks, nil
}

func insertJobs(ctx context.Context, tx *sql.Tx, tasks []domain.Task, now time.Time) error {
	placeholders := make([]string, len(tasks))
	var values []any
	for i, task := range tasks {
		jobID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generate job id: %w", err)
		}

		headers, err := encodeHeaders(task.Request.Headers)
		if err != nil {
			return err
		}

		placeholders[i] = `(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?)`
		values = append(values,
			jobID[:], task.TenantID[:], task.ID[:], *task.NextRunAt,
			task.Priority, task.Request.Method, task.Request.URL, headers, nullableBytes(task.Request.Body), nullableString(task.Request.SecretRef), int64(task.Timeout/time.Second),
			task.Retry.MaxAttempts, int64(task.Retry.BackoffBase/time.Second),
			*task.NextRunAt, now, now)
	}

	// A resume inside the lookahead can set next_run_at back to a time that already has its job.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO jobs (
			id, tenant_id, task_id, scheduled_at,
			priority, http_method, endpoint, headers, body, secret_ref, timeout_seconds,
			max_attempts, backoff_base_seconds,
			status, next_attempt_at, created_at, updated_at
		) VALUES `+strings.Join(placeholders, ", ")+`
		ON DUPLICATE KEY UPDATE id = id`,
		values...); err != nil {
		return fmt.Errorf("insert jobs: %w", err)
	}
	return nil
}

func updateNextRuns(ctx context.Context, tx *sql.Tx, tasks []domain.Task, nextRunTimes []sql.NullTime) error {
	rows := make([]string, len(tasks))
	values := make([]any, 0, 2*len(tasks))
	for i, task := range tasks {
		rows[i] = `ROW(CAST(? AS BINARY(16)), CAST(? AS DATETIME(3)))`
		values = append(values, task.ID[:], nextRunTimes[i])
	}

	// Placeholders inside VALUES have no column to take a type from, so MySQL reads them as utf8mb4 text without the casts.
	// version stays untouched: bumping it would invalidate every client's If-Match on each run.
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		JOIN (VALUES `+strings.Join(rows, ", ")+`) AS next_runs (id, next_run_at)
			ON tasks.id = next_runs.id
		SET tasks.next_run_at = next_runs.next_run_at`,
		values...); err != nil {
		return fmt.Errorf("update next runs: %w", err)
	}
	return nil
}
