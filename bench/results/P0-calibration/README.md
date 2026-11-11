# P0 · Calibration

> Does the bench measure well, and is it never the bottleneck itself?

**Load:** minimal, ~10 min ([`bench/scripts/p0-calibration.sh`](../../scripts/p0-calibration.sh))
**Status:** run on 2026-11-10 (`20261110T184939Z`). The bench measures well; it found one platform bug (fixed) and one delay that the later tests traced to the host.

## Before the plan: the first runs

Two runs before the tests were defined, with the services' default settings, found the first bottleneck:

| Run | Load | Executors × workers | Ran | Throughput | Start delay p50 / p99 | Duplicates |
|---|---|---|---|---|---|---|
| E0 | 10 jobs/s, 120 s, 50 ms endpoints | 1 × 50 | 1,200 / 1,200 | **5.0 / s** | 0.002 s / p0 1.4 s, p3 **185 s** | 0 |
| E1 | 50 jobs/s, 120 s, 50 ms endpoints, +1 ms | 1 × 10,000 | 6,000 / 6,000 | 50.4 / s | 0.002 s / 1.24 s | 0 |

> [!NOTE]
> **The executor's claim loop caps throughput.** It claims every 10 s, up to 10 s ahead, and a job holds its worker while it waits for its time. One executor runs at most `WORKER_COUNT / 10 s` jobs per second: 5 with 50 workers. Above that, the queue grows and strict priority leaves priority 3 waiting for minutes. With 10,000 workers the ceiling moves to 1,000 jobs/s. The tests run with 10,000 workers and a claim every second.

E1 also showed 16% of the jobs claimed on time but reaching the endpoint 1.23 s late, between the claim and the HTTP call: the same late batches the calibration found below.

## Steps

| # | Checks | How | Passes when |
|---|---|---|---|
| 1 | Connectivity | `check.sh --quick` | every link is up |
| 2 | Idle floor | 60 s of telemetry with no load | we know each component's floor and what the telemetry costs |
| 3 | Emulated latency | `SELECT 1` from a pod, Toxiproxy without and with the toxic | the toxic adds ~1 ms |
| 4 | Fake endpoint headroom | fortio inside the cluster, 2,000 calls/s with 50 ms calls | ≥ 2,000 calls/s with < 10 ms p99 overhead |
| 5 | Reference run | 50 jobs/s for 60 s, 1 executor × 10,000 workers | every job runs once; repeatable |

Clock skew needs no check: every container shares the WSL kernel and its clock.

## Results

| Check | Result | Detail |
|---|---|---|
| Connectivity | pass | 26 checks ok |
| Telemetry is cheap | pass | 15 millicores while sampling every 2 s |
| Latency adds ~1 ms | pass | 0.23 ms through Toxiproxy alone, 1.40 ms with the toxic: +1.17 ms |
| Fake endpoint headroom | **fail, harmless** | 1,996 of 2,000 calls/s, 0 errors, 627 of 2,000 millicores; p99 67 ms for a 50 ms sleep (+17 ms, above the 10 ms target) |
| Reference run | pass | 3,000 / 3,000 succeeded, 0 duplicates, start delay p50 0.005 s, p90 0.202 s, p99 0.206 s |

**Idle floor:** Cassandra 99 millicores and 826 MiB, MySQL 26 millicores and 642 MiB, Toxiproxy 1 millicore, every service under 1 millicore and 3-7 MiB.

The fake endpoint misses the overhead target, but it does not affect what the tests measure: the start delay is taken when a call arrives, before the endpoint does anything, and the highest planned load (800 jobs/s in P2) is well below the 2,000 calls/s it sustained.

## Findings

> [!IMPORTANT]
> **CPU limits were off.** k3s misreads WSL's cgroup v1 layout ("Disabling CPU quotas due to missing cpu controller") and starts the kubelet with `cpuCFSQuota: false`, although the kernel supports quotas. Every pod could use all 12 CPUs instead of its 0.5 vCPU. Fixed in [`k3d.yaml`](../../infra/kubernetes/k3d.yaml) with `--kubelet-arg=cpu-cfs-quota=true`; the executor's cgroup now has `cfs_quota_us = 50000` per 100 ms. E0 and E1 ran without limits.

> [!NOTE]
> **Whole claim batches start late by the same amount.** The executor claims every 10 s the jobs due in the next 10 s. In the reference run, 2 of the 6 batches started every one of their jobs 0.20 s late (950 jobs, 32%); the other 4 were on time (2-9 ms). E1 showed the same shape with 1.23 s. The delay is constant inside a batch and does not depend on how many jobs are due each second, so it does not come from the load.
>
> **Update after P1-P9:** the same blocks show up in every test, every ~31 s: a whole second of jobs claimed after its time, 0.3 to 1 s late. They coincide with gaps in the telemetry, which also queries MySQL, and with bursts of disk writes. Linux writes dirty pages back every 30 s (`vm.dirty_expire_centisecs = 3000`), so the likely cause is the host's writeback stalling MySQL's `fsync`s, and the claim with them: a limit of the bench, not of the executor. Not proven yet: lowering that setting should make the late blocks change their rhythm.

Also seen: during the reference run the executor was throttled up to 522 ms per second at 85 millicores on average, so it bursts above its 0.5 vCPU at the top of each second, when 50 calls fire at once.

The MySQL commit rate of this run is wrong (`Com_commit` misses autocommit statements); the telemetry now reads `Handler_commit`.

## Conclusions

**Yes, the bench measures well, and it is not the bottleneck.** The telemetry costs 15 millicores, the fake endpoint takes 2,000 calls/s, and Toxiproxy adds the 1 ms it should.

Two things matter when reading the other tests:

- CPU limits had to be forced on. Without that fix, every pod would have used the 12 CPUs of the host.
- Now and then a whole second of jobs starts up to 1 s late, every ~31 s. It looks like the host's disk, not the scheduler: it sets a floor of ~0.8 s under any p99 measured here.
