# The life of a job during a test

> From the task the bench writes into MySQL to the result the executor saves, with the time each step happens.

`t` is the time the job is due.

| When | Who | What happens |
|---|---|---|
| t − 60 s | `seed.sh` | Writes the test's tasks straight into the `tasks` table (the Task API is not under test). The first one is due 60 s later, longer than the scheduler's lookahead. |
| t − 30 s | scheduler | Every 5 s it takes the tasks due in the next 30 s and inserts one `pending` row in `jobs` per task. |
| t − 10 s … t | executor | Every 10 s it claims the jobs due in the next 10 s with `FOR UPDATE SKIP LOCKED`, at most as many as free workers, and marks them `running` with a lease. |
| until t | a worker | Waits for the job's exact time, holding its worker slot. This is what capped E0 at 5 jobs/s. |
| t | executor → target | `POST http://target:8080/t?lat=…` with `X-Scheduled-At`, `X-Attempt` and `Idempotency-Key`. |
| t + delay | target | Records how late the call arrived (arrival − `X-Scheduled-At`), sleeps as the URL says, answers 200 or 503. |
| after the call | executor → MySQL | Saves the result: `succeeded`, back to `pending` with a backoff to retry, or `dead` when the attempts run out. |
| later | reconciler, archiver | Every minute, the reconciler puts back the jobs whose lease expired (their executor died), and the archiver moves jobs finished over an hour ago to Cassandra. |

## Why the delay is measured at the endpoint

`started_at` in MySQL is the claim time, up to 10 s before the job is due, not the call time. The fake endpoint sees the real arrival, so the start delay comes from there. `X-Scheduled-At` has second precision, so every seeded job is due on a whole second.
