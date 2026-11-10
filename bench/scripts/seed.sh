#!/usr/bin/env bash
# Inserts `at` tasks straight into MySQL: RATE per second for DURATION seconds, the first one due 60 s from now.
# It bypasses the Task API on purpose: the API is not under test, and SQL creates 100,000 tasks in seconds.
# Every job is due on a whole second (X-Scheduled-At has second precision) and priorities 0-3 are mixed evenly.
#   bench/scripts/seed.sh RUN RATE DURATION [LAT_MS] [ERR] [steady|burst]
source "$(dirname "$0")/lib.sh"

RUN="$1" RATE="$2" DURATION="$3" LAT="${4:-50}" ERR="${5:-0}" MODE="${6:-steady}"
TOTAL=$((RATE * DURATION))
if [[ "$MODE" == "burst" ]]; then
  OFFSET="0"                 # every job on the same second
else
  OFFSET="(i DIV $RATE)"     # RATE jobs on each second
fi

log "seeding $TOTAL tasks for run $RUN ($MODE, ${LAT} ms endpoints, err $ERR)"
mysql "
SET SESSION cte_max_recursion_depth = $((TOTAL + 1));
SET @start = DATE_FORMAT(UTC_TIMESTAMP() + INTERVAL 60 SECOND, '%Y-%m-%d %H:%i:%s');

INSERT INTO tasks (id, tenant_id, name, schedule_type, run_at, timezone, priority,
                   http_method, endpoint, timeout_seconds, max_attempts, backoff_base_seconds,
                   status, next_run_at, created_at, updated_at)
WITH RECURSIVE n (i) AS (
    SELECT 0 UNION ALL SELECT i + 1 FROM n WHERE i + 1 < $TOTAL
)
SELECT UUID_TO_BIN(UUID()), $(tenant_sql "$RUN"), CONCAT('bench-$RUN-', i), 'at',
       @start + INTERVAL $OFFSET SECOND, 'UTC', i % 4,
       'POST', CONCAT('http://target:8080/t?lat=$LAT&dist=lognormal&err=$ERR&p=', i % 4),
       10, 3, 1,
       'active', @start + INTERVAL $OFFSET SECOND, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)
FROM n;

SELECT COUNT(*) AS seeded, MIN(run_at) AS first_due, MAX(run_at) AS last_due
FROM tasks WHERE tenant_id = $(tenant_sql "$RUN");"
