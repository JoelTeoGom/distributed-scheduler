package domain

import "time"

type ScheduleType string

const ScheduleAt ScheduleType = "at"

type Schedule struct {
	Type     ScheduleType
	RunAt    time.Time
	CronExpr string
	Timezone string // IANA name, e.g. "Europe/Madrid"
}
