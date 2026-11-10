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

# mysql_root "<sql>" runs SQL as root, for server settings (SET GLOBAL). Plain output, no table borders.
mysql_root() {
  "${COMPOSE[@]}" exec -T mysql mysql -uroot -proot -N -B -e "$1" 2>/dev/null
}

# The tenant id of a run: 00000000-0000-0000-0000-<run padded to 12 digits>.
tenant_sql() {
  printf "UUID_TO_BIN('00000000-0000-0000-0000-%012d')" "$1"
}

log() { printf '\033[1m==> %s\033[0m\n' "$*"; }
