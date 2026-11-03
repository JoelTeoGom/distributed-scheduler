package mysql

import (
	"database/sql"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/domain"
)

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanJob(scanner rowScanner) (*domain.Job, error) {
	var (
		job          domain.Job
		httpStatus   sql.NullInt64
		errorMessage sql.NullString
		startedAt    sql.NullTime
		finishedAt   sql.NullTime
	)

	err := scanner.Scan(
		&job.ID, &job.TenantID, &job.TaskID, &job.ScheduledAt,
		&job.Priority, &job.Status, &job.Attempt,
		&httpStatus, &errorMessage, &job.Result.ResponseBody,
		&startedAt, &finishedAt,
	)
	if err != nil {
		return nil, err
	}

	job.Result.Error = errorMessage.String
	job.Result.StartedAt = timePointer(startedAt)
	job.Result.FinishedAt = timePointer(finishedAt)
	if httpStatus.Valid {
		status := int(httpStatus.Int64)
		job.Result.HTTPStatus = &status
	}
	return &job, nil
}

func jobColumns() string {
	return `id, tenant_id, task_id, scheduled_at,
	priority, status, attempt,
	http_status, error, response_body,
	started_at, finished_at`
}

func timePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
