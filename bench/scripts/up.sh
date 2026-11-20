#!/usr/bin/env bash
# Brings up the whole bench: databases, Toxiproxy and the fake endpoint in compose, a k3d cluster with the six services, and the emulated latency.
#   bench/scripts/up.sh                       # 1 ms latency, no disk cap
#   LATENCY=off bench/scripts/up.sh
#   DISK_CAP=1 bench/scripts/up.sh            # only on a kernel with BLK_DEV_THROTTLING (not the stock WSL one)
#   DISK_DEVICE=/dev/sdd bench/scripts/up.sh  # with DISK_CAP=1, if Docker's data is not on /dev/sdc (`df /var/lib/docker`)
#   MONITORING=1 bench/scripts/up.sh          # also the monitoring stack (bench/scripts/monitoring-up.sh)
source "$(dirname "$0")/lib.sh"

SERVICES=(task-service job-service scheduler-service executor-service reconciler-service archiver-service)

[[ -f .env ]] || { echo "missing .env: cp .env.example .env and set JWT_SECRET"; exit 1; }
# shellcheck disable=SC1091
source .env

log "images"
docker compose -f bench/infra/kubernetes/compose.build.yml build --quiet

log "databases, Toxiproxy and the fake endpoint (compose) and migrations"
# --force-recreate on the fake endpoint picks up a rebuilt image, whose tag never changes.
"${COMPOSE[@]}" up -d --wait mysql cassandra toxiproxy
"${COMPOSE[@]}" up -d --wait --force-recreate fake-endpoint
"${COMPOSE[@]}" run --rm migrate-tasks
"${COMPOSE[@]}" run --rm migrate-jobs
"${COMPOSE[@]}" run --rm migrate-cassandra

log "k3d cluster"
if ! k3d cluster list "$CLUSTER" >/dev/null 2>&1; then
  k3d cluster create --config bench/infra/kubernetes/k3d.yaml
fi
# mysql:8.4 is already here from compose; latency.sh runs its client in a pod to measure the round trip.
IMAGES=(mysql:8.4)
for s in "${SERVICES[@]}"; do IMAGES+=("scheduler/$s:bench"); done
k3d image import -c "$CLUSTER" "${IMAGES[@]}"

log "manifests"
kubectl apply -f bench/infra/kubernetes/manifests/namespace.yaml
kubectl -n "$NS" create secret generic scheduler-env \
  --from-literal=DATABASE_DSN='scheduler:scheduler@tcp(mysql:3306)/scheduler?parseTime=true&loc=UTC&time_zone=%27%2B00%3A00%27' \
  --from-literal=JWT_SECRET="$JWT_SECRET" \
  --dry-run=client -o yaml | kubectl apply -f -
# Until round 2 the fake endpoint ran in the cluster, behind a NodePort; that Service cannot become one without a selector.
kubectl -n "$NS" delete deploy/fake-endpoint --ignore-not-found >/dev/null
if [[ "$(kubectl -n "$NS" get svc fake-endpoint -o jsonpath='{.spec.selector}' 2>/dev/null)" != "" ]]; then
  kubectl -n "$NS" delete svc fake-endpoint >/dev/null
fi
kubectl apply -f bench/infra/kubernetes/manifests/databases.yaml -f bench/infra/kubernetes/manifests/services.yaml -f bench/infra/kubernetes/manifests/fake-endpoint.yaml
# The image tags never change (:bench), so a rebuilt image only reaches the pods through a restart.
kubectl -n "$NS" rollout restart deploy >/dev/null
for d in "${SERVICES[@]}"; do
  kubectl -n "$NS" rollout status "deploy/$d" --timeout=180s
done

bench/scripts/latency.sh "${LATENCY:-1ms}"
bench/scripts/durability.sh

if [[ "${MONITORING:-0}" == "1" ]]; then
  bench/scripts/monitoring-up.sh
fi

log "ready"
kubectl -n "$NS" get pods -o wide
curl -fsS -X POST "$FAKE_ENDPOINT_URL/reset" && echo "fake endpoint reachable on $FAKE_ENDPOINT_URL"
