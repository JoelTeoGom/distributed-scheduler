# How a query reaches MySQL

> The services connect to `mysql:3306` as they would anywhere; Kubernetes sends that address to Toxiproxy, which adds 1 ms and forwards to MySQL.

On one PC a pod reaches the database in ~0.05 ms. On AWS the database usually sits in another availability zone, ~1 ms away, and every query pays it. The bench adds that millisecond without touching the services.

## The path

```text
pod (e.g. the executor)
  │  mysql:3306
  ▼
Service "mysql"            no selector; its EndpointSlice points at 172.28.0.12
  │
  ▼
Toxiproxy  172.28.0.12     +1 ms on every reply (downstream)
  │
  ▼
MySQL      172.28.0.10
```

- [`bench/k8s/databases.yaml`](../../../bench/k8s/databases.yaml): the `mysql` and `cassandra` Services, with EndpointSlices that point at Toxiproxy instead of a pod.
- [`bench/toxiproxy.json`](../../../bench/toxiproxy.json): one proxy per database, `:3306 → 172.28.0.10:3306` and `:9042 → 172.28.0.11:9042`.
- [`bench/scripts/latency.sh`](../../../bench/scripts/latency.sh): adds or removes a `latency` toxic on both proxies through Toxiproxy's API (`127.0.0.1:8474`), live.

## Why not `tc netem`

`netem` delays packets in the kernel and is the usual tool. The WSL kernel is built without it (`CONFIG_NET_SCH_NETEM` is not set), so `tc` answers "Specified qdisc kind is unknown". Toxiproxy does the same job in user space.

## Cassandra needs one more step

The Cassandra driver (gocql) connects to the address it is given, then asks the node for its address in `system.local` and reconnects there. That address would be the real one, skipping the proxy. Cassandra runs with `CASSANDRA_BROADCAST_RPC_ADDRESS=172.28.0.12`, so it announces Toxiproxy's.

## How it was checked

From a pod, 500 `SELECT 1` in a row over one connection, average round trip:

| Toxic | Round trip |
|---|---|
| off | 0.21 ms |
| 1 ms | 1.36 ms |

`nodetool clientstats` shows every Cassandra client connecting from 172.28.0.12, Toxiproxy's address. Every run records the active toxics in its `run.txt`.
