//go:build integration

// Needs Cassandra on 127.0.0.1:9042 with the scheduler keyspace and the job-service migrations applied.
package cassandra

import (
	"context"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/domain"
)

func TestCreateJobsWritesBothTables(t *testing.T) {
	session, err := Open([]string{"127.0.0.1"}, "scheduler")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	repository := NewJobHistoryRepository(session)

	scheduledAt := time.Date(2026, 10, 30, 10, 0, 0, 123000000, time.UTC)
	startedAt, finishedAt := scheduledAt.Add(time.Second), scheduledAt.Add(2*time.Second)
	httpStatus := 503
	dead := &domain.Job{
		ID: uuid.Must(uuid.NewV7()), TenantID: uuid.New(), TaskID: uuid.New(), ScheduledAt: scheduledAt,
		Priority: 1, Status: domain.StatusDead, Attempt: 200,
		Result: domain.Result{StartedAt: &startedAt, FinishedAt: &finishedAt, HTTPStatus: &httpStatus, Error: "upstream failed", ResponseBody: []byte("boom")},
	}
	// No response: every result column is left unset.
	timedOut := &domain.Job{
		ID: uuid.Must(uuid.NewV7()), TenantID: uuid.New(), TaskID: uuid.New(), ScheduledAt: scheduledAt,
		Priority: 2, Status: domain.StatusSucceeded, Attempt: 1,
	}
	t.Cleanup(func() { deleteJob(t, session, dead); deleteJob(t, session, timedOut) })

	createdJobs, err := repository.CreateJobs(context.Background(), []*domain.Job{dead, timedOut})
	if err != nil {
		t.Fatal(err)
	}
	if len(createdJobs) != 2 {
		t.Fatalf("got %d created jobs, want 2", len(createdJobs))
	}

	var (
		status       string
		attempt      int16
		priority     int8
		storedStatus *int16
		errorMessage string
		responseBody []byte
		storedStart  *time.Time
		storedFinish *time.Time
	)
	if err := session.Query(`
		SELECT status, attempt, priority, http_status, error, response_body, started_at, finished_at
		FROM job_history
		WHERE tenant_id = ? AND task_id = ? AND bucket = 202610 AND scheduled_at = ? AND job_id = ?`,
		gocql.UUID(dead.TenantID), gocql.UUID(dead.TaskID), scheduledAt, gocql.UUID(dead.ID)).
		Scan(&status, &attempt, &priority, &storedStatus, &errorMessage, &responseBody, &storedStart, &storedFinish); err != nil {
		t.Fatal(err)
	}
	if status != "dead" || attempt != 200 || priority != 1 || *storedStatus != 503 || errorMessage != "upstream failed" ||
		string(responseBody) != "boom" || !storedStart.Equal(startedAt) || !storedFinish.Equal(finishedAt) {
		t.Errorf("job_history row: %s %d %d %v %q %q %v %v", status, attempt, priority, *storedStatus, errorMessage, responseBody, storedStart, storedFinish)
	}

	var (
		taskID         gocql.UUID
		bucket         int
		storedSchedule time.Time
	)
	if err := session.Query(`
		SELECT task_id, bucket, scheduled_at
		FROM job_by_id
		WHERE tenant_id = ? AND job_id = ?`,
		gocql.UUID(timedOut.TenantID), gocql.UUID(timedOut.ID)).
		Scan(&taskID, &bucket, &storedSchedule); err != nil {
		t.Fatal(err)
	}
	if uuid.UUID(taskID) != timedOut.TaskID || bucket != 202610 || !storedSchedule.Equal(scheduledAt) {
		t.Errorf("job_by_id row: %v %d %v", taskID, bucket, storedSchedule)
	}

	if _, err := repository.CreateJobs(context.Background(), []*domain.Job{dead}); err != nil {
		t.Errorf("writing the same job again: %v", err)
	}
}

func TestCreateJobsReturnsOnlyWrittenJobs(t *testing.T) {
	session, err := Open([]string{"127.0.0.1"}, "scheduler")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	job := &domain.Job{ID: uuid.New(), TenantID: uuid.New(), TaskID: uuid.New(), ScheduledAt: time.Now(), Status: domain.StatusSucceeded, Attempt: 1}

	createdJobs, err := NewJobHistoryRepository(session).CreateJobs(ctx, []*domain.Job{job})
	if err != nil || len(createdJobs) != 0 {
		t.Errorf("got %d created jobs and error %v, want none: the failure is only logged", len(createdJobs), err)
	}
}

func deleteJob(t *testing.T, session *gocql.Session, job *domain.Job) {
	t.Helper()
	bucket := bucketOf(job.ScheduledAt)
	if err := session.Query(`DELETE FROM job_history WHERE tenant_id = ? AND task_id = ? AND bucket = ? AND scheduled_at = ? AND job_id = ?`,
		gocql.UUID(job.TenantID), gocql.UUID(job.TaskID), bucket, job.ScheduledAt, gocql.UUID(job.ID)).Exec(); err != nil {
		t.Errorf("delete job history: %v", err)
	}
	if err := session.Query(`DELETE FROM job_by_id WHERE tenant_id = ? AND job_id = ?`,
		gocql.UUID(job.TenantID), gocql.UUID(job.ID)).Exec(); err != nil {
		t.Errorf("delete job location: %v", err)
	}
}
