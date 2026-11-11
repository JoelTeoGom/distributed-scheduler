# The life of a job during a test

> From the task the bench writes into MySQL to the result the executor saves, with the time each step happens.

`t` is the time the job is due. The times are the tests' settings; the services' defaults are slower (see [service settings](../../../benchmarks.md#service-settings-for-the-tests)).

| When | Who | What happens |
|---|---|---|
| t − 60 s | `workload.py` | Writes the test's tasks straight into the `tasks` table (the Task API is not under test). The first one is due 60 s later, longer than the scheduler's lookahead. |
| t − 10 s | scheduler | Every second it takes the tasks due in the next 10 s, at most 1,000 at a time, and inserts one `pending` row in `jobs` per task. A batch of 1,000 takes ~3.3 s, so it creates ~300 jobs per second: the limit the tests found. |
| t − 1 s … t | executor | Every second it claims the jobs due in the next second with `FOR UPDATE SKIP LOCKED`, at most as many as free workers, and marks them `running` with a lease. |
| until t | a worker | Waits for the job's exact time, holding its worker slot. With the default 10 s this is what capped E0 at 5 jobs/s. |
| t | executor → fake endpoint | `POST http://fake-endpoint:8080/t?lat=…` with `X-Scheduled-At`, `X-Attempt` and `Idempotency-Key`. |
| t + delay | fake endpoint | Records how late the call arrived (arrival − `X-Scheduled-At`), sleeps as the URL says, answers 200 or 503. |
| after the call | executor → MySQL | Saves the result: `succeeded`, back to `pending` with a backoff to retry, or `dead` when the attempts run out. |
| later | reconciler, archiver | Every 10 s, the reconciler puts back the jobs whose lease expired (their executor died), and the archiver moves jobs finished over an hour ago to Cassandra. |

## Why the delay is measured at the endpoint

`started_at` in MySQL is the claim time, up to a second before the job is due, not the call time. The fake endpoint sees the real arrival, so the start delay comes from there. `X-Scheduled-At` has second precision, so every seeded job is due on a whole second.
