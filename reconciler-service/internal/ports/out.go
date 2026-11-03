package ports

import "context"

type JobRepository interface {
	// ReconcileJobs releases up to jobLimit jobs whose lease expired and returns how many it changed.
	ReconcileJobs(ctx context.Context, jobLimit int) (int64, error)
}

type ReconcilerService interface {
	StartReconciler(ctx context.Context) error
}
