# task-service

Creates and manages tasks: HTTP calls scheduled once (`at`) or on a cron expression. A task is the configuration; each execution is a job, handled by other services.

The tenant always comes from the JWT, never from the path or the body. A task of another tenant is reported as `404`. Full API: [docs/openapi.yaml](docs/openapi.yaml).

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/tasks` | Create a task |
| `GET` | `/tasks` | List the tenant's tasks |
| `GET` | `/tasks/{id}` | Get a task |
| `PATCH` | `/tasks/{id}` | Edit a task |
| `POST` | `/tasks/{id}/pause` | Pause a task |
| `POST` | `/tasks/{id}/resume` | Resume a task |
| `DELETE` | `/tasks/{id}` | Soft delete a task |
| `GET` | `/health` | Health check, no token needed |

## Use cases

### Create a task

The domain validates the task and `next_run_at` is calculated: the date itself for `at`, the next occurrence for `cron`. An `at` in the past is rejected.

With an `Idempotency-Key`, the key is looked up first. The same key with the same body returns the original task with `200`, and with a different body returns `422`.

### Edit a task

`PATCH` needs `If-Match` with the version being edited. A missing header gives `428` and a stale version gives `412`. Only the fields sent change, while `schedule`, `request` and `retry` are replaced as a whole. Changing the schedule recalculates `next_run_at`.

### Pause and resume

Repeating either one changes nothing and returns `200`. A resumed cron task jumps to its next future run and missed runs are not recovered, while an `at` task keeps its pending run, so it fires right away if it became overdue. A finished `at` task never runs again.

### Delete a task

A soft delete. The task answers `404` everywhere and leaves the list, but stays in the table so that a retried `POST` still finds it.

## Design decisions

**Two layers of idempotency.** The first lookup answers a normal retry, even if the task would no longer validate, such as an `at` whose date has passed. The `UNIQUE (tenant_id, idempotency_key)` is the real guarantee: when two requests race, one gets error 1062 and returns the winner's task. Keys are compared byte by byte, so `ABC` and `abc` are different keys.

**`next_run_at` belongs to the scheduler.** `Update` never writes it, and `Reschedule` only does for resumes and schedule changes. Otherwise a `PATCH` of the name could overwrite the value the scheduler has just advanced. For the same reason the scheduler must not change `version`, or a task running every minute would invalidate every client's `If-Match`.

**Validation lives in the domain.** The same rules apply on creation and on edition, and HTTP knows nothing about them. Only the conversion from JSON stays in the adapter.

**URLs are checked against SSRF.** A task must use `https` and cannot point to `localhost`, private addresses or the cloud metadata service. This is not enough on its own: DNS can change later, so the executor checks the resolved address again on every connection (see [its SSRF section](../executor-service/readme.md#security-ssrf)).

**Secrets stay out of the task.** `Authorization`, `Cookie` and similar headers are rejected, as are credentials inside the URL. Tasks hold a `secret_ref` instead.

**UUIDv7 ids.** They are time-ordered, so inserts append to the clustered index, and `id` works as the pagination cursor.

**UTC everywhere.** Dates are stored in UTC, and the DSN sets the session `time_zone`, because `loc=UTC` alone does not affect `NOW()`. Cron expressions are evaluated in each task's timezone, so "09:00 in Madrid" follows daylight saving.

**Forced index for the list.** Without `FORCE INDEX`, MySQL chose `uq_tasks_idempotency` and sorted every task of the tenant on each page: 2,000 rows read per page instead of 50, measured with a tenant of 2,000 tasks.

## Structure

```
cmd/api/                  wiring and process lifecycle
internal/
├── config/               environment variables and .env.local
├── domain/               Task, validation and business errors
├── ports/                in.go: what the service offers; out.go: what it needs
├── service/              use cases
└── adapters/
    ├── in/httpapi/       server, routes, handlers and DTOs
    └── out/
        ├── mysql/        task repository
        ├── cron/         next run calculation
        └── token/        JWT verification
migrations/               schema SQL
test/e2e/                 tests against the running service
docs/                     OpenAPI spec
```

## Run

Starts MySQL 8.4 on port `3307` and applies the migrations:

```bash
docker compose up -d
cp .env.example .env.local   # then set JWT_SECRET (at least 32 bytes)
go run ./cmd/api
```

The service listens on `:8080`.

## Test

```bash
go test ./...
JWT_SECRET=<same-secret> go test -tags e2e -count=1 ./test/e2e
```

The e2e tests need the service running.
