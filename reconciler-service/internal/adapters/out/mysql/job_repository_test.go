//go:build integration

// Needs MySQL at RECONCILER_TEST_DSN with the job-service migrations applied.
// ReconcileJobs releases every expired job in the table, not only the ones seeded here.
package mysql_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JoelTeoGom/distributed-scheduler/reconciler-service/internal/adapters/out/mysql"
)

const maxAttempts = 3

type reconciledJob struct {
	status         string
	attempt        int
	workerID       sql.NullString
	leaseExpiresAt sql.NullTime
	errorMessage   sql.NullString
	finishedAt     sql.NullTime
}

func TestReconcileJobs(t *testing.T) {
	db := openDatabase(t)
	repository := mysql.NewJobRepository(db)

	expiredLease := time.Now().Add(-time.Minute)
	activeLease := time.Now().Add(time.Minute)
	withAttemptsLeft := insertJob(t, db, "running", 1, &expiredLease)
	onLastAttempt := insertJob(t, db, "running", maxAttempts, &expiredLease)
	stillRunning := insertJob(t, db, "running", 1, &activeLease)
	pending := insertJob(t, db, "pending", 0, nil)

	if _, err := repository.ReconcileJobs(context.Background(), 1000); err != nil {
		t.Fatal(err)
	}

	job := readJob(t, db, withAttemptsLeft)
	if job.status != "pending" || job.attempt != 1 || job.workerID.Valid || job.leaseExpiresAt.Valid ||
		job.errorMessage.String != "lease expired" || job.finishedAt.Valid {
		t.Errorf("job with attempts left: got %+v, want pending with the same attempt and no lease", job)
	}

	job = readJob(t, db, onLastAttempt)
	if job.status != "dead" || job.attempt != maxAttempts || job.errorMessage.String != "lease expired" || !job.finishedAt.Valid {
		t.Errorf("job on its last attempt: got %+v, want dead and finished", job)
	}

	job = readJob(t, db, stillRunning)
	if job.status != "running" || job.workerID.String != "test-executor" || !job.leaseExpiresAt.Valid {
		t.Errorf("job with an active lease: got %+v, want it untouched", job)
	}

	if job = readJob(t, db, pending); job.status != "pending" || job.errorMessage.Valid {
		t.Errorf("pending job: got %+v, want it untouched", job)
	}
}

func TestReconcileJobsInBatches(t *testing.T) {
	db := openDatabase(t)
	repository := mysql.NewJobRepository(db)

	expiredLease := time.Now().Add(-time.Minute)
	var jobIDs []uuid.UUID
	for range 5 {
		jobIDs = append(jobIDs, insertJob(t, db, "running", 1, &expiredLease))
	}

	reconciledJobs, err := repository.ReconcileJobs(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if reconciledJobs != 2 {
		t.Errorf("first batch: got %d jobs, want 2", reconciledJobs)
	}

	for reconciledJobs == 2 {
		if reconciledJobs, err = repository.ReconcileJobs(context.Background(), 2); err != nil {
			t.Fatal(err)
		}
	}

	for _, jobID := range jobIDs {
		if job := readJob(t, db, jobID); job.status != "pending" {
			t.Errorf("job %s: got status %s after draining, want pending", jobID, job.status)
		}
	}
}

func openDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("RECONCILER_TEST_DSN")
	if dsn == "" {
		t.Skip("RECONCILER_TEST_DSN is not set")
	}

	db, err := mysql.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertJob(t *testing.T, db *sql.DB, status string, attempt int, leaseExpiresAt *time.Time) uuid.UUID {
	t.Helper()
	jobID := uuid.Must(uuid.NewV7())
	tenantID := uuid.Must(uuid.NewV7())
	taskID := uuid.Must(uuid.NewV7())

	workerID := sql.NullString{}
	lease := sql.NullTime{}
	if leaseExpiresAt != nil {
		workerID = sql.NullString{String: "test-executor", Valid: true}
		lease = sql.NullTime{Time: leaseExpiresAt.UTC(), Valid: true}
	}

	if _, err := db.Exec(`
		INSERT INTO jobs (
			id, tenant_id, task_id, scheduled_at,
			priority, http_method, endpoint, timeout_seconds, max_attempts, backoff_base_seconds,
			status, attempt, next_attempt_at,
			worker_id, lease_expires_at,
			created_at, updated_at
		) VALUES (?, ?, ?, NOW(3), 2, 'POST', 'https://example.com/hook', 30, ?, 10, ?, ?, NOW(3), ?, ?, NOW(3), NOW(3))`,
		jobID[:], tenantID[:], taskID[:], maxAttempts, status, attempt, workerID, lease); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM jobs WHERE id = ?`, jobID[:]); err != nil {
			t.Errorf("delete job %s: %v", jobID, err)
		}
	})
	return jobID
}

func readJob(t *testing.T, db *sql.DB, jobID uuid.UUID) reconciledJob {
	t.Helper()
	var job reconciledJob
	if err := db.QueryRow(`
		SELECT status, attempt, worker_id, lease_expires_at, error, finished_at
		FROM jobs
		WHERE id = ?`,
		jobID[:]).Scan(&job.status, &job.attempt, &job.workerID, &job.leaseExpiresAt, &job.errorMessage, &job.finishedAt); err != nil {
		t.Fatal(err)
	}
	return job
}
