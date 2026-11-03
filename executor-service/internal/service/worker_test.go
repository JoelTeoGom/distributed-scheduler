package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/domain"
)

type fakeJobRepository struct {
	mutex       sync.Mutex
	claimable   []*domain.Job
	claimed     chan struct{}
	saved       map[uuid.UUID]string
	saveFailed  map[uuid.UUID]bool
	claimedOnce sync.Once
}

func newFakeJobRepository(jobs ...*domain.Job) *fakeJobRepository {
	return &fakeJobRepository{claimable: jobs, claimed: make(chan struct{}), saved: map[uuid.UUID]string{}, saveFailed: map[uuid.UUID]bool{}}
}

func (f *fakeJobRepository) ClaimJobs(_ context.Context, _, _ int) ([]*domain.Job, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	jobs := f.claimable
	f.claimable = nil
	f.claimedOnce.Do(func() { close(f.claimed) })
	return jobs, nil
}

func (f *fakeJobRepository) save(ctx context.Context, job *domain.Job, outcome string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if ctx.Err() != nil {
		f.saveFailed[job.ID] = true
		return ctx.Err()
	}
	f.saved[job.ID] = outcome
	return nil
}

func (f *fakeJobRepository) SaveSucceeded(ctx context.Context, job *domain.Job) error {
	return f.save(ctx, job, "succeeded")
}

func (f *fakeJobRepository) SaveRetry(ctx context.Context, job *domain.Job, _ time.Time) error {
	return f.save(ctx, job, "retry")
}

func (f *fakeJobRepository) SaveDead(ctx context.Context, job *domain.Job) error {
	return f.save(ctx, job, "dead")
}

func (f *fakeJobRepository) ReleaseJob(ctx context.Context, job *domain.Job) error {
	return f.save(ctx, job, "released")
}

func (f *fakeJobRepository) outcome(job *domain.Job) (string, bool) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.saved[job.ID], f.saveFailed[job.ID]
}

// fakeExecutor answers 200 after requestDuration, or fails when the job context is cancelled first.
type fakeExecutor struct {
	requestDuration time.Duration
	started         chan struct{}
}

func (f *fakeExecutor) Execute(ctx context.Context, _ *domain.Job) (domain.Result, error) {
	f.started <- struct{}{}
	select {
	case <-time.After(f.requestDuration):
		status := 200
		return domain.Result{HTTPStatus: &status}, nil
	case <-ctx.Done():
		return domain.Result{Error: "context canceled"}, nil
	}
}

func newJob(nextAttemptAt time.Time) *domain.Job {
	job := &domain.Job{ID: uuid.New(), Attempt: 1, NextAttemptAt: nextAttemptAt}
	job.Config.Retry.MaxAttempts = 3
	return job
}

// runPool claims the jobs on the first tick, waits until the request is out, sends the signal
// and returns how long the pool took to stop after it.
func runPool(t *testing.T, repository *fakeJobRepository, executor *fakeExecutor, gracePeriod time.Duration) time.Duration {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	pool := NewWorkerService(repository, executor, 10, 1, gracePeriod)

	stopped := make(chan error, 1)
	go func() { stopped <- pool.StartWorkerPool(ctx, 4) }()

	<-repository.claimed
	<-executor.started
	cancel()
	signalledAt := time.Now()

	if err := <-stopped; err != nil {
		t.Errorf("pool returned %v, want nil on shutdown", err)
	}
	return time.Since(signalledAt)
}

func TestShutdownFinishesSentJobsAndReleasesWaitingOnes(t *testing.T) {
	inFlight := newJob(time.Now())
	waiting := newJob(time.Now().Add(time.Hour))
	repository := newFakeJobRepository(inFlight, waiting)
	executor := &fakeExecutor{requestDuration: 300 * time.Millisecond, started: make(chan struct{}, 2)}

	stoppedAfter := runPool(t, repository, executor, 5*time.Second)

	if outcome, _ := repository.outcome(inFlight); outcome != "succeeded" {
		t.Errorf("job in flight: got %q, want it to finish and save succeeded", outcome)
	}
	if outcome, _ := repository.outcome(waiting); outcome != "released" {
		t.Errorf("job waiting for its time: got %q, want released", outcome)
	}
	if stoppedAfter < 250*time.Millisecond || stoppedAfter > 2*time.Second {
		t.Errorf("pool stopped %v after the signal, want it to wait for the request (300 ms)", stoppedAfter)
	}
}

func TestShutdownCancelsJobsAfterGracePeriod(t *testing.T) {
	slow := newJob(time.Now())
	repository := newFakeJobRepository(slow)
	executor := &fakeExecutor{requestDuration: time.Hour, started: make(chan struct{}, 1)}

	stoppedAfter := runPool(t, repository, executor, 200*time.Millisecond)

	if stoppedAfter < 150*time.Millisecond || stoppedAfter > 2*time.Second {
		t.Errorf("pool stopped %v after the signal, want about the 200 ms grace period", stoppedAfter)
	}
	if outcome, saveFailed := repository.outcome(slow); outcome != "" || !saveFailed {
		t.Errorf("cut job: got %q, saveFailed=%v; want no saved outcome, left to the reconciler", outcome, saveFailed)
	}
}
