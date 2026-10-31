package httpapi

import (
	"fmt"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/ports"
)

var priorityByName = map[string]domain.Priority{
	"critical": domain.PriorityCritical,
	"high":     domain.PriorityHigh,
	"normal":   domain.PriorityNormal,
	"low":      domain.PriorityLow,
}

var priorityNames = map[domain.Priority]string{
	domain.PriorityCritical: "critical",
	domain.PriorityHigh:     "high",
	domain.PriorityNormal:   "normal",
	domain.PriorityLow:      "low",
}

func parsePriority(name string) (domain.Priority, error) {
	priority, known := priorityByName[name]
	if !known {
		return 0, fmt.Errorf("%w: priority must be critical, high, normal or low", domain.ErrInvalidInput)
	}
	return priority, nil
}

func (r CreateTaskRequest) toInput() (ports.CreateTaskInput, error) {
	priority, err := parsePriority(r.Priority)
	if err != nil {
		return ports.CreateTaskInput{}, err
	}

	return ports.CreateTaskInput{
		Name:     r.Name,
		Schedule: r.Schedule.toDomain(),
		Priority: priority,
		Request:  r.Request.toDomain(),
		Timeout:  secondsToDuration(r.TimeoutSeconds),
		Retry:    r.Retry.toDomain(),
	}, nil
}

func (r UpdateTaskRequest) toInput() (ports.UpdateTaskInput, error) {
	input := ports.UpdateTaskInput{Name: r.Name}

	if r.Schedule != nil {
		schedule := r.Schedule.toDomain()
		input.Schedule = &schedule
	}
	if r.Priority != nil {
		priority, err := parsePriority(*r.Priority)
		if err != nil {
			return ports.UpdateTaskInput{}, err
		}
		input.Priority = &priority
	}
	if r.Request != nil {
		request := r.Request.toDomain()
		input.Request = &request
	}
	if r.TimeoutSeconds != nil {
		timeout := secondsToDuration(*r.TimeoutSeconds)
		input.Timeout = &timeout
	}
	if r.Retry != nil {
		retry := r.Retry.toDomain()
		input.Retry = &retry
	}

	return input, nil
}

func (d ScheduleDTO) toDomain() domain.Schedule {
	schedule := domain.Schedule{
		Type:     domain.ScheduleType(d.Type),
		CronExpr: d.CronExpr,
		Timezone: d.Timezone,
	}
	if d.RunAt != nil {
		schedule.RunAt = d.RunAt.UTC()
	}
	return schedule
}

func (d HTTPRequestDTO) toDomain() domain.Request {
	return domain.Request{
		Method:    d.Method,
		URL:       d.URL,
		Headers:   d.Headers,
		Body:      []byte(d.Body),
		SecretRef: d.SecretRef,
	}
}

func (d RetryDTO) toDomain() domain.RetryPolicy {
	return domain.RetryPolicy{
		MaxAttempts: d.MaxAttempts,
		BackoffBase: secondsToDuration(d.BackoffBaseSeconds),
	}
}

func toTaskResponse(task domain.Task) TaskResponse {
	return TaskResponse{
		ID:       task.ID.String(),
		Name:     task.Name,
		Schedule: toScheduleDTO(task.Schedule),
		Priority: priorityNames[task.Priority],
		Request: HTTPRequestDTO{
			Method:    task.Request.Method,
			URL:       task.Request.URL,
			Headers:   task.Request.Headers,
			Body:      string(task.Request.Body),
			SecretRef: task.Request.SecretRef,
		},
		TimeoutSeconds: durationToSeconds(task.Timeout),
		Retry: RetryDTO{
			MaxAttempts:        task.Retry.MaxAttempts,
			BackoffBaseSeconds: durationToSeconds(task.Retry.BackoffBase),
		},
		Status:    string(task.Status),
		NextRunAt: task.NextRunAt,
		Version:   task.Version,
		CreatedAt: task.CreatedAt,
		UpdatedAt: task.UpdatedAt,
	}
}

func toScheduleDTO(schedule domain.Schedule) ScheduleDTO {
	dto := ScheduleDTO{
		Type:     string(schedule.Type),
		CronExpr: schedule.CronExpr,
		Timezone: schedule.Timezone,
	}
	if !schedule.RunAt.IsZero() {
		dto.RunAt = &schedule.RunAt
	}
	return dto
}

func toTaskPageResponse(page ports.TaskPage) TaskPageResponse {
	tasks := make([]TaskResponse, 0, len(page.Tasks))
	for _, task := range page.Tasks {
		tasks = append(tasks, toTaskResponse(task))
	}
	return TaskPageResponse{Tasks: tasks, NextCursor: page.NextCursor}
}

func secondsToDuration(seconds uint32) time.Duration {
	return time.Duration(seconds) * time.Second
}

func durationToSeconds(duration time.Duration) uint32 {
	return uint32(duration / time.Second)
}
