package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/ports"
)

type WorkerService struct {
	db                  ports.JobRepository
	requests            ports.RequestExecutor
	jobQueueSize        int
	timeLimit           int
	shutdownGracePeriod time.Duration
}

var _ ports.WorkerService = (*WorkerService)(nil)

func NewWorkerService(db ports.JobRepository, requests ports.RequestExecutor, jobQueueSize, timeLimit int, shutdownGracePeriod time.Duration) *WorkerService {
	return &WorkerService{db: db, requests: requests, jobQueueSize: jobQueueSize, timeLimit: timeLimit, shutdownGracePeriod: shutdownGracePeriod}
}

// ctx is the shutdown signal: it stops the claims and releases jobs still waiting for their time.
// Requests already sent run on jobContext, which is only cancelled when the grace period runs out.
func (s *WorkerService) StartWorkerPool(ctx context.Context, workerCount int) error {
	jobContext, cancelJobs := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelJobs()

	//available workers (WE WILL USE IT TO LIMIT DB CALLS FOR JOBS)
	// Never closed: a worker that took a job could still send to it.
	sem := make(chan struct{}, workerCount)

	jobChan := make(chan *domain.Job, s.jobQueueSize)

	//Spin up the workers
	var workers sync.WaitGroup
	for i := range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			s.StartWorker(ctx, jobContext, i, sem, jobChan)
		}()
	}

	// The claim takes jobs due up to timeLimit ahead, so ticking at the same interval leaves no gap between claims.
	ticker := time.NewTicker(time.Duration(s.timeLimit) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// The pool is the only sender, so it closes the channel: workers drain what is left and stop.
			close(jobChan)
			s.waitForWorkers(&workers, cancelJobs)
			return nil
		case <-ticker.C:
			semSize := len(sem)

			availableWorkers := workerCount - semSize
			if availableWorkers <= 0 {
				continue
			}
			jobs, err := s.db.ClaimJobs(ctx, s.timeLimit, availableWorkers)
			if err != nil {
				// A database outage must not stop the pool: the next tick claims again.
				slog.ErrorContext(ctx, "claim jobs", "error", err)
				continue
			}
			// The claim never takes more jobs than free workers, so the send never waits for long.
			// Stopping halfway would leave claimed jobs out of the channel until the reconciler.
			for _, job := range jobs {
				jobChan <- job
			}
		}
	}

}

func (s *WorkerService) waitForWorkers(workers *sync.WaitGroup, cancelJobs context.CancelFunc) {
	workersDone := make(chan struct{})
	go func() {
		workers.Wait()
		close(workersDone)
	}()

	select {
	case <-workersDone:
	case <-time.After(s.shutdownGracePeriod):
		// The requests still running are cut. Their save fails too, so the reconciler takes them back.
		slog.Warn("grace period over, cancelling the jobs still running")
		cancelJobs()
		<-workersDone
	}
}

func (s *WorkerService) StartWorker(ctx, jobContext context.Context, workerID int, sem chan struct{}, jobChan chan *domain.Job) {
	for job := range jobChan {
		err := s.executeJob(ctx, jobContext, job, sem)
		if errors.Is(err, domain.ErrLeaseLost) {
			slog.WarnContext(jobContext, "job result discarded, lease lost", "job_id", job.ID, "attempt", job.Attempt)
		} else if err != nil {
			slog.ErrorContext(jobContext, "execute job", "job_id", job.ID, "error", err)
		}
	}
}

func (s *WorkerService) executeJob(ctx, jobContext context.Context, job *domain.Job, sem chan struct{}) error {
	// Acquire a worker slot
	sem <- struct{}{}
	defer func() { <-sem }()

	//Wait till the execution time to trigger the job
	select {
	case <-ctx.Done():
		// Shutting down before the request was sent: the job goes back as if it was never claimed.
		return s.db.ReleaseJob(jobContext, job)
	case <-time.After(time.Until(job.NextAttemptAt)):
	}

	// From here the request is out, so it uses jobContext and is allowed to finish during the shutdown.
	// The executor applies job.Config.Timeout to the request.
	result, err := s.requests.Execute(jobContext, job)
	job.Result = result

	// The request was never sent, and a retry would be blocked again.
	if err != nil {
		err := s.db.SaveDead(jobContext, job)
		if err != nil {
			return err
		}
		return nil
	}

	if job.Result.HTTPStatus != nil && *job.Result.HTTPStatus >= 200 && *job.Result.HTTPStatus < 300 {
		err := s.db.SaveSucceeded(jobContext, job)
		if err != nil {
			return err
		}
		return nil
	}

	if job.Attempt >= job.Config.Retry.MaxAttempts {
		err := s.db.SaveDead(jobContext, job)
		if err != nil {
			return err
		}
		return nil
	}

	nextAttemptAt := time.Now().Add(job.Config.Retry.Backoff(job.Attempt))
	err = s.db.SaveRetry(jobContext, job, nextAttemptAt)
	if err != nil {
		return err
	}

	return nil
}
