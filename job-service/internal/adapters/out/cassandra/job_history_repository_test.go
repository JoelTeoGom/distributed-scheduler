//go:build integration

// Needs Cassandra on 127.0.0.1:9042 with the scheduler keyspace and the migrations applied.
package cassandra

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
)

var testNow = time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)

type fixture struct {
	ctx        context.Context
	session    *gocql.Session
	repository *JobHistoryRepository
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	session, err := Open([]string{"127.0.0.1"}, "scheduler")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	return fixture{
		ctx:        context.Background(),
		session:    session,
		repository: NewJobHistoryRepository(session, func() time.Time { return testNow }),
	}
}

func (f fixture) insertHistory(t *testing.T, tenantID, taskID uuid.UUID, scheduledAt time.Time) uuid.UUID {
	t.Helper()
	jobID := uuid.New()
	err := f.session.Query(`
		INSERT INTO job_history (tenant_id, task_id, bucket, scheduled_at, job_id, status, attempt, priority)
		VALUES (?, ?, ?, ?, ?, 'succeeded', 1, 2)`,
		gocql.UUID(tenantID), gocql.UUID(taskID), bucketOf(startOfMonth(scheduledAt)), scheduledAt, gocql.UUID(jobID)).Exec()
	if err != nil {
		t.Fatal(err)
	}
	return jobID
}

func (f fixture) insertLocation(t *testing.T, tenantID, jobID, taskID uuid.UUID, scheduledAt time.Time) {
	t.Helper()
	err := f.session.Query(`
		INSERT INTO job_by_id (tenant_id, job_id, task_id, bucket, scheduled_at)
		VALUES (?, ?, ?, ?, ?)`,
		gocql.UUID(tenantID), gocql.UUID(jobID), gocql.UUID(taskID), bucketOf(startOfMonth(scheduledAt)), scheduledAt).Exec()
	if err != nil {
		t.Fatal(err)
	}
}

func jobIDs(jobs []*domain.Job) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, job.ID)
	}
	return ids
}

func TestGetMapsEveryColumn(t *testing.T) {
	f := newFixture(t)
	tenantID, taskID, jobID := uuid.New(), uuid.New(), uuid.New()
	scheduledAt := time.Date(2026, 10, 30, 10, 0, 0, 123000000, time.UTC)
	startedAt, finishedAt := scheduledAt.Add(time.Second), scheduledAt.Add(2*time.Second)

	err := f.session.Query(`
		INSERT INTO job_history (tenant_id, task_id, bucket, scheduled_at, job_id, status, attempt, priority, http_status, error, response_body, started_at, finished_at)
		VALUES (?, ?, 202610, ?, ?, 'dead', 200, 1, 503, 'upstream failed', ?, ?, ?)`,
		gocql.UUID(tenantID), gocql.UUID(taskID), scheduledAt, gocql.UUID(jobID), []byte("boom"), startedAt, finishedAt).Exec()
	if err != nil {
		t.Fatal(err)
	}
	f.insertLocation(t, tenantID, jobID, taskID, scheduledAt)

	job, err := f.repository.Get(f.ctx, tenantID, jobID)
	if err != nil {
		t.Fatal(err)
	}

	if job.ID != jobID || job.TenantID != tenantID || job.TaskID != taskID {
		t.Errorf("identity: %+v", job)
	}
	if !job.ScheduledAt.Equal(scheduledAt) || job.ScheduledAt.Location() != time.UTC {
		t.Errorf("scheduled at: %v", job.ScheduledAt)
	}
	if job.Status != domain.StatusDead || job.Attempt != 200 || job.Config.Priority != domain.PriorityHigh {
		t.Errorf("state: %+v", job)
	}
	if *job.Result.HTTPStatus != 503 || job.Result.Error != "upstream failed" || string(job.Result.ResponseBody) != "boom" {
		t.Errorf("result: %+v", job.Result)
	}
	if !job.Result.StartedAt.Equal(startedAt) || !job.Result.FinishedAt.Equal(finishedAt) {
		t.Errorf("times: %+v", job.Result)
	}
}

func TestGetKeepsNullColumnsEmpty(t *testing.T) {
	f := newFixture(t)
	tenantID, taskID := uuid.New(), uuid.New()
	scheduledAt := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	jobID := f.insertHistory(t, tenantID, taskID, scheduledAt)
	f.insertLocation(t, tenantID, jobID, taskID, scheduledAt)

	job, err := f.repository.Get(f.ctx, tenantID, jobID)
	if err != nil {
		t.Fatal(err)
	}

	result := job.Result
	if result.HTTPStatus != nil || result.StartedAt != nil || result.FinishedAt != nil || result.Error != "" || len(result.ResponseBody) != 0 {
		t.Errorf("result: %+v", result)
	}
}

