# Benchmarks: first round

A short, reproducible benchmark of the scheduler on one machine, shaped like a small AWS deployment (EKS + RDS). The goal is not the real ceiling of the design. It is to find the **first bottleneck** of this implementation, explain it, and know what to change next.

Time budget: about 2 hours, setup included. Everything is in [`bench/`](../../bench).

> [!WARNING]
> Every number here comes from an **emulated** AWS environment on one machine. It shows the order of magnitude and, above all, **where the system breaks first**. A real AWS deployment has more network latency, slower disks and noisy neighbours, so expect lower absolute numbers there. Comparisons between runs (1 vs 4 executors, 50 vs 200 workers) are more reliable than the absolute values. See [threats to validity](#7-threats-to-validity).

## 1. Questions

1. How many jobs per second does **one executor** sustain before the scheduling lag breaks the SLO?
2. Does throughput **scale with more executors**, or do they fight over MySQL?
3. How must the **worker pool** grow when endpoints are slow (Little's law)?
4. How long does a **burst** (the 09:00 problem) take to drain, and does priority hold?
5. What happens to in-flight jobs when an **executor pod dies**?

Out of scope this round: the Task and Job APIs (plain reads and writes, not the risky part), multi-node failures, rolling updates and long soaks.

## 2. Environment: AWS, emulated on one machine

AWS is emulated by its **shapes** (nodes, pod sizes, database size, network latency), not by its APIs: the system uses none (no S3, SQS or Lambda), so LocalStack adds nothing. The disk is **not** emulated: see [latency and disk](#latency-and-disk-and-how-to-check-them).

```text
WSL2 (Ubuntu) + Docker Engine
└── docker network distributed-scheduler_default (172.28.0.0/16)   = the VPC
    ├── k3d cluster "scheduler-bench"                               = EKS
    │   ├── server node                                               control plane
    │   ├── agent node 1 (4 GB) ┐ pods: task, job, scheduler,
    │   └── agent node 2 (4 GB) ┘ executor × N, reconciler, archiver, target
    ├── toxiproxy  172.28.0.12   +1 ms on every reply               = the network between AZs
    │      │  pods → mysql:3306 and cassandra:9042 land here
    ├── mysql      172.28.0.10   2 vCPU / 4 GB, local disk          = RDS db.t4g.medium
    └── cassandra  172.28.0.11   2 vCPU / 2 GB                      = a small Cassandra node
```

| AWS | Emulated with |
|---|---|
| EKS, 2 worker nodes | [k3d](https://k3d.io) (k3s in Docker): 1 server + 2 agents with 4 GB each ([`k3d.yaml`](../../bench/k3d.yaml)) |
| Pods sized like Fargate tasks (0.5 vCPU / 1 GB) | `requests` = `limits` = 500m / 1Gi, `GOMEMLIMIT=900MiB` ([`k8s/services.yaml`](../../bench/k8s/services.yaml)) |
| RDS MySQL `db.t4g.medium`: 2 vCPU / 4 GB | MySQL 8.4 in compose: `cpus: 2`, `mem_limit: 4g`, buffer pool 3 GB (RDS uses ~75% of RAM; the 128 MB default is not realistic). Durable defaults as on RDS: `innodb_flush_log_at_trx_commit=1`, binlog on, `sync_binlog=1` |
| gp3 disk: 3,000 IOPS, 125 MB/s | **Not emulated** on the stock WSL kernel (no `BLK_DEV_THROTTLING`). [`compose.disk.yml`](../../bench/compose.disk.yml) has the cap for a kernel that supports it (`DISK_CAP=1`). E5 measures the cost of `fsync` instead |
| Database outside the cluster, inside the VPC | MySQL and Cassandra in compose, reached from the pods through Services + EndpointSlices that point at Toxiproxy's fixed IP ([`k8s/databases.yaml`](../../bench/k8s/databases.yaml)) |
| ~1 ms round trip between the app and the database (different AZs) | [Toxiproxy](https://github.com/Shopify/toxiproxy) in compose, one proxy per database ([`toxiproxy.json`](../../bench/toxiproxy.json)), with a 1 ms `latency` toxic downstream ([`latency.sh`](../../bench/latency.sh)). The WSL kernel has no `netem`, so `tc` cannot do it |
| Tenant endpoints on the internet | a fake target Deployment ([`target/`](../../bench/target)) |

### Host

Windows + WSL2 (Ubuntu) with **Docker Engine installed inside WSL** (with Docker Desktop, containers run in another VM), plus `k3d`, `kubectl` and `sysstat` (for `iostat`). The buildx plugin is not needed: `up.sh` builds the images with `docker compose` ([`compose.build.yml`](../../bench/compose.build.yml)), which has BuildKit built in. Docker in WSL runs on cgroup v1, which the kubelet refuses since Kubernetes 1.35, so [`k3d.yaml`](../../bench/k3d.yaml) passes `--kubelet-arg=fail-cgroupv1=false`. Give WSL fixed resources in `%UserProfile%\.wslconfig`:

```ini
[wsl2]
processors=8
memory=12GB
```

### Latency and disk, and how to check them

- **Latency.** The pods never reach MySQL and Cassandra directly: `mysql:3306` and `cassandra:9042` resolve to Toxiproxy, which forwards to the databases. `latency.sh 1ms` adds a `latency` toxic of 1 ms on the downstream side (database → pod) of both proxies through Toxiproxy's API (`127.0.0.1:8474`), and `latency.sh off` removes it, live, with no restart. At the end it runs 500 `SELECT 1` over one connection from a pod and prints the average round trip: it must go up by about 1 ms with the toxic. Cassandra announces Toxiproxy's IP as its `broadcast_rpc_address`: otherwise gocql would discover the real node address in `system.local` and connect to it, skipping the proxy. Every `run.txt` records the toxics that were active.
- **Disk.** Not capped. The stock WSL kernel is built without `BLK_DEV_THROTTLING`: Docker accepts `blkio_config`, warns in `docker info` (`No blkio throttle.write_iops_device support`) and drops it, so `docker inspect` shows a cap that is not applied. `DISK_CAP=1` (with `DISK_DEVICE`, the device of `df /var/lib/docker`) is only for a kernel that has it; check it with `iostat` (at most ~3,000 writes per second on that device). What the disk costs is measured in E5 by changing how often MySQL calls `fsync`.
- **Durability.** `durability.sh 1` or `2` sets `innodb_flush_log_at_trx_commit` live (`SET GLOBAL`); without an argument it prints it, with `sync_binlog`. Every `run.txt` records it. It goes back to 1 when MySQL restarts.

### System under test

| Setting | Value | Where |
|---|---|---|
| Scheduler poll / lookahead | 5 s / 30 s | code |
| Executor replicas × `WORKER_COUNT` | 1 × 50 (varied in E2, E3) | `kubectl scale`, `kubectl set env` |
| Executor MySQL pool | **5 open connections** | `executor-service/internal/adapters/out/mysql/db.go` |
| Executor HTTP pool | `MaxIdleConnsPerHost: 10` | `executor-service/internal/adapters/out/httpclient/executor.go` |
| `ALLOW_PRIVATE_DESTINATIONS` | `true`: the target is a ClusterIP, which the SSRF check blocks | env |
| Archiver `SUCCEEDED_RETENTION` | 1 h, so jobs stay in MySQL during a run | env |

## 3. Metrics

| Metric | Meaning | How |
|---|---|---|
| **Start lag** p50 / p95 / p99 / max, per priority | when the request reached the target − `X-Scheduled-At`, first attempts only | target `/stats` |
| **Completion lag** p50 / p99 / max | `finished_at − scheduled_at`: includes the call and the result write | SQL |
| **Throughput** | succeeded jobs per second, sustained | SQL |
| **Correctness** | expected vs created vs succeeded; jobs left pending; duplicate successes (same `Idempotency-Key` answered 200 twice) | SQL + `/stats` |
| CPU and memory per pod | peak millicores and MiB | `kubectl top pods` every 5 s |
| CPU and memory of the databases | | `docker stats` every 5 s |
| MySQL | inserts, updates, commits, redo `fsync`s and row lock waits per second | `SHOW GLOBAL STATUS` before/after |
| Disk | writes per second, write latency (`w_await`), queue (`aqu-sz`) | `iostat -dxm 5` |

**SLO for this round:** start lag p99 < 2 s for priority 0 and < 30 s for priority 3; 0 lost jobs; duplicate successes only after a fault.

`started_at` in MySQL is **not** the call time: the executor sets it at claim time, up to one polling interval before the job is due. That is why the start lag is measured at the target. `X-Scheduled-At` has second precision, so every seeded job is due on a whole second.

## 4. The bench

| File | What it does |
|---|---|
| `up.sh` | Starts MySQL, Cassandra and Toxiproxy (compose), runs the migrations, builds the 7 images, creates the k3d cluster, imports the images, applies the manifests, applies the latency |
| `latency.sh 1ms` / `off` | Adds or removes the latency toxic on both proxies and measures the `SELECT 1` round trip from a pod |
| `durability.sh [1\|2]` | Sets `innodb_flush_log_at_trx_commit` live, or prints it |
| `run.sh RUN RATE DURATION [LAT_MS] [ERR] [steady\|burst]` | One full run: reset the target, start `collect.sh`, seed, wait, print and save the results to `bench/results/run-RUN/` |
| `seed.sh` | Inserts `at` tasks straight into MySQL: `RATE` per second for `DURATION` s, the first due 60 s later (longer than the 30 s lookahead), priorities 0–3 mixed. It bypasses the Task API on purpose: the API is not under test, and SQL creates 100,000 tasks in seconds. Each run has its own `tenant_id` |
| `collect.sh` | `kubectl top`, `docker stats`, `iostat` every 5 s; MySQL counters at the start and the end |
| `results.sh RUN` | Correctness, throughput, completion lag per priority, start lag from the target, MySQL rates, peak CPU and memory per pod |
| `down.sh` | Deletes the cluster and the databases |
| `target/` | The fake endpoint: `/t?lat=50&dist=lognormal&err=0.05&p=0` sleeps around `lat` ms with a long tail, answers `503` with probability `err`, records the start lag; `GET /stats`, `POST /reset` |

```bash
cp .env.example .env        # set JWT_SECRET
bench/up.sh                 # LATENCY=off to start without the latency (bench/latency.sh 1ms adds it later)
bench/run.sh 0 10 120       # E0
```

## 5. Experiments

| # | Question | Setup | Commands | Expected (hypothesis) |
|---|---|---|---|---|
| E0 | Does it work? | 1 executor × 50 workers, lat 50 ms | `run.sh 0 10 120` | 1,200 jobs, 0 lost, 0 duplicates, start lag p99 < 1 s |
| E1 | Ceiling of one executor | 1 × 50, lat 50 ms | `run.sh 11 50 120`, then 12 → 100/s, 13 → 200/s, 14 → 400/s, 15 → 800/s | 50 workers / 0.05 s allow 1,000/s, so the knee comes earlier: from the **5 MySQL connections** that every result write shares (each commit pays the 1 ms and an `fsync`), or from the single scheduler |
| E2 | Scale out | 1, 2, 4 executors | `kubectl -n scheduler scale deploy/executor-service --replicas=2`, then `run.sh 21 <rate above E1's knee> 120`; same for 4 | Throughput grows with executors until MySQL saturates: commits, lock waits and CPU go up and the curve flattens |
| E3 | Slow endpoints | 1 executor, lat 1,000 ms, 50 vs 200 workers | `run.sh 31 100 120 1000`; `kubectl -n scheduler set env deploy/executor-service WORKER_COUNT=200`; `run.sh 32 100 120 1000` | 100/s × 1 s = 100 jobs in flight needed. With 50 workers the lag grows without limit; with 200 it holds. Memory grows with the jobs in flight |
| E4 | 09:00 burst | 2 executors, lat 50 ms | `run.sh 4 20000 1 50 0 burst` | Drain time ≈ 20,000 / E2 throughput. Priority 0 drains first; priority 3 has the longest lag |
| E5 | How much the redo `fsync` costs | E2's best setup | Rerun E2's best after `durability.sh 2`, then `durability.sh 1` to restore | `1` (RDS default) calls `fsync` on the redo log at every commit; `2` writes it at every commit and calls `fsync` about once per second. If the throughput rises a lot with `2` and `Innodb_os_log_fsyncs` drops, commits were the limit. `sync_binlog=1` stays: the binlog still pays one `fsync` per commit group, so this isolates the redo log only |
| E6 | Executor pod dies | 2 executors, lat 1,000 ms | `run.sh 6 50 180 1000` and, 120 s later (60 s of lead + 60 s of load), `kubectl -n scheduler delete pod <one executor> --grace-period=0 --force` | Its in-flight jobs come back once the lease expires (due time + 10 s timeout + 30 s) and the reconciler runs (every minute): a lag spike of about 40–100 s. Duplicate successes ≤ its jobs in flight. 0 lost |

Record every run in one table:

| Run | Executors × workers | Target lat | Offered/s | Sustained/s | Start lag p50 / p99 / max (s) | Lost | Dup | Executor CPU / mem | MySQL commits/s, fsyncs/s, lock waits/s |
|---|---|---|---|---|---|---|---|---|---|

## 6. SQL

The seed is in [`bench/seed.sh`](../../bench/seed.sh) and the result queries in [`bench/results.sh`](../../bench/results.sh). The seed generates the tasks with a recursive CTE:

```sql
INSERT INTO tasks (...)
WITH RECURSIVE n (i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM n WHERE i + 1 < :total)
SELECT UUID_TO_BIN(UUID()), :run_tenant, ..., 'at',
       @start + INTERVAL (i DIV :rate) SECOND,      -- :rate jobs on each whole second
       ..., i % 4,                                  -- priorities mixed evenly
       CONCAT('http://target:8080/t?lat=50&dist=lognormal&err=0&p=', i % 4), ...
FROM n;
```

## 7. Threats to validity

- **One host.** Every pod, MySQL, Cassandra and the target share the same CPUs. Limits reduce interference but don't remove it.
- **Nodes are containers.** The k3d agents limit memory but not CPU: CPU is limited per pod only.
- **Emulated latency.** A fixed 1 ms, without jitter, not real network behaviour (packet loss, tail latency).
- **Toxiproxy is in the path.** Every byte to and from the databases goes through a userspace proxy that copies it, even with the latency off. It adds a little latency and CPU of its own, and it is one more process sharing the host. The baseline without the toxic is therefore not "no network" but "the proxy alone".
- **Disk not capped.** The stock WSL kernel has no `BLK_DEV_THROTTLING`, so MySQL writes to the local disk with no IOPS or bandwidth cap, and `fsync` on local NVMe is far faster than on gp3, which goes over the network. Database-bound numbers here are an upper bound; E5 shows how much they depend on `fsync`.
- **Small tables.** The hot table fits in the buffer pool; in production it would be larger, and indexes would miss the cache more often.
- **One endpoint, plain HTTP.** No TLS handshakes, and connection reuse is not the same as with real tenants spread over many HTTPS hosts. Executor CPU is lower than in production.

## 8. Results

*(fill in after the runs)*

Start the results, and the article, with the warning at the top and this note on every number that depends on the emulation:

> [!NOTE]
> Measured on k3d with 1 ms of emulated latency to MySQL and Cassandra (Toxiproxy) and no disk cap (local disk). On AWS, expect the database-bound numbers to be lower.

- The table of every run ([§5](#5-experiments)).
- One graph: sustained throughput and start lag p99 against offered load (E1, E2).
- The first bottleneck and the evidence for it (which metric saturated first).
- What to change next, and the expected gain.
