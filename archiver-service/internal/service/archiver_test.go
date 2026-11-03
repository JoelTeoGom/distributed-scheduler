package service

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/domain"
)

type fakeJobRepository struct {
	archivableJobs []*domain.Job
	deletedJobs    []*domain.Job
	listCalls      int
}

func (f *fakeJobRepository) ListArchivableJobs(_ context.Context, _, _ time.Time, jobLimit int) ([]*domain.Job, error) {
	f.listCalls++
	return slices.Clone(f.archivableJobs[:min(jobLimit, len(f.archivableJobs))]), nil
}

func (f *fakeJobRepository) DeleteJobs(_ context.Context, jobs []*domain.Job) (int64, error) {
	for _, job := range jobs {
		f.archivableJobs = slices.DeleteFunc(f.archivableJobs, func(archivable *domain.Job) bool { return archivable.ID == job.ID })
		f.deletedJobs = append(f.deletedJobs, job)
	}
	return int64(len(jobs)), nil
}

type fakeJobHistoryRepository struct {
	failingJobs map[uuid.UUID]bool
}

func (f *fakeJobHistoryRepository) CreateJobs(_ context.Context, jobs []*domain.Job) ([]*domain.Job, error) {
	var createdJobs []*domain.Job
	for _, job := range jobs {
		if !f.failingJobs[job.ID] {
			createdJobs = append(createdJobs, job)
		}
	}
	return createdJobs, nil
}

func newJobs(count int) []*domain.Job {
	jobs := make([]*domain.Job, count)
	for i := range jobs {
		jobs[i] = &domain.Job{ID: uuid.New(), Status: domain.StatusSucceeded, Attempt: 1}
	}
	return jobs
}

func newArchiver(jobs *fakeJobRepository, history *fakeJobHistoryRepository) *ArchiverService {
	return NewArchiverService(jobs, history, time.Minute, 2, time.Hour, 14*24*time.Hour)
}

func TestArchiveJobsDrainsInBatches(t *testing.T) {
	jobs := &fakeJobRepository{archivableJobs: newJobs(5)}
	newArchiver(jobs, &fakeJobHistoryRepository{}).archiveJobs(context.Background())

	if len(jobs.deletedJobs) != 5 || len(jobs.archivableJobs) != 0 {
		t.Errorf("deleted %d jobs, %d left; want 5 and 0", len(jobs.deletedJobs), len(jobs.archivableJobs))
	}
	if jobs.listCalls != 3 {
		t.Errorf("listed %d times, want 3 batches of 2, 2 and 1", jobs.listCalls)
	}
}

func TestArchiveJobsStopsWhenCassandraFails(t *testing.T) {
	archivableJobs := newJobs(4)
	failingJobs := map[uuid.UUID]bool{}
	for _, job := range archivableJobs {
		failingJobs[job.ID] = true
	}
	jobs := &fakeJobRepository{archivableJobs: archivableJobs}

	newArchiver(jobs, &fakeJobHistoryRepository{failingJobs: failingJobs}).archiveJobs(context.Background())

	if len(jobs.deletedJobs) != 0 {
		t.Errorf("deleted %d jobs that never reached Cassandra", len(jobs.deletedJobs))
	}
	if jobs.listCalls != 1 {
		t.Errorf("listed %d times, want 1: the same batch would come back on every loop", jobs.listCalls)
	}
}

func TestArchiveJobsDeletesOnlyWrittenJobs(t *testing.T) {
	archivableJobs := newJobs(3)
	failingJob := archivableJobs[0]
	jobs := &fakeJobRepository{archivableJobs: archivableJobs}

	newArchiver(jobs, &fakeJobHistoryRepository{failingJobs: map[uuid.UUID]bool{failingJob.ID: true}}).archiveJobs(context.Background())

	if len(jobs.deletedJobs) != 2 || slices.Contains(jobs.deletedJobs, failingJob) {
		t.Errorf("deleted %d jobs, want the 2 written ones", len(jobs.deletedJobs))
	}
	if len(jobs.archivableJobs) != 1 || jobs.archivableJobs[0] != failingJob {
		t.Errorf("the failing job must stay in MySQL for the next sweep")
	}
}

func TestStartArchiverStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := newArchiver(&fakeJobRepository{}, &fakeJobHistoryRepository{}).StartArchiver(ctx); err != nil {
		t.Errorf("got %v, want nil: a cancelled context is a normal shutdown", err)
	}
}
