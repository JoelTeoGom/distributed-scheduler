# executor-service

Runs jobs. It claims due jobs from the MySQL `jobs` table, waits until each one's time, makes the HTTP call the tenant configured and saves the result. A job is one execution of a task. Creating tasks and jobs belongs to other services.

It has no API: it only talks to MySQL and to the tenants' endpoints. Work in progress is tracked in [docs/executor-plan.md](../docs/executor-plan.md).

## Use cases

### Claim jobs

Every polling interval, the executor claims as many jobs as it has free workers, among the `pending` ones due before the next poll. In one short transaction it selects them with `FOR UPDATE SKIP LOCKED`, marks them `running` with its own id and a lease, and adds one to `attempt`.

### Execute a job

Each job waits until its `next_attempt_at`, never before, and then sends the request with the job's timeout. The response body is read up to a limit. A failed request goes into the result, not into an error: a timeout, a refused connection or a `500` are outcomes of the job, not failures of the executor.

## Design decisions

**The table is the queue.** `SKIP LOCKED` lets several executors claim at the same time without waiting for each other's rows. The transaction commits before any HTTP call, so row locks last milliseconds and a slow endpoint never holds one.

**Jobs are claimed ahead of time.** The claim takes jobs due up to one polling interval ahead, and each waits on its own timer. Without that lead, a job could start up to a whole interval late.

**The lease starts at the job's time, not at the claim.** It is `GREATEST(next_attempt_at, NOW()) + timeout + 30 s`. Counted from the claim, the reconciler would take back jobs still waiting for their timer.

**`worker_id` is the executor, not a goroutine.** It holds `EXECUTOR_ID`, or the hostname when it is empty, which in Kubernetes is the pod name. Inside a pod each job belongs to one goroutine, so the database only needs to know which instance claimed it.

**`attempt` grows on the claim.** A job whose executor dies mid-request has already spent its attempt, so the reconciler does not need to count it again, and the executor decides between retry and dead by comparing it with `max_attempts` without adding one.

**One update per outcome.** Succeeded, retry and dead each have their own query, because each clears different columns: retry frees `worker_id` and moves `next_attempt_at`, while succeeded and dead keep `worker_id` to show which executor ran the job. A generic update with a list of fields would leave the service in charge of remembering them.

**`attempt` is the fencing token.** Every result update is guarded by `status = 'running' AND worker_id = ? AND attempt = ?`, with the attempt of this claim. If the lease expired and the job was claimed again, even by the same executor, the late write affects 0 rows and returns `ErrLeaseLost`. The result is dropped: the job may run twice, and the `Idempotency-Key` covers that.

**Late results after a lease expires** are walked through step by step in [docs/design/late-results.md](../docs/design/late-results.md).

**What customers must do with duplicate deliveries** is in [docs/client/receiving-jobs.md](../docs/client/receiving-jobs.md).

**Every job sends `Idempotency-Key: <job id>`.** Execution is at least once: a retry or an expired lease can run a job twice, and the endpoint can use the key to detect the duplicate. `X-Scheduled-At` tells the endpoint which run this is and `X-Attempt` which attempt. All three are set after the tenant's headers, so a task cannot overwrite them.

### Security: SSRF

The executor calls URLs chosen by tenants from inside the network, next to MySQL, other services and the cloud metadata service. SSRF (Server-Side Request Forgery) is a tenant using it to reach them. The task-service already rejects internal URLs, but that is not enough: the DNS answer can change between the check and the call, a public name can resolve to an internal IP, and the same IP has several spellings.

**The real IP is checked at dial time.** The dialer's `Control` runs after DNS resolution and before `connect()`, with the exact address being dialed. It runs on every connection, so it covers every IP of a name and DNS rebinding. It allows only `tcp4` and `tcp6`, ports 80 and 443, and public unicast addresses. IPv4-mapped IPv6 is unmapped first, and the ranges `IsGlobalUnicast` and `IsPrivate` let through (CGNAT, NAT64, 6to4, reserved) are blocked as well.

> `Control` is commented out in `NewExecutor` for now, so local tests can call endpoints on `localhost`. It has to be enabled before deploying.

**No proxy.** A proxy resolves the destination itself, so `Control` would only see the proxy's address. Only `http.DefaultTransport` reads `HTTP_PROXY` and `HTTPS_PROXY`; the executor uses its own `Transport`, with `Proxy: nil` written explicitly.

**No redirects.** A public endpoint could answer `302` to an internal URL. The 3xx is stored as the job's result, so the job calls exactly the configured URL.

**Generic error for blocked destinations.** The stored error is visible to the tenant, so a blocked dial is saved as `destination not allowed`. With the dial detail, the tenant would learn the internal IP behind a name, or map the network from "refused" vs "timeout".

**Limits on everything.** A timeout per job, timeouts for connect and TLS, and a limited response body, so an endpoint that never answers or returns gigabytes cannot take a worker down.

Network-level protection (an egress `NetworkPolicy`, IMDSv2) is left to the deployment. An egress proxy like Stripe's Smokescreen is out of scope for this prototype.

## Structure

```
cmd/                      wiring and process lifecycle
internal/
├── config/               environment variables and .env.local
├── domain/               Job, its status, request, result and errors
├── ports/                out.go: what the service needs and offers
├── service/              worker pool: claim, wait and execute
└── adapters/out/
    ├── mysql/            job claim repository
    └── httpclient/       HTTP executor and the SSRF dial check
```

## Run

It needs MySQL 8.4 with the job-service migrations applied (`job-service/migrations/mysql`).

```bash
cp .env.example .env.local
go run ./cmd
```
