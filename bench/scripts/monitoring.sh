#!/usr/bin/env bash
# Opens Grafana, Prometheus and Alertmanager on localhost through kubectl port-forward, until Ctrl+C.
# A forward dies with its pod, so each one reconnects on its own after a restart.
#   bench/scripts/monitoring.sh
source "$(dirname "$0")/lib.sh"

# forward <service> <local port> <service port>
forward() {
  while true; do
    kubectl -n "$MONITORING_NS" port-forward --address 127.0.0.1 "svc/$1" "$2:$3" >/dev/null 2>&1
    sleep 1
  done &
}

trap 'kill $(jobs -p) 2>/dev/null' EXIT

forward kube-prometheus-stack-grafana 3000 80
forward kube-prometheus-stack-prometheus 9090 9090
forward kube-prometheus-stack-alertmanager 9093 9093

echo "Grafana       http://127.0.0.1:3000   anonymous viewer; to edit, log in as admin with:"
echo "              kubectl -n $MONITORING_NS get secret kube-prometheus-stack-grafana -o jsonpath='{.data.admin-password}' | base64 -d"
echo "Prometheus    http://127.0.0.1:9090"
echo "Alertmanager  http://127.0.0.1:9093"
echo "Ctrl+C to close them."
wait
