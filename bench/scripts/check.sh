#!/usr/bin/env bash
# Checks every link of the platform, from Docker to one job run end to end. Run it after up.sh and before a test.
#   bench/scripts/check.sh          # everything, ~2 min
#   bench/scripts/check.sh --quick  # without the end-to-end job
source "$(dirname "$0")/lib.sh"

failures=0
pass() { printf '  \033[32mok\033[0m    %-46s %s\n' "$1" "${2:-}"; }
fail() { printf '  \033[31mFAIL\033[0m  %-46s %s\n' "$1" "${2:-}"; failures=$((failures + 1)); }

log "Docker and compose (plays RDS)"
engine_os="$(docker info --format '{{.OperatingSystem}}' 2>/dev/null || true)"
if [[ -z "$engine_os" ]]; then
  fail "docker engine" "not running"
elif [[ "$engine_os" == "Docker Desktop" ]]; then
  # Docker Desktop's WSL integration replaces /var/run/docker.sock when it starts, hiding WSL's own engine.
  fail "docker engine" "this is Docker Desktop, not WSL's engine: turn off its WSL integration, then sudo systemctl restart docker"
else
  pass "docker engine" "$(docker version --format '{{.Server.Version}}') on $engine_os"
fi
for service in mysql cassandra toxiproxy; do
  state="$("${COMPOSE[@]}" ps --format '{{.State}} {{.Health}}' "$service" 2>/dev/null || true)"
  expected="running healthy"
  [[ "$service" == toxiproxy ]] && expected="running " # no healthcheck
  if [[ "$state" == "$expected" ]]; then pass "$service container" "$state"; else fail "$service container" "${state:-missing}"; fi
done
proxies="$(curl -fsS "$TOXIPROXY_URL/proxies" 2>/dev/null || true)"
for proxy in mysql cassandra; do
  detail="$(python3 -I -c '
import json, sys
proxy = json.loads(sys.argv[1] or "{}").get(sys.argv[2])
if not proxy or not proxy["enabled"]:
    sys.exit(1)
toxics = ", ".join("+%s ms" % t["attributes"]["latency"] for t in proxy["toxics"] if t["type"] == "latency") or "no latency"
print("%s -> %s, %s" % (proxy["listen"], proxy["upstream"], toxics))' "$proxies" "$proxy" 2>/dev/null)" \
    && pass "toxiproxy $proxy proxy" "$detail" || fail "toxiproxy $proxy proxy" "missing or disabled"
done

log "Kubernetes (plays EKS)"
ready="$(kubectl get nodes --no-headers 2>/dev/null | awk '$2 == "Ready"' | wc -l)"
if [[ "$ready" == 4 ]]; then pass "nodes ready" "4 of 4 (node-0 + 3 workers)"; else fail "nodes ready" "$ready of 4"; fi
while read -r deployment available wanted; do
  if [[ "$available" == "$wanted" ]]; then pass "deployment $deployment" "$available/$wanted available"; else fail "deployment $deployment" "${available:-0}/$wanted available"; fi
done < <(kubectl -n "$NS" get deploy -o jsonpath='{range .items[*]}{.metadata.name} {.status.availableReplicas} {.spec.replicas}{"\n"}{end}' 2>/dev/null)

log "From WSL into the cluster (load balancer)"
for endpoint in "task-service http://127.0.0.1:8080/health" "job-service http://127.0.0.1:8081/health" "fake-endpoint $FAKE_ENDPOINT_URL/stats"; do
  read -r name url <<<"$endpoint"
  if curl -fsS -o /dev/null --max-time 5 "$url"; then pass "$name" "$url"; else fail "$name" "$url"; fi
done

log "Inside the cluster: DNS, ports and HTTP"
probe="check-net-$$"
trap 'kubectl -n "$NS" delete pod "$probe" --now >/dev/null 2>&1 || true' EXIT
kubectl -n "$NS" run "$probe" --restart=Never --image=nicolaka/netshoot -- sleep 600 >/dev/null
if kubectl -n "$NS" wait --for=condition=Ready "pod/$probe" --timeout=90s >/dev/null 2>&1; then
  for service in mysql cassandra fake-endpoint; do
    cluster_ip="$(kubectl -n "$NS" get svc "$service" -o jsonpath='{.spec.clusterIP}')"
    resolved="$(kubectl -n "$NS" exec "$probe" -- dig +short +search "$service" 2>/dev/null | tail -1)"
    if [[ -n "$resolved" && "$resolved" == "$cluster_ip" ]]; then pass "dns $service" "-> $resolved"; else fail "dns $service" "got '${resolved}', want $cluster_ip"; fi
  done
  for target in "mysql 3306" "cassandra 9042" "fake-endpoint 8080"; do
    read -r host port <<<"$target"
    if kubectl -n "$NS" exec "$probe" -- nc -z -w 3 "$host" "$port" >/dev/null 2>&1; then pass "tcp $host:$port" "open"; else fail "tcp $host:$port" "closed"; fi
  done
  if kubectl -n "$NS" exec "$probe" -- curl -fsS -o /dev/null --max-time 5 http://fake-endpoint:8080/stats; then pass "http fake-endpoint:8080" "200"; else fail "http fake-endpoint:8080"; fi
else
  fail "probe pod" "not ready in 90 s"
fi

log "Through Toxiproxy"
if rtt="$(sql_round_trip_us 2>/dev/null)" && [[ "$rtt" =~ ^[0-9]+$ ]]; then
  pass "SELECT 1 from a pod" "$rtt us per round trip"
else
  fail "SELECT 1 from a pod" "no answer"
fi
clients="$("${COMPOSE[@]}" exec -T cassandra nodetool clientstats --all 2>/dev/null | awk '/^\// { print $1 }' || true)"
total="$(grep -c . <<<"$clients" || true)"
direct="$(grep . <<<"$clients" | grep -vc '^/172\.28\.0\.12:' || true)"
if [[ "$total" == 0 ]]; then
  fail "cassandra clients via toxiproxy" "no clients listed (nodetool unreachable?)"
elif [[ "$direct" == 0 ]]; then
  pass "cassandra clients via toxiproxy" "$total of $total from 172.28.0.12"
else
  fail "cassandra clients via toxiproxy" "$direct of $total skip the proxy"
fi

if [[ "${1:-}" != "--quick" ]]; then
  log "One job end to end: seed -> scheduler -> executor -> fake endpoint -> MySQL"
  run="$(date +%s)"
  if ! bench/scripts/seed.sh "$run" 1 1 50 0 >/dev/null 2>&1; then
    fail "job run $run" "could not seed the task into MySQL"
  else
    status=""
    for _ in $(seq 1 30); do
      status="$(mysql_value "SELECT status FROM jobs WHERE tenant_id = $(tenant_sql "$run")" || true)"
      [[ "$status" == succeeded || "$status" == dead ]] && break
      sleep 5
    done
    if [[ "$status" == succeeded ]]; then
      late="$(mysql_value "SELECT ROUND(TIMESTAMPDIFF(MICROSECOND, scheduled_at, finished_at) / 1e6, 3) FROM jobs WHERE tenant_id = $(tenant_sql "$run")")"
      pass "job run $run" "succeeded, finished ${late} s after it was due"
    else
      fail "job run $run" "status '${status:-no job yet}' after 150 s"
    fi
  fi
fi

echo
if [[ "$failures" == 0 ]]; then log "all checks passed"; else log "$failures checks failed"; exit 1; fi
