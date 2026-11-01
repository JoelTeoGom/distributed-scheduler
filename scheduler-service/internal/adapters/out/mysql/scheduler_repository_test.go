//go:build integration

// Needs MySQL at SCHEDULER_TEST_DSN with the task-service and job-service migrations applied.
// The scheduler claims every due task it finds, so point it at a throwaway database.
package mysql_test

import (
	"context"
	"database/sql"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/adapters/out/cron"
	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/adapters/out/mysql"
	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/ports"
	"github.com/JoelTeoGom/distributed-scheduler/scheduler-service/internal/service"
)

const (
	tasksPerWave      = 500
	waveCount         = 6
	waveInterval      = 5 * time.Second
	burstTaskCount    = 2500
	burstLead         = 25 * time.Second
	minimumLead       = 10 * time.Second
	leadSpread        = 30 * time.Second
	scheduleLookahead = 30 * time.Second
	cronTaskEvery     = 5
	seedBatchSize     = 500
	drainTimeout      = 90 * time.Second
)

var timezones = []string{"UTC", "Europe/Madrid", "America/New_York", "Asia/Tokyo"}

type seededTask struct {
	id         uuid.UUID
	cronExpr   string // empty = 'at' task
	timezone   string
	firstRunAt time.Time
}

type timedRepository struct {
	ports.SchedulerRepository
	mutex     sync.Mutex
	durations []time.Duration
	failures  []error
}

func (r *timedRepository) ScheduleJobs(ctx context.Context, now time.Time, nextRuns ports.NextRunCalculator) error {
	startedAt := time.Now()
	err := r.SchedulerRepository.ScheduleJobs(ctx, now, nextRuns)

	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.durations = append(r.durations, time.Since(startedAt))
	if err != nil && ctx.Err() == nil {
		r.failures = append(r.failures, err)
	}
	return err
}

type fixture struct {
	db         *sql.DB
	tenantID   uuid.UUID
	repository *timedRepository
	stop       func()
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("SCHEDULER_TEST_DSN")
	if dsn == "" {
		t.Skip("SCHEDULER_TEST_DSN is not set")
	}

	ctx, cancel := context.WithCancel(context.Background())
	db, err := mysql.Open(ctx, dsn)
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	repository := &timedRepository{SchedulerRepository: mysql.NewSchedulerRepository(db)}
	schedulerService := service.NewSchedulerService(repository, cron.NewCalculator())

	stopped := make(chan error, 1)
	go func() { stopped <- schedulerService.Start(ctx) }()

	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancel()
			if err := <-stopped; err != nil {
				t.Errorf("start: %v", err)
			}
		})
	}
	t.Cleanup(func() {
		stop()
		db.Close()
	})

	return &fixture{db: db, tenantID: uuid.Must(uuid.NewV7()), repository: repository, stop: stop}
}

func (f *fixture) seedTasks(t *testing.T, count int, firstRunAt func() time.Time) []seededTask {
	t.Helper()
	tasks := make([]seededTask, 0, count)
	for len(tasks) < count {
		batch := make([]seededTask, 0, seedBatchSize)
		for i := 0; i < seedBatchSize && len(tasks)+i < count; i++ {
			task := seededTask{
				id:         uuid.Must(uuid.NewV7()),
				timezone:   timezones[rand.N(len(timezones))],
				firstRunAt: firstRunAt().UTC().Truncate(time.Millisecond),
			}
			if (len(tasks)+i)%cronTaskEvery == 0 {
				task.cronExpr = "* * * * *"
			}
			batch = append(batch, task)
		}
		f.insertTasks(t, batch)
		tasks = append(tasks, batch...)
	}
	return tasks
}

