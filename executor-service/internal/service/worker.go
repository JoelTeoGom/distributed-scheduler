package service

import (
	"context"
	"time"

	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/domain"
	"github.com/JoelTeoGom/distributed-scheduler/executor-service/internal/ports"
)

type WorkerService struct {
	db           ports.JobRepository
	jobQueueSize int
	timeLimit    int
}

var _ ports.WorkerService = (*WorkerService)(nil)

func NewWorkerService(db ports.JobRepository, jobQueueSize, timeLimit int) *WorkerService {
	return &WorkerService{db: db, jobQueueSize: jobQueueSize, timeLimit: timeLimit}
}

func (s *WorkerService) StartWorkerPool(ctx context.Context, workerCount int) error {
	//available workers (WE WILL USE IT TO LIMIT DB CALLS FOR JOBS)
	sem := make(chan struct{}, workerCount)
	defer close(sem)

	jobChan := make(chan *domain.Job, s.jobQueueSize)
	defer close(jobChan)

	//Spin up the workers
	for i := range workerCount {
		go s.StartWorker(ctx, i, sem, jobChan)
	}

	ticker := time.NewTicker(10 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			semSize := len(sem)
			if semSize == 0 {
				continue
			}
			availableWorkers := workerCount - semSize
			if availableWorkers <= 0 {
				continue
			}
			jobs, err := s.db.ObtainJobs(ctx, s.timeLimit, availableWorkers)
			if err != nil {
				return err
			}
			for _, job := range jobs {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case jobChan <- job:
				}
			}
		}
	}

}

func (s *WorkerService) StartWorker(ctx context.Context, workerID int, sem chan struct{}, jobChan chan *domain.Job) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobChan:
			if !ok {
				return
			}
			s.executeJob(ctx, job, sem)
		}
	}
}

func (s *WorkerService) executeJob(ctx context.Context, job *domain.Job, sem chan struct{}) error {
	// Acquire a worker slot
	sem <- struct{}{}
	defer func() { <-sem }()

	//Wait till the execution time to trigger the job
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Until(job.NextAttemptAt)):
	}

	executionContext, cancel := context.WithTimeout(ctx, job.Config.Timeout)
	defer cancel()

	// Execute the job
	result := s.requests.Execute(ctx, job)

	return nil
}
