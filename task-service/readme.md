# task-service

Creates and manages tasks: HTTP calls scheduled once (`at`) or on a cron expression. A task is the configuration; each execution is a job, handled by other services.

The tenant comes from the JWT. `POST /tasks` accepts an `Idempotency-Key`, and `PATCH` uses `If-Match` for optimistic locking. Full API: [docs/openapi.yaml](docs/openapi.yaml).

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
