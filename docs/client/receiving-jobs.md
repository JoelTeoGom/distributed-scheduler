# Receiving jobs: delivery guarantees and idempotency

What your endpoint can expect when the scheduler calls it, and what it must do so each job takes effect once.

## Delivery is at least once

Your endpoint may receive the same job more than once:

- after a retry;
- after a timeout, when we could not know whether you processed it;
- after one of our executors failed mid-call.

Exactly-once delivery over a network does not exist. Deduplication on your side is what makes the effect happen once.

## Success and retries

Any `2xx` is a success. Anything else (other status codes, timeouts, connection errors) is retried with exponential backoff, up to the task's maximum attempts. Redirects are not followed: a `3xx` counts as a failure.

> **Coming later:** `429` and `503` will honour `Retry-After`. Today they are retried on the normal backoff.

After the last attempt the job goes to the dead letter queue, where it can be retried manually with the same job ID.

## Headers we send

Every attempt carries:

| Header | Value | Use |
|---|---|---|
| `Idempotency-Key` | the job ID | Same value on every retry and re-execution of that job |
| `X-Scheduled-At` | RFC 3339 time | Which run this is, such as the report for October 30 |
| `X-Attempt` | `n` | Attempt number, for your logs |

## What you must do

1. **Store each `Idempotency-Key` you process**, with a `UNIQUE` constraint, in the same transaction as the work it triggers.
2. **If a key arrives again and was already processed, don't repeat the work.** Answer `2xx` immediately, ideally with the same response as the first time.
3. **If a key is still being processed** (the first request hasn't finished), answer `409` or `503` so we retry later, instead of running it twice in parallel.
4. **Answer within the task's timeout.** Do long work asynchronously and return `202`.

## Example: a payment endpoint

Without deduplication, a timeout followed by a retry charges the customer twice. With it, the second request returns the stored `200` and nothing is charged again.

```sql
CREATE TABLE processed_requests (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    status_code     INT,
    response_body   BLOB,
    created_at      DATETIME(3)
);
```

```go
package main

import (
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/go-sql-driver/mysql"
)

type server struct {
	db *sql.DB
}

func (s *server) handlePayment(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		http.Error(w, "missing Idempotency-Key", http.StatusBadRequest)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(r.Context(),
		`INSERT INTO processed_requests (idempotency_key, created_at) VALUES (?, NOW(3))`, key)
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
		s.replay(w, r, key)
		return
	}
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}

	statusCode, responseBody := http.StatusOK, []byte(`{"charged":true}`) // charge the card here

	if _, err := tx.ExecContext(r.Context(),
		`UPDATE processed_requests SET status_code = ?, response_body = ? WHERE idempotency_key = ?`,
		statusCode, responseBody, key); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(statusCode)
	w.Write(responseBody)
}

// replay answers a key that was already processed with the stored response.
func (s *server) replay(w http.ResponseWriter, r *http.Request, key string) {
	var statusCode int
	var responseBody []byte
	err := s.db.QueryRowContext(r.Context(),
		`SELECT status_code, response_body FROM processed_requests WHERE idempotency_key = ?`, key).
		Scan(&statusCode, &responseBody)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(statusCode)
	w.Write(responseBody)
}

func main() {
	db, err := sql.Open("mysql", "user:password@tcp(localhost:3306)/payments")
	if err != nil {
		log.Fatal(err)
	}
	s := &server{db: db}
	http.HandleFunc("POST /payments", s.handlePayment)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

If the first request is still running, the second `INSERT` waits on InnoDB's row lock until the first one commits, then gets error `1062` and replays the stored response. The work never runs twice in parallel, but the second request waits instead of getting the `409` from step 3.

## Retention

Keep processed keys at least as long as a job can still be retried, which is the dead letter retry window of 14 days, then expire them.
