-- Tasks: the configuration a tenant creates. Jobs (executions) live in another table.
-- All DATETIME values are stored in UTC.

CREATE TABLE tasks (
    id                   BINARY(16)        NOT NULL,              -- UUIDv7: time-ordered, appends to the clustered index
    tenant_id            BINARY(16)        NOT NULL,
    name                 VARCHAR(255)      NOT NULL,

    -- schedule
    schedule_type        ENUM('at','cron') NOT NULL,
    run_at               DATETIME(3)       NULL,                  -- only for 'at'
    cron_expr            VARCHAR(255)      NULL,                  -- only for 'cron' (5 fields)
    timezone             VARCHAR(64)       NOT NULL DEFAULT 'UTC',-- IANA name, used to evaluate cron_expr

    -- 0 = critical, 1 = high, 2 = normal, 3 = low (numeric so ORDER BY priority works)
    priority             TINYINT UNSIGNED  NOT NULL,

    -- the HTTP request each job makes
    http_method          VARCHAR(10)       NOT NULL,
    endpoint             VARCHAR(2048)     NOT NULL,
    headers              JSON              NULL,                  -- non-sensitive headers only
    body                 MEDIUMBLOB        NULL,
    secret_ref           VARCHAR(255)      NULL,                  -- reference to a secret, never the secret
    timeout_seconds      INT UNSIGNED      NOT NULL,

    -- retry policy
    max_attempts         TINYINT UNSIGNED  NOT NULL,
    backoff_base_seconds INT UNSIGNED      NOT NULL,

    -- lifecycle (not execution status)
    status               ENUM('active','paused','deleted') NOT NULL,
    next_run_at          DATETIME(3)       NULL,                  -- NULL = nothing left to run ('at' already fired)

    -- idempotency of POST /tasks (optional)
    idempotency_key      VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NULL, -- from the Idempotency-Key header, compared byte by byte
    request_hash         BINARY(32)        NULL,                  -- SHA-256 of the body: same key + different body -> 422

    version              INT UNSIGNED      NOT NULL DEFAULT 1,    -- optimistic locking
    created_at           DATETIME(3)       NOT NULL,
    updated_at           DATETIME(3)       NOT NULL,

    PRIMARY KEY (id),
    KEY        idx_tasks_tenant      (tenant_id, id),             -- list by tenant, cursor = last id (UUIDv7 is ordered)
    KEY        idx_tasks_due         (status, next_run_at),       -- scheduler: status = 'active' AND next_run_at <= ?
    UNIQUE KEY uq_tasks_idempotency  (tenant_id, idempotency_key),-- several NULLs allowed

    CONSTRAINT chk_tasks_schedule CHECK (
        (schedule_type = 'at'   AND run_at IS NOT NULL AND cron_expr IS NULL) OR
        (schedule_type = 'cron' AND cron_expr IS NOT NULL AND run_at IS NULL)
    ),
    CONSTRAINT chk_tasks_priority CHECK (priority <= 3)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
