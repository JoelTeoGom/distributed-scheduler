#!/usr/bin/env bash
# Deletes the cluster and the databases, data included. bench/results stays.
source "$(dirname "$0")/lib.sh"

k3d cluster delete "$CLUSTER" || true
"${COMPOSE[@]}" down -v
