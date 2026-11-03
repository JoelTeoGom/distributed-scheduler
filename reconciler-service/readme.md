# reconciler-service

Recovers jobs whose executor died. Every interval it looks for `running` jobs whose lease expired and returns them to `pending`, or moves them to `dead` when they have no attempts left. It has no API: it only talks to MySQL.

## Use cases

### Reconcile jobs

A job stays `running` while an executor holds it. If the executor crashes, is killed or cannot save the result, nobody updates the job again, and its lease expires. The reconciler takes those jobs back in batches, with the error `lease expired`, so another executor can claim them.

## Design decisions

**A separate service, not part of the executor.** There are many executors and one sweep is enough. It works like a cron job of our own, and it scales and deploys on its own. If it is down, executions keep running: jobs only take longer to recover.

**The lost attempt is not counted again.** The claim already added one to `attempt`, so a job on its last attempt goes to `dead` instead of `pending`, and never runs more than `max_attempts` times.

**Back to pending at once.** `next_attempt_at` becomes now, so the next claim takes the job without waiting for a backoff: the job is already late.

**Safe with two replicas.** The update only touches rows that are still `running` with an expired lease, and InnoDB locks them, so two sweeps at the same time never release the same job twice. One replica is enough; a second one only does extra work.

**Every minute, in batches.** A job stays stuck at most its lease plus one interval. Each update is limited to `RECONCILE_BATCH_SIZE` rows, so a sweep after a long outage never locks the whole table at once. The `idx_jobs_lease (status, lease_expires_at)` index finds the expired jobs.

## Structure

```
cmd/                      wiring and process lifecycle
internal/
├── config/               environment variables and .env.local
├── ports/                out.go: what the service needs and offers
├── service/              the reconcile loop
└── adapters/out/
    └── mysql/            the reconcile query
```

## Run

It needs MySQL 8.4 with the job-service migrations applied (`job-service/migrations/mysql`).

```bash
cp .env.example .env.local
go run ./cmd
```

The repository test needs the same database. It releases every expired job in the table, not only the ones it seeds:

```bash
RECONCILER_TEST_DSN='scheduler:scheduler@tcp(localhost:3307)/scheduler?parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27' \
  go test -tags integration ./internal/adapters/out/mysql
```
