package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/ports"
)

type JobService struct {
	hotJobs  ports.HotJobRepository
	archived ports.JobHistoryRepository
	clock    func() time.Time
}

var _ ports.JobService = (*JobService)(nil)

func NewJobService(hotJobs ports.HotJobRepository, archived ports.JobHistoryRepository, clock func() time.Time) *JobService {
	return &JobService{hotJobs: hotJobs, archived: archived, clock: clock}
}

func (s *JobService) Get(ctx context.Context, tenantID, jobID uuid.UUID) (*domain.Job, error) {
	var (
		hotJob, archivedJob *domain.Job
		hotErr, archivedErr error
		group               errgroup.Group
	)

	// Errors stay outside the group: one store failing must not hide a job found in the other.
	group.Go(func() error {
		hotJob, hotErr = s.hotJobs.Get(ctx, tenantID, jobID)
		return nil
	})
	group.Go(func() error {
		archivedJob, archivedErr = s.archived.Get(ctx, tenantID, jobID)
		return nil
	})
	_ = group.Wait()

	// During the archive handover the job is in both stores, and MySQL wins.
	switch {
	case hotErr == nil:
		return hotJob, nil
	case archivedErr == nil:
		return archivedJob, nil
	case !errors.Is(hotErr, domain.ErrNotFound):
		return nil, hotErr
	default:
		return nil, archivedErr
	}
}

func (s *JobService) History(ctx context.Context, query ports.HistoryQuery) (ports.JobPage, error) {
	var hotJobs, archivedJobs []*domain.Job

	group, groupContext := errgroup.WithContext(ctx)
	group.Go(func() error {
		var err error
		hotJobs, err = s.hotJobs.History(groupContext, query.TenantID, query.TaskID, query.Before, query.Limit)
		return err
	})
	group.Go(func() error {
		var err error
		archivedJobs, err = s.archived.History(groupContext, query.TenantID, query.TaskID, query.Before, query.Limit)
		return err
	})
	if err := group.Wait(); err != nil {
		return ports.JobPage{}, err
	}

	jobs := mergeHistory(hotJobs, archivedJobs, query.Limit)
	page := ports.JobPage{Jobs: jobs}
	if len(jobs) == query.Limit {
		page.NextCursor = jobs[len(jobs)-1].ScheduledAt.UTC().Format(time.RFC3339Nano)
	}
	return page, nil
}

func (s *JobService) ListDead(ctx context.Context, query ports.DeadQuery) (ports.JobPage, error) {
	jobs, err := s.hotJobs.ListDead(ctx, query.TenantID, query.After, query.Limit)
	if err != nil {
		return ports.JobPage{}, err
	}

	page := ports.JobPage{Jobs: jobs}
	if len(jobs) == query.Limit {
		page.NextCursor = jobs[len(jobs)-1].ID.String()
	}
	return page, nil
}

func (s *JobService) Retry(ctx context.Context, tenantID, jobID uuid.UUID) error {
	err := s.hotJobs.Retry(ctx, tenantID, jobID, s.now())
	if !errors.Is(err, domain.ErrNotRetryable) {
		return err
	}

	// The update matched nothing, so only reading the job tells a missing job from a live one.
	if _, err := s.hotJobs.Get(ctx, tenantID, jobID); err != nil {
		return err
	}
	return domain.ErrNotRetryable
}

// DATETIME(3) keeps milliseconds, so the time matches what is stored.
func (s *JobService) now() time.Time {
	return s.clock().UTC().Truncate(time.Millisecond)
}
