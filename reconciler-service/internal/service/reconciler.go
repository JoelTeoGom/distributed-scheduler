package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/reconciler-service/internal/ports"
)

type ReconcilerService struct {
	db        ports.JobRepository
	interval  time.Duration
	batchSize int
}

var _ ports.ReconcilerService = (*ReconcilerService)(nil)

func NewReconcilerService(db ports.JobRepository, interval time.Duration, batchSize int) *ReconcilerService {
	return &ReconcilerService{db: db, interval: interval, batchSize: batchSize}
}

func (s *ReconcilerService) StartReconciler(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for {
				reconciledJobs, err := s.db.ReconcileJobs(ctx, s.batchSize)
				if err != nil {
					slog.ErrorContext(ctx, "reconcile jobs", "error", err)
					break
				}
				if reconciledJobs > 0 {
					slog.InfoContext(ctx, "jobs reconciled", "count", reconciledJobs)
				}
				// A full batch means more expired jobs are waiting.
				if reconciledJobs < int64(s.batchSize) {
					break
				}
			}
		}

	}
}