func (f *fixture) insertTasks(t *testing.T, tasks []seededTask) {
	t.Helper()
	now := time.Now().UTC()
	placeholders := make([]string, len(tasks))
	var values []any
	for i, task := range tasks {
		scheduleType := "cron"
		runAt := sql.NullTime{}
		if task.cronExpr == "" {
			scheduleType = "at"
			runAt = sql.NullTime{Time: task.firstRunAt, Valid: true}
		}

		placeholders[i] = `(?, ?, 'load test', ?, ?, ?, ?, ?, 'POST', 'https://example.com/hook', '{"X-Source": "load-test"}', 'payload', 'vault/key', 30, 3, 10, 'active', ?, ?, ?)`
		values = append(values,
			task.id[:], f.tenantID[:], scheduleType, runAt,
			sql.NullString{String: task.cronExpr, Valid: task.cronExpr != ""}, task.timezone, rand.N(4),
			task.firstRunAt, now, now)
	}

	if _, err := f.db.Exec(`
		INSERT INTO tasks (
			id, tenant_id, name,
			schedule_type, run_at, cron_expr, timezone,
			priority,
			http_method, endpoint, headers, body, secret_ref, timeout_seconds,
			max_attempts, backoff_base_seconds,
			status, next_run_at, created_at, updated_at
		) VALUES `+strings.Join(placeholders, ", "),
		values...); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) waitForFirstJobs(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(drainTimeout)
	for {
		var tasksWithoutJob int
		if err := f.db.QueryRow(`
			SELECT COUNT(*)
			FROM tasks t
			LEFT JOIN jobs j ON j.task_id = t.id
			WHERE t.tenant_id = ? AND j.id IS NULL`,
			f.tenantID[:]).Scan(&tasksWithoutJob); err != nil {
			t.Fatal(err)
		}
		if tasksWithoutJob == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d tasks still have no job after %s", tasksWithoutJob, drainTimeout)
		}
		time.Sleep(time.Second)
	}
}

func (f *fixture) verify(t *testing.T, tasks []seededTask) {
	t.Helper()
	f.stop()

	if len(f.repository.failures) > 0 {
		t.Fatalf("%d ticks failed, first: %v", len(f.repository.failures), f.repository.failures[0])
	}

	scheduledTimes, jobCount := f.verifyLeadTimes(t)
	f.verifyJobChains(t, tasks, scheduledTimes)
	f.verifySnapshots(t)

	longestTick := slices.Max(f.repository.durations)
	t.Logf("tasks=%d jobs=%d ticks=%d longest tick=%s", len(tasks), jobCount, len(f.repository.durations), longestTick)
	t.Logf("tick durations: %v", f.repository.durations)
}

