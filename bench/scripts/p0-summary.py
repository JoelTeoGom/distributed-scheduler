#!/usr/bin/env python3
"""Summarises a P0 run as Markdown: python3 bench/scripts/p0-summary.py bench/results/P0-calibration/runs/<time>"""
import csv
import json
import re
import statistics
import sys
from collections import defaultdict
from pathlib import Path

run = Path(sys.argv[1])


def rows(name):
    with open(run / name, newline="") as file:
        return list(csv.DictReader(file))


def percentile(values, p):
    ordered = sorted(values)
    return ordered[max(0, -(-len(ordered) * p // 100) - 1)] if ordered else float("nan")


def app(component):
    # executor-service-6689dcc4cd-k6szw -> executor-service
    return re.sub(r"-[0-9a-f]{8,10}-[0-9a-z]{5}$", "", component)


phases = {row["phase"]: (float(row["start"]), float(row["end"])) for row in rows("phases.csv")}
telemetry = defaultdict(list)  # (phase, component, metric) -> values
for row in rows("telemetry.csv"):
    t = float(row["time"])
    for phase, (start, end) in phases.items():
        if start <= t <= end:
            telemetry[(phase, app(row["component"]), row["metric"])].append(float(row["value"]))


def series(phase, component, metric):
    return telemetry.get((phase, component, metric), [])


verdicts = []


def verdict(name, ok, detail):
    verdicts.append(f"| {name} | {'pass' if ok else '**fail**'} | {detail} |")


print(f"# P0 · Calibration, run {run.name}\n")

check = re.sub(r"\x1b\[[0-9;]*m", "", (run / "check.txt").read_text())  # drop the colours
failed = len(re.findall(r"^\s+FAIL\s", check, re.M))
passed = len(re.findall(r"^\s+ok\s", check, re.M))
verdict("Connectivity", failed == 0 and passed > 0, f"{passed} checks ok, {failed} failed")

print("## Idle floor (60 s, no load)\n")
print("| Component | CPU avg (millicores) | CPU max | Memory (MiB) |")
print("|---|---|---|---|")
components = sorted({component for (phase, component, metric) in telemetry if phase == "idle" and metric == "cpu_mcores"})
for component in components:
    cpu = series("idle", component, "cpu_mcores")
    memory = series("idle", component, "mem_mib")
    print(f"| {component} | {statistics.mean(cpu):.1f} | {max(cpu):.1f} | {statistics.mean(memory):.0f} |" if memory
          else f"| {component} | {statistics.mean(cpu):.1f} | {max(cpu):.1f} | |")
telemetry_cpu = statistics.mean(series("idle", "telemetry", "cpu_mcores") or [0])
verdict("Telemetry is cheap", telemetry_cpu < 50, f"{telemetry_cpu:.0f} millicores while sampling every 2 s")

print("\n## Emulated latency (SELECT 1 from a pod, 500 in a row, 3 times each)\n")
rtt = defaultdict(list)
for row in rows("rtt.csv"):
    if row["round_trip_us"].isdigit():
        rtt[row["toxic"]].append(int(row["round_trip_us"]))
off, on = statistics.mean(rtt["off"]), statistics.mean(rtt["1ms"])
print(f"| Toxic | Round trip |\n|---|---|\n| off (Toxiproxy alone) | {off / 1000:.2f} ms |\n| 1 ms | {on / 1000:.2f} ms |")
verdict("Latency adds ~1 ms", 0.8 <= (on - off) / 1000 <= 1.5, f"+{(on - off) / 1000:.2f} ms per round trip ({off / 1000:.2f} -> {on / 1000:.2f} ms)")

print("\n## Fake endpoint headroom (fortio, 2,000 calls/s, 200 connections, 50 ms per call)\n")
fortio = json.loads((run / "fortio.json").read_text())
histogram = {p["Percentile"]: p["Value"] for p in fortio["DurationHistogram"]["Percentiles"]}
codes = fortio.get("RetCodes", {})
endpoint_cpu = series("fortio", "fake-endpoint", "cpu_mcores")
endpoint_throttled = series("fortio", "fake-endpoint", "throttled_ms_per_s")
print(f"- Achieved: **{fortio['ActualQPS']:.0f} calls/s** of 2,000, {sum(codes.values())} calls, codes {codes}")
print(f"- Call duration p50 {histogram.get(50, 0) * 1000:.1f} ms, p90 {histogram.get(90, 0) * 1000:.1f} ms, p99 {histogram.get(99, 0) * 1000:.1f} ms (the sleep alone is 50 ms)")
print(f"- Fake endpoint CPU: max {max(endpoint_cpu or [0]):.0f} millicores of 2,000, throttled max {max(endpoint_throttled or [0]):.0f} ms/s")
overhead = histogram.get(99, 0) * 1000 - 50
verdict("Fake endpoint has headroom", fortio["ActualQPS"] >= 1900 and overhead < 10,
        f"{fortio['ActualQPS']:.0f} calls/s, p99 +{overhead:.1f} ms over the 50 ms sleep")

print("\n## Reference run (50 jobs/s for 60 s, 1 executor × 10,000 workers, +1 ms)\n")
stats = json.loads((run / "fake-endpoint-stats.json").read_text())
jobs = rows("jobs.csv")
succeeded = sum(row["status"] == "succeeded" for row in jobs)
print("| Priority | Jobs | p50 | p90 | p99 | max |\n|---|---|---|---|---|---|")
for priority, lag in sorted(stats["start_lag_by_priority"].items()):
    print(f"| {priority} | {lag['jobs']} | {lag['p50_s']:.3f} s | {lag['p90_s']:.3f} s | {lag['p99_s']:.3f} s | {lag['max_s']:.3f} s |")
overall = stats["start_lag_all"]
print(f"| all | {overall['jobs']} | {overall['p50_s']:.3f} s | {overall['p90_s']:.3f} s | {overall['p99_s']:.3f} s | {overall['max_s']:.3f} s |")
print(f"\n- {succeeded} of {len(jobs)} jobs succeeded, {stats['duplicate_successes']} duplicate successes")

# Why are some jobs late? Join each call with its job row: how early it was claimed, and how many were claimed with it.
claimed = {row["key"]: row for row in jobs}
batch_sizes = defaultdict(int)
for row in jobs:
    batch_sizes[row["claimed_s"]] += 1
late, on_time = [], []
for event in rows("events.csv"):
    job = claimed.get(event["key"])
    if not job or not event["lag_s"] or event["attempt"] != "1":
        continue
    record = (float(event["lag_s"]), float(job["scheduled_s"]) - float(job["claimed_s"]), batch_sizes[job["claimed_s"]],
              int(event["arrived_ms"]) % 1000)
    (late if record[0] > 0.5 else on_time).append(record)
if late:
    print(f"- **{len(late)} jobs ({len(late) / (len(late) + len(on_time)) * 100:.0f}%) started more than 0.5 s late**:"
          f" lag {min(r[0] for r in late):.2f} to {max(r[0] for r in late):.2f} s,"
          f" claimed {min(r[1] for r in late):.1f} to {max(r[1] for r in late):.1f} s before they were due,"
          f" in claims of {min(r[2] for r in late)} to {max(r[2] for r in late)} jobs")
    print(f"- On time: claimed {min(r[1] for r in on_time):.1f} to {max(r[1] for r in on_time):.1f} s before they were due,"
          f" in claims of {min(r[2] for r in on_time)} to {max(r[2] for r in on_time)} jobs")
executor = "executor-service"
print(f"- Executor CPU max {max(series('reference', executor, 'cpu_mcores') or [0]):.0f} millicores of 500,"
      f" throttled max {max(series('reference', executor, 'throttled_ms_per_s') or [0]):.0f} ms/s,"
      f" memory max {max(series('reference', executor, 'mem_mib') or [0]):.0f} MiB")
print(f"- MySQL commits max {max(series('reference', 'mysql', 'commits_per_s') or [0]):.0f}/s,"
      f" CPU max {max(series('reference', 'mysql', 'cpu_mcores') or [0]):.0f} millicores")
verdict("Reference run", succeeded == len(jobs) and stats["duplicate_successes"] == 0,
        f"{succeeded}/{len(jobs)} succeeded, p99 {overall['p99_s']:.2f} s")

print("\n## Verdict\n")
print("| Check | Result | Detail |\n|---|---|---|")
print("\n".join(verdicts))
