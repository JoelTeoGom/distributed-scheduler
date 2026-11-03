package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/ports"
)

type ArchiverService struct {
	jobs               ports.JobRepository
	history            ports.JobHistoryRepository
	interval           time.Duration
	batchSize          int
	succeededRetention time.Duration
	deadRetention      time.Duration
}

var _ ports.ArchiverService = (*ArchiverService)(nil)

func NewArchiverService(jobs ports.JobRepository, history ports.JobHistoryRepository, interval time.Duration, batchSize int, succeededRetention, deadRetention time.Duration) *ArchiverService {
	return &ArchiverService{
		jobs:               jobs,
		history:            history,
		interval:           interval,
		batchSize:          batchSize,
		succeededRetention: succeededRetention,
		deadRetention:      deadRetention,
	}
}

func (s *ArchiverService) StartArchiver(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.archiveJobs(ctx)
		}
	}
}

func (s *ArchiverService) archiveJobs(ctx context.Context) {
	for {
		now := time.Now()
		jobs, err := s.jobs.ListArchivableJobs(ctx, now.Add(-s.succeededRetention), now.Add(-s.deadRetention), s.batchSize)
		if err != nil {
			slog.ErrorContext(ctx, "list archivable jobs", "error", err)
			return
		}
		if len(jobs) == 0 {
			return
		}

		// Cassandra goes first: a job deleted before it reached the history would be lost.
		createdJobs, err := s.history.CreateJobs(ctx, jobs)
		if err != nil {
			slog.ErrorContext(ctx, "create job history", "error", err)
			return
		}

		deletedJobs, err := s.jobs.DeleteJobs(ctx, createdJobs)
		if err != nil {
			slog.ErrorContext(ctx, "delete archived jobs", "error", err)
			return
		}
		slog.InfoContext(ctx, "jobs archived", "count", deletedJobs)

		// A short batch means nothing is left. Nothing deleted means Cassandra is failing, and the next
		// list would return the same jobs, so the loop waits for the next tick instead.
		if len(jobs) < s.batchSize || deletedJobs == 0 {
			return
		}
	}
}
