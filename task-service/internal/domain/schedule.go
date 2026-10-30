package domain

import "time"

type ScheduleType string

const (
	ScheduleAt   ScheduleType = "at"
	ScheduleCron ScheduleType = "cron"
)

// Schedule says when the task fires.
// at:   RunAt is set, CronExpr is empty.
// cron: CronExpr is set (5 fields), RunAt is zero, Timezone is used to evaluate it.
type Schedule struct {
	Type     ScheduleType
	RunAt    time.Time
	CronExpr string
	Timezone string // IANA name, e.g. "Europe/Madrid"
}
