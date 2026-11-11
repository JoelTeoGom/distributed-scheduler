# shellcheck shell=bash disable=SC2034
# Shared by the bench scripts. Run them from anywhere; they cd to the repo root.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

CLUSTER=scheduler-bench
NS=scheduler
FAKE_ENDPOINT_URL=http://127.0.0.1:18090
TOXIPROXY_URL=http://127.0.0.1:8474
COMPOSE=(docker compose -f docker-compose.yml -f bench/infra/databases/compose.yml)
# Off by default: the WSL kernel has no BLK_DEV_THROTTLING, so Docker accepts the cap and silently drops it.
if [[ "${DISK_CAP:-0}" == "1" ]]; then
  COMPOSE+=(-f bench/infra/databases/compose.disk-cap.yml)
fi

# mysql "<sql>" runs SQL as the app user against the benchmark database.
mysql() {
  "${COMPOSE[@]}" exec -T mysql mysql -uscheduler -pscheduler --table scheduler -e "$1" 2>/dev/null
}

# mysql_value "<sql>" is mysql without table borders, for reading one value in a script.
mysql_value() {
  "${COMPOSE[@]}" exec -T mysql mysql -uscheduler -pscheduler -N -B scheduler -e "$1" 2>/dev/null
}

# mysql_root "<sql>" runs SQL as root, for server settings (SET GLOBAL). Plain output, no table borders.
mysql_root() {
  "${COMPOSE[@]}" exec -T mysql mysql -uroot -proot -N -B -e "$1" 2>/dev/null
}

# sql_round_trip_us prints the average round trip of SELECT 1 from a pod to mysql:3306, in microseconds, as the services see it.
# One session, queries one after another, so each one waits for the previous reply; the connect is amortised.
# The pod is read through its logs: with `kubectl run -i`, a pod that ends before the attach loses its output.
sql_round_trip_us() {
  local pod="sql-rtt-$RANDOM"
  kubectl -n "$NS" run "$pod" --restart=Never --image=mysql:8.4 --image-pull-policy=Never --command -- bash -c '
    n=500
    start=$(date +%s%N)
    yes "SELECT 1;" | head -n $n | mysql -h mysql -uscheduler -pscheduler -N -B >/dev/null 2>&1 || exit 1
    end=$(date +%s%N)
    echo $(( (end - start) / n / 1000 ))' >/dev/null
  kubectl -n "$NS" wait --for=jsonpath='{.status.phase}'=Succeeded "pod/$pod" --timeout=60s >/dev/null 2>&1 \
    && kubectl -n "$NS" logs "$pod"
  local status=$?
  kubectl -n "$NS" delete pod "$pod" --now >/dev/null 2>&1
  return $status
}

# The tenant id of a run: 00000000-0000-0000-0000-<run padded to 12 digits>.
tenant_sql() {
  printf "UUID_TO_BIN('00000000-0000-0000-0000-%012d')" "$1"
}

log() { printf '\033[1m==> %s\033[0m\n' "$*"; }
