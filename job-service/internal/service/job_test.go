package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/ports"
	"github.com/JoelTeoGom/distributed-scheduler/job-service/internal/service"
)

var (
	testNow    = time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	testTenant = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testTask   = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	errStore   = errors.New("store is down")
)

type fakeHotRepository struct {
	jobs       map[uuid.UUID]*domain.Job
	history    []*domain.Job
	dead       []*domain.Job
	getErr     error
	historyErr error
	retryErr   error

	mutex       sync.Mutex
	retriedAt   time.Time
	getCalls    int
	lastBefore  *time.Time
	lastAfter   *uuid.UUID
	lastLimit   int
	lastTenants []uuid.UUID
}

func (f *fakeHotRepository) record(tenantID uuid.UUID) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.lastTenants = append(f.lastTenants, tenantID)
}

func (f *fakeHotRepository) Get(_ context.Context, tenantID, jobID uuid.UUID) (*domain.Job, error) {
	f.record(tenantID)
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	job, found := f.jobs[jobID]
	if !found || job.TenantID != tenantID {
		return nil, domain.ErrNotFound
	}
	return job, nil
}

func (f *fakeHotRepository) History(_ context.Context, tenantID, _ uuid.UUID, before *time.Time, limit int) ([]*domain.Job, error) {
	f.record(tenantID)
	f.lastBefore, f.lastLimit = before, limit
	return f.history, f.historyErr
}

func (f *fakeHotRepository) ListDead(_ context.Context, tenantID uuid.UUID, after *uuid.UUID, limit int) ([]*domain.Job, error) {
	f.record(tenantID)
	f.lastAfter, f.lastLimit = after, limit
	return f.dead, nil
}

func (f *fakeHotRepository) Retry(_ context.Context, tenantID, _ uuid.UUID, now time.Time) error {
	f.record(tenantID)
	f.retriedAt = now
	return f.retryErr
}

type fakeHistoryRepository struct {
	jobs       map[uuid.UUID]*domain.Job
	history    []*domain.Job
	getErr     error
	historyErr error

	lastBefore *time.Time
	lastLimit  int
	lastTenant uuid.UUID
}

func (f *fakeHistoryRepository) Get(_ context.Context, tenantID, jobID uuid.UUID) (*domain.Job, error) {
	f.lastTenant = tenantID
	if f.getErr != nil {
		return nil, f.getErr
	}
	job, found := f.jobs[jobID]
	if !found || job.TenantID != tenantID {
		return nil, domain.ErrNotFound
	}
	return job, nil
}

func (f *fakeHistoryRepository) History(_ context.Context, tenantID, _ uuid.UUID, before *time.Time, limit int) ([]*domain.Job, error) {
	f.lastTenant = tenantID
	f.lastBefore, f.lastLimit = before, limit
	return f.history, f.historyErr
}

type fixture struct {
	hot      *fakeHotRepository
	archived *fakeHistoryRepository
	service  *service.JobService
}

func newFixture() fixture {
	hot := &fakeHotRepository{jobs: map[uuid.UUID]*domain.Job{}}
	archived := &fakeHistoryRepository{jobs: map[uuid.UUID]*domain.Job{}}
	return fixture{hot: hot, archived: archived, service: service.NewJobService(hot, archived, func() time.Time { return testNow })}
}

func jobAt(hoursAgo int, status domain.Status) *domain.Job {
	return &domain.Job{
		ID:          uuid.New(),
		TenantID:    testTenant,
		TaskID:      testTask,
		ScheduledAt: testNow.Add(-time.Duration(hoursAgo) * time.Hour),
		Status:      status,
	}
}

func historyQuery(limit int) ports.HistoryQuery {
	return ports.HistoryQuery{TenantID: testTenant, TaskID: testTask, Limit: limit}
}

func assertOrder(t *testing.T, got []*domain.Job, want ...*domain.Job) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d jobs, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got job scheduled at %v, want %v", i, got[i].ScheduledAt, want[i].ScheduledAt)
		}
	}
}

