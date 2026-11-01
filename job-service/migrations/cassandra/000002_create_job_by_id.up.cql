-- Lookup for GET /jobs/{id}: Cassandra can only read by the full partition key,
-- so this table says where the job lives in job_history.
CREATE TABLE IF NOT EXISTS job_by_id (
    tenant_id     uuid,
    job_id        uuid,
    task_id       uuid,
    bucket        int,
    scheduled_at  timestamp,
    PRIMARY KEY ((tenant_id, job_id))
) WITH default_time_to_live = 2592000;  -- same TTL as job_history
