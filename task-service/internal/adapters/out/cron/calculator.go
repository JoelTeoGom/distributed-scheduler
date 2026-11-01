package cron

import (
	"fmt"
	"time"

	cronlib "github.com/robfig/cron/v3"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/ports"
)

var parser = cronlib.NewParser(cronlib.Minute | cronlib.Hour | cronlib.Dom | cronlib.Month | cronlib.Dow)

type Calculator struct{}

var _ ports.NextRunCalculator = Calculator{}

func NewCalculator() Calculator {
	return Calculator{}
}

func (Calculator) Next(cronExpr, timezone string, after time.Time) (time.Time, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: unknown timezone", domain.ErrInvalidInput)
	}

	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: invalid cron_expr", domain.ErrInvalidInput)
	}

	next := schedule.Next(after.In(location))
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("%w: cron_expr never fires", domain.ErrInvalidInput)
	}
	return next.UTC(), nil
}
