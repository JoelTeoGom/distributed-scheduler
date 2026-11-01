package ports

import "time"

type NextRunCalculator interface {
	Next(cronExpr, timezone string, after time.Time) (time.Time, error)
}
