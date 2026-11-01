-- Jobs: one execution of a task. This hot table is also the queue:
-- executors claim rows with SELECT ... FOR UPDATE SKIP LOCKED.
-- Terminal jobs are moved to Cassandra by the archiver, dead ones after the DLQ retention window.
-- All DATETIME values are stored in UTC.

CREATE TABLE jobs (
    id                   BINARY(16)        NOT NULL,              -- UUIDv7; also the Idempotency-Key sent to the endpoint
    tenant_id            BINARY(16)        NOT NULL,              -- copied from the task: DLQ per tenant + tenant isolation
    task_id              BINARY(16)        NOT NULL,
    scheduled_at         DATETIME(3)       NOT NULL,              -- logical fire time; started_at - scheduled_at = scheduling lag

    -- snapshot of the task config when the job was created:
    -- editing the task does not change jobs that already exist
    priority             TINYINT UNSIGNED  NOT NULL,              -- 0 = critical ... 3 = low
    http_method          VARCHAR(10)       NOT NULL,
    endpoint             VARCHAR(2048)     NOT NULL,
    headers              JSON              NULL,
    body                 MEDIUMBLOB        NULL,
    secret_ref           VARCHAR(255)      NULL,
    timeout_seconds      INT UNSIGNED      NOT NULL,
    max_attempts         TINYINT UNSIGNED  NOT NULL,
    backoff_base_seconds INT UNSIGNED      NOT NULL,

    -- execution state
    -- no 'failed': a failed attempt goes back to 'pending' (retry) or to 'dead' (retries exhausted)
    status               ENUM('pending','running','succeeded','dead') NOT NULL,
    attempt              TINYINT UNSIGNED  NOT NULL DEFAULT 0,    -- incremented on claim
    next_attempt_at      DATETIME(3)       NOT NULL,              -- = scheduled_at at first, then now + backoff

    -- lease (who holds the job and until when)
    worker_id            VARCHAR(64)       NULL,
    lease_expires_at     DATETIME(3)       NULL,                  -- now + timeout + margin, set on claim

    -- result of the last attempt
    started_at           DATETIME(3)       NULL,
    finished_at          DATETIME(3)       NULL,
    http_status          SMALLINT UNSIGNED NULL,
    error                VARCHAR(1024)     NULL,                  -- 'timeout', 'lease expired', connection error...
    response_body        BLOB              NULL,                  -- truncated by the executor (e.g. 4 KB)

    created_at           DATETIME(3)       NOT NULL,
    updated_at           DATETIME(3)       NOT NULL,

    PRIMARY KEY (id),                                             -- get by id
    UNIQUE KEY uq_jobs_task_scheduled (task_id, scheduled_at),    -- scheduler never creates the same job twice + history
    KEY        idx_jobs_tenant_status (tenant_id, status),        -- DLQ inbox (id comes for free: InnoDB appends the PK)
    KEY        idx_jobs_claim         (status, priority, next_attempt_at), -- executor claim
    KEY        idx_jobs_lease         (status, lease_expires_at), -- reconciler: running + expired lease

    CONSTRAINT chk_jobs_priority CHECK (priority <= 3)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
