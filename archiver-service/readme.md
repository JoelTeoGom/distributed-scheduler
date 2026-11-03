# archiver-service

Moves terminal jobs out of the MySQL hot table. Every interval it reads `succeeded` and `dead` jobs older than their retention, writes them to the Cassandra history and then deletes them from MySQL. It has no API: it only talks to MySQL and Cassandra.

## Use cases

### Archive jobs

A finished job never changes again, but it stays in the `jobs` table, which is also the queue. The archiver takes a batch of them, writes each one to `job_history` and `job_by_id`, and deletes from MySQL the ones that were written.

## Design decisions

**Cassandra first, then MySQL.** Writing to two stores cannot be atomic, so the order decides what a crash leaves behind. If the archiver dies after writing to Cassandra, the job is in both stores until the next sweep, and the job-service already handles that: MySQL wins and duplicates are removed by job id. The other order could delete a job that never reached the history.

**Writing twice is harmless.** Cassandra inserts are upserts by primary key, so a job written again after a crash overwrites its own rows.

**The delete checks the status and the attempt.** It deletes `(id, status, attempt)` as they were read. A dead job retried by hand in between goes back to `pending` with its attempts cleared, so it no longer matches and stays in the queue. Its copy in Cassandra is overwritten when it is archived again.

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
