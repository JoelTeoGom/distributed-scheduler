#!/usr/bin/env bash
# Installs the monitoring stack (bench/infra/monitoring): mysqld-exporter in compose, next to MySQL, and
# kube-prometheus-stack in the cluster. Idempotent; run it again after changing values.yaml.
#   bench/scripts/monitoring-up.sh          # on a bench that is already up
#   MONITORING=1 bench/scripts/up.sh        # the whole bench with the stack
# Then bench/scripts/monitoring.sh opens Grafana, Prometheus and Alertmanager.
source "$(dirname "$0")/lib.sh"

CHART_VERSION=92.22.0

command -v helm >/dev/null || { echo "missing helm (v3.22.1 was used): https://helm.sh/docs/intro/install/"; exit 1; }

log "mysqld-exporter (compose)"
"${COMPOSE[@]}" up -d --wait mysqld-exporter

log "kube-prometheus-stack $CHART_VERSION"
helm upgrade --install kube-prometheus-stack oci://ghcr.io/prometheus-community/charts/kube-prometheus-stack \
  --version "$CHART_VERSION" --namespace "$MONITORING_NS" --create-namespace \
  --values bench/infra/monitoring/values.yaml --wait --timeout 10m

kubectl -n "$MONITORING_NS" get pods -o wide
