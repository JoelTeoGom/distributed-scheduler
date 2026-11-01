package service_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/ports"
	"github.com/JoelTeoGom/distributed-scheduler/task-service/internal/service"
)

var (
	testNow    = time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	testTenant = uuid.MustParse("11111111-1111-1111-1111-111111111111")
)

type storedTask struct {
	task        domain.Task
	idempotency *ports.IdempotencyKey
}

// fakeRepository mirrors the SQL rules: version bumps, tenant scoping and hidden deleted tasks.
type fakeRepository struct {
	tasks             map[uuid.UUID]*storedTask
	rescheduleCalls   int
	updateCalls       int
	raceOnIdempotency *domain.Task
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{tasks: map[uuid.UUID]*storedTask{}}
}

func (f *fakeRepository) Create(_ context.Context, task domain.Task, idempotency *ports.IdempotencyKey) error {
	if f.raceOnIdempotency != nil {
		f.tasks[f.raceOnIdempotency.ID] = &storedTask{task: *f.raceOnIdempotency, idempotency: idempotency}
		f.raceOnIdempotency = nil
		return domain.ErrDuplicateKey
	}
	if idempotency != nil {
		for _, stored := range f.tasks {
			if stored.idempotency != nil && stored.task.TenantID == task.TenantID && stored.idempotency.Key == idempotency.Key {
				return domain.ErrDuplicateKey
			}
		}
	}
	f.tasks[task.ID] = &storedTask{task: task, idempotency: idempotency}
	return nil
}

func (f *fakeRepository) GetByID(_ context.Context, tenantID, taskID uuid.UUID) (domain.Task, error) {
	stored, found := f.tasks[taskID]
	if !found || stored.task.TenantID != tenantID || stored.task.Status == domain.StatusDeleted {
		return domain.Task{}, domain.ErrNotFound
	}
	return stored.task, nil
}

func (f *fakeRepository) GetByIdempotencyKey(_ context.Context, tenantID uuid.UUID, key string) (domain.Task, ports.IdempotencyKey, error) {
	if f.raceOnIdempotency != nil {
		return domain.Task{}, ports.IdempotencyKey{}, domain.ErrNotFound
	}
	for _, stored := range f.tasks {
		if stored.idempotency != nil && stored.task.TenantID == tenantID && stored.idempotency.Key == key {
			return stored.task, *stored.idempotency, nil
		}
	}
	return domain.Task{}, ports.IdempotencyKey{}, domain.ErrNotFound
}

func (f *fakeRepository) List(_ context.Context, tenantID uuid.UUID, _ ports.PageRequest) (ports.TaskPage, error) {
	var tasks []domain.Task
	for _, stored := range f.tasks {
		if stored.task.TenantID == tenantID && stored.task.Status != domain.StatusDeleted {
			tasks = append(tasks, stored.task)
		}
	}
	return ports.TaskPage{Tasks: tasks}, nil
}

func (f *fakeRepository) Update(_ context.Context, task domain.Task, expectedVersion uint32) error {
	f.updateCalls++
	stored, found := f.tasks[task.ID]
	if !found || stored.task.Version != expectedVersion {
		return domain.ErrVersionConflict
	}
	nextRunAt := stored.task.NextRunAt
	stored.task = task
	stored.task.NextRunAt = nextRunAt
	stored.task.Version = expectedVersion + 1
	return nil
}

func (f *fakeRepository) Reschedule(_ context.Context, task domain.Task, expectedVersion uint32) error {
	f.rescheduleCalls++
	stored, found := f.tasks[task.ID]
	if !found || stored.task.Version != expectedVersion {
		return domain.ErrVersionConflict
	}
	stored.task = task
	stored.task.Version = expectedVersion + 1
	return nil
}

type fakeCalculator struct {
	interval time.Duration
	err      error
}

func (f fakeCalculator) Next(_, _ string, after time.Time) (time.Time, error) {
	if f.err != nil {
		return time.Time{}, f.err
	}
	return after.Add(f.interval), nil
}

type fixture struct {
	repository *fakeRepository
	service    *service.TaskService
	now        *time.Time
}

