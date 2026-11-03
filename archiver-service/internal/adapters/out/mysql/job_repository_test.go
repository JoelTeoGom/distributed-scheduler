//go:build integration

// Needs MySQL at ARCHIVER_TEST_DSN with the job-service migrations applied.
// Seeded jobs finished in 2000, so the cutoffs never reach the real rows of the table.
package mysql_test

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/adapters/out/mysql"
	"github.com/JoelTeoGom/distributed-scheduler/archiver-service/internal/domain"
)

var (
	longAgo         = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	succeededBefore = longAgo.Add(time.Hour)
	deadBefore      = longAgo.Add(24 * time.Hour)
)

func TestListArchivableJobs(t *testing.T) {
	db := openDatabase(t)
	repository := mysql.NewJobRepository(db)

	oldSucceeded := insertJob(t, db, "succeeded", longAgo)
	oldDead := insertJob(t, db, "dead", longAgo)
	recentSucceeded := insertJob(t, db, "succeeded", succeededBefore.Add(time.Minute))
	deadInsideRetention := insertJob(t, db, "dead", deadBefore.Add(time.Minute))
	pending := insertJob(t, db, "pending", longAgo)

	jobs, err := repository.ListArchivableJobs(context.Background(), succeededBefore, deadBefore, 100)
	if err != nil {
		t.Fatal(err)
	}

	listed := jobIDs(jobs)
	for _, jobID := range []uuid.UUID{oldSucceeded, oldDead} {
		if !slices.Contains(listed, jobID) {
			t.Errorf("job %s is past its retention but was not listed", jobID)
		}
	}
	for _, jobID := range []uuid.UUID{recentSucceeded, deadInsideRetention, pending} {
		if slices.Contains(listed, jobID) {
			t.Errorf("job %s was listed but must stay", jobID)
		}
	}

	for _, job := range jobs {
		if job.ID != oldSucceeded {
			continue
		}
		if job.Status != domain.StatusSucceeded || job.Attempt != 1 || *job.Result.HTTPStatus != 200 ||
			string(job.Result.ResponseBody) != "ok" || !job.Result.FinishedAt.Equal(longAgo) || job.Result.Error != "" {
			t.Errorf("succeeded job: got %+v, result %+v", job, job.Result)
		}
	}
}

func TestDeleteJobsSkipsJobsThatChanged(t *testing.T) {
	db := openDatabase(t)
	repository := mysql.NewJobRepository(db)

	insertJob(t, db, "succeeded", longAgo)
	retriedByHand := insertJob(t, db, "dead", longAgo)

	jobs, err := repository.ListArchivableJobs(context.Background(), succeededBefore, deadBefore, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d archivable jobs, want 2", len(jobs))
	}

	// What the job-service manual retry does between the read and the delete.
	if _, err := db.Exec(`UPDATE jobs SET status = 'pending', attempt = 0 WHERE id = ?`, retriedByHand[:]); err != nil {
		t.Fatal(err)
	}

	deletedJobs, err := repository.DeleteJobs(context.Background(), jobs)
	if err != nil {
		t.Fatal(err)
	}
	if deletedJobs != 1 {
		t.Errorf("got %d deleted jobs, want 1", deletedJobs)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM jobs WHERE id = ?`, retriedByHand[:]).Scan(&status); err != nil {
		t.Fatalf("retried job was deleted: %v", err)
	}
}

func TestDeleteJobsWithNoJobs(t *testing.T) {
	db := openDatabase(t)
	deletedJobs, err := mysql.NewJobRepository(db).DeleteJobs(context.Background(), nil)
	if err != nil || deletedJobs != 0 {
		t.Errorf("got %d, %v; want 0, nil", deletedJobs, err)
	}
}

func openDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("ARCHIVER_TEST_DSN")
	if dsn == "" {
		t.Skip("ARCHIVER_TEST_DSN is not set")
	}

	db, err := mysql.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertJob(t *testing.T, db *sql.DB, status string, finishedAt time.Time) uuid.UUID {
	t.Helper()
	jobID := uuid.Must(uuid.NewV7())
	tenantID := uuid.Must(uuid.NewV7())
	taskID := uuid.Must(uuid.NewV7())

	if _, err := db.Exec(`
		INSERT INTO jobs (
			id, tenant_id, task_id, scheduled_at,
			priority, http_method, endpoint, timeout_seconds, max_attempts, backoff_base_seconds,
			status, attempt, next_attempt_at,
			started_at, finished_at, http_status, response_body,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, 2, 'POST', 'https://example.com/hook', 30, 3, 10, ?, 1, ?, ?, ?, 200, 'ok', NOW(3), NOW(3))`,
		jobID[:], tenantID[:], taskID[:], finishedAt, status, finishedAt, finishedAt, finishedAt); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM jobs WHERE id = ?`, jobID[:]); err != nil {
			t.Errorf("delete job %s: %v", jobID, err)
		}
	})
	return jobID
}

func jobIDs(jobs []*domain.Job) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, job.ID)
	}
	return ids
}
