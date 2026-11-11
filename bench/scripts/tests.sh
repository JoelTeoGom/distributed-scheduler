#!/usr/bin/env bash
# The tests of benchmarks.md, P1 to P9. Each run records telemetry, every call the fake endpoint receives, every
# job row and CPU profiles of the executor and the scheduler, then writes its report next to the test's README.
#   bench/scripts/tests.sh P3            # one test
#   bench/scripts/tests.sh P1 P2 P3      # several, in order
#   bench/scripts/tests.sh all           # P1 to P9, about 2 hours
# Raw data: bench/results/<test>/runs/<time>/ (not committed). Report: bench/results/<test>/report.md + charts/.
source "$(dirname "$0")/lib.sh"

RESULTS=bench/results
declare -A TEST_DIRS=([P1]=P1-smoke [P2]=P2-capacity [P3]=P3-normal-day [P4]=P4-0900-peak [P5]=P5-slow-endpoints
  [P6]=P6-noisy-neighbour [P7]=P7-executor-dies [P8]=P8-mysql-outage [P9]=P9-long-run)

# next_run prints a new run number: tenants and task names carry it, so a run can count and clean up its own.
next_run() {
  local counter="$RESULTS/.run-counter" run
  run=$(( $(cat "$counter" 2>/dev/null || echo 100) + 1 ))
  echo "$run" > "$counter"
  echo "$run"
}

tenant_range_sql() { # every tenant of run $1
  printf "tenant_id BETWEEN UUID_TO_BIN('00000000-0000-%04d-0000-000000000000') AND UUID_TO_BIN('00000000-0000-%04d-0000-ffffffffffff')" "$1" "$1"
}

# configure EXECUTORS: the deployment every test starts from, and a clean queue.
configure() {
  kubectl -n "$NS" scale deploy/executor-service --replicas="$1" >/dev/null
  kubectl -n "$NS" rollout status deploy/executor-service --timeout=180s >/dev/null
  bench/scripts/latency.sh 1ms >/dev/null
  bench/scripts/durability.sh 1 >/dev/null
  local waited=0
  until [[ "$(mysql_value "SELECT COUNT(*) FROM jobs WHERE status IN ('pending', 'running')")" == 0 ]]; do
    (( waited++ > 120 )) && { echo "the queue is not empty after 10 minutes" >&2; return 1; }
    sleep 5
  done
}

# start TEST LABEL: a run directory, telemetry and a clean fake endpoint. Sets OUT, RUN and TELEMETRY.
start() {
  RUN="$(next_run)"
  OUT="$RESULTS/${TEST_DIRS[$1]}/runs/$(date -u +%Y%m%dT%H%M%SZ)${2:+-$2}"
  mkdir -p "$OUT"
  {
    echo "test=$1 label=${2:-} run=$RUN date=$(date -u +%FT%TZ) commit=$(git rev-parse --short HEAD)"
    echo "executors=$(kubectl -n "$NS" get deploy executor-service -o jsonpath='{.spec.replicas}')"
    kubectl -n "$NS" get deploy executor-service scheduler-service reconciler-service archiver-service \
      -o jsonpath='{range .items[*]}{.metadata.name}: {range .spec.template.spec.containers[0].env[*]}{.name}={.value} {end}{"\n"}{end}'
    bench/scripts/durability.sh
    echo "mysql_toxics=$(curl -fsS "$TOXIPROXY_URL/proxies/mysql/toxics")"
  } > "$OUT/run.txt"
  echo "phase,start,end" > "$OUT/phases.csv"
  curl -fsS -X POST "$FAKE_ENDPOINT_URL/reset"
  python3 bench/scripts/telemetry.py --out "$OUT/telemetry.csv" &
  TELEMETRY=$!
  BACKGROUND=()
  log "$1 ${2:-} (run $RUN) -> $OUT"
}

