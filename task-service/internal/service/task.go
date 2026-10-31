package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/ports"
)

type TaskService struct {
	tasks    ports.TaskRepository
	nextRuns ports.NextRunCalculator
	clock    func() time.Time
}

var _ ports.TaskService = (*TaskService)(nil)

func NewTaskService(tasks ports.TaskRepository, nextRuns ports.NextRunCalculator, clock func() time.Time) *TaskService {
	return &TaskService{tasks: tasks, nextRuns: nextRuns, clock: clock}
}

func (s *TaskService) Create(ctx context.Context, tenantID uuid.UUID, idempotency *ports.IdempotencyKey, input ports.CreateTaskInput) (domain.Task, bool, error) {
	// A retry must replay even if the task would no longer validate, such as an expired run_at.
	if idempotency != nil {
		task, err := s.replay(ctx, tenantID, idempotency)
		if !errors.Is(err, domain.ErrNotFound) {
			return task, err == nil, err
		}
	}

	taskID, err := uuid.NewV7()
	if err != nil {
		return domain.Task{}, false, fmt.Errorf("generate task id: %w", err)
	}

	now := s.now()
	task, err := domain.NewTask(domain.NewTaskParams{
		ID:       taskID,
		TenantID: tenantID,
		Name:     input.Name,
		Schedule: input.Schedule,
		Priority: input.Priority,
		Request:  input.Request,
		Timeout:  input.Timeout,
		Retry:    input.Retry,
	}, now)
	if err != nil {
		return domain.Task{}, false, err
	}

	task.NextRunAt, err = s.nextRun(task.Schedule, now)
	if err != nil {
		return domain.Task{}, false, err
	}

	err = s.tasks.Create(ctx, task, idempotency)
	if errors.Is(err, domain.ErrDuplicateKey) && idempotency != nil {
		task, err := s.replay(ctx, tenantID, idempotency)
		return task, err == nil, err
	}
	if err != nil {
		return domain.Task{}, false, err
	}
	return task, false, nil
}

func (s *TaskService) Get(ctx context.Context, tenantID, taskID uuid.UUID) (domain.Task, error) {
	return s.tasks.GetByID(ctx, tenantID, taskID)
}

func (s *TaskService) List(ctx context.Context, tenantID uuid.UUID, page ports.PageRequest) (ports.TaskPage, error) {
	return s.tasks.List(ctx, tenantID, page)
}

func (s *TaskService) Update(ctx context.Context, tenantID, taskID uuid.UUID, expectedVersion uint32, input ports.UpdateTaskInput) (domain.Task, error) {
	task, err := s.tasks.GetByID(ctx, tenantID, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	if task.Version != expectedVersion {
		return domain.Task{}, domain.ErrVersionConflict
	}

	now := s.now()
	if err := applyChanges(&task, input, now); err != nil {
		return domain.Task{}, err
	}
	task.UpdatedAt = now

	if input.Schedule == nil {
		return saved(task, s.tasks.Update(ctx, task, expectedVersion))
	}

	task.NextRunAt, err = s.nextRun(task.Schedule, now)
	if err != nil {
		return domain.Task{}, err
	}
	return saved(task, s.tasks.Reschedule(ctx, task, expectedVersion))
}

func (s *TaskService) Pause(ctx context.Context, tenantID, taskID uuid.UUID) (domain.Task, error) {
	task, err := s.tasks.GetByID(ctx, tenantID, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	if task.Status == domain.StatusPaused {
		return task, nil
	}

	task.Pause(s.now())
	return saved(task, s.tasks.Update(ctx, task, task.Version))
}

func (s *TaskService) Resume(ctx context.Context, tenantID, taskID uuid.UUID) (domain.Task, error) {
	task, err := s.tasks.GetByID(ctx, tenantID, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	if task.Status != domain.StatusPaused {
		return task, nil
	}

	now := s.now()
	// An at task keeps its pending run: if it is overdue, the scheduler fires it right away.
	nextRunAt := task.NextRunAt
	if task.Schedule.Type == domain.ScheduleCron {
		nextRunAt, err = s.nextRun(task.Schedule, now)
		if err != nil {
			return domain.Task{}, err
		}
	}

	task.Resume(nextRunAt, now)
	return saved(task, s.tasks.Reschedule(ctx, task, task.Version))
}

func (s *TaskService) Delete(ctx context.Context, tenantID, taskID uuid.UUID) error {
	task, err := s.tasks.GetByID(ctx, tenantID, taskID)
	if err != nil {
		return err
	}

	task.Delete(s.now())
	return s.tasks.Update(ctx, task, task.Version)
}

func (s *TaskService) replay(ctx context.Context, tenantID uuid.UUID, idempotency *ports.IdempotencyKey) (domain.Task, error) {
	task, stored, err := s.tasks.GetByIdempotencyKey(ctx, tenantID, idempotency.Key)
	if err != nil {
		return domain.Task{}, err
	}
	if stored.RequestHash != idempotency.RequestHash {
		return domain.Task{}, domain.ErrIdempotencyMismatch
	}
	return task, nil
}

func (s *TaskService) nextRun(schedule domain.Schedule, after time.Time) (*time.Time, error) {
	if schedule.Type == domain.ScheduleAt {
		return &schedule.RunAt, nil
	}

	next, err := s.nextRuns.Next(schedule.CronExpr, schedule.Timezone, after)
	if err != nil {
		return nil, err
	}
	return &next, nil
}

// The repository bumps the version in SQL, so the returned task has to match it.
func saved(task domain.Task, err error) (domain.Task, error) {
	if err != nil {
		return domain.Task{}, err
	}
	task.Version++
	return task, nil
}

// DATETIME(3) keeps milliseconds, so the returned task matches what was stored.
func (s *TaskService) now() time.Time {
	return s.clock().UTC().Truncate(time.Millisecond)
}

func applyChanges(task *domain.Task, input ports.UpdateTaskInput, now time.Time) error {
	if input.Name != nil {
		if err := task.Rename(*input.Name); err != nil {
			return err
		}
	}
	if input.Schedule != nil {
		if err := task.ChangeSchedule(*input.Schedule, now); err != nil {
			return err
		}
	}
	if input.Priority != nil {
		if err := task.ChangePriority(*input.Priority); err != nil {
			return err
		}
	}
	if input.Request != nil {
		if err := task.ChangeRequest(*input.Request); err != nil {
			return err
		}
	}
	if input.Timeout != nil {
		if err := task.ChangeTimeout(*input.Timeout); err != nil {
			return err
		}
	}
	if input.Retry != nil {
		if err := task.ChangeRetry(*input.Retry); err != nil {
			return err
		}
	}
	return nil
}
