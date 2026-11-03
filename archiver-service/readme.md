# archiver-service

Moves terminal jobs out of the MySQL hot table. Every interval it reads `succeeded` and `dead` jobs older than their retention, writes them to the Cassandra history and then deletes them from MySQL. It has no API: it only talks to MySQL and Cassandra.

## Use cases

### Archive jobs

A finished job never changes again, but it stays in the `jobs` table, which is also the queue. The archiver takes a batch of them, writes each one to `job_history` and `job_by_id`, and deletes from MySQL the ones that were written.

## Design decisions

**Cassandra first, then MySQL.** Writing to two stores cannot be atomic, so the order decides what a crash leaves behind. If the archiver dies after writing to Cassandra, the job is in both stores until the next sweep, and the job-service already handles that: MySQL wins and duplicates are removed by job id. The other order could delete a job that never reached the history.

**Writing twice is harmless.** Cassandra inserts are upserts by primary key, so a job written again after a crash overwrites its own rows.

**The delete checks the status and the attempt.** It deletes `(id, status, attempt)` as they were read. A dead job retried by hand in between goes back to `pending` with its attempts cleared, so it no longer matches and stays in the queue. Its copy in Cassandra is overwritten when it is archived again.

**An index for the sweep.** `idx_jobs_archive (status, finished_at)`, in the job-service migrations, turns each status into one range of the index. Without it every sweep reads the whole table.

**Parallel inserts, not batches.** Cassandra has no bulk insert: a batch across partitions goes through one coordinator and its batch log, and is slower than separate writes. Each job is written by its own goroutine, at most 64 at a time. Against a local node, 500 jobs take about 20 ms instead of 1.8 s one by one. A job that fails is logged and left out, so it stays in MySQL until the next sweep.

**Empty columns are left unset in Cassandra.** Binding `null` in an `INSERT` writes a tombstone, and tombstones slow down reads until compaction removes them. A job with no response, for example, leaves `http_status` and `response_body` unset.

**Batches until there is nothing left, or no progress.** Each tick repeats list, write and delete while batches come back full. If a batch deletes nothing, Cassandra is failing and the next list would return the same jobs, so the loop waits for the next tick instead of spinning.

**Each status has its own retention.** Succeeded jobs leave after `SUCCEEDED_RETENTION` (1 hour). Dead jobs stay `DEAD_RETENTION` (14 days), the window in which a tenant can retry them from the DLQ, which only reads MySQL.

## Structure

```
cmd/                      wiring and process lifecycle
internal/
├── config/               environment variables and .env.local
├── domain/               the archived Job and its result
├── ports/                out.go: what the service needs and offers
├── service/              the archive loop
└── adapters/out/
    ├── mysql/            read archivable jobs and delete them
    └── cassandra/        write the job history
```

## Run

It needs MySQL 8.4 with the job-service migrations applied (`job-service/migrations/mysql`), and Cassandra with the keyspace and tables from `job-service/migrations/cassandra`.

```bash
cp .env.example .env.local
go run ./cmd
```

The service tests use in-memory fakes and need nothing: `go test ./...`. The adapter tests need both stores. The MySQL jobs they seed finished in 2000, so they never touch real rows:

```bash
ARCHIVER_TEST_DSN='scheduler:scheduler@tcp(localhost:3307)/scheduler?parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27' \
  go test -tags integration ./internal/adapters/...
```
