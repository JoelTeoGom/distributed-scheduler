-- History of a task, newest first.
-- Partition (tenant_id, task_id, bucket): one partition per task per month,
-- so a cron that runs every minute never builds one huge partition,
-- and a read without the right tenant cannot reach the rows.
CREATE TABLE IF NOT EXISTS job_history (
    tenant_id     uuid,
    task_id       uuid,
    bucket        int,        -- month of scheduled_at, e.g. 202610
    scheduled_at  timestamp,
    job_id        uuid,
    status        text,       -- succeeded | dead
    attempt       tinyint,
    priority      tinyint,
    http_status   smallint,
    error         text,
    response_body blob,       -- already truncated by the executor
    started_at    timestamp,
    finished_at   timestamp,
    PRIMARY KEY ((tenant_id, task_id, bucket), scheduled_at, job_id)
) WITH CLUSTERING ORDER BY (scheduled_at DESC, job_id DESC)
  AND default_time_to_live = 2592000;  -- 30 days, then Cassandra deletes the row
