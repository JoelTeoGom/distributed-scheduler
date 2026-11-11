#!/usr/bin/env bash
# P0 · Calibration: does the bench measure well, and is it never the bottleneck itself? About 10 minutes.
#   bench/scripts/p0-calibration.sh
# Writes bench/results/P0-calibration/runs/<UTC time>/ and prints the summary.
source "$(dirname "$0")/lib.sh"

OUT="bench/results/P0-calibration/runs/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$OUT"
FORTIO_IMAGE=fortio/fortio:1.75.3
REFERENCE_RATE=50 REFERENCE_SECONDS=60 REFERENCE_WORKERS=10000

# phase NAME runs the rest of the line and records when it started and ended, to cut the telemetry later.
phase() {
  local name="$1"; shift
  local start; start="$(date +%s.%N)"
  "$@"
  echo "$name,$start,$(date +%s.%N)" >> "$OUT/phases.csv"
}

{
  echo "test=P0 date=$(date -u +%FT%TZ) commit=$(git rev-parse --short HEAD)"
  bench/scripts/durability.sh
} | tee "$OUT/run.txt"
echo "phase,start,end" > "$OUT/phases.csv"

log "1. connectivity"
bench/scripts/check.sh --quick | tee "$OUT/check.txt"

python3 bench/scripts/telemetry.py --out "$OUT/telemetry.csv" &
TELEMETRY=$!
trap 'kill $TELEMETRY 2>/dev/null || true; kubectl -n "$NS" delete pod fortio --now >/dev/null 2>&1 || true' EXIT

log "2. idle floor: 60 s with no load"
phase idle sleep 60

log "3. latency: SELECT 1 round trip, Toxiproxy without and with the 1 ms toxic"
measure_rtt() {
  for _ in 1 2 3; do echo "$1,$(sql_round_trip_us)"; done >> "$OUT/rtt.csv"
}
echo "toxic,round_trip_us" > "$OUT/rtt.csv"
bench/scripts/latency.sh off >/dev/null
phase rtt-off measure_rtt off
bench/scripts/latency.sh 1ms >/dev/null
phase rtt-1ms measure_rtt 1ms
cat "$OUT/rtt.csv"

log "4. fake endpoint headroom: 2,000 calls/s for 30 s, straight from fortio inside the cluster"
fortio_load() {
  kubectl -n "$NS" run fortio --restart=Never --image="$FORTIO_IMAGE" -- \
    load -qps 2000 -c 200 -t 30s -json - "http://fake-endpoint:8080/t?lat=50" >/dev/null
  kubectl -n "$NS" wait --for=jsonpath='{.status.phase}'=Succeeded pod/fortio --timeout=120s >/dev/null
  kubectl -n "$NS" logs fortio | sed -n '/^{$/,$p' > "$OUT/fortio.json"
  kubectl -n "$NS" delete pod fortio --now >/dev/null
}
phase fortio fortio_load
curl -fsS -X POST "$FAKE_ENDPOINT_URL/reset"

log "5. reference run: $REFERENCE_RATE jobs/s for $REFERENCE_SECONDS s, 1 executor × $REFERENCE_WORKERS workers"
kubectl -n "$NS" scale deploy/executor-service --replicas=1 >/dev/null
kubectl -n "$NS" set env deploy/executor-service WORKER_COUNT="$REFERENCE_WORKERS" >/dev/null
kubectl -n "$NS" rollout status deploy/executor-service --timeout=120s >/dev/null
reference_run() {
  local run; run="$(date +%s)"
  echo "$run" > "$OUT/reference-tenant.txt"
  bench/scripts/seed.sh "$run" "$REFERENCE_RATE" "$REFERENCE_SECONDS" 50 0 > "$OUT/seed.txt"
  # 60 s until the first job is due, the load itself, then wait for the queue to drain (at most 3 minutes more).
  sleep $((60 + REFERENCE_SECONDS))
  for _ in $(seq 1 90); do
    [[ "$(mysql_value "SELECT COUNT(*) FROM jobs WHERE tenant_id = $(tenant_sql "$run") AND status IN ('pending', 'running')")" == 0 ]] && break
    sleep 2
  done
  curl -fsS "$FAKE_ENDPOINT_URL/stats" > "$OUT/fake-endpoint-stats.json"
  curl -fsS "$FAKE_ENDPOINT_URL/events" > "$OUT/events.csv"
  mysql_value "SELECT BIN_TO_UUID(id), UNIX_TIMESTAMP(scheduled_at), UNIX_TIMESTAMP(started_at), UNIX_TIMESTAMP(finished_at), priority, status, attempt
               FROM jobs WHERE tenant_id = $(tenant_sql "$run")" | tr '\t' ',' | sed '1i key,scheduled_s,claimed_s,finished_s,priority,status,attempt' > "$OUT/jobs.csv"
}
curl -fsS -X POST "$FAKE_ENDPOINT_URL/reset"
phase reference reference_run

kill "$TELEMETRY" && wait "$TELEMETRY" 2>/dev/null || true
log "summary"
python3 bench/scripts/p0-summary.py "$OUT" | tee "$OUT/summary.md"