func newFixture() fixture {
	repository := newFakeRepository()
	now := testNow
	taskService := service.NewTaskService(repository, fakeCalculator{interval: time.Hour}, func() time.Time { return now })
	return fixture{repository: repository, service: taskService, now: &now}
}

func cronInput() ports.CreateTaskInput {
	return ports.CreateTaskInput{
		Name:     "  nightly report  ",
		Schedule: domain.Schedule{Type: domain.ScheduleCron, CronExpr: "0 9 * * *", Timezone: "Europe/Madrid"},
		Priority: domain.PriorityNormal,
		Request:  domain.Request{Method: "post", URL: "https://api.example.com/hook"},
		Timeout:  30 * time.Second,
		Retry:    domain.RetryPolicy{MaxAttempts: 3, BackoffBase: 10 * time.Second},
	}
}

func atInput() ports.CreateTaskInput {
	input := cronInput()
	input.Schedule = domain.Schedule{Type: domain.ScheduleAt, RunAt: testNow.Add(24 * time.Hour)}
	return input
}

func createTask(t *testing.T, f fixture, input ports.CreateTaskInput) domain.Task {
	t.Helper()
	task, alreadyCreated, err := f.service.Create(context.Background(), testTenant, nil, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if alreadyCreated {
		t.Fatal("a new task must not be reported as already created")
	}
	return task
}

func idempotencyKey(body string) *ports.IdempotencyKey {
	return &ports.IdempotencyKey{Key: "request-1", RequestHash: sha256.Sum256([]byte(body))}
}

func TestCreateCronTask(t *testing.T) {
	task := createTask(t, newFixture(), cronInput())

	if task.Name != "nightly report" || task.Request.Method != "POST" {
		t.Errorf("not normalized: %q %q", task.Name, task.Request.Method)
	}
	if task.Status != domain.StatusActive || task.Version != 1 {
		t.Errorf("status %s version %d", task.Status, task.Version)
	}
	if task.ID.Version() != 7 {
		t.Errorf("id must be UUIDv7, got v%d", task.ID.Version())
	}
	if task.NextRunAt == nil || !task.NextRunAt.Equal(testNow.Add(time.Hour)) {
		t.Errorf("next run: %v", task.NextRunAt)
	}
}

func TestCreateAtTaskRunsAtItsDateInUTCByDefault(t *testing.T) {
	task := createTask(t, newFixture(), atInput())

	if task.NextRunAt == nil || !task.NextRunAt.Equal(testNow.Add(24*time.Hour)) {
		t.Errorf("next run: %v", task.NextRunAt)
	}
	if task.Schedule.Timezone != "UTC" {
		t.Errorf("timezone: %q", task.Schedule.Timezone)
	}
}

func TestCreateTruncatesTimestampsToMilliseconds(t *testing.T) {
	f := newFixture()
	*f.now = testNow.Add(1234567 * time.Nanosecond)

	task := createTask(t, f, cronInput())

	if want := testNow.Add(time.Millisecond); !task.CreatedAt.Equal(want) {
		t.Errorf("created at %v, want %v", task.CreatedAt, want)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	testCases := map[string]func(*ports.CreateTaskInput){
		"empty name":             func(in *ports.CreateTaskInput) { in.Name = "   " },
		"long name":              func(in *ports.CreateTaskInput) { in.Name = strings.Repeat("a", 256) },
		"unknown schedule type":  func(in *ports.CreateTaskInput) { in.Schedule.Type = "weekly" },
		"cron with four fields":  func(in *ports.CreateTaskInput) { in.Schedule.CronExpr = "* * * *" },
		"cron with timezone":     func(in *ports.CreateTaskInput) { in.Schedule.CronExpr = "CRON_TZ=UTC * * * * *" },
		"cron with run_at":       func(in *ports.CreateTaskInput) { in.Schedule.RunAt = testNow.Add(time.Hour) },
		"unknown timezone":       func(in *ports.CreateTaskInput) { in.Schedule.Timezone = "Mars/Olympus" },
		"local timezone":         func(in *ports.CreateTaskInput) { in.Schedule.Timezone = "Local" },
		"unknown priority":       func(in *ports.CreateTaskInput) { in.Priority = domain.PriorityLow + 1 },
		"unknown method":         func(in *ports.CreateTaskInput) { in.Request.Method = "TRACE" },
		"http url":               func(in *ports.CreateTaskInput) { in.Request.URL = "http://api.example.com" },
		"url without host":       func(in *ports.CreateTaskInput) { in.Request.URL = "https://" },
		"url with credentials":   func(in *ports.CreateTaskInput) { in.Request.URL = "https://user:pass@example.com" },
		"long url":               func(in *ports.CreateTaskInput) { in.Request.URL = "https://a.com/" + strings.Repeat("a", 2048) },
		"localhost":              func(in *ports.CreateTaskInput) { in.Request.URL = "https://localhost:8080/x" },
		"localhost trailing dot": func(in *ports.CreateTaskInput) { in.Request.URL = "https://LOCALHOST./x" },
		"internal domain":        func(in *ports.CreateTaskInput) { in.Request.URL = "https://db.internal/x" },
		"loopback":               func(in *ports.CreateTaskInput) { in.Request.URL = "https://127.0.0.1/x" },
		"private network":        func(in *ports.CreateTaskInput) { in.Request.URL = "https://10.0.0.5/x" },
		"aws metadata":           func(in *ports.CreateTaskInput) { in.Request.URL = "https://169.254.169.254/latest" },
		"unspecified":            func(in *ports.CreateTaskInput) { in.Request.URL = "https://0.0.0.0/x" },
		"ipv6 loopback":          func(in *ports.CreateTaskInput) { in.Request.URL = "https://[::1]/x" },
		"ipv4 mapped ipv6":       func(in *ports.CreateTaskInput) { in.Request.URL = "https://[::ffff:10.0.0.5]/x" },
		"authorization header": func(in *ports.CreateTaskInput) {
			in.Request.Headers = map[string]string{"AUTHORIZATION": "Bearer x"}
		},
		"long secret ref":    func(in *ports.CreateTaskInput) { in.Request.SecretRef = strings.Repeat("a", 256) },
		"zero timeout":       func(in *ports.CreateTaskInput) { in.Timeout = 0 },
		"timeout over limit": func(in *ports.CreateTaskInput) { in.Timeout = 2 * time.Hour },
		"fractional timeout": func(in *ports.CreateTaskInput) { in.Timeout = 1500 * time.Millisecond },
		"zero attempts":      func(in *ports.CreateTaskInput) { in.Retry.MaxAttempts = 0 },
		"too many attempts":  func(in *ports.CreateTaskInput) { in.Retry.MaxAttempts = 256 },
		"zero backoff":       func(in *ports.CreateTaskInput) { in.Retry.BackoffBase = 0 },
		"fractional backoff": func(in *ports.CreateTaskInput) { in.Retry.BackoffBase = 500 * time.Millisecond },
		"backoff over limit": func(in *ports.CreateTaskInput) { in.Retry.BackoffBase = 48 * time.Hour },
	}

	for name, mutate := range testCases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			input := cronInput()
			mutate(&input)

			_, _, err := f.service.Create(context.Background(), testTenant, nil, input)

			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("want ErrInvalidInput, got %v", err)
			}
			if len(f.repository.tasks) != 0 {
				t.Error("an invalid task must not be stored")
			}
		})
	}
}

