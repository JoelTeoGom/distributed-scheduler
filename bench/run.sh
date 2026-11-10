#!/usr/bin/env bash
# One experiment run, start to finish: reset the target, collect metrics, seed the tasks, wait, print and save the results.
#   bench/run.sh RUN RATE DURATION [LAT_MS] [ERR] [MODE]
#   bench/run.sh 1 100 120              # 100 jobs/s for 120 s, endpoints take 50 ms, no errors
#   bench/run.sh 3 100 120 1000         # slow endpoints (1 s)
#   bench/run.sh 4 20000 1 50 0 burst   # 20,000 jobs due on the same second
# DRAIN (default 60) is how long to wait after the last job is due.
source "$(dirname "$0")/lib.sh"

RUN="$1" RATE="$2" DURATION="$3" LAT="${4:-50}" ERR="${5:-0}" MODE="${6:-steady}"
LEAD=60 # seconds between seeding and the first due job: longer than the scheduler's 30 s lookahead
OUT="bench/results/run-$RUN"
mkdir -p "$OUT"

{
  echo "run=$RUN rate=$RATE duration=$DURATION lat_ms=$LAT err=$ERR mode=$MODE"
  echo "date=$(date -u +%FT%TZ) commit=$(git rev-parse --short HEAD)"
  bench/durability.sh
  echo "mysql_toxics=$(curl -fsS "$TOXIPROXY_URL/proxies/mysql/toxics")"
  kubectl -n "$NS" get deploy executor-service -o jsonpath='executors={.spec.replicas} workers={.spec.template.spec.containers[0].env[?(@.name=="WORKER_COUNT")].value}{"\n"}'
} | tee "$OUT/run.txt"

curl -fsS -X POST "$TARGET_URL/reset"
bench/collect.sh "$RUN" &
COLLECTOR=$!
trap 'kill $COLLECTOR 2>/dev/null || true' EXIT

bench/seed.sh "$RUN" "$RATE" "$DURATION" "$LAT" "$ERR" "$MODE"

WAIT=$((LEAD + DURATION + ${DRAIN:-60}))
log "waiting ${WAIT}s for the jobs to run"
sleep "$WAIT"

kill "$COLLECTOR" 2>/dev/null || true
wait "$COLLECTOR" 2>/dev/null || true
bench/results.sh "$RUN" | tee "$OUT/results.txt"
