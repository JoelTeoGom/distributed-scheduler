#!/usr/bin/env bash
# Emulated network latency on everything MySQL and Cassandra send back (so every round trip to them pays it).
#   bench/scripts/latency.sh 1ms     # ~ app and database in different AZs
#   bench/scripts/latency.sh off
# The pods reach both databases through Toxiproxy (bench/infra/databases/compose.yml): this adds or removes a latency toxic,
# downstream (database -> pod), on both proxies through its HTTP API. The WSL kernel has no netem, so no tc.
source "$(dirname "$0")/lib.sh"

DELAY="${1:-1ms}"
TOXIC=latency_downstream

for proxy in mysql cassandra; do
  # Remove it first, so the call is idempotent and "1ms" after "2ms" replaces it. 404 = there was none.
  curl -sS -o /dev/null -X DELETE "$TOXIPROXY_URL/proxies/$proxy/toxics/$TOXIC"
  if [[ "$DELAY" == "off" ]]; then
    log "$proxy: latency off"
  else
    ms="${DELAY%ms}"
    curl -fsS -o /dev/null -X POST "$TOXIPROXY_URL/proxies/$proxy/toxics" -H 'Content-Type: application/json' \
      -d "{\"name\": \"$TOXIC\", \"type\": \"latency\", \"stream\": \"downstream\", \"attributes\": {\"latency\": $ms, \"jitter\": 0}}"
    log "$proxy: +${ms} ms on every reply"
  fi
done

log "SELECT 1 round trip from a pod (mysql:3306 through Toxiproxy)"
echo "SELECT 1: $(sql_round_trip_us || echo "?") us per round trip (average of 500)"
