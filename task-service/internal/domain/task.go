package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maxNameLength = 255
	maxTimeout    = time.Hour
)

// Status is the lifecycle of the task, not the execution status (that lives in the job).
type Status string

const (
	StatusActive  Status = "active"
	StatusPaused  Status = "paused"
	StatusDeleted Status = "deleted"
)

// Task is the configuration a tenant creates. Each execution is a job.
type Task struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Name      string
	Schedule  Schedule
	Priority  Priority
	Request   Request
	Timeout   time.Duration
	Retry     RetryPolicy
	Status    Status
	NextRunAt *time.Time // nil = nothing left to run
	Version   uint32     // optimistic locking
	CreatedAt time.Time
	UpdatedAt time.Time
}

type NewTaskParams struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Name     string
	Schedule Schedule
	Priority Priority
	Request  Request
	Timeout  time.Duration
	Retry    RetryPolicy
}

func NewTask(params NewTaskParams, now time.Time) (Task, error) {
	task := Task{
		ID:        params.ID,
		TenantID:  params.TenantID,
		Status:    StatusActive,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := task.Rename(params.Name); err != nil {
		return Task{}, err
	}
	if err := task.ChangeSchedule(params.Schedule, now); err != nil {
		return Task{}, err
	}
	if err := task.ChangePriority(params.Priority); err != nil {
		return Task{}, err
	}
	if err := task.ChangeRequest(params.Request); err != nil {
		return Task{}, err
	}
	if err := task.ChangeTimeout(params.Timeout); err != nil {
		return Task{}, err
	}
	if err := task.ChangeRetry(params.Retry); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (t *Task) Rename(name string) error {
	name = strings.TrimSpace(name)
	if length := utf8.RuneCountInString(name); length == 0 || length > maxNameLength {
		return invalid("name must be between 1 and 255 characters")
	}
	t.Name = name
	return nil
}

func (t *Task) ChangeSchedule(schedule Schedule, now time.Time) error {
	if schedule.Timezone == "" {
		schedule.Timezone = defaultTimezone
	}
	if err := schedule.validate(now); err != nil {
		return err
	}
	t.Schedule = schedule
	return nil
}

func (t *Task) ChangePriority(priority Priority) error {
	if priority > PriorityLow {
		return invalid("unknown priority")
	}
	t.Priority = priority
	return nil
}

func (t *Task) ChangeRequest(request Request) error {
	request.Method = strings.ToUpper(request.Method)
	if err := request.validate(); err != nil {
		return err
	}
	t.Request = request
	return nil
}

func (t *Task) ChangeTimeout(timeout time.Duration) error {
	if timeout <= 0 || timeout > maxTimeout || timeout%time.Second != 0 {
		return invalid("timeout must be a whole number of seconds, up to one hour")
	}
	t.Timeout = timeout
	return nil
}

func (t *Task) ChangeRetry(retry RetryPolicy) error {
	if err := retry.validate(); err != nil {
		return err
	}
	t.Retry = retry
	return nil
}

func (t *Task) Pause(now time.Time) {
	t.Status = StatusPaused
	t.UpdatedAt = now
}

func (t *Task) Resume(nextRunAt *time.Time, now time.Time) {
	t.Status = StatusActive
	t.NextRunAt = nextRunAt
	t.UpdatedAt = now
}

func (t *Task) Delete(now time.Time) {
	t.Status = StatusDeleted
	t.UpdatedAt = now
}
