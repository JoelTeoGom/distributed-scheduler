# distributed-job-scheduler

![architecture](docs/distributed-scheduler.png)

A distributed job scheduler in Go: `at` and `cron` as a service. Users schedule HTTP calls to their own endpoints (once, at a given time, or on a cron expression) with priorities, automatic retries, a dead letter queue and a queryable execution history.

Designed for big tech scale (~1B executions/day, ~500k/s peak). Built and run here at a much smaller scale.

---

## Requirements

**Functional**

1. Schedule a task to run now, at a date/time (`at`) or on a cron expression.
2. Each task has a priority (`critical`, `high`, `normal`, `low`). Critical never waits behind low.
3. Failed jobs are retried up to a configurable maximum, then set aside (DLQ) for manual review or retry.
4. Query a job's status and a task's execution history.

**Non-functional**

- **Durability:** an accepted task is never lost.
- **At-least-once execution**, never dropped silently. Each job carries a stable idempotency key so the receiver can dedupe.
- **Scheduling latency (p99):** critical < 1 s, high < 20 s, normal < a few min, low < 20 min.
- **No starvation** for low priority, and **fairness** between tenants.
- **API availability:** 99.99%.
- **Security:** secrets encrypted, never in logs or history.

A **task** is the configuration (schedule, priority, request, retry policy). A **job** is one execution of it.

---

## How it works

1. **Task Service** creates and edits tasks in MySQL. The tenant comes from the JWT; `POST` takes an `Idempotency-Key`.
2. **Scheduler** scans its own task partitions with a lookahead, keeps due tasks in an in-memory timer, and at fire time inserts the job and advances `next_run_at` in one transaction (`UNIQUE(task_id, scheduled_at)`).
3. **Executors** claim batches from the jobs table with `FOR UPDATE SKIP LOCKED`, mark them `running` with a lease and commit, then call each endpoint in its own goroutine with a context timeout.
4. Results are written only if the job is still theirs (`WHERE status = 'running' AND worker_id = ?`). Failures go back to `pending` with backoff, or to `dead` after `max_attempts`.
5. **Reconciler** returns `running` jobs with an expired lease to `pending` (or `dead`).
6. **Archiver** writes terminal jobs to Cassandra and only then deletes them from the hot table.
7. **Job Service** serves status, history (hot table + Cassandra), the DLQ inbox and manual retry.

---

## Design decisions

**The jobs table is the queue (no broker).**
A broker (SQS, RabbitMQ) means writing the job to the database _and_ to the queue: a dual write that can leave one without the other. Claiming rows with `SKIP LOCKED` keeps one source of truth. Kafka was discarded anyway: it commits offsets per partition, so one slow job blocks the rest. Cost: all claiming load lands on MySQL, so the jobs table has to be sharded earlier.

**MySQL for tasks and active jobs.**
Every job goes through several state transitions, and each one must be conditional (`UPDATE ... WHERE status = 'running'`). In MySQL that is a cheap `WHERE`. Postgres was discarded for this table: every index points to the physical row, so each update rewrites the indexes (write amplification), indexing `status` makes those updates non-HOT, and a high-churn queue table means vacuum and bloat.

**Cassandra for history only.**
Cassandra handles huge write volumes, but it resolves conflicts by last-write-wins and conditional updates need Paxos, which is too expensive on every transition. Once a job is terminal it never changes, so those problems disappear: history is append-only, partitioned by `(task_id, time bucket)` with TTL for retention. The hot table stays small and fast.

**Leases in the database, not Redis locks.**
The claim already writes to the database, so a Redis lock adds a second source of truth that can disagree with it (and can be lost on failover) without saving any write. A lease (`lease_expires_at`, per job from its own timeout) plus a reconciler covers dead executors.

**At-least-once plus idempotency, not exactly-once.**
If a call times out, there is no way to know whether the endpoint processed it. Exactly-once delivery does not exist; the job id is sent as `Idempotency-Key` on every attempt and the receiver dedupes.

**Never run a job that is not persisted.**
Critical jobs skip the queue wait but are inserted as `running` and committed before the call.

**Precision from an in-memory timer.**
Polling the tasks table every ~30 s with a lookahead, then firing from an in-memory timer at the exact second, gives sub-second precision without polling the database every second. Executors poll adaptively (immediately when batches are full, backing off when empty).

---

## Scaling path

- Shard tasks and jobs by hash (e.g. Vitess); one scheduler owns a set of task partitions through a lease.
- Executors per shard; weighted claiming by priority and tenant.
- Concurrency limits per tenant/endpoint, so the 09:00 burst does not flood customer endpoints.
- Multi-region for 99.99%.

---

## Status

Work in progress. Design notes and the full discussion of alternatives live in [joel-software-notes](https://github.com/JoelTeoGom/joel-software-notes).
