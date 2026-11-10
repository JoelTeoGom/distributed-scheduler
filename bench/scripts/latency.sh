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

# Check from inside the cluster: the average round trip of SELECT 1 over one connection, as the services see it.
# One session, queries one after another, so each one waits for the previous reply; the connect is amortised.
log "SELECT 1 round trip from a pod (mysql:3306 through Toxiproxy)"
kubectl -n "$NS" run "sql-rtt-$RANDOM" --rm -i --quiet --restart=Never --image=mysql:8.4 --image-pull-policy=Never \
  --command -- bash -c '
    n=500
    start=$(date +%s%N)
    yes "SELECT 1;" | head -n $n | mysql -h mysql -uscheduler -pscheduler -N -B >/dev/null 2>&1
    end=$(date +%s%N)
    echo "SELECT 1: $(( (end - start) / n / 1000 )) us per round trip (average of $n)"' || true