# seed ARGS...: workload.py with this run's number. Sets LOAD_START and LOAD_END (epoch seconds).
seed() {
  python3 bench/scripts/workload.py "$@" --run "$RUN" > "$OUT/workload.json"
  LOAD_START="$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["start"])' "$OUT/workload.json")"
  LOAD_END=$(( LOAD_START + $(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["duration"])' "$OUT/workload.json") ))
  echo "load,$LOAD_START,$LOAD_END" >> "$OUT/phases.csv"
  echo "  seeded $(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["seeded"])' "$OUT/workload.json") tasks, load from $(date -d "@$LOAD_START" +%T) to $(date -d "@$LOAD_END" +%T)"
}

sleep_until() { local wait=$(( $1 - $(date +%s) )); (( wait > 0 )) && sleep "$wait"; return 0; }

# profile_at EPOCH: 20 s CPU profiles of one executor and the scheduler from that moment, then heap and goroutines.
profile_at() {
  (
    sleep_until "$1"
    local executor; executor="$(kubectl -n "$NS" get pods -l app=executor-service -o jsonpath='{.items[0].metadata.name}')"
    local scheduler; scheduler="$(kubectl -n "$NS" get pods -l app=scheduler-service -o jsonpath='{.items[0].metadata.name}')"
    kubectl -n "$NS" port-forward "pod/$executor" 16060:6060 >/dev/null 2>&1 & local forward_executor=$!
    kubectl -n "$NS" port-forward "pod/$scheduler" 16061:6060 >/dev/null 2>&1 & local forward_scheduler=$!
    sleep 2
    local label; label="$(date +%s)"
    curl -fsS -o "$OUT/executor-cpu-$label.pprof" "http://127.0.0.1:16060/debug/pprof/profile?seconds=20" & local executor_cpu=$!
    curl -fsS -o "$OUT/scheduler-cpu-$label.pprof" "http://127.0.0.1:16061/debug/pprof/profile?seconds=20" & local scheduler_cpu=$!
    wait "$executor_cpu" "$scheduler_cpu" 2>/dev/null
    curl -fsS -o "$OUT/executor-heap-$label.pprof" "http://127.0.0.1:16060/debug/pprof/heap"
    curl -fsS -o "$OUT/executor-goroutines-$label.txt" "http://127.0.0.1:16060/debug/pprof/goroutine?debug=1"
    echo "profile,$label,$(date +%s)" >> "$OUT/phases.csv"
    kill "$forward_executor" "$forward_scheduler" 2>/dev/null
  ) &
  BACKGROUND+=($!)
}

# fault_at EPOCH NAME COMMAND...: runs the command at that moment and marks it on the charts.
fault_at() {
  local at="$1" name="$2"; shift 2
  ( sleep_until "$at"; local began; began="$(date +%s)"; "$@"; echo "$name,$began,$(date +%s)" >> "$OUT/phases.csv" ) &
  BACKGROUND+=($!)
}

# finish [DRAIN_SECONDS]: stops the run's crons when the load ends, waits for its jobs to finish, collects, reports.
finish() {
  local drain="${1:-300}"
  sleep_until "$LOAD_END"
  mysql_value "UPDATE tasks SET status = 'deleted', next_run_at = NULL WHERE name LIKE 'run$RUN-%' AND status = 'active'" >/dev/null
  local deadline=$(( $(date +%s) + drain ))
  until [[ "$(mysql_value "SELECT COUNT(*) FROM jobs WHERE $(tenant_range_sql "$RUN") AND status IN ('pending', 'running')")" == 0 ]]; do
    (( $(date +%s) > deadline )) && { echo "  jobs still unfinished after a $drain s drain" | tee -a "$OUT/run.txt"; break; }
    sleep 3
  done
  echo "drain,$LOAD_END,$(date +%s)" >> "$OUT/phases.csv"
  [[ ${#BACKGROUND[@]} -gt 0 ]] && { wait "${BACKGROUND[@]}" 2>/dev/null || true; } # profiles and faults still running
  BACKGROUND=()
  sleep 4 # one more telemetry sample after the last job
  kill "$TELEMETRY" 2>/dev/null; wait "$TELEMETRY" 2>/dev/null || true

  curl -fsS "$FAKE_ENDPOINT_URL/stats" > "$OUT/fake-endpoint-stats.json"
  curl -fsS "$FAKE_ENDPOINT_URL/events" > "$OUT/events.csv"
  mysql_value "SELECT BIN_TO_UUID(id), CAST(SUBSTRING(BIN_TO_UUID(tenant_id), 25) AS UNSIGNED), priority, status, attempt,
                      UNIX_TIMESTAMP(scheduled_at), UNIX_TIMESTAMP(started_at), UNIX_TIMESTAMP(finished_at),
                      IFNULL(http_status, ''), REPLACE(IFNULL(LEFT(error, 60), ''), ',', ';')
               FROM jobs WHERE $(tenant_range_sql "$RUN")" | tr '\t' ',' \
    | sed '1i key,tenant,priority,status,attempt,scheduled_s,claimed_s,finished_s,http_status,error' > "$OUT/jobs.csv"
  python3 bench/scripts/report.py "$OUT" | tail -3
}

# sustained RUN_DIR: first-attempt calls per second while they arrived, from the fake endpoint's events.
sustained() {
  python3 - "$1" <<'EOF'
import csv, sys
arrivals = [int(row["arrived_ms"]) for row in csv.DictReader(open(sys.argv[1] + "/events.csv")) if row["attempt"] == "1"]
span = (max(arrivals) - min(arrivals)) / 1000 if len(arrivals) > 1 else 1
print(round(len(arrivals) / max(span, 1), 1))
EOF
}

p99() { python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["start_lag_all"]["p99_s"])' "$1/fake-endpoint-stats.json"; }

# The model's minute crons fire on second :00; profile the second minute boundary of the load.
minute_peak() { echo $(( (LOAD_START / 60 + 2) * 60 - 5 )); }

P1() {
  configure 2
  start P1
  seed model --scale 0.1 --duration 180
  profile_at "$(minute_peak)"
  finish
}

P2() {
  # One executor, more load each step until it breaks, then the same load on 2 and 3 executors.
  local rate knee=""
  configure 1
  for rate in 50 100 200 400 800; do
    start P2 "1x-$rate"
    seed steady --rate "$rate" --duration 60 --endpoint "lat=50&dist=lognormal"
    profile_at $(( LOAD_START + 30 ))
    finish 180
    local got; got="$(sustained "$OUT")"
    echo "  $rate jobs/s offered, $got sustained, p99 $(p99 "$OUT") s"
    if python3 -c 'import sys; got, rate, p99 = map(float, sys.argv[1:]); sys.exit(0 if got < 0.9 * rate or p99 > 2 else 1)' "$got" "$rate" "$(p99 "$OUT")"; then
      knee="$rate"; break
    fi
  done
  knee="${knee:-800}"
  for executors in 2 3; do
    configure "$executors"
  start P2 "${executors}x-$knee"
    seed steady --rate "$knee" --duration 60 --endpoint "lat=50&dist=lognormal"
    profile_at $(( LOAD_START + 30 ))
    finish 180
    echo "  $executors executors at $knee jobs/s: $(sustained "$OUT") sustained, p99 $(p99 "$OUT") s"
  done
  configure 2
}

P3() {
  configure 2
  start P3
  seed model --duration 300
  profile_at "$(minute_peak)"
  finish
}

P4() {
  configure 2
  start P4
  seed model --duration 480 --peak-in 60
  local peak; peak="$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["peak"])' "$OUT/workload.json")"
  echo "  09:00 peak at $(date -d "@$peak" +%T)"
  profile_at $(( peak - 3 ))
  finish 600
}

P5() {
  local timeout
  for timeout in 30 20; do
    configure 2
  start P5 "timeout-$timeout"
    seed steady --rate 20 --duration 240 --endpoint "dist=uniform&min=10000&max=30000" --timeout "$timeout"
    profile_at $(( LOAD_START + 120 ))
    finish 400
  done
}

P6() {
  configure 2
  start P6
  seed model --duration 300 --noisy-tenant
  profile_at "$(minute_peak)"
  finish 400
}

P7() {
  configure 2
  start P7
  seed model --duration 360
  profile_at "$(minute_peak)"
  kill_executor() {
    local pod; pod="$(kubectl -n "$NS" get pods -l app=executor-service -o jsonpath='{.items[0].metadata.name}')"
    kubectl -n "$NS" delete pod "$pod" --grace-period=0 --force >/dev/null 2>&1
    echo "  killed $pod"
  }
  fault_at $(( LOAD_START + 180 )) kill-executor kill_executor
  finish 600
}

P8() {
  configure 2
  start P8
  seed model --duration 360
  profile_at "$(minute_peak)"
  cut_mysql() {
    curl -fsS -o /dev/null -X POST "$TOXIPROXY_URL/proxies/mysql" -d '{"enabled": false}'
    echo "  MySQL cut"
    sleep 30
    curl -fsS -o /dev/null -X POST "$TOXIPROXY_URL/proxies/mysql" -d '{"enabled": true}'
    echo "  MySQL back"
  }
  fault_at $(( LOAD_START + 180 )) mysql-cut cut_mysql
  finish 600
}

P9() {
  configure 2
  start P9
  seed model --duration 1800
  profile_at $(( LOAD_START + 300 ))
  profile_at $(( LOAD_START + 1700 ))
  finish
}

trap 'kill $TELEMETRY 2>/dev/null || true' EXIT
TELEMETRY=""
BACKGROUND=()
tests=("$@")
[[ "${tests[0]:-}" == all ]] && tests=(P1 P2 P3 P4 P5 P6 P7 P8 P9)
[[ ${#tests[@]} -gt 0 ]] || { echo "usage: bench/scripts/tests.sh P1 [P2 ...] | all"; exit 1; }
for test in "${tests[@]}"; do
  [[ -n "${TEST_DIRS[$test]:-}" ]] || { echo "unknown test $test"; exit 1; }
  "$test"
done
log "done: ${tests[*]}"