// Lead is how long before its fire time a job reached the hot table.
func (f *fixture) verifyLeadTimes(t *testing.T) (map[uuid.UUID][]time.Time, int) {
	t.Helper()
	rows, err := f.db.Query(`
		SELECT task_id, scheduled_at, created_at
		FROM jobs
		WHERE tenant_id = ?
		ORDER BY task_id, scheduled_at`,
		f.tenantID[:])
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	scheduledTimes := make(map[uuid.UUID][]time.Time)
	jobCount := 0
	var shortestLead, longestLead time.Duration
	for rows.Next() {
		var (
			taskID      uuid.UUID
			scheduledAt time.Time
			createdAt   time.Time
		)
		if err := rows.Scan(&taskID, &scheduledAt, &createdAt); err != nil {
			t.Fatal(err)
		}
		scheduledTimes[taskID] = append(scheduledTimes[taskID], scheduledAt)

		lead := scheduledAt.Sub(createdAt)
		if jobCount == 0 || lead < shortestLead {
			shortestLead = lead
		}
		if lead > longestLead {
			longestLead = lead
		}
		jobCount++

		if lead <= 0 {
			t.Errorf("task %s: job for %s created %s late", taskID, scheduledAt, -lead)
		}
		if lead > scheduleLookahead+time.Millisecond {
			t.Errorf("task %s: job for %s created %s early, past the lookahead", taskID, scheduledAt, lead)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	t.Logf("job lead: shortest=%s longest=%s", shortestLead, longestLead)
	return scheduledTimes, jobCount
}

func (f *fixture) verifyJobChains(t *testing.T, tasks []seededTask, scheduledTimes map[uuid.UUID][]time.Time) {
	t.Helper()
	calculator := cron.NewCalculator()
	for _, task := range tasks {
		var nextRunAt sql.NullTime
		if err := f.db.QueryRow(`SELECT next_run_at FROM tasks WHERE id = ?`, task.id[:]).Scan(&nextRunAt); err != nil {
			t.Fatal(err)
		}

		jobTimes := scheduledTimes[task.id]
		if len(jobTimes) == 0 {
			t.Fatalf("task %s has no job", task.id)
		}

		if task.cronExpr == "" {
			if len(jobTimes) != 1 || !jobTimes[0].Equal(task.firstRunAt) || nextRunAt.Valid {
				t.Fatalf("at task %s: jobs=%v next_run_at=%v, want one job at %s and NULL", task.id, jobTimes, nextRunAt, task.firstRunAt)
			}
			continue
		}

		// Every occurrence must have exactly one job, with no gap up to the current next_run_at.
		expected := task.firstRunAt
		for _, scheduledAt := range jobTimes {
			if !scheduledAt.Equal(expected) {
				t.Fatalf("cron task %s: job at %s, want %s", task.id, scheduledAt, expected)
			}
			var err error
			if expected, err = calculator.Next(task.cronExpr, task.timezone, expected); err != nil {
				t.Fatal(err)
			}
		}
		if !nextRunAt.Time.Equal(expected) {
			t.Fatalf("cron task %s: next_run_at = %v, want %s", task.id, nextRunAt, expected)
		}
	}
}

func (f *fixture) verifySnapshots(t *testing.T) {
	t.Helper()
	var wrongJobs int
	if err := f.db.QueryRow(`
		SELECT COUNT(*)
		FROM jobs j
		JOIN tasks t ON t.id = j.task_id
		WHERE t.tenant_id = ?
		  AND NOT (
			j.tenant_id = t.tenant_id
			AND j.priority = t.priority
			AND j.http_method = t.http_method
			AND j.endpoint = t.endpoint
			AND j.headers = t.headers
			AND j.body = t.body
			AND j.secret_ref = t.secret_ref
			AND j.timeout_seconds = t.timeout_seconds
			AND j.max_attempts = t.max_attempts
			AND j.backoff_base_seconds = t.backoff_base_seconds
			AND j.status = 'pending'
			AND j.attempt = 0
			AND j.next_attempt_at = j.scheduled_at
		  )`,
		f.tenantID[:]).Scan(&wrongJobs); err != nil {
		t.Fatal(err)
	}
	if wrongJobs != 0 {
		t.Errorf("%d jobs do not match their task", wrongJobs)
	}
}

func TestSchedulerKeepsUpWithOverlappingWaves(t *testing.T) {
	f := newFixture(t)

	var tasks []seededTask
	var seedDurations []time.Duration
	for wave := range waveCount {
		if wave > 0 {
			time.Sleep(waveInterval)
		}
		startedAt := time.Now()
		tasks = append(tasks, f.seedTasks(t, tasksPerWave, func() time.Time {
			return startedAt.Add(minimumLead + rand.N(leadSpread))
		})...)
		seedDurations = append(seedDurations, time.Since(startedAt))
	}
	t.Logf("wave insert durations: %v", seedDurations)

	f.waitForFirstJobs(t)
	f.verify(t, tasks)
}

func TestSchedulerDrainsBurstLargerThanOneBatch(t *testing.T) {
	f := newFixture(t)

	sameInstant := time.Now().Add(burstLead)
	tasks := f.seedTasks(t, burstTaskCount, func() time.Time { return sameInstant })

	f.waitForFirstJobs(t)
	f.verify(t, tasks)
}
