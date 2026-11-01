package ports

import (
	"context"
	"time"
)

type NextRunCalculator interface {
	Next(cronExpr, timezone string, after time.Time) (time.Time, error)
}

type SchedulerRepository interface {
	ScheduleJobs(ctx context.Context, now time.Time) error
}

type SchedulerService interface {
	Start(ctx context.Context) error
}
