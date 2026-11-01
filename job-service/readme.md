# job-service

The read side of the scheduler, plus manual retries. It answers three questions: what happened to a job, what is the execution history of a task, and which jobs ran out of retries. A job is one execution of a task. Creating, scheduling and running jobs belong to other services.

The tenant always comes from the JWT, never from the path or the body. A job of another tenant is reported as `404`, never `403`. Full API: [docs/openapi.yaml](docs/openapi.yaml).

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/jobs/{id}` | Get a job's status |
| `GET` | `/tasks/{id}/jobs` | Get a task's execution history |
| `GET` | `/jobs?status=dead` | List the dead letter queue (DLQ) |
| `POST` | `/jobs/{id}/retry` | Retry a dead job |
| `GET` | `/health` | Health check, no token needed |

## Where jobs live

| Store | Holds | Why |
|---|---|---|
| MySQL `jobs` | pending and running jobs, plus dead jobs inside the DLQ retention window | every transition is conditional, and it is also the queue |
| Cassandra `job_history` | terminal jobs moved by the archiver, for 30 days | append-only history, written once and never changed |
| Cassandra `job_by_id` | where each archived job lives in `job_history` | Cassandra can only read by the full partition key |

## Use cases

### Get a job

Both stores are queried in parallel. During the archive handover a job can be in both, and MySQL wins. If one store fails, a job found in the other is still returned. If neither can confirm the job, the request fails with `500` rather than a false `404`.

In Cassandra this takes two reads: `job_by_id` says where the job is, and `job_history` returns it.

### Execution history

Both stores are queried in parallel, each sorted by `scheduled_at` descending, and the two lists are merged with two pointers. Appending would break the order, because an old dead job can stay in MySQL while newer jobs are already in Cassandra. Duplicates from the handover are removed by job id, keeping the MySQL copy.

The cursor is the `scheduled_at` of the last job, which is unique per task. If a store fails the request fails too: a partial history would look complete.

### DLQ inbox

MySQL only, paginated by job id. Since ids are UUIDv7, the list is in time order.

### Manual retry

A dead job goes back to `pending` with its attempts and its last result cleared. It keeps its id and `scheduled_at`: the id is the `Idempotency-Key` the endpoint receives, so it can deduplicate, and `scheduled_at` tells which run this is.

The update is conditional on `status = 'dead'`. If it changes nothing, the job is read to tell `404` (missing, or already archived) from `409` (not dead). The request answers `202`, since the job runs later.

## Design decisions

**The tenant is part of the Cassandra partition key.** `job_history` is keyed by `(tenant_id, task_id, bucket)` and `job_by_id` by `(tenant_id, job_id)`. A read with the wrong tenant finds nothing, and one without it is rejected by Cassandra, so isolation does not depend on remembering a filter. In MySQL it depends on the `WHERE`, so every query has it.

**One partition per task per month.** `bucket` is the UTC month of `scheduled_at`. It stops a task that runs every minute from building one huge partition. The repository walks the months backwards, and the service knows nothing about buckets.

**Cursors, not page numbers.** Cassandra has no `OFFSET` and no cheap `COUNT`, so the API returns `next_cursor` instead of a page count. Every page costs the same, and the same cursor works on both stores.

**Archived jobs have a reduced view.** Cassandra stores the result of a job but not its configuration, so `request`, `retry` and `timeout_seconds` are absent for archived jobs. History lives 30 days, and copying request bodies into it costs more than it gives.

**This service never reads `tasks`.** Each service owns its data. A task that does not exist, or belongs to another tenant, returns an empty history.

**Forced indexes for the DLQ.** Without `FORCE INDEX`, MySQL intersected two indexes on later pages and sorted the result: 2,000 index reads per page instead of 50, measured with 300,000 jobs.

## Structure

```
cmd/api/                  wiring and process lifecycle
internal/
├── config/               environment variables and .env.local
├── domain/               Job, its status and business errors
├── ports/                in.go: what the service offers; out.go: what it needs
├── service/              use cases and the history merge
└── adapters/
    ├── in/httpapi/       server, routes, handlers and DTOs
    └── out/
        ├── mysql/        hot jobs repository
        ├── cassandra/    job history repository
        └── token/        JWT verification
migrations/
├── mysql/                hot jobs table
└── cassandra/            keyspace, job history and lookup by id
docs/                     OpenAPI spec
```

## Run

It needs MySQL 8.4 with `migrations/mysql` applied, and Cassandra 5.0 with `migrations/cassandra/keyspace.cql` and then the rest of `migrations/cassandra` applied. There is no docker compose for this service yet.

```bash
cp .env.example .env.local   # then set JWT_SECRET (at least 32 bytes)
go run ./cmd/api
```

The service listens on `:8081`. Use the same `JWT_SECRET` as the Task Service so one token works for both.

If Cassandra runs in Docker, it has to advertise an address reachable from the host, such as `CASSANDRA_BROADCAST_RPC_ADDRESS=127.0.0.1` with `CASSANDRA_RPC_ADDRESS=0.0.0.0`, or the driver cannot connect.

## Test

```bash
go test ./...
go test -tags integration ./internal/adapters/out/cassandra/   # needs Cassandra on 127.0.0.1:9042
```
