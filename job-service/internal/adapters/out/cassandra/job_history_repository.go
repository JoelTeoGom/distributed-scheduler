package cassandra

import (
	"context"
	"errors"
	"fmt"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/ports"
)

// Rows live 30 days after archiving and dead jobs wait in MySQL first, so three calendar months cover them.
const searchedMonths = 3

type JobHistoryRepository struct {
	session *gocql.Session
	clock   func() time.Time
}

var _ ports.JobHistoryRepository = (*JobHistoryRepository)(nil)

func NewJobHistoryRepository(session *gocql.Session, clock func() time.Time) *JobHistoryRepository {
	return &JobHistoryRepository{session: session, clock: clock}
}

func (r *JobHistoryRepository) Get(ctx context.Context, tenantID, jobID uuid.UUID) (*domain.Job, error) {
	var (
		taskID      gocql.UUID
		bucket      int
		scheduledAt time.Time
	)

	err := r.session.Query(`
		SELECT task_id, bucket, scheduled_at
		FROM job_by_id
		WHERE tenant_id = ? AND job_id = ?`,
		gocql.UUID(tenantID), gocql.UUID(jobID)).
		ScanContext(ctx, &taskID, &bucket, &scheduledAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("cassandra: select job location: %w", err)
	}

	var row jobRow
	err = r.session.Query(`
		SELECT `+jobColumns()+`
		FROM job_history
		WHERE tenant_id = ? AND task_id = ? AND bucket = ? AND scheduled_at = ? AND job_id = ?`,
		gocql.UUID(tenantID), taskID, bucket, scheduledAt, gocql.UUID(jobID)).
		ScanContext(ctx, row.destinations()...)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("cassandra: select job: %w", err)
	}
	return row.toJob(), nil
}

func (r *JobHistoryRepository) History(ctx context.Context, tenantID, taskID uuid.UUID, before *time.Time, limit int) ([]*domain.Job, error) {
	currentMonth := startOfMonth(r.clock())
	oldestMonth := currentMonth.AddDate(0, 1-searchedMonths, 0)

	month := currentMonth
	if before != nil {
		month = startOfMonth(*before)
	}

	var jobs []*domain.Job
	for ; !month.Before(oldestMonth) && len(jobs) < limit; month = month.AddDate(0, -1, 0) {
		monthJobs, err := r.historyOfMonth(ctx, tenantID, taskID, bucketOf(month), before, limit-len(jobs))
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, monthJobs...)
	}
	return jobs, nil
}

func (r *JobHistoryRepository) historyOfMonth(ctx context.Context, tenantID, taskID uuid.UUID, bucket int, before *time.Time, limit int) ([]*domain.Job, error) {
	query := r.session.Query(`
		SELECT `+jobColumns()+`
		FROM job_history
		WHERE tenant_id = ? AND task_id = ? AND bucket = ?
		LIMIT ?`,
		gocql.UUID(tenantID), gocql.UUID(taskID), bucket, limit)

	if before != nil {
		query = r.session.Query(`
			SELECT `+jobColumns()+`
			FROM job_history
			WHERE tenant_id = ? AND task_id = ? AND bucket = ? AND scheduled_at < ?
			LIMIT ?`,
			gocql.UUID(tenantID), gocql.UUID(taskID), bucket, *before, limit)
	}

	iterator := query.IterContext(ctx)

	var jobs []*domain.Job
	for {
		var row jobRow
		if !iterator.Scan(row.destinations()...) {
			break
		}
		jobs = append(jobs, row.toJob())
	}
	if err := iterator.Close(); err != nil {
		return nil, fmt.Errorf("cassandra: list jobs: %w", err)
	}
	return jobs, nil
}

func startOfMonth(moment time.Time) time.Time {
	moment = moment.UTC()
	return time.Date(moment.Year(), moment.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func bucketOf(month time.Time) int {
	return month.Year()*100 + int(month.Month())
}
