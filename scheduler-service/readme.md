# scheduler-service

Turns tasks into jobs. Every 5 seconds it takes the active tasks due in the next 30 seconds, inserts one `pending` job per task and moves each task's `next_run_at` forward. It has no API: it only talks to MySQL. Running the job at its exact time is the executors' work.

## Use cases

### Schedule jobs

In one transaction it selects up to 1000 due tasks with `FOR UPDATE`, ordered by `next_run_at`, and calculates each one's next run: none for an `at` task, the next occurrence for a `cron` task, in the task's timezone. Then it inserts the jobs in one statement and updates `next_run_at`, and commits.

Each job copies the task's request, timeout, retry policy and priority, so editing the task later does not change jobs that already exist. Its `scheduled_at` and first `next_attempt_at` are the task's `next_run_at`.

## Design decisions

**Jobs are created ahead of time.** The 30 second lookahead puts each job in the table before it is due, so an executor can claim it early and fire it from a timer at the exact time. Polling every few seconds is enough, because precision comes from the executor, not from this loop.

**The job and the next run move together.** Inserting the job and advancing `next_run_at` happen in the same transaction, so a crash in between leaves neither.

**No job is created twice.** `UNIQUE (task_id, scheduled_at)` with `ON DUPLICATE KEY UPDATE id = id` turns a repeated insert into a no-op. It happens when a resume inside the lookahead sets `next_run_at` back to a time that already has its job.

**Safe with two replicas.** The select uses `FOR UPDATE`, not `SKIP LOCKED`: a second scheduler waits for the first to commit, then reads the advanced `next_run_at` and skips those tasks. It adds no throughput, only availability.

**Missed runs are caught up, not skipped.** The next run is calculated from the previous `next_run_at`, not from now. After an outage, each tick creates the next missed run of every overdue task until it catches up. A resumed task is different: the task-service moves it to its next future run.

**`version` is never touched.** Bumping it on every run would invalidate the `If-Match` of every client editing a task that runs every minute.

**A database error does not stop the loop.** It is logged, and the next tick tries again.

## To do

- The interval (5 s), the lookahead (30 s) and the batch (1000) are constants. At most 1000 tasks per tick is about 200 jobs per second, so they should become configuration, and a full batch should start the next one at once.
- Several schedulers only add availability. Scaling out needs task partitions, each owned by one scheduler through a lease.

## Structure

```
cmd/                      wiring and process lifecycle
internal/
├── config/               environment variables and .env.local
├── domain/               Task, its schedule, request and errors
├── ports/                out.go: what the service needs and offers
├── service/              the scheduling loop
└── adapters/out/
    ├── mysql/            select due tasks, insert jobs, advance next_run_at
    └── cron/             next run calculation
```

## Run

It needs MySQL 8.4 with the task-service and job-service migrations applied.

```bash
cp .env.example .env.local
go run ./cmd
```

## Test

```bash
go test ./...
SCHEDULER_TEST_DSN='<dsn of a throwaway database>' go test -tags integration ./internal/adapters/out/mysql
```

The integration test schedules every due task it finds, so point it at a database you can empty.
