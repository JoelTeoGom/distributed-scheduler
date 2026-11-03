package cassandra

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	gocql "github.com/apache/cassandra-gocql-driver/v2"

	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/ports"
)

const maxConcurrentInserts = 64

type JobHistoryRepository struct {
	session *gocql.Session
}

var _ ports.JobHistoryRepository = (*JobHistoryRepository)(nil)

func NewJobHistoryRepository(session *gocql.Session) *JobHistoryRepository {
	return &JobHistoryRepository{session: session}
}

func (r *JobHistoryRepository) CreateJobs(ctx context.Context, jobs []*domain.Job) ([]*domain.Job, error) {
	var (
		createdJobs []*domain.Job
		mu          sync.Mutex
		wg          sync.WaitGroup
	)

	// Caps the inserts in flight, so a big batch never opens thousands of requests to Cassandra.
	sem := make(chan struct{}, maxConcurrentInserts)
	for _, job := range jobs {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := r.createJob(ctx, job); err != nil {
				// The job stays in MySQL and the next sweep writes it again.
				slog.ErrorContext(ctx, "create job history", "job_id", job.ID, "error", err)
				return
			}

			mu.Lock()
			createdJobs = append(createdJobs, job)
			mu.Unlock()
		}()
	}

	wg.Wait()
	return createdJobs, nil
}

// job_history goes first, so job_by_id never points to a row that is not there.
func (r *JobHistoryRepository) createJob(ctx context.Context, job *domain.Job) error {
	bucket := bucketOf(job.ScheduledAt)

	err := r.session.Query(`
		INSERT INTO job_history (
			tenant_id, task_id, bucket, scheduled_at, job_id,
			status, attempt, priority,
			http_status, error, response_body,
			started_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		gocql.UUID(job.TenantID), gocql.UUID(job.TaskID), bucket, job.ScheduledAt, gocql.UUID(job.ID),
		string(job.Status), int16(job.Attempt), int8(job.Priority),
		httpStatusValue(job.Result.HTTPStatus), textValue(job.Result.Error), bytesValue(job.Result.ResponseBody),
		timeValue(job.Result.StartedAt), timeValue(job.Result.FinishedAt)).
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("cassandra: insert job history: %w", err)
	}

	err = r.session.Query(`
		INSERT INTO job_by_id (tenant_id, job_id, task_id, bucket, scheduled_at)
		VALUES (?, ?, ?, ?, ?)`,
		gocql.UUID(job.TenantID), gocql.UUID(job.ID), gocql.UUID(job.TaskID), bucket, job.ScheduledAt).
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("cassandra: insert job location: %w", err)
	}
	return nil
}
