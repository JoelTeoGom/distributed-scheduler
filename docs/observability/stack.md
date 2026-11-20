# The monitoring stack

> What runs, why it is there, and what it costs. Everything lives in [bench/infra/monitoring](../../bench/infra/monitoring) and runs on the same PC as the bench.

## Run it

```bash
MONITORING=1 bench/scripts/up.sh      # the whole bench with the stack
bench/scripts/monitoring-up.sh        # or add it to a bench that is already up; run it again after editing values.yaml
bench/scripts/monitoring.sh           # Grafana :3000, Prometheus :9090, Alertmanager :9093 on 127.0.0.1
```

`monitoring.sh` uses `kubectl port-forward`, so the cluster is not recreated to add NodePorts. Each forward reconnects on its own when its pod restarts. Grafana lets anonymous viewers in, so the screenshot script can open it in kiosk mode. To edit dashboards, log in as `admin` with the password the script prints the command for.

It needs Helm. v3.22.1 was used.

## What runs

| Component | Where | Why it is here |
|---|---|---|
| **Prometheus** | server node | Scrapes every target every **5 s**, evaluates rules every 5 s, and keeps **2 days** (8 GB at most). With 5 s a 1-minute rate has 12 samples, enough for alert windows of a few minutes. |
| **Alertmanager** | server node | Groups the alerts and routes them. There is no real receiver: step 4 adds one that writes to a log, and the report reads the `ALERTS` series. |
| **Grafana** | server node | Dashboards. Its sidecar loads every ConfigMap labelled `grafana_dashboard: "1"`, so dashboards live as code in the repo. |
| **kube-state-metrics** | server node | The state of Kubernetes objects: limits, restarts, OOM kills, replicas. |
| **Prometheus Operator** | server node | Turns `PodMonitor` and `PrometheusRule` objects into Prometheus configuration. It is part of the chart. |
| **node-exporter** | every node (DaemonSet) | The machine: CPU, memory, disk and network. See [host CPU](#host-cpu-node-exporter-sees-the-whole-pc). |
| **cAdvisor** | inside each kubelet | CPU, throttling and memory per container. It is not installed separately: Prometheus scrapes it from the kubelet. |
| **mysqld-exporter** | compose, `172.28.0.14` | MySQL runs outside the cluster, like RDS. Next to it, the exporter reads its status and Prometheus scrapes it as a static target. |

Versions are pinned. The chart is kube-prometheus-stack **92.22.0**, and the chart version fixes every image it pulls: Prometheus v3.16.0, Alertmanager v0.34.2, Grafana 13.3.0, Prometheus Operator v0.94.2, node-exporter v1.12.1 and kube-state-metrics v2.20.0. The exporter is `prom/mysqld-exporter:v0.21.0`.

### Placement

The k3d server is tainted `CriticalAddonsOnly=true:NoExecute`, like an EKS control plane, and has no memory cap. Everything but node-exporter runs there, through a `nodeSelector` on `node-role.kubernetes.io/control-plane` and that toleration. The stack takes no memory from the 4 GB of each app node.

**The CPU is shared anyway.** It is one PC: Prometheus scraping and evaluating rules competes with the executors for the same 12 CPUs. The cost is measured below at idle, and again under load in step 6.

### Scraped and not scraped

All 26 targets are UP: the API server, CoreDNS, the kubelet ×4 (`/metrics`, `/metrics/cadvisor`, `/metrics/probes`), node-exporter ×4, kube-state-metrics, mysqld-exporter, and the stack's own components.

k3s runs etcd, kube-scheduler, kube-controller-manager and kube-proxy inside its own process and does not expose their metrics. Their scrapes and their default rules are turned off, so no target is DOWN by design.

**cAdvisor is scraped every 10 s, not 5 s.** The chart sets that on purpose: the kubelet refreshes container stats every ~10 s, so a faster scrape would only repeat values. CPU and throttling per pod therefore have a 10 s resolution, and any alert on them needs a window of at least a minute.

### Storage

Prometheus keeps its data in an `emptyDir` of up to 10 GiB. That survives a restart of the container, after an OOM kill for example, but not a new pod. A local-path volume cannot be used here: k3s's provisioner creates the folder with a helper pod that does not tolerate the server's taint, and k3s rewrites that configuration on every start. The report reads Prometheus right after each run, so losing older runs is acceptable.

At idle it ingests ~15,000 samples/s, roughly 2 GB a day, so 2 days fit within the 8 GB cap.

## mysqld-exporter

- **Its own MySQL user,** `exporter`, created by a one-shot compose service from [mysql-exporter-user.sql](../../bench/infra/databases/mysql-exporter-user.sql) on every start (idempotent). It has `PROCESS`, `REPLICATION CLIENT` and `SELECT` on `performance_schema` only: no access to the scheduler's data, and at most 3 connections.
- **Direct to MySQL, not through Toxiproxy.** Its queries neither pay the emulated 1 ms nor show up in the proxy.
- **Collectors:** global status and variables (on by default), `info_schema.innodb_metrics`, `info_schema.processlist` (by state and user, not by host: every client is Toxiproxy's IP), and `perf_schema` table and index I/O waits.

The counters [telemetry.py](../../bench/scripts/telemetry.py) reads, under their names here (step 3 compares them):

| telemetry.py | Prometheus |
|---|---|
| `Handler_commit` (commits/s) | `rate(mysql_global_status_handlers_total{handler="commit"}[1m])` |
| `Com_insert`, `Com_update` | `mysql_global_status_commands_total{command="insert"\|"update"}` |
| `Innodb_os_log_fsyncs` | `mysql_global_status_innodb_os_log_fsyncs` |
| `Innodb_row_lock_waits` | `mysql_global_status_innodb_row_lock_waits` (also `mysql_info_schema_innodb_metrics_lock_lock_row_lock_waits_total`) |
| `Threads_running`, `Threads_connected` | `mysql_global_status_threads_running`, `mysql_global_status_threads_connected` |

Plus what telemetry.py never had: row lock time, the buffer pool (`mysql_global_status_buffer_pool_pages`), and the processlist by state.

## Host CPU: node-exporter sees the whole PC

A k3d node is a Docker container on WSL's kernel, and `/proc/stat` is not namespaced. node-exporter in any node therefore reads the kernel's counters. Checked:

- every node-exporter reports **12 CPUs**, though the nodes are containers;
- the idle counter of `cpu0` is the same on all four nodes and in WSL's own `/proc/stat` (38,803 to 38,808 s, read seconds apart);
- the busy fraction agrees across the four, within 0.4 points.

So `1 - avg(rate(node_cpu_seconds_total{mode="idle"}[1m]))` is the CPU of the whole WSL VM, the same whichever node it comes from. That answers the question round 2 left open: whether the host itself was saturated in `u1-1cpu` and at scale 3. The answer comes with the next run of each.

Two limits:

- **Memory is per node.** k3d gives each agent a fake `/proc/meminfo` for its 4 GB, so `node_memory_MemTotal_bytes` is 4 GiB on the agents and 15.5 GiB on the server.
- **The WSL VM is not the whole Windows machine.** Its 12 CPUs are all of the PC's logical processors, but load on the Windows side only shows as less CPU for WSL, never as steal (always 0).

## WSL's clock jumps 1 s every ~32 s

The stack found this on its first day. _Measured on 2026-11-20._

**The wall clock steps forward ~1 s every ~32 s.** Sampling `time.time() - time.monotonic()` in WSL for 80 s, the gap grew by +1.02 s, +0.99 s and +0.98 s, at 32 s intervals, and stayed flat in between. The kernel's clocksource is `tsc`, although `hyperv_clocksource_tsc_page` is available. The VM's clock runs ~3% slow, and time sync (`systemd-timesyncd` is active, and Hyper-V's TimeSync too) puts it back in one step. Every container shares this kernel, so every pod, MySQL and the fake endpoint see the same jumps.

What it breaks:

- **Process start times seem to change.** A start time is computed from the kernel's boot time, which moves with each step: 9 times in 20 minutes on every node. Anything that counts changes of `process_start_time_seconds` sees restarts that never happened. The chart's `AlertmanagerClusterCrashlooping` does exactly that, so it is turned off. Restarts come from kube-state-metrics (`kube_pod_container_status_restarts_total`).
- **Prometheus skips a rule evaluation every ~3 minutes.** Every rule group missed one evaluation at 17:04:28 and again at 17:07:48, 170 s and 200 s apart. The slowest group takes 15 ms against its 5 s interval, and scrapes kept their pace (5.0 to 5.8 s apart): the 5 s slots, aligned on the wall clock, drift 3% against the timer until one is skipped. The chart's `PrometheusMissingRuleEvaluations` is pending because of it and will fire.
- **Probably the late seconds P0 found.** [P0](../../bench/results/P0-calibration/README.md) found "every ~31 s, a whole second of jobs claimed after its time, 0.3 to 1 s late". It blamed the host's disk writeback, without proof. A 1 s step every ~32 s fits better: when the wall clock jumps, a whole second of jobs becomes due at once. **Not checked yet.**

It matters for the SLO: priority 0's threshold is 1 s, the size of one step.

## What it costs

At idle, with the bench up and no load, averaged over 2 to 5 minutes:

| Component | CPU (millicores) | Memory (MiB) |
|---|---|---|
| Prometheus | 42 | 390 (limit 1,024) |
| Grafana | 11 | 389, plus 158 for its two sidecars |
| Prometheus Operator | 8 | 34 |
| node-exporter, each of 4 | 2 | 17 |
| Alertmanager | 1.5 | 52 |
| kube-state-metrics | 1 | 29 |
| mysqld-exporter | ~0 | 11 |
| **Total** | **~72** | **~1,130** |

72 millicores is 0.6% of the PC's 12 CPUs, and 14% of one executor's 0.5 vCPU. The ~1.1 GB sits on the server, outside the app nodes.

**~90,000 series.** Half come from the kubelet and cAdvisor (45,000). The API server adds 27,000, mostly request-latency histograms that the bench does not need. They are the first thing to drop if Prometheus needs room. MySQL adds 1,300.

The cost under load, and whether the metrics slow the services down, is measured in step 6.
