package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/ports"
)

type SchedulerService struct {
	db       ports.SchedulerRepository
	nextRuns ports.NextRunCalculator
	clock    func() time.Time
}

var _ ports.SchedulerService = (*SchedulerService)(nil)

func NewSchedulerService(db ports.SchedulerRepository, nextRuns ports.NextRunCalculator) *SchedulerService {
	return &SchedulerService{db: db, nextRuns: nextRuns}
}

func (s *SchedulerService) Start(ctx context.Context) error {
	scheduleJobsTicker := time.NewTicker(5 * time.Second)
	defer scheduleJobsTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-scheduleJobsTicker.C:
			// A database outage must not stop the scheduler: the next tick retries and the pool reconnects.
			if err := s.db.ScheduleJobs(ctx, time.Now(), s.nextRuns); err != nil {
				slog.ErrorContext(ctx, "schedule jobs", "error", err)
			}
		}
	}

}
