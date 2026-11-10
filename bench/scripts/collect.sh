#!/usr/bin/env bash
# Samples resources every 5 s until killed (bench/scripts/run.sh starts and stops it):
#   pods.csv   CPU and memory of every pod (metrics-server, bundled with k3s)
#   dbs.csv    CPU and memory of the MySQL and Cassandra containers
#   iostat.txt disk latency and queue in WSL, if sysstat is installed
#   mysql-before.txt / mysql-after.txt  global counters, to diff
source "$(dirname "$0")/lib.sh"

RUN="$1"
OUT="bench/results/run-$RUN"
mkdir -p "$OUT"

STATUS="SHOW GLOBAL STATUS WHERE Variable_name IN
  ('Uptime', 'Com_insert', 'Com_update', 'Com_delete', 'Com_commit', 'Innodb_rows_updated', 'Innodb_os_log_fsyncs',
   'Innodb_data_fsyncs', 'Innodb_row_lock_waits', 'Innodb_row_lock_time', 'Threads_running', 'Threads_connected',
   'Innodb_buffer_pool_reads', 'Innodb_buffer_pool_read_requests')"
mysql "$STATUS" > "$OUT/mysql-before.txt"

DB_IDS="$("${COMPOSE[@]}" ps -q mysql cassandra | tr '\n' ' ')"
IOSTAT_PID=""
if command -v iostat >/dev/null; then
  iostat -dxm 5 > "$OUT/iostat.txt" &
  IOSTAT_PID=$!
fi

finish() {
  [[ -n "$IOSTAT_PID" ]] && kill "$IOSTAT_PID" 2>/dev/null
  mysql "$STATUS" > "$OUT/mysql-after.txt"
  exit 0
}
trap finish TERM INT

echo "time,pod,cpu_millicores,memory_mib" > "$OUT/pods.csv"
echo "time,container,cpu_percent,memory" > "$OUT/dbs.csv"
while true; do
  now="$(date -u +%T)"
  kubectl -n "$NS" top pods --no-headers 2>/dev/null \
    | awk -v t="$now" '{ gsub("m","",$2); gsub("Mi","",$3); print t "," $1 "," $2 "," $3 }' >> "$OUT/pods.csv" || true
  # shellcheck disable=SC2086
  docker stats --no-stream --format "$now,{{.Name}},{{.CPUPerc}},{{.MemUsage}}" $DB_IDS >> "$OUT/dbs.csv" || true
  sleep 5 &
  wait $!
done
