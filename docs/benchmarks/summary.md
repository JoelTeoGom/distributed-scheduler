# Benchmarks in one page

> Every test, what it answered, and what each change bought. The details are in [benchmarks.md](../../benchmarks.md), each test's README and [the peak tuning PDF](peak-tuning.pdf).

**The bench:** a small AWS on one PC (WSL2, 12 CPUs).

- **Platform:** k3d plays EKS, with pods of 0.5 vCPU and 1 GiB; MySQL 8.4 plays RDS, with 2 vCPU and `fsync` on every commit.
- **Network:** 1 ms between the pods and the databases, added by Toxiproxy.
- **Customers:** a fake endpoint plays their APIs.
- **Load:** a cron platform like a real one: 1,000 customers, 50,000 tasks and bursts aligned on second :00. The hardest test is the 09:00 peak, ~30,000 jobs due on the same second.

## 1. Each piece

| Piece | Round 1 | Now | Evidence |
|---|---|---|---|
| **Scheduler** | ❌ the bottleneck | ✅ fixed | An N+1, one `UPDATE` per task, capped it at ~300 jobs/s with its CPU at 20%. One statement per batch now: a tick of 1,000 tasks went from 5.5 s to 54 ms. Open: priority at the source. |
| **Executors** | ⚠️ the next limit | ⚠️ the limit at the peak | At 0.5 vCPU they hit their quota at the bursts; at 1 CPU they doubled the throughput. They ran with GOMAXPROCS 12 instead of 1, because Go 1.25 cannot read the limit in WSL; fixed. Past ~7,500 calls at once per executor, first attempts time out. |
| **MySQL** | ✅ fine | ⚠️ the limit past 4 executors | Up to ~2,700 commits/s at half its CPU. With 5 or 6 executors it rises to 72-89% of its CPU without more throughput. The disk is uncapped, so its numbers are optimistic. |
| **Cassandra, reconciler, archiver, APIs** | ✅ | ✅ | Never the limit. The reconciler recovered everything lost to failures and overload. |
| **The bench** | ✅ | ✅ corrected | The fake endpoint ran out of memory at ~30,000 open calls: it now runs outside the cluster with 4 GB. The report timed the peak optimistically: fixed. |

## 2. The 09:00 peak, step by step

Each step keeps the ones before it. A job counts as started when it first reached the endpoint, retries included.

| Step | Jobs/s draining the peak | Every job of the peak started | Gain |
|---|---|---|---|
| Round 1 | 319 | 94.1 s | |
| Fix the scheduler's N+1 | 599 | 50.1 s | ×1.9 |
| GOMAXPROCS from each pod's CPU limit | 749 | 40.0 s | +25%, 64% less throttling |
| 200 ms scheduler tick and 4 executors | 5,810 | 5.2 s | ×7.8 |
| Claims every 250 ms, 500 ms ahead | **6,998** | **4.3 s** | +20% |
| Executors at 1 CPU, the only change of resources | **13,166** | **2.3 s** | ×1.9 |

**×22 with the same resources, ×41 with the executors at 1 CPU.** Each setting ran once, and runs with the same settings differed by up to 30%.

## 3. Each test

| Test | Question | Result |
|---|---|---|
| [P0 · Calibration](../../bench/results/P0-calibration/README.md) | Does the bench measure well? | Yes: telemetry costs 15 millicores, the latency adds 1.17 ms. It found CPU limits off in k3s (fixed) and a few seconds starting late every ~30 s, most likely the host's disk. |
| [P1 · Smoke test](../../bench/results/P1-smoke/README.md) | Does every job run once and on time? | Yes: 400 jobs, the 3 dead on the always-down endpoint, p99 0.24 s. |
| [P2 · Capacity](../../bench/results/P2-capacity/README.md) | How many jobs per second, and do executors scale? | Round 1: ~300 jobs/s with 1, 2 or 3 executors, held by the scheduler. Round 2: 327, 398 and 407 at 400 offered; it scales, and the limit is ~330 jobs/s per executor of 0.5 vCPU. |
| [P3 · Normal day](../../bench/results/P3-normal-day/README.md) | Do jobs start on time? | p99 6.67 s → 2.28 s. Every-minute bursts under 0.5 s; the 5-minute one (5,005 jobs) still ~2.3 s, from the executors' CPU. |
| [P4 · The 09:00 peak](../../bench/results/P4-0900-peak/README.md) | How long until the peak has started? Do urgent jobs go first? | 94 s → 50 s → 40 s. Within the 60 s objective; priority 0 still far from its 5 s. |
| [P5 · Slow endpoints](../../bench/results/P5-slow-endpoints/README.md) | What if customers take 10-30 s? | With a 30 s timeout, harmless (p99 0.53 s). With 20 s, customers finished 4,326 calls the platform gave up on: retries need idempotency keys. |
| [P6 · Noisy neighbour](../../bench/results/P6-noisy-neighbour/README.md) | Can one broken customer slow the others? | No: the others' p99 rose 13%, under the 20% allowed. |
| [P7 · An executor dies](../../bench/results/P7-executor-dies/README.md) | Are jobs lost or run twice? | 0 lost, 0 duplicates. |
| [P8 · MySQL cut for 30 s](../../bench/results/P8-mysql-outage/README.md) | Is anything lost? | 0 lost, 16 duplicates (at-least-once). It recovered on its own. |
| [P9 · Long run](../../bench/results/P9-long-run/README.md) | Does it degrade over time? | No in 30 minutes: stable p99, flat memory. |
| [P10 · The ceiling](../../bench/results/P10-ceiling/README.md) | How far do 1, 2 and 3 executors go? | ~1,000 jobs/s with 3. Past the knee throughput falls (803 jobs/s at 2,000 offered) and overload brings duplicates (313). ~1,000 workers per executor are enough. |
| [P11 · Peak tuning](../../bench/results/P11-peak/README.md) | How far do settings alone go? | 6,998 jobs/s with the same resources, 13,166 at 1 CPU. 4 executors is the sweet spot. Workers trade throughput for priority: with 2,000, priority 0 starts within 0.8 s. Three times the peak (~90,000 jobs) does not scale: the best run took 70 s. |

**No job was ever lost**, in 12 tests and ~1.6 million jobs. Duplicates came only after a failure or under overload.

## 4. Real bugs found

1. **An N+1 in the scheduler,** one `UPDATE` per task. Fixed.
2. **GOMAXPROCS 12 on pods of 0.5 vCPU,** from WSL's hybrid cgroups. Fixed in the manifests.
3. **A scheduler batch over 4,095 tasks** exceeds MySQL's 65,535 placeholders per statement and stalls the scheduler for good. Rejected at startup now.
4. **The executor claims as many jobs as it has free workers,** not as many as it can fire. Under overload, throughput falls and jobs run twice. Open.
5. **The bench measured wrong in two places:** the fake endpoint ran out of memory and the report timed the peak optimistically. Fixed.

## 5. Still open

- **Executor:** claim what it can fire, and save results in batches (one round trip to MySQL per job today).
- **Scaling past one shard,** with one scheduler per shard.
- **Priority at the source,** when the scheduler creates the jobs.
- **Repeating each setting** to report medians.