func TestGetReportsNotFound(t *testing.T) {
	f := newFixture(t)
	tenantID, taskID := uuid.New(), uuid.New()
	scheduledAt := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	jobID := f.insertHistory(t, tenantID, taskID, scheduledAt)
	f.insertLocation(t, tenantID, jobID, taskID, scheduledAt)
	expiredJobID := uuid.New()
	f.insertLocation(t, tenantID, expiredJobID, taskID, scheduledAt)

	testCases := map[string][2]uuid.UUID{
		"another tenant":       {uuid.New(), jobID},
		"missing job":          {tenantID, uuid.New()},
		"location without job": {tenantID, expiredJobID},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			if _, err := f.repository.Get(f.ctx, testCase[0], testCase[1]); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("got %v", err)
			}
		})
	}
}

func TestHistoryCrossesMonthsNewestFirst(t *testing.T) {
	f := newFixture(t)
	tenantID, taskID := uuid.New(), uuid.New()
	want := []uuid.UUID{
		f.insertHistory(t, tenantID, taskID, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)),
		f.insertHistory(t, tenantID, taskID, time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)),
		f.insertHistory(t, tenantID, taskID, time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)),
		f.insertHistory(t, tenantID, taskID, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)),
		f.insertHistory(t, tenantID, taskID, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)),
	}
	f.insertHistory(t, tenantID, taskID, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC))
	f.insertHistory(t, tenantID, uuid.New(), time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC))

	everything, err := f.repository.History(f.ctx, tenantID, taskID, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(jobIDs(everything), want) {
		t.Errorf("got %v\nwant %v", jobIDs(everything), want)
	}

	firstPage, err := f.repository.History(f.ctx, tenantID, taskID, nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(jobIDs(firstPage), want[:3]) {
		t.Fatalf("first page: %v", jobIDs(firstPage))
	}

	secondPage, err := f.repository.History(f.ctx, tenantID, taskID, &firstPage[2].ScheduledAt, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(jobIDs(secondPage), want[3:]) {
		t.Errorf("second page: %v", jobIDs(secondPage))
	}
}

func TestHistoryIsolatesTenants(t *testing.T) {
	f := newFixture(t)
	tenantID, taskID := uuid.New(), uuid.New()
	f.insertHistory(t, tenantID, taskID, time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))

	jobs, err := f.repository.History(f.ctx, uuid.New(), taskID, nil, 50)

	if err != nil || len(jobs) != 0 {
		t.Errorf("another tenant saw %d jobs, err %v", len(jobs), err)
	}
}

func TestHistorySkipsEmptyMonths(t *testing.T) {
	f := newFixture(t)
	tenantID, taskID := uuid.New(), uuid.New()
	september := f.insertHistory(t, tenantID, taskID, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))

	jobs, err := f.repository.History(f.ctx, tenantID, taskID, nil, 10)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(jobIDs(jobs), []uuid.UUID{september}) {
		t.Errorf("got %v", jobIDs(jobs))
	}
}

func TestHistoryIsEmptyForACursorOlderThanTheWindow(t *testing.T) {
	f := newFixture(t)
	tenantID, taskID := uuid.New(), uuid.New()
	f.insertHistory(t, tenantID, taskID, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC))
	before := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	jobs, err := f.repository.History(f.ctx, tenantID, taskID, &before, 10)

	if err != nil || len(jobs) != 0 {
		t.Errorf("got %d jobs, err %v", len(jobs), err)
	}
}

func TestACancelledContextStopsTheQueries(t *testing.T) {
	f := newFixture(t)
	tenantID, taskID := uuid.New(), uuid.New()
	scheduledAt := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	jobID := f.insertHistory(t, tenantID, taskID, scheduledAt)
	f.insertLocation(t, tenantID, jobID, taskID, scheduledAt)
	cancelled, cancel := context.WithCancel(f.ctx)
	cancel()

	if _, err := f.repository.Get(cancelled, tenantID, jobID); !errors.Is(err, context.Canceled) {
		t.Errorf("get: %v", err)
	}
	if _, err := f.repository.History(cancelled, tenantID, taskID, nil, 10); !errors.Is(err, context.Canceled) {
		t.Errorf("history: %v", err)
	}
}

func TestBucketsUseUTCAndCrossYears(t *testing.T) {
	madrid := time.FixedZone("CET", 3600)

	if got := bucketOf(startOfMonth(time.Date(2026, 11, 1, 0, 30, 0, 0, madrid))); got != 202610 {
		t.Errorf("bucket in UTC: %d", got)
	}
	if got := bucketOf(startOfMonth(time.Date(2027, 1, 20, 0, 0, 0, 0, time.UTC)).AddDate(0, -2, 0)); got != 202611 {
		t.Errorf("year boundary: %d", got)
	}
}
