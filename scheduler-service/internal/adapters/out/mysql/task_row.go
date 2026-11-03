package mysql

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/domain"
)

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanTask(scanner rowScanner, extraDestinations ...any) (domain.Task, error) {
	var (
		task               domain.Task
		runAt              sql.NullTime
		cronExpr           sql.NullString
		headers            []byte
		secretRef          sql.NullString
		timeoutSeconds     int64
		backoffBaseSeconds int64
		nextRunAt          sql.NullTime
	)

	destinations := append([]any{
		&task.ID, &task.TenantID, &task.Name,
		&task.Schedule.Type, &runAt, &cronExpr, &task.Schedule.Timezone,
		&task.Priority,
		&task.Request.Method, &task.Request.URL, &headers, &task.Request.Body, &secretRef, &timeoutSeconds,
		&task.Retry.MaxAttempts, &backoffBaseSeconds,
		&task.Status, &nextRunAt,
		&task.Version, &task.CreatedAt, &task.UpdatedAt,
	}, extraDestinations...)

	if err := scanner.Scan(destinations...); err != nil {
		return domain.Task{}, err
	}

	if len(headers) > 0 {
		if err := json.Unmarshal(headers, &task.Request.Headers); err != nil {
			return domain.Task{}, fmt.Errorf("decode headers: %w", err)
		}
	}

	task.Schedule.RunAt = runAt.Time
	task.Schedule.CronExpr = cronExpr.String
	task.Request.SecretRef = secretRef.String
	task.Timeout = time.Duration(timeoutSeconds) * time.Second
	task.Retry.BackoffBase = time.Duration(backoffBaseSeconds) * time.Second
	if nextRunAt.Valid {
		task.NextRunAt = &nextRunAt.Time
	}
	return task, nil
}

// Headers travel as a string: MySQL rejects binary parameters for JSON columns.
func encodeHeaders(headers map[string]string) (sql.NullString, error) {
	if len(headers) == 0 {
		return sql.NullString{}, nil
	}

	encoded, err := json.Marshal(headers)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("encode headers: %w", err)
	}
	return sql.NullString{String: string(encoded), Valid: true}, nil
}

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func nullableBytes(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	return value
}

func taskColumns() string {
	return `id, tenant_id, name,
	schedule_type, run_at, cron_expr, timezone,
	priority,
	http_method, endpoint, headers, body, secret_ref, timeout_seconds,
	max_attempts, backoff_base_seconds,
	status, next_run_at,
	version, created_at, updated_at`
}
