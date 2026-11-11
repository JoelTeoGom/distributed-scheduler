# Benchmarks

> How the scheduler behaves under the load of a real platform: how many jobs it runs, how late they start, and what breaks first.

The tests answer the questions a product owner would ask before launch. The work went in four steps:

1. **Build the platform.** A small AWS-shaped deployment on one PC: a Kubernetes cluster that plays EKS, with the databases outside it like RDS, 1 ms away. See [the platform](#1-the-platform).
2. **Model the load.** A cron platform with 1,000 customers and 50,000 tasks, whose crons all land on second :00. See [the workload](#2-the-workload-a-platform-like-a-real-one).
3. **Decide what to measure.** Objectives for the start delay and for lost or duplicated jobs, and telemetry of every pod every 2 seconds. See [how we measure](#3-how-we-measure).
4. **Run the tests.** A calibration round, P0, then nine tests, P1 to P9, each answering one question. See [the tests](#4-the-tests) and [what we learned](#5-what-we-learned).

| Test | Question |
|---|---|
| [P0](#p0--calibration) | Does the bench measure well, and is it never the bottleneck itself? |
| [P1](#p1--smoke-test) | Does every job run once, and on time, when nothing is going on? |
| [P2](#p2--capacity) | How many jobs per second can we run, and does adding servers help? |
| [P3](#p3--normal-day) | On a normal day, do jobs start when they should? |
| [P4](#p4--the-0900-peak) | At 09:00, when 30,000 jobs are due in the same second, how long until they have all run? Do the important ones go first? |
| [P5](#p5--slow-endpoints) | What if our customers' endpoints are slow, 10 to 30 seconds per call? |
| [P6](#p6--noisy-neighbour) | Can one customer with a broken endpoint slow everybody else down? |
| [P7](#p7--an-executor-dies) | If a server dies in the middle of the work, are its jobs lost or run twice? |
| [P8](#p8--mysql-outage) | If the database disappears for 30 seconds, do we lose anything? |
| [P9](#p9--long-run) | After hours of running, is it still as fast as at the start? |

**Status:** every test ran on 2026-11-10. [What we learned](#5-what-we-learned) sums them up.

> [!WARNING]
> Every number here comes from an **emulated** AWS on one machine. It shows the order of magnitude and, above all, **what breaks first**. Comparisons between runs (1 vs 3 executors, before vs after a change) are more reliable than absolute values. See [threats to validity](#7-threats-to-validity).

---

## 1. The platform

![Benchmark platform](docs/benchmarks/platform.png)

*Drawn from [docs/benchmarks/diagram](docs/benchmarks/diagram): edit `draw.mjs` and run `render.sh` to export it again.*

> [!NOTE]
> This is the most realistic setup that can be built quickly on one PC: a real Kubernetes cluster (k3d) with a control plane and three worker nodes, every service sized like an AWS Fargate task, and the databases outside the cluster like RDS, 1 ms away as in another availability zone. The services run unchanged: the same images and configuration as anywhere else.

### What runs where

| Component | Plays the role of | Runs on | CPU | Memory |
|---|---|---|---|---|
| node-0 | EKS control plane | k3d server | shared | no cap |
| node-1, node-2, node-3 | EKS worker nodes | k3d agents | shared | 4 GB each |
| executor-service × 2 | | worker nodes | 0.5 vCPU each | 1 GiB each |
| scheduler-service | | a worker node | 0.5 vCPU | 1 GiB |
| reconciler, archiver, task-service, job-service | | worker nodes | 0.5 vCPU each | 1 GiB each |
| fake-endpoint | customers' APIs | a worker node | 2 vCPU | 512 MiB |
| MySQL 8.4 | RDS `db.t4g.medium` | docker compose | 2 vCPU | 4 GB (3 GB buffer pool) |
| Cassandra 5 | a small Cassandra node | docker compose | 2 vCPU | 2 GB |
| Toxiproxy | the network between AZs | docker compose | | |

CPU and memory limits equal requests on every pod, so no pod can borrow from another. CoreDNS, metrics-server and the local-path provisioner come with k3s.

> [!IMPORTANT]
> **Decisions forced by the machine** (Windows + WSL2 + Docker Engine 29):
> - **Latency with Toxiproxy, not `tc netem`.** The WSL kernel has no `netem`. The pods reach `mysql:3306` and `cassandra:9042` through Toxiproxy, which delays every reply by 1 ms. Cassandra announces Toxiproxy's address, or the driver would find the real one and skip it.
> - **No disk cap.** The WSL kernel cannot throttle block I/O, and Docker drops the cap without an error. MySQL writes to the local disk, faster than an RDS volume.
> - **Three worker nodes, not two.** Seven pods of 1 GiB do not fit in two nodes of 4 GB, and node-0 runs no app pods, like an EKS control plane.
> - **cgroup v1.** Docker in WSL runs on cgroup v1, which the kubelet refuses since Kubernetes 1.35 unless told otherwise (`fail-cgroupv1=false`).
> - **CPU limits forced on.** k3s misreads WSL's cgroup v1 layout and starts the kubelet without CPU quotas, so every pod could use all 12 CPUs. `cpu-cfs-quota=true` turns them back on: found by the calibration round, [P0](bench/results/P0-calibration/README.md).

### Service settings for the tests

Every database poll runs faster than the defaults, and the executors have workers to spare. The first runs, before the tests, showed why: the executor claims every `JOB_TIME_LIMIT_SECONDS`, and a job holds its worker while it waits for its time, so one executor runs at most `WORKER_COUNT / JOB_TIME_LIMIT_SECONDS` jobs per second, 5 with the defaults (see [P0](bench/results/P0-calibration/README.md)).

| Service | Setting | Default | In the tests |
|---|---|---|---|
| executor | replicas × `WORKER_COUNT` | 1 × 50 | **2 × 10,000** |
| executor | claim every `JOB_TIME_LIMIT_SECONDS`, that far ahead | 10 s | **1 s** |
| scheduler | `SCHEDULE_INTERVAL` / `SCHEDULE_LOOKAHEAD` | 5 s / 30 s | **1 s / 10 s** |
| reconciler | `RECONCILE_INTERVAL` | 1 min | **10 s** |
| archiver | `ARCHIVE_INTERVAL` | 1 min | **10 s** |

The executor and the scheduler serve Go's `pprof` on `:6060` (`PPROF_ADDR`), off unless set.

How it works underneath, in separate articles:

- [How a query reaches MySQL](docs/benchmarks/articles/latency.md): the path through Toxiproxy, and how the 1 ms was checked.
- [The life of a job during a test](docs/benchmarks/articles/life-of-a-job.md): from the seeded task to the saved result.
- [The fake endpoint](docs/benchmarks/articles/fake-endpoint.md): what the jobs call, and how it measures them.


## 2. The workload: a platform like a real one

What defines a cron platform is not its average load but its **aligned peaks**: people schedule at the top of the minute and the hour (`0 * * * *`, `0 9 * * *`), so the average is low and everything lands on second :00.

The model: **1,000 customers and 50,000 active tasks**, the 10 largest customers holding 40% of them.

| Kind of task | Share | Tasks | When they fire |
|---|---|---|---|
| Cron every minute | 2% | 1,000 | 1,000 jobs on second :00 of every minute |
| Cron every 5 minutes | 8% | 4,000 | 4,000 jobs at :00, :05, :10… |
| Cron every hour | 20% | 10,000 | 10,000 jobs at the top of the hour |
| Cron every day at 09:00 | 30% | 15,000 | 15,000 jobs at 09:00:00 |
| One-off (`at`): reminders, delayed webhooks | 40% | ~20,000 per hour | at random times, ~5.5 per second |

About **38 jobs per second on average**, with peaks of 1,000 jobs every minute, 5,000 every 5 minutes, 15,000 every hour and **30,000 at 09:00:00**, when all of them meet.

Every task calls a customer endpoint with one of these behaviours:

| Endpoint | Share | Behaviour |
|---|---|---|
| Fast | 80% | ~150 ms, with a long tail |
| Slow | 12% | 1 to 10 s |
| Very slow | 5% | 10 to 30 s; some reach the timeout |
| Flaky | 2% | 503 on 30% of the calls, so it retries |
| Down | 1% | always 503, ends `dead` |

Every task has a 30 s timeout, 3 attempts and a 5 s backoff. Priorities: 5% critical (0), 15% high (1), 60% normal (2), 20% low (3). The base deployment runs **2 executors**, as production would for availability.


## 3. How we measure

**Service level objectives** for this round:

| | Target |
|---|---|
| On time, normal load | p99 start delay ≤ 1 s, every priority |
| On time, during a peak | priority 0 p99 ≤ 5 s; the whole peak run within 60 s |
| Nothing lost | 0 lost jobs; duplicates only after a failure, at most the jobs in flight |
| Isolation | a customer with a broken endpoint raises the others' p99 by less than 20% |
| Retries | flaky endpoints succeed within their 3 attempts; dead ones end `dead` |

**Start delay** is when the call reached the endpoint minus when the job was due, measured by the fake endpoint. Every test reports it as **p50, p90, p99 and max**, per priority and over time.

**Resources, every 2 seconds**, read straight from the kernel (cgroups and `/proc`), with nothing installed in the cluster:

| What | Metrics | Units |
|---|---|---|
| Every pod | CPU, % of its limit, time throttled by the limit, memory in use | millicores, %, ms per second, MiB |
| MySQL, Cassandra, Toxiproxy | CPU, memory | millicores, MiB |
| Disk | writes, throughput, write latency, busy time | per second, MB/s, ms, % |
| MySQL | commits, redo `fsync`s, row lock waits, running queries, connections | per second, count |
| Queue | jobs pending and running, finished and dead per second | count, per second |

**Every call and every job:** the fake endpoint records each call it receives (arrival, due time, priority, tenant, endpoint profile, attempt, status), and each job row is exported from MySQL at the end, so a delay can be followed from the claim to the call.

**Profiling:** each test takes 20-second CPU profiles of one executor and of the scheduler at its busiest moment, plus the executor's heap and goroutines.

**Durations**, kept short for this first round:

| Test | Load | Duration |
|---|---|---|
| Calibration | minimal | ~10 min |
| P1 | 10% of the model | 3 min |
| P2 | fast endpoints, 50 → 800 jobs/s in steps; then 2 and 3 executors | ~20 min |
| P3 | the model | 5 min |
| P4 | the model + the 09:00 peak | 8 min |
| P5 | 20 jobs/s, every endpoint 10-30 s; timeout 30 s, then 20 s | 2 × 4 min |
| P6 | the model + one large customer whose endpoint hangs | 5 min |
| P7 | P3 + one executor killed halfway | 6 min |
| P8 | P3 + MySQL cut for 30 s | 6 min |
| P9 | the model | 30 min |

Each test also waits one minute before the load and until the queue is empty after it.

## 4. The tests

Each test has its question, what it does, its results and conclusions here, and a full write-up behind its link: how it runs, what to look at, every chart over time, the resources each component used and the CPU profiles.

### P0 · Calibration

> Does the bench measure well, and is it never the bottleneck itself?

**What we test:** the bench itself, before any test: every network link, what each component uses when idle, the 1 ms of emulated latency, how much load the fake endpoint takes, and a reference run of 50 jobs per second.

| Check | Result |
|---|---|
| Connectivity | 26 checks ok |
| Telemetry cost | 15 millicores while sampling every 2 s |
| Emulated latency | +1.17 ms per query with the toxic |
| Fake endpoint headroom | 1,996 of 2,000 calls/s, 0 errors |
| Reference run | 3,000 / 3,000 succeeded, 0 duplicates, start delay p99 0.21 s |

**Conclusions:** the bench measures well and is not the bottleneck. It found CPU limits off (k3s misreads WSL's cgroups; forced on now), and whole seconds of jobs starting up to 1 s late every ~31 s, which the later tests point to the host's disk writeback.

→ [The full test](bench/results/P0-calibration/README.md): the steps, the first runs before the plan, and both findings.

### P1 · Smoke test

> Does every job run once, and on time, when nothing is going on?

**What we test:** 10% of the model for 3 minutes on 2 executors: the first test after calibration, with nothing else in the way.

<!-- results:P1 -->
| Run | Jobs | Succeeded | Dead | Unfinished | Duplicates | Calls/s | Start delay p50 / p90 / p99 / max |
|---|---|---|---|---|---|---|---|
| [run](bench/results/P1-smoke/reports/20261110T191929Z/report.md) | 400 | 397 | 3 | 0 | 0 | 2.3 | 0.01 / 0.07 / 0.24 / 0.72 s |
<!-- /results:P1 -->

**Conclusions:** Yes: every job ran once (the 3 dead are the always-down endpoint), p99 0.24 s, and nothing came close to a limit.

→ [The full test](bench/results/P1-smoke/README.md): how it runs, what to look at, every chart, resources and CPU profiles.

### P2 · Capacity

> How many jobs per second can we run, and does adding servers help?

**What we test:** one executor with fast endpoints, 50 to 800 jobs per second in 60-second steps until it cannot keep up; then that rate on 2 and 3 executors.

<!-- results:P2 -->
| Run | Jobs | Succeeded | Dead | Unfinished | Duplicates | Calls/s | Start delay p50 / p90 / p99 / max |
|---|---|---|---|---|---|---|---|
| [1x-50](bench/results/P2-capacity/reports/20261110T192413Z-1x-50/report.md) | 3,000 | 3,000 | 0 | 0 | 0 | 50.8 | 0.00 / 0.01 / 0.80 / 0.80 s |
| [1x-100](bench/results/P2-capacity/reports/20261110T192623Z-1x-100/report.md) | 6,000 | 6,000 | 0 | 0 | 0 | 101.7 | 0.00 / 0.01 / 0.80 / 0.80 s |
| [1x-200](bench/results/P2-capacity/reports/20261110T192832Z-1x-200/report.md) | 12,000 | 12,000 | 0 | 0 | 0 | 203.4 | 0.01 / 0.10 / 0.81 / 1.01 s |
| [1x-400](bench/results/P2-capacity/reports/20261110T193042Z-1x-400/report.md) | 24,000 | 24,000 | 0 | 0 | 0 | 300.9 | 3.36 / 21.85 / 26.65 / 27.95 s |
| [2x-400](bench/results/P2-capacity/reports/20261110T193326Z-2x-400/report.md) | 24,000 | 24,000 | 0 | 0 | 0 | 336.5 | 3.52 / 11.73 / 13.92 / 14.22 s |
| [3x-400](bench/results/P2-capacity/reports/20261110T193552Z-3x-400/report.md) | 24,000 | 24,000 | 0 | 0 | 0 | 330.3 | 4.28 / 12.47 / 14.67 / 16.07 s |

![Capacity](bench/results/P2-capacity/capacity.svg)
<!-- /results:P2 -->

**Conclusions:** About 300 jobs/s, and more executors do not help: the **scheduler** is the limit. It creates at most 1,000 jobs per tick with one `UPDATE` per task, ~3.3 s per tick, so ~300 jobs/s; it used 100 of its 500 millicores, waiting on the database. The executors sit idle waiting for jobs, and MySQL had room left.

→ [The full test](bench/results/P2-capacity/README.md): how it runs, what to look at, every chart, resources and CPU profiles.

### P3 · Normal day

> On a normal day, do jobs start when they should?

**What we test:** the whole model for 5 minutes on 2 executors: 1,000 jobs on second :00 of every minute, 4,000 more at the 5-minute mark, one-off jobs in between. The reference for P6, P7 and P8.

<!-- results:P3 -->
| Run | Jobs | Succeeded | Dead | Unfinished | Duplicates | Calls/s | Start delay p50 / p90 / p99 / max |
|---|---|---|---|---|---|---|---|
| [run](bench/results/P3-normal-day/reports/20261110T193822Z/report.md) | 10,667 | 10,496 | 171 | 0 | 0 | 35.7 | 0.60 / 2.19 / 6.67 / 6.68 s |
<!-- /results:P3 -->

**Conclusions:** On ordinary seconds yes (≤ 0.9 s); at the 5-minute mark, with 5,005 jobs due at once, no: 6.7 s. Part is the executors firing ~3,000 calls at 0.5 vCPU (~2 s), most is the scheduler's last batches of 1,000 arriving after their time.

→ [The full test](bench/results/P3-normal-day/README.md): how it runs, what to look at, every chart, resources and CPU profiles.

### P4 · The 09:00 peak

> When 30,000 jobs are due in the same second, how long until they have all run? Do the important ones go first?

**What we test:** the model for 8 minutes with the 09:00 moved into the run, when every kind of cron meets: about 30,000 jobs due on one second.

<!-- results:P4 -->
| Run | Jobs | Succeeded | Dead | Unfinished | Duplicates | Calls/s | Start delay p50 / p90 / p99 / max |
|---|---|---|---|---|---|---|---|
| [run](bench/results/P4-0900-peak/reports/20261110T194554Z/report.md) | 44,667 | 44,168 | 499 | 0 | 1 | 90.3 | 23.36 / 77.38 / 91.74 / 94.14 s |

- **09:00 peak:** 30,007 jobs due on the same second. The last one started **94.1 s** late (p50 43.3 s, p99 91.7 s); priority 0 p99 **91.6 s**.
<!-- /results:P4 -->

**Conclusions:** No: 94 s for the whole peak, against 60 s, and priority 0 as late as everyone else. The scheduler creates the 30 batches of 1,000 in ~100 s and picks tasks by time alone, so priority cannot help jobs that do not exist yet.

→ [The full test](bench/results/P4-0900-peak/README.md): how it runs, what to look at, every chart, resources and CPU profiles.

### P5 · Slow endpoints

> What if our customers' endpoints are slow, 10 to 30 seconds per call?

**What we test:** 20 jobs per second for 4 minutes, every endpoint taking 10 to 30 s; once with the 30 s timeout and once with 20 s, which half the calls exceed.

<!-- results:P5 -->
| Run | Jobs | Succeeded | Dead | Unfinished | Duplicates | Calls/s | Start delay p50 / p90 / p99 / max |
|---|---|---|---|---|---|---|---|
| [timeout-30](bench/results/P5-slow-endpoints/reports/20261110T195707Z-timeout-30/report.md) | 4,800 | 4,800 | 0 | 0 | 0 | 20.1 | 0.00 / 0.00 / 0.53 / 0.93 s |
| [timeout-20](bench/results/P5-slow-endpoints/reports/20261110T200247Z-timeout-20/report.md) | 4,800 | 4,182 | 618 | 0 | 0 | 20.1 | 0.00 / 0.01 / 0.94 / 0.96 s |
<!-- /results:P5 -->

**Conclusions:** Harmless with the 30 s timeout (4,800/4,800, p99 0.53 s). With 20 s, 618 jobs ended dead and the customers finished 4,326 calls the platform had given up on, so the work was repeated: retries need an idempotency key.

→ [The full test](bench/results/P5-slow-endpoints/README.md): how it runs, what to look at, every chart, resources and CPU profiles.

### P6 · Noisy neighbour

> Can one customer with a broken endpoint slow everybody else down?

**What we test:** P3's load, with one of the ten largest customers' endpoint hanging past the 30 s timeout.

<!-- results:P6 -->
| Run | Jobs | Succeeded | Dead | Unfinished | Duplicates | Calls/s | Start delay p50 / p90 / p99 / max |
|---|---|---|---|---|---|---|---|
| [run](bench/results/P6-noisy-neighbour/reports/20261110T200915Z/report.md) | 10,667 | 10,109 | 558 | 0 | 0 | 35.7 | 0.82 / 2.98 / 7.56 / 7.66 s |

- **Noisy tenant:** 1,338 calls to its hanging endpoint. Every other tenant: p99 **7.562 s**, p50 0.824 s, over 10,221 jobs.
<!-- /results:P6 -->

**Conclusions:** No: the others' p99 rose 13% (7.56 s against 6.67 s in P3), under the 20% allowed. The hanging customer held a few hundred of the 20,000 workers.

→ [The full test](bench/results/P6-noisy-neighbour/README.md): how it runs, what to look at, every chart, resources and CPU profiles.

### P7 · An executor dies

> If a server dies in the middle of the work, are its jobs lost or run twice?

**What we test:** P3's load for 6 minutes, with one of the two executors killed at 3:00 with no grace period.

<!-- results:P7 -->
| Run | Jobs | Succeeded | Dead | Unfinished | Duplicates | Calls/s | Start delay p50 / p90 / p99 / max |
|---|---|---|---|---|---|---|---|
| [run](bench/results/P7-executor-dies/reports/20261110T201711Z/report.md) | 12,000 | 11,821 | 179 | 0 | 0 | 33.4 | 0.59 / 2.12 / 6.13 / 6.22 s |

- **kill-executor** at 3:08: 1,009 jobs started more than 5 s late, the worst 6 s late. Duplicate successes: **0**.
<!-- /results:P7 -->

**Conclusions:** Nothing lost, nothing run twice, and no visible delay. The kill came between bursts, when the executor held few jobs; a kill during a burst would test the reconciler harder.

→ [The full test](bench/results/P7-executor-dies/README.md): how it runs, what to look at, every chart, resources and CPU profiles.

### P8 · MySQL outage

> If the database disappears for 30 seconds, do we lose anything?

**What we test:** P3's load for 6 minutes, with every connection to MySQL refused for 30 s at 3:00.

<!-- results:P8 -->
| Run | Jobs | Succeeded | Dead | Unfinished | Duplicates | Calls/s | Start delay p50 / p90 / p99 / max |
|---|---|---|---|---|---|---|---|
| [run](bench/results/P8-mysql-outage/reports/20261110T202542Z/report.md) | 13,000 | 12,883 | 117 | 0 | 16 | 34.3 | 0.90 / 35.99 / 39.59 / 40.38 s |

- **mysql-cut** at 3:07: 5,270 jobs started more than 5 s late, the worst 40 s late. Duplicate successes: **16**.
<!-- /results:P8 -->

**Conclusions:** Nothing lost; 16 jobs ran twice because their result could not be saved during the cut, and the reconciler ran them again after their lease. Every service recovered on its own.

→ [The full test](bench/results/P8-mysql-outage/README.md): how it runs, what to look at, every chart, resources and CPU profiles.

### P9 · Long run

> After hours of running, is it still as fast as at the start?

**What we test:** the model for 30 minutes, with CPU profiles at 5 and at 28 minutes.

<!-- results:P9 -->
| Run | Jobs | Succeeded | Dead | Unfinished | Duplicates | Calls/s | Start delay p50 / p90 / p99 / max |
|---|---|---|---|---|---|---|---|
| [run](bench/results/P9-long-run/reports/20261110T203454Z/report.md) | 66,000 | 65,297 | 703 | 0 | 0 | 35.4 | 0.64 / 4.70 / 8.70 / 8.90 s |
<!-- /results:P9 -->

**Conclusions:** Yes: the p99 of each 5-minute window stays between 6 and 9 s with no trend, and memory is flat. A run of hours is still to do.

→ [The full test](bench/results/P9-long-run/README.md): how it runs, what to look at, every chart, resources and CPU profiles.

## 5. What we learned

> [!IMPORTANT]
> **The bottleneck is the scheduler, not the hardware.** One scheduler creates about 300 jobs per second, because each tick takes at most 1,000 tasks and updates each one with its own round trip to MySQL. It sets the capacity (P2), the 6.7 s at every 5-minute burst (P3) and the 94 s of the 09:00 peak (P4), while it uses a fifth of its CPU. Ticking more often does not help, since a tick already lasts longer than the interval; fewer round trips per tick, or several schedulers, would.

1. **The hardware is not the problem.** With 0.5 vCPU per service, 2 CPUs for MySQL and 4 GB nodes, nothing ran out except the executor's CPU for a second or two at each burst. MySQL peaked at ~2,500 commits/s with at most 3 row lock waits per second. The rest never saw more than ~330 jobs/s, though, because the scheduler did not let it: once the scheduler is faster, the next limit will show.
2. **The next limit is the executor, at bursts, and it scales out.** Two executors at 0.5 vCPU take ~2 s to fire 3,000 calls at once (P3). Unlike the scheduler, more replicas or more CPU fix it.
3. **Nothing is ever lost.** In every test, every job ended `succeeded` or `dead`. Duplicates only came after a failure: 16 when MySQL disappeared (P8), 0 when an executor died (P7).
4. **A customer cannot hurt the others,** neither by being slow (P5) nor by hanging (P6): workers are not scarce.
5. **Timeouts shorter than the endpoint repeat work at the customer.** The executor gives up, the endpoint finishes anyway, and the retry runs it again: 4,326 times in P5 with a 20 s timeout.
6. **Priority only orders what already exists.** At the 09:00 peak, priority 0 was as late as priority 3 (P4).

### What could be improved

Ideas from the results, not built:

- **Scheduler:** advance the batch's tasks with one statement instead of one per task; larger batches; several schedulers, each owning a share of the tasks (the partitions in the [readme's to-do list](readme.md#to-do)).
- **Priority at the source:** take tasks in order of time and then priority, and create jobs of large crons (hourly, daily) further ahead, so a peak is ready before it starts.
- **Executor:** more CPU or replicas for bursts, and results saved in batches instead of one `UPDATE` per job over 5 connections.
- **Idempotency:** an idempotency key on every call, so customers can drop retries of work they already did.
- **The bench:** a run of hours; network latency with jitter; a capped disk, closer to RDS; and checking the 30 s writeback hypothesis of [P0](bench/results/P0-calibration/README.md).

## 6. Running it

```text
bench/
├── infra/
│   ├── kubernetes/      the cluster (plays EKS): k3d.yaml, the manifests, the image build
│   └── databases/       MySQL, Cassandra and Toxiproxy in docker compose (plays RDS)
├── fake-endpoint/       the Go service the jobs call, in place of the customers' APIs
├── scripts/             bring the platform up and down, seed, run, measure
└── results/             one folder per test, P0 to P9
```

```bash
cp .env.example .env            # set JWT_SECRET
bench/scripts/up.sh             # databases, Toxiproxy, k3d cluster, images, 1 ms latency
bench/scripts/check.sh          # every link, from Docker to one job end to end
bench/scripts/p0-calibration.sh # P0
bench/scripts/tests.sh P3       # one test; `all` runs P1 to P9, about 2 hours
python3 bench/scripts/index.py  # write the results into each test's README and the summaries below
bench/scripts/down.sh           # delete everything except bench/results
```

`bench/scripts/latency.sh 1ms|off` changes the latency live, and `bench/scripts/durability.sh 1|2` the MySQL `fsync` policy.

## 7. Threats to validity

- **One host.** Every pod, both databases and the fake endpoint share 12 CPUs. Limits reduce the interference but do not remove it.
- **Nodes are containers.** The worker nodes cap memory but not CPU; CPU is capped per pod.
- **Toxiproxy is in the path.** Every byte to and from the databases goes through a proxy in user space, even with the latency off: the baseline is "the proxy alone", not "no network".
- **Fixed latency.** 1 ms without jitter, loss or tail, unlike a real network.
- **Disk not capped.** `fsync` on a local SSD is far faster than on an RDS volume: database-bound numbers are an upper bound.
- **Small tables.** The hot table fits in memory; in production it would be larger and miss the cache more often.
- **One endpoint, plain HTTP.** No TLS, and connection reuse is easier than with thousands of customer hosts.
