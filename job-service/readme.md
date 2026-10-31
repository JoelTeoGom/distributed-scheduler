# job-service

Reads the executions of tasks: job status, the execution history of a task and the dead letter queue (DLQ), plus manual retries of dead jobs. A job is one execution of a task. Scheduling and running jobs belong to other services.

The tenant comes from the JWT. Full API: [docs/openapi.yaml](docs/openapi.yaml).

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/jobs/{id}` | Get a job's status |
| `GET` | `/tasks/{id}/jobs` | Get a task's execution history |
| `GET` | `/jobs?status=dead` | List the DLQ inbox |
| `POST` | `/jobs/{id}/retry` | Retry a dead job |
| `GET` | `/health` | Health check, no token needed |

**Status:** work in progress. The HTTP layer is done, but the use cases and the MySQL adapter are not, so only `/health` and token rejection work for now.

## Structure

```
cmd/api/                  wiring and process lifecycle
internal/
├── config/               environment variables and .env.local
├── domain/               Job, its status and business errors
├── ports/                in.go: what the service offers; out.go: what it needs
├── service/              use cases (pending)
└── adapters/
    ├── in/httpapi/       server, routes, handlers and DTOs
    └── out/
        ├── mysql/        job repository (pending)
        └── token/        JWT verification
migrations/               schema SQL for the jobs table
docs/                     OpenAPI spec
```

## Run

```bash
cp .env.example .env.local   # then set JWT_SECRET (at least 32 bytes)
go run ./cmd/api
```

The service listens on `:8081`. Use the same `JWT_SECRET` as the Task Service so one token works for both.

## Test

```bash
go test ./...
```