func TestCreateRejectsInvalidAtSchedules(t *testing.T) {
	testCases := map[string]domain.Schedule{
		"in the past":  {Type: domain.ScheduleAt, RunAt: testNow.Add(-time.Second)},
		"right now":    {Type: domain.ScheduleAt, RunAt: testNow},
		"without date": {Type: domain.ScheduleAt},
		"with cron":    {Type: domain.ScheduleAt, RunAt: testNow.Add(time.Hour), CronExpr: "* * * * *"},
	}

	for name, schedule := range testCases {
		t.Run(name, func(t *testing.T) {
			input := atInput()
			input.Schedule = schedule

			_, _, err := newFixture().service.Create(context.Background(), testTenant, nil, input)

			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("want ErrInvalidInput, got %v", err)
			}
		})
	}
}

func TestCreateAcceptsPublicEndpoints(t *testing.T) {
	for _, endpoint := range []string{"https://api.example.com/hook?a=1", "https://8.8.8.8/x", "https://[2606:4700::1111]/x", "https://localhost.example.com"} {
		t.Run(endpoint, func(t *testing.T) {
			input := cronInput()
			input.Request.URL = endpoint
			if _, _, err := newFixture().service.Create(context.Background(), testTenant, nil, input); err != nil {
				t.Errorf("should be accepted: %v", err)
			}
		})
	}
}

