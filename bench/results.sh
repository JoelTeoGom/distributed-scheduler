#!/usr/bin/env bash
# Prints the results of a run: correctness and throughput, completion lag per priority, start lag from the target, MySQL rates.
#   bench/results.sh RUN
source "$(dirname "$0")/lib.sh"

RUN="$1"
OUT="bench/results/run-$RUN"
TENANT="$(tenant_sql "$RUN")"

log "run $RUN: correctness and throughput (MySQL)"
mysql "
SELECT (SELECT COUNT(*) FROM tasks WHERE tenant_id = $TENANT)                 AS expected,
       COUNT(*)                                                                AS created,
       SUM(status = 'succeeded')                                               AS succeeded,
       SUM(status IN ('pending', 'running'))                                   AS not_finished,
       SUM(status = 'dead')                                                    AS dead,
       SUM(attempt > 1)                                                        AS retried,
       ROUND(SUM(status = 'succeeded')
             / GREATEST(TIMESTAMPDIFF(SECOND, MIN(scheduled_at), MAX(finished_at)), 1), 1) AS sustained_per_s
FROM jobs
WHERE tenant_id = $TENANT;"

log "run $RUN: completion lag, finished_at - scheduled_at, includes the call (MySQL)"
mysql "
WITH lag AS (
    SELECT priority,
           TIMESTAMPDIFF(MICROSECOND, scheduled_at, finished_at) / 1e6 AS s,
           PERCENT_RANK() OVER (PARTITION BY priority
                                ORDER BY TIMESTAMPDIFF(MICROSECOND, scheduled_at, finished_at)) AS pr
    FROM jobs
    WHERE tenant_id = $TENANT AND status = 'succeeded'
)
SELECT priority, COUNT(*) AS jobs,
       ROUND(MAX(CASE WHEN pr <= 0.50 THEN s END), 3) AS p50_s,
       ROUND(MAX(CASE WHEN pr <= 0.99 THEN s END), 3) AS p99_s,
       ROUND(MAX(s), 3)                               AS max_s
FROM lag GROUP BY priority ORDER BY priority;"

log "run $RUN: start lag, request arrival - X-Scheduled-At, first attempts (target)"
curl -fsS "$TARGET_URL/stats" | tee "$OUT/target-stats.json"

if [[ -f "$OUT/mysql-before.txt" && -f "$OUT/mysql-after.txt" ]]; then
  log "run $RUN: MySQL per second over the run"
  # Both files are mysql --table output: | Variable_name | Value |
  awk -F'|' 'NR==FNR { gsub(/ /,"",$2); gsub(/ /,"",$3); b[$2]=$3; next }
             { gsub(/ /,"",$2); gsub(/ /,"",$3); a[$2]=$3 }
             END {
               up = a["Uptime"] - b["Uptime"]; if (up <= 0) up = 1
               printf "seconds            %d\n", up
               n = split("Com_insert Com_update Com_delete Com_commit Innodb_rows_updated Innodb_os_log_fsyncs Innodb_data_fsyncs Innodb_row_lock_waits Innodb_buffer_pool_reads", k, " ")
               for (i = 1; i <= n; i++) printf "%-22s %10.1f/s\n", k[i], (a[k[i]] - b[k[i]]) / up
               printf "%-22s %10.1f ms total\n", "Innodb_row_lock_time", a["Innodb_row_lock_time"] - b["Innodb_row_lock_time"]
             }' "$OUT/mysql-before.txt" "$OUT/mysql-after.txt"
fi

if [[ -f "$OUT/pods.csv" ]]; then
  log "run $RUN: peak CPU (millicores) and memory (MiB) per pod"
  awk -F, 'NR > 1 { if ($3 + 0 > cpu[$2]) cpu[$2] = $3 + 0; if ($4 + 0 > mem[$2]) mem[$2] = $4 + 0 }
           END { for (k in cpu) printf "%-50s cpu %5d m   mem %5d Mi\n", k, cpu[k], mem[k] }' "$OUT/pods.csv" | sort
fi
