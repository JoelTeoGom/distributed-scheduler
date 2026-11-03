package mysql

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/domain"
)

const maxErrorLength = 1024

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanJob(scanner rowScanner) (*domain.Job, error) {
	var (
		job                domain.Job
		headers            []byte
		secretRef          sql.NullString
		timeoutSeconds     int64
		backoffBaseSeconds int64
		workerName         sql.NullString
		leaseExpiresAt     sql.NullTime
		startedAt          sql.NullTime
		finishedAt         sql.NullTime
		httpStatus         sql.NullInt64
		errorMessage       sql.NullString
	)

	err := scanner.Scan(
		&job.ID, &job.TenantID, &job.TaskID, &job.ScheduledAt,
		&job.Config.Priority,
		&job.Config.Request.Method, &job.Config.Request.URL, &headers, &job.Config.Request.Body, &secretRef, &timeoutSeconds,
		&job.Config.Retry.MaxAttempts, &backoffBaseSeconds,
		&job.Status, &job.Attempt, &job.NextAttemptAt,
		&workerName, &leaseExpiresAt,
		&startedAt, &finishedAt, &httpStatus, &errorMessage, &job.Result.ResponseBody,
		&job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if len(headers) > 0 {
		if err := json.Unmarshal(headers, &job.Config.Request.Headers); err != nil {
			return nil, fmt.Errorf("decode headers: %w", err)
		}
	}

	job.Config.Request.SecretRef = secretRef.String
	job.Config.Timeout = time.Duration(timeoutSeconds) * time.Second
	job.Config.Retry.BackoffBase = time.Duration(backoffBaseSeconds) * time.Second
	job.Result.Error = errorMessage.String
	job.Result.StartedAt = timePointer(startedAt)
	job.Result.FinishedAt = timePointer(finishedAt)

	if httpStatus.Valid {
		status := int(httpStatus.Int64)
		job.Result.HTTPStatus = &status
	}
	if workerName.Valid {
		job.AssignedWorker = &domain.Worker{Name: workerName.String, LeaseExpiresAt: leaseExpiresAt.Time}
	}
	return &job, nil
}

func nullableInt(value *int) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*value), Valid: true}
}

// The error column is VARCHAR(1024), and a url.Error carries the whole URL, which can be 2048 characters.
func nullableError(message string) sql.NullString {
	if message == "" {
		return sql.NullString{}
	}
	if characters := []rune(message); len(characters) > maxErrorLength {
		message = string(characters[:maxErrorLength])
	}
	return sql.NullString{String: message, Valid: true}
}

func timePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func jobColumns() string {
	return `id, tenant_id, task_id, scheduled_at,
	priority,
	http_method, endpoint, headers, body, secret_ref, timeout_seconds,
	max_attempts, backoff_base_seconds,
	status, attempt, next_attempt_at,
	worker_id, lease_expires_at,
	started_at, finished_at, http_status, error, response_body,
	created_at, updated_at`
}