func TestCreatePropagatesCalculatorError(t *testing.T) {
	repository := newFakeRepository()
	calculatorError := errors.New("never fires")
	taskService := service.NewTaskService(repository, fakeCalculator{err: calculatorError}, func() time.Time { return testNow })

	_, _, err := taskService.Create(context.Background(), testTenant, nil, cronInput())

	if !errors.Is(err, calculatorError) || len(repository.tasks) != 0 {
		t.Errorf("got %v with %d stored tasks", err, len(repository.tasks))
	}
}

func TestCreateReturnsTheExistingTaskForTheSameRequest(t *testing.T) {
	f := newFixture()
	ctx := context.Background()

	first, alreadyCreated, err := f.service.Create(ctx, testTenant, idempotencyKey("body"), cronInput())
	if err != nil || alreadyCreated {
		t.Fatalf("first: alreadyCreated=%v err=%v", alreadyCreated, err)
	}

	second, alreadyCreated, err := f.service.Create(ctx, testTenant, idempotencyKey("body"), cronInput())
	if err != nil || !alreadyCreated {
		t.Fatalf("second: alreadyCreated=%v err=%v", alreadyCreated, err)
	}
	if second.ID != first.ID || len(f.repository.tasks) != 1 {
		t.Errorf("the retry created a new task: %v vs %v", second.ID, first.ID)
	}
}

func TestCreateRejectsAKeyReusedWithAnotherBody(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	if _, _, err := f.service.Create(ctx, testTenant, idempotencyKey("body"), cronInput()); err != nil {
		t.Fatal(err)
	}

	_, alreadyCreated, err := f.service.Create(ctx, testTenant, idempotencyKey("another body"), cronInput())

	if !errors.Is(err, domain.ErrIdempotencyMismatch) || alreadyCreated {
		t.Errorf("alreadyCreated=%v err=%v", alreadyCreated, err)
	}
}

func TestCreateReturnsAnExistingAtTaskWhoseDateHasPassed(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	first, _, err := f.service.Create(ctx, testTenant, idempotencyKey("body"), atInput())
	if err != nil {
		t.Fatal(err)
	}

	*f.now = testNow.Add(48 * time.Hour)
	second, alreadyCreated, err := f.service.Create(ctx, testTenant, idempotencyKey("body"), atInput())

	if err != nil || !alreadyCreated || second.ID != first.ID {
		t.Errorf("alreadyCreated=%v err=%v", alreadyCreated, err)
	}
}

func TestCreateReturnsTheWinnerWhenAConcurrentRequestWinsTheInsert(t *testing.T) {
	f := newFixture()
	winner := createTask(t, newFixture(), cronInput())
	f.repository.raceOnIdempotency = &winner

	task, alreadyCreated, err := f.service.Create(context.Background(), testTenant, idempotencyKey("body"), cronInput())

	if err != nil || !alreadyCreated || task.ID != winner.ID {
		t.Errorf("alreadyCreated=%v err=%v id=%v", alreadyCreated, err, task.ID)
	}
}

func TestUpdateWithoutScheduleLeavesNextRunToTheScheduler(t *testing.T) {
	f := newFixture()
	task := createTask(t, f, cronInput())
	name := "renamed"
	*f.now = testNow.Add(time.Minute)

	updated, err := f.service.Update(context.Background(), testTenant, task.ID, 1, ports.UpdateTaskInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}

	if updated.Name != "renamed" || updated.Version != 2 || !updated.UpdatedAt.Equal(*f.now) {
		t.Errorf("updated: %+v", updated)
	}
	if f.repository.updateCalls != 1 || f.repository.rescheduleCalls != 0 {
		t.Errorf("update=%d reschedule=%d", f.repository.updateCalls, f.repository.rescheduleCalls)
	}
	if !updated.CreatedAt.Equal(testNow) {
		t.Errorf("created at changed: %v", updated.CreatedAt)
	}
}

