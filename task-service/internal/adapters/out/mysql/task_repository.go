package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/ports"
)

const errDuplicateEntry = 1062

const taskColumns = `id, tenant_id, name,
	schedule_type, run_at, cron_expr, timezone,
	priority,
	http_method, endpoint, headers, body, secret_ref, timeout_seconds,
	max_attempts, backoff_base_seconds,
	status, next_run_at,
	version, created_at, updated_at`

type TaskRepository struct {
	db *sql.DB
}

var _ ports.TaskRepository = (*TaskRepository)(nil)

func NewTaskRepository(db *sql.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

func (r *TaskRepository) Create(ctx context.Context, task domain.Task, idempotency *ports.IdempotencyKey) error {
	configuration, err := taskConfigurationValues(task)
	if err != nil {
		return err
	}

	var idempotencyKey, requestHash any
	if idempotency != nil {
		idempotencyKey = idempotency.Key
		requestHash = idempotency.RequestHash[:]
	}

	values := append([]any{task.ID[:], task.TenantID[:]}, configuration...)
	values = append(values, nullableTimePointer(task.NextRunAt), idempotencyKey, requestHash, task.CreatedAt, task.UpdatedAt)

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO tasks (
			id, tenant_id, name,
			schedule_type, run_at, cron_expr, timezone,
			priority,
			http_method, endpoint, headers, body, secret_ref, timeout_seconds,
			max_attempts, backoff_base_seconds,
			status, next_run_at,
			idempotency_key, request_hash,
			version, created_at, updated_at
		) VALUES (
			?, ?, ?,
			?, ?, ?, ?,
			?,
			?, ?, ?, ?, ?, ?,
			?, ?,
			'active', ?,
			?, ?,
			1, ?, ?
		)`, values...)
	if isDuplicate(err) {
		return domain.ErrDuplicateKey
	}
	if err != nil {
		return fmt.Errorf("mysql: insert task: %w", err)
	}
	return nil
}

func (r *TaskRepository) GetByID(ctx context.Context, tenantID, taskID uuid.UUID) (domain.Task, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+taskColumns+`
		FROM tasks
		WHERE id = ? AND tenant_id = ? AND status <> 'deleted'`,
		taskID[:], tenantID[:])

	task, err := scanTask(row)
	if err != nil {
		return domain.Task{}, translateSelectError(err)
	}
	return task, nil
}

func (r *TaskRepository) GetByIdempotencyKey(ctx context.Context, tenantID uuid.UUID, key string) (domain.Task, ports.IdempotencyKey, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+taskColumns+`, request_hash
		FROM tasks
		WHERE tenant_id = ? AND idempotency_key = ?`,
		tenantID[:], key)

	var requestHash []byte
	task, err := scanTask(row, &requestHash)
	if err != nil {
		return domain.Task{}, ports.IdempotencyKey{}, translateSelectError(err)
	}

	idempotency := ports.IdempotencyKey{Key: key}
	copy(idempotency.RequestHash[:], requestHash)
	return task, idempotency, nil
}

func (r *TaskRepository) List(ctx context.Context, tenantID uuid.UUID, page ports.PageRequest) (ports.TaskPage, error) {
	// Without the hint MySQL picks uq_tasks_idempotency and sorts every task of the tenant.
	query := `
		SELECT ` + taskColumns + `
		FROM tasks FORCE INDEX (idx_tasks_tenant)
		WHERE tenant_id = ? AND status <> 'deleted'
		ORDER BY id
		LIMIT ?`
	values := []any{tenantID[:]}

	if page.Cursor != "" {
		cursor, err := uuid.Parse(page.Cursor)
		if err != nil {
			return ports.TaskPage{}, fmt.Errorf("%w: invalid cursor", domain.ErrInvalidInput)
		}
		query = `
			SELECT ` + taskColumns + `
			FROM tasks FORCE INDEX (idx_tasks_tenant)
			WHERE tenant_id = ? AND id > ? AND status <> 'deleted'
			ORDER BY id
			LIMIT ?`
		values = append(values, cursor[:])
	}

	// One extra row tells whether a next page exists.
	rows, err := r.db.QueryContext(ctx, query, append(values, page.Limit+1)...)
	if err != nil {
		return ports.TaskPage{}, fmt.Errorf("mysql: list tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]domain.Task, 0, page.Limit+1)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return ports.TaskPage{}, fmt.Errorf("mysql: scan task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return ports.TaskPage{}, fmt.Errorf("mysql: list tasks: %w", err)
	}

	if len(tasks) <= page.Limit {
		return ports.TaskPage{Tasks: tasks}, nil
	}
	tasks = tasks[:page.Limit]
	return ports.TaskPage{Tasks: tasks, NextCursor: tasks[len(tasks)-1].ID.String()}, nil
}

func (r *TaskRepository) Update(ctx context.Context, task domain.Task, expectedVersion uint32) error {
	configuration, err := taskConfigurationValues(task)
	if err != nil {
		return err
	}

	values := append(configuration, task.Status, task.UpdatedAt, task.ID[:], task.TenantID[:], expectedVersion)
	return r.save(ctx, `
		UPDATE tasks
		SET name                 = ?,
		    schedule_type        = ?,
		    run_at               = ?,
		    cron_expr            = ?,
		    timezone             = ?,
		    priority             = ?,
		    http_method          = ?,
		    endpoint             = ?,
		    headers              = ?,
		    body                 = ?,
		    secret_ref           = ?,
		    timeout_seconds      = ?,
		    max_attempts         = ?,
		    backoff_base_seconds = ?,
		    status               = ?,
		    version              = version + 1,
		    updated_at           = ?
		WHERE id = ? AND tenant_id = ? AND version = ?`, values)
}

func (r *TaskRepository) Reschedule(ctx context.Context, task domain.Task, expectedVersion uint32) error {
	configuration, err := taskConfigurationValues(task)
	if err != nil {
		return err
	}

	values := append(configuration, task.Status, nullableTimePointer(task.NextRunAt), task.UpdatedAt, task.ID[:], task.TenantID[:], expectedVersion)
	return r.save(ctx, `
		UPDATE tasks
		SET name                 = ?,
		    schedule_type        = ?,
		    run_at               = ?,
		    cron_expr            = ?,
		    timezone             = ?,
		    priority             = ?,
		    http_method          = ?,
		    endpoint             = ?,
		    headers              = ?,
		    body                 = ?,
		    secret_ref           = ?,
		    timeout_seconds      = ?,
		    max_attempts         = ?,
		    backoff_base_seconds = ?,
		    status               = ?,
		    next_run_at          = ?,
		    version              = version + 1,
		    updated_at           = ?
		WHERE id = ? AND tenant_id = ? AND version = ?`, values)
}

func (r *TaskRepository) save(ctx context.Context, query string, values []any) error {
	result, err := r.db.ExecContext(ctx, query, values...)
	if err != nil {
		return fmt.Errorf("mysql: update task: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("mysql: rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return domain.ErrVersionConflict
	}
	return nil
}

func translateSelectError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	return fmt.Errorf("mysql: select task: %w", err)
}

func isDuplicate(err error) bool {
	var mysqlError *mysqldriver.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == errDuplicateEntry
}
