package cassandra

import (
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
)

type jobRow struct {
	tenantID     gocql.UUID
	taskID       gocql.UUID
	scheduledAt  time.Time
	jobID        gocql.UUID
	status       string
	attempt      int16
	priority     int8
	httpStatus   *int16
	errorMessage string
	responseBody []byte
	startedAt    *time.Time
	finishedAt   *time.Time
}

func jobColumns() string {
	return `tenant_id, task_id, scheduled_at, job_id,
	status, attempt, priority,
	http_status, error, response_body,
	started_at, finished_at`
}

func (r *jobRow) destinations() []any {
	return []any{
		&r.tenantID, &r.taskID, &r.scheduledAt, &r.jobID,
		&r.status, &r.attempt, &r.priority,
		&r.httpStatus, &r.errorMessage, &r.responseBody,
		&r.startedAt, &r.finishedAt,
	}
}

func (r *jobRow) toJob() *domain.Job {
	job := &domain.Job{
		ID:          uuid.UUID(r.jobID),
		TenantID:    uuid.UUID(r.tenantID),
		TaskID:      uuid.UUID(r.taskID),
		ScheduledAt: r.scheduledAt.UTC(),
		Config:      domain.JobConfig{Priority: domain.Priority(r.priority)},
		Status:      domain.Status(r.status),
		Attempt:     int(r.attempt),
		Result: domain.Result{
			StartedAt:    utcPointer(r.startedAt),
			FinishedAt:   utcPointer(r.finishedAt),
			Error:        r.errorMessage,
			ResponseBody: r.responseBody,
		},
	}

	if r.httpStatus != nil {
		httpStatus := int(*r.httpStatus)
		job.Result.HTTPStatus = &httpStatus
	}
	return job
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