func TestGetReturnsAHotJob(t *testing.T) {
	f := newFixture()
	job := jobAt(1, domain.StatusRunning)
	f.hot.jobs[job.ID] = job

	got, err := f.service.Get(context.Background(), testTenant, job.ID)

	if err != nil || got != job {
		t.Errorf("got %v err %v", got, err)
	}
}

func TestGetReturnsAnArchivedJob(t *testing.T) {
	f := newFixture()
	job := jobAt(1, domain.StatusSucceeded)
	f.archived.jobs[job.ID] = job

	got, err := f.service.Get(context.Background(), testTenant, job.ID)

	if err != nil || got != job {
		t.Errorf("got %v err %v", got, err)
	}
}

func TestGetPrefersMySQLDuringTheArchiveHandover(t *testing.T) {
	f := newFixture()
	job := jobAt(1, domain.StatusSucceeded)
	archivedCopy := *job
	f.hot.jobs[job.ID] = job
	f.archived.jobs[job.ID] = &archivedCopy

	got, err := f.service.Get(context.Background(), testTenant, job.ID)

	if err != nil || got != job {
		t.Errorf("must return the MySQL copy, err %v", err)
	}
}

func TestGetReportsNotFoundWhenNoStoreHasTheJob(t *testing.T) {
	f := newFixture()

	_, err := f.service.Get(context.Background(), testTenant, uuid.New())

	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestGetHidesJobsOfOtherTenants(t *testing.T) {
	f := newFixture()
	hotJob, archivedJob := jobAt(1, domain.StatusDead), jobAt(2, domain.StatusSucceeded)
	f.hot.jobs[hotJob.ID] = hotJob
	f.archived.jobs[archivedJob.ID] = archivedJob
	otherTenant := uuid.New()

	for _, jobID := range []uuid.UUID{hotJob.ID, archivedJob.ID} {
		if _, err := f.service.Get(context.Background(), otherTenant, jobID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("got %v", err)
		}
	}
}

func TestGetSurvivesTheFailureOfOneStore(t *testing.T) {
	t.Run("cassandra down, job in mysql", func(t *testing.T) {
		f := newFixture()
		job := jobAt(1, domain.StatusRunning)
		f.hot.jobs[job.ID] = job
		f.archived.getErr = errStore

		got, err := f.service.Get(context.Background(), testTenant, job.ID)

		if err != nil || got != job {
			t.Errorf("got %v err %v", got, err)
		}
	})

	t.Run("mysql down, job in cassandra", func(t *testing.T) {
		f := newFixture()
		job := jobAt(1, domain.StatusSucceeded)
		f.archived.jobs[job.ID] = job
		f.hot.getErr = errStore

		got, err := f.service.Get(context.Background(), testTenant, job.ID)

		if err != nil || got != job {
			t.Errorf("got %v err %v", got, err)
		}
	})
}

func TestGetDoesNotReportNotFoundWhenAStoreIsDown(t *testing.T) {
	t.Run("cassandra down", func(t *testing.T) {
		f := newFixture()
		f.archived.getErr = errStore

		if _, err := f.service.Get(context.Background(), testTenant, uuid.New()); !errors.Is(err, errStore) {
			t.Errorf("got %v", err)
		}
	})

	t.Run("mysql down", func(t *testing.T) {
		f := newFixture()
		f.hot.getErr = errStore

		if _, err := f.service.Get(context.Background(), testTenant, uuid.New()); !errors.Is(err, errStore) {
			t.Errorf("got %v", err)
		}
	})
}

func TestHistoryMergesBothStoresInOrder(t *testing.T) {
	f := newFixture()
	newestArchived := jobAt(1, domain.StatusSucceeded)
	running := jobAt(2, domain.StatusRunning)
	olderArchived := jobAt(3, domain.StatusSucceeded)
	oldDead := jobAt(4, domain.StatusDead)
	oldestArchived := jobAt(5, domain.StatusSucceeded)
	f.hot.history = []*domain.Job{running, oldDead}
	f.archived.history = []*domain.Job{newestArchived, olderArchived, oldestArchived}

	page, err := f.service.History(context.Background(), historyQuery(10))
	if err != nil {
		t.Fatal(err)
	}

	assertOrder(t, page.Jobs, newestArchived, running, olderArchived, oldDead, oldestArchived)
	if page.NextCursor != "" {
		t.Errorf("a short page has no cursor: %q", page.NextCursor)
	}
}

func TestHistoryDeduplicatesKeepingTheMySQLCopy(t *testing.T) {
	f := newFixture()
	job := jobAt(1, domain.StatusSucceeded)
	archivedCopy := *job
	older := jobAt(2, domain.StatusSucceeded)
	f.hot.history = []*domain.Job{job}
	f.archived.history = []*domain.Job{&archivedCopy, older}

	page, err := f.service.History(context.Background(), historyQuery(10))
	if err != nil {
		t.Fatal(err)
	}

	assertOrder(t, page.Jobs, job, older)
}

func TestHistoryTakesTheFirstLimitJobsAndReturnsACursor(t *testing.T) {
	f := newFixture()
	first, second, third := jobAt(1, domain.StatusSucceeded), jobAt(2, domain.StatusDead), jobAt(3, domain.StatusSucceeded)
	f.hot.history = []*domain.Job{second}
	f.archived.history = []*domain.Job{first, third}

	page, err := f.service.History(context.Background(), historyQuery(2))
	if err != nil {
		t.Fatal(err)
	}

	assertOrder(t, page.Jobs, first, second)
	if want := second.ScheduledAt.Format(time.RFC3339Nano); page.NextCursor != want {
		t.Errorf("cursor %q, want %q", page.NextCursor, want)
	}
}

func TestHistoryCursorIsInUTC(t *testing.T) {
	f := newFixture()
	madrid := time.FixedZone("CEST", 2*60*60)
	job := jobAt(1, domain.StatusSucceeded)
	job.ScheduledAt = job.ScheduledAt.In(madrid)
	f.hot.history = []*domain.Job{job}

	page, err := f.service.History(context.Background(), historyQuery(1))
	if err != nil {
		t.Fatal(err)
	}

	if want := job.ScheduledAt.UTC().Format(time.RFC3339Nano); page.NextCursor != want {
		t.Errorf("cursor %q, want %q", page.NextCursor, want)
	}
}

func TestHistoryPassesTheSameQueryToBothStores(t *testing.T) {
	f := newFixture()
	before := testNow.Add(-time.Hour)
	query := historyQuery(7)
	query.Before = &before

	if _, err := f.service.History(context.Background(), query); err != nil {
		t.Fatal(err)
	}

	if f.hot.lastBefore != &before || f.archived.lastBefore != &before {
		t.Error("both stores must receive the cursor")
	}
	if f.hot.lastLimit != 7 || f.archived.lastLimit != 7 {
		t.Errorf("limits: %d and %d", f.hot.lastLimit, f.archived.lastLimit)
	}
	if f.hot.lastTenants[0] != testTenant || f.archived.lastTenant != testTenant {
		t.Error("both stores must receive the tenant")
	}
}

func TestHistoryIsEmptyWithoutJobs(t *testing.T) {
	page, err := newFixture().service.History(context.Background(), historyQuery(10))

	if err != nil || len(page.Jobs) != 0 || page.NextCursor != "" {
		t.Errorf("page %+v err %v", page, err)
	}
}

func TestHistoryFailsIfAStoreFails(t *testing.T) {
	testCases := map[string]func(fixture){
		"mysql":     func(f fixture) { f.hot.historyErr = errStore },
		"cassandra": func(f fixture) { f.archived.historyErr = errStore },
	}

	for name, breakStore := range testCases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			breakStore(f)

			if _, err := f.service.History(context.Background(), historyQuery(10)); !errors.Is(err, errStore) {
				t.Errorf("a partial history must not look complete: %v", err)
			}
		})
	}
}

