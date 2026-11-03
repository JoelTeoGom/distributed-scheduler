# Late results after a lease expires

An executor can finish a job after its lease has expired. By then the reconciler may have returned the job to `pending` and another executor may have claimed it. This note explains why the late result never corrupts the job's state, what it cannot prevent, and how to make it rare.

## The race

```text
t0  executor A claims the job      -> status = running, worker_id = A, attempt = 1,
                                      lease_expires_at = scheduled time + timeout + 30 s
t1  A gets a 200 at 2:58 of a 3:00 timeout and sends the "succeeded" UPDATE,
    but the DB connection is slow
t2  the lease expires; the reconciler runs
      UPDATE jobs SET status = IF(attempt >= max_attempts, 'dead', 'pending'), ...
      WHERE status = 'running' AND lease_expires_at < NOW(3)
t3  executor B claims the job      -> running, worker_id = B, attempt = 2
t4  A's UPDATE finally arrives
```

## Why the state stays correct

Every result write is a conditional update (compare-and-set):

```sql
WHERE id = ? AND status = 'running' AND worker_id = ? AND attempt = ?
```

`attempt` is incremented on every claim, so it acts as a fencing token. A late write from an older claim, made by the same pod or by another one, affects 0 rows.

Where A's write can land:

| A's write arrives | Row at that moment | Outcome |
|---|---|---|
| Before the reconciler | `running`, `worker_id = A`, `attempt = 1` | A's write matches and the job is `succeeded`. The reconciler's `WHERE status = 'running'` no longer matches, and B never claims it. |
| Between the reconciler and B's claim | `pending` or `dead`, `worker_id = NULL` | 0 rows: `status` is not `running`. A's result is dropped and B runs the job again. |
| After B's claim | `running`, `worker_id = B`, `attempt = 2` | 0 rows: `attempt` no longer matches, even if B is the same pod as A. B's state is untouched. |

There is no race inside MySQL. Two UPDATEs on the same row are serialized by InnoDB's exclusive row lock, and an UPDATE evaluates its `WHERE` on the latest committed version of the row (a current read), even under `REPEATABLE READ`.

## Refinement: accept late successes

A success that lands between the reconciler and the next claim can still be kept, as long as nobody has claimed the job again:

```sql
UPDATE jobs SET status = 'succeeded', ...
WHERE id = ? AND attempt = ? AND status IN ('running', 'pending', 'dead')
```

The reconciler does not change `attempt`, so this matches until a new claim. Late failures are always discarded: the retry is already scheduled.

## What the guard cannot prevent

The endpoint receiving the call twice: A already called it, and B calls it again. That is the at-least-once contract, and the `Idempotency-Key` covers it. See [Receiving jobs](../client/receiving-jobs.md).

## Making it rare

- **Lease margin larger than the timeout.** The lease is the timeout plus 30 s, and the HTTP context is the timeout.
- **A short timeout for the result write.** The result UPDATE has its own timeout (e.g. 5 s) and never runs past the `lease_expires_at` returned by the claim. Abandoning a late write is better than writing it late.
- **A metric for discarded late results** (0 rows affected). If it grows, the lease margin is too short or the database is slow.