func TestUpdateWithScheduleReschedules(t *testing.T) {
	f := newFixture()
	task := createTask(t, f, cronInput())
	*f.now = testNow.Add(30 * time.Minute)
	schedule := domain.Schedule{Type: domain.ScheduleAt, RunAt: testNow.Add(10 * time.Hour)}

	updated, err := f.service.Update(context.Background(), testTenant, task.ID, 1, ports.UpdateTaskInput{Schedule: &schedule})
	if err != nil {
		t.Fatal(err)
	}

	if f.repository.rescheduleCalls != 1 || !updated.NextRunAt.Equal(schedule.RunAt) {
		t.Errorf("reschedule=%d next=%v", f.repository.rescheduleCalls, updated.NextRunAt)
	}
	if updated.Schedule.Timezone != "UTC" {
		t.Errorf("timezone: %q", updated.Schedule.Timezone)
	}
}

func TestUpdateRejectsAStaleVersion(t *testing.T) {
	f := newFixture()
	task := createTask(t, f, cronInput())
	name := "renamed"

	_, err := f.service.Update(context.Background(), testTenant, task.ID, 9, ports.UpdateTaskInput{Name: &name})

	if !errors.Is(err, domain.ErrVersionConflict) || f.repository.updateCalls != 0 {
		t.Errorf("err=%v writes=%d", err, f.repository.updateCalls)
	}
}

func TestUpdateRejectsInvalidChangesWithoutSaving(t *testing.T) {
	emptyName := " "
	priority := domain.PriorityLow + 1
	request := domain.Request{Method: "GET", URL: "https://10.0.0.1"}
	timeout := time.Duration(0)
	retry := domain.RetryPolicy{}
	schedule := domain.Schedule{Type: domain.ScheduleAt, RunAt: testNow.Add(-time.Hour)}

	testCases := map[string]ports.UpdateTaskInput{
		"name":     {Name: &emptyName},
		"priority": {Priority: &priority},
		"request":  {Request: &request},
		"timeout":  {Timeout: &timeout},
		"retry":    {Retry: &retry},
		"schedule": {Schedule: &schedule},
	}

	for name, input := range testCases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			task := createTask(t, f, cronInput())

			_, err := f.service.Update(context.Background(), testTenant, task.ID, 1, input)

			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("want ErrInvalidInput, got %v", err)
			}
			if f.repository.updateCalls+f.repository.rescheduleCalls != 0 {
				t.Error("an invalid change must not be saved")
			}
		})
	}
}

func TestUpdateAppliesEveryField(t *testing.T) {
	f := newFixture()
	task := createTask(t, f, cronInput())
	priority := domain.PriorityCritical
	request := domain.Request{Method: "GET", URL: "https://other.example.com"}
	timeout := time.Minute
	retry := domain.RetryPolicy{MaxAttempts: 9, BackoffBase: time.Minute}

	updated, err := f.service.Update(context.Background(), testTenant, task.ID, 1, ports.UpdateTaskInput{
		Priority: &priority, Request: &request, Timeout: &timeout, Retry: &retry,
	})
	if err != nil {
		t.Fatal(err)
	}

	if updated.Priority != priority || updated.Request.URL != request.URL || updated.Timeout != timeout || updated.Retry != retry {
		t.Errorf("updated: %+v", updated)
	}
}