func TestListDeadReturnsACursorOnAFullPage(t *testing.T) {
	f := newFixture()
	first, second := jobAt(1, domain.StatusDead), jobAt(2, domain.StatusDead)
	f.hot.dead = []*domain.Job{first, second}
	after := uuid.New()

	page, err := f.service.ListDead(context.Background(), ports.DeadQuery{TenantID: testTenant, After: &after, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}

	assertOrder(t, page.Jobs, first, second)
	if page.NextCursor != second.ID.String() {
		t.Errorf("cursor: %q", page.NextCursor)
	}
	if f.hot.lastAfter != &after || f.hot.lastLimit != 2 || f.hot.lastTenants[0] != testTenant {
		t.Errorf("query not forwarded: %+v", f.hot)
	}
}

func TestListDeadHasNoCursorOnAShortPage(t *testing.T) {
	f := newFixture()
	f.hot.dead = []*domain.Job{jobAt(1, domain.StatusDead)}

	page, err := f.service.ListDead(context.Background(), ports.DeadQuery{TenantID: testTenant, Limit: 2})

	if err != nil || page.NextCursor != "" {
		t.Errorf("cursor %q err %v", page.NextCursor, err)
	}
}

func TestRetryPassesTheTimeTruncatedToMilliseconds(t *testing.T) {
	hot := &fakeHotRepository{jobs: map[uuid.UUID]*domain.Job{}}
	clock := func() time.Time { return testNow.Add(1234567 * time.Nanosecond) }
	jobService := service.NewJobService(hot, &fakeHistoryRepository{}, clock)

	if err := jobService.Retry(context.Background(), testTenant, uuid.New()); err != nil {
		t.Fatal(err)
	}

	if want := testNow.Add(time.Millisecond); !hot.retriedAt.Equal(want) {
		t.Errorf("got %v, want %v", hot.retriedAt, want)
	}
	if hot.getCalls != 0 {
		t.Error("a successful retry needs no extra read")
	}
}

func TestRetryOfAJobThatIsNotDeadIsAConflict(t *testing.T) {
	f := newFixture()
	job := jobAt(1, domain.StatusRunning)
	f.hot.jobs[job.ID] = job
	f.hot.retryErr = domain.ErrNotRetryable

	if err := f.service.Retry(context.Background(), testTenant, job.ID); !errors.Is(err, domain.ErrNotRetryable) {
		t.Errorf("got %v", err)
	}
}

func TestRetryOfAMissingOrArchivedJobIsNotFound(t *testing.T) {
	f := newFixture()
	archived := jobAt(1, domain.StatusDead)
	f.archived.jobs[archived.ID] = archived
	f.hot.retryErr = domain.ErrNotRetryable

	if err := f.service.Retry(context.Background(), testTenant, archived.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestRetryOfAJobOfAnotherTenantIsNotFound(t *testing.T) {
	f := newFixture()
	job := jobAt(1, domain.StatusDead)
	f.hot.jobs[job.ID] = job
	f.hot.retryErr = domain.ErrNotRetryable

	if err := f.service.Retry(context.Background(), uuid.New(), job.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("got %v", err)
	}
}

func TestRetryPropagatesStoreFailures(t *testing.T) {
	t.Run("update fails", func(t *testing.T) {
		f := newFixture()
		f.hot.retryErr = errStore

		if err := f.service.Retry(context.Background(), testTenant, uuid.New()); !errors.Is(err, errStore) {
			t.Errorf("got %v", err)
		}
	})

	t.Run("read after the update fails", func(t *testing.T) {
		f := newFixture()
		f.hot.retryErr = domain.ErrNotRetryable
		f.hot.getErr = errStore

		if err := f.service.Retry(context.Background(), testTenant, uuid.New()); !errors.Is(err, errStore) {
			t.Errorf("got %v", err)
		}
	})
}
