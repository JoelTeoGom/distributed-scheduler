package domain

import (
	"time"

	"github.com/google/uuid"
)

// Job is a terminal job on its way from the MySQL hot table to the Cassandra history.
type Job struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	TaskID      uuid.UUID
	ScheduledAt time.Time
	Priority    Priority
	Status      Status
	Attempt     int
	Result      Result
}

// Only terminal jobs are archived: pending and running ones still change.
type Status string

const (
	StatusSucceeded Status = "succeeded"
	StatusDead      Status = "dead"
)

// Priority is numeric: lower = more urgent.
type Priority uint8