func TestPauseAndResumeACronTask(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	task := createTask(t, f, cronInput())

	paused, err := f.service.Pause(ctx, testTenant, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != domain.StatusPaused || paused.Version != 2 {
		t.Errorf("paused: %s v%d", paused.Status, paused.Version)
	}

	*f.now = testNow.Add(5 * time.Hour)
	resumed, err := f.service.Resume(ctx, testTenant, task.ID)
	if err != nil {
		t.Fatal(err)
	}

	if resumed.Status != domain.StatusActive || resumed.Version != 3 {
		t.Errorf("resumed: %s v%d", resumed.Status, resumed.Version)
	}
	if want := f.now.Add(time.Hour); !resumed.NextRunAt.Equal(want) {
		t.Errorf("cron must skip missed runs: got %v, want %v", resumed.NextRunAt, want)
	}
}

func TestPauseAndResumeRepeatedAreNoOps(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	task := createTask(t, f, cronInput())

	resumed, err := f.service.Resume(ctx, testTenant, task.ID)
	if err != nil || resumed.Version != 1 || f.repository.rescheduleCalls != 0 {
		t.Errorf("resume on active: v%d err=%v", resumed.Version, err)
	}

	if _, err := f.service.Pause(ctx, testTenant, task.ID); err != nil {
		t.Fatal(err)
	}
	paused, err := f.service.Pause(ctx, testTenant, task.ID)
	if err != nil || paused.Version != 2 || f.repository.updateCalls != 1 {
		t.Errorf("pause on paused: v%d err=%v writes=%d", paused.Version, err, f.repository.updateCalls)
	}
}

func TestResumeKeepsAnOverdueAtTaskPending(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	task := createTask(t, f, atInput())
	if _, err := f.service.Pause(ctx, testTenant, task.ID); err != nil {
		t.Fatal(err)
	}

	*f.now = testNow.Add(72 * time.Hour)
	resumed, err := f.service.Resume(ctx, testTenant, task.ID)
	if err != nil {
		t.Fatal(err)
	}

	if resumed.NextRunAt == nil || !resumed.NextRunAt.Equal(task.Schedule.RunAt) {
		t.Errorf("an overdue task must stay due: %v", resumed.NextRunAt)
	}
}

func TestResumeDoesNotRefireAnAtTaskAlreadyExecuted(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	task := createTask(t, f, atInput())
	if _, err := f.service.Pause(ctx, testTenant, task.ID); err != nil {
		t.Fatal(err)
	}
	f.repository.tasks[task.ID].task.NextRunAt = nil

	resumed, err := f.service.Resume(ctx, testTenant, task.ID)
	if err != nil {
		t.Fatal(err)
	}

	if resumed.NextRunAt != nil {
		t.Errorf("a finished at task must not run again: %v", resumed.NextRunAt)
	}
}

func TestDeleteHidesTheTask(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	task := createTask(t, f, cronInput())

	if err := f.service.Delete(ctx, testTenant, task.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := f.service.Get(ctx, testTenant, task.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get: %v", err)
	}
	if _, err := f.service.Pause(ctx, testTenant, task.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("pause: %v", err)
	}
	if _, err := f.service.Resume(ctx, testTenant, task.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("resume: %v", err)
	}
	if err := f.service.Delete(ctx, testTenant, task.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
	if f.repository.tasks[task.ID].task.Status != domain.StatusDeleted {
		t.Error("delete must be soft")
	}
	page, err := f.service.List(ctx, testTenant, ports.PageRequest{Limit: 10})
	if err != nil || len(page.Tasks) != 0 {
		t.Errorf("list: %d err=%v", len(page.Tasks), err)
	}
}

func TestCreateReturnsAnExistingDeletedTask(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	first, _, err := f.service.Create(ctx, testTenant, idempotencyKey("body"), cronInput())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.Delete(ctx, testTenant, first.ID); err != nil {
		t.Fatal(err)
	}

	existingTask, alreadyCreated, err := f.service.Create(ctx, testTenant, idempotencyKey("body"), cronInput())

	if err != nil || !alreadyCreated || existingTask.Status != domain.StatusDeleted {
		t.Errorf("alreadyCreated=%v err=%v status=%s", alreadyCreated, err, existingTask.Status)
	}
}

func TestOtherTenantsCannotReachTheTask(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	task := createTask(t, f, cronInput())
	otherTenant := uuid.New()
	name := "stolen"

	if _, err := f.service.Get(ctx, otherTenant, task.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get: %v", err)
	}
	if _, err := f.service.Update(ctx, otherTenant, task.ID, 1, ports.UpdateTaskInput{Name: &name}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("update: %v", err)
	}
	if err := f.service.Delete(ctx, otherTenant, task.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("delete: %v", err)
	}
}
