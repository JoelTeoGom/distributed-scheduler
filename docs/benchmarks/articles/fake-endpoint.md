# The fake endpoint

> What the jobs call during a test: a small Go service that behaves like a customer's API and measures how late each call arrives.

In production every job calls a customer's API. In the bench they all call [`bench/fake-endpoint`](../../../bench/fake-endpoint), one Deployment with 2 vCPU, big enough never to be the bottleneck. Each job carries in its URL how the endpoint must behave, so every test picks its scenario without touching the endpoint.

| Route | What it does |
|---|---|
| `/t?lat=50&dist=lognormal&err=0.05&p=2&t=7&k=fast` | Sleeps about `lat` ms (fixed, lognormal with a long tail like real endpoints, or `dist=uniform` between `min` and `max`), answers 503 with probability `err`, else 200. `p`, `t` and `k` are the job's priority, tenant and endpoint profile, to split the results by each. |
| `GET /stats` | Requests, status codes, duplicate successes (the same `Idempotency-Key` answered 200 twice) and start delay percentiles per priority. |
| `GET /events` | Every call as CSV: arrival, due time, delay, priority, tenant, profile, attempt, status and key. The reports are built from it. |
| `POST /reset` | Clears everything before a test. |

## What it measures

- **Start delay:** arrival time minus `X-Scheduled-At`, taken before sleeping, on first attempts only so retries do not mix in their backoff.
- **Duplicates:** at-least-once delivery means a job may run twice after a failure. Counting 200s per `Idempotency-Key` shows how often it happens.
- **Work done after a timeout:** when the executor gives up before the endpoint answers, the endpoint still finishes and records the call as 499. The reports count those apart (*timed out, done anyway*): the customer did the work, and the retry will make it do it again.

The executor blocks private addresses to prevent SSRF. Its address is a ClusterIP, so the bench runs the executor with `ALLOW_PRIVATE_DESTINATIONS=true`.
