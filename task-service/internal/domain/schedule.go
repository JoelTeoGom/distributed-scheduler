package domain

import (
	"strings"
	"time"
)

const (
	defaultTimezone   = "UTC"
	maxTimezoneLength = 64
	maxCronExprLength = 255
	cronFieldCount    = 5
)

type ScheduleType string

const (
	ScheduleAt   ScheduleType = "at"
	ScheduleCron ScheduleType = "cron"
)

type Schedule struct {
	Type     ScheduleType
	RunAt    time.Time
	CronExpr string
	Timezone string // IANA name, e.g. "Europe/Madrid"
}

func (s Schedule) validate(now time.Time) error {
	if err := validateTimezone(s.Timezone); err != nil {
		return err
	}

	switch s.Type {
	case ScheduleAt:
		if s.CronExpr != "" {
			return invalid("an at schedule cannot have cron_expr")
		}
		if !s.RunAt.After(now) {
			return invalid("run_at must be in the future")
		}
	case ScheduleCron:
		if !s.RunAt.IsZero() {
			return invalid("a cron schedule cannot have run_at")
		}
		if len(s.CronExpr) > maxCronExprLength || len(strings.Fields(s.CronExpr)) != cronFieldCount {
			return invalid("cron_expr must have 5 fields")
		}
	default:
		return invalid("schedule type must be at or cron")
	}
	return nil
}

func validateTimezone(timezone string) error {
	// LoadLocation also accepts "Local", which would depend on the server.
	if len(timezone) > maxTimezoneLength || timezone == "Local" {
		return invalid("unknown timezone")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return invalid("unknown timezone")
	}
	return nil
}
