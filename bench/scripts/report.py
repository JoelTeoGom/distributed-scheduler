#!/usr/bin/env python3
"""Writes the report of one test run: Markdown with tables and SVG charts, next to the test's README.

    python3 bench/scripts/report.py bench/results/P3-normal-day/runs/<time>

Reads the run's telemetry.csv, events.csv (every call the fake endpoint received), jobs.csv (every job row),
phases.csv, workload.json, run.txt and the pprof profiles. Writes bench/results/<test>/reports/<time>/report.md.
The charts are static images: the exact numbers are in the tables under them.
"""
import csv
import json
import math
import re
import statistics
import subprocess
import sys
from collections import defaultdict
from pathlib import Path

RUN = Path(sys.argv[1]).resolve()
TEST_DIR = RUN.parent.parent
OUT = TEST_DIR / "reports" / RUN.name
CHARTS = OUT / "charts"

# Categorical slots in fixed order (validated: adjacent CVD ΔE ≥ 9 on white); text never takes a series colour.
SERIES = ["#2a78d6", "#eb6834", "#1baf7a", "#eda100", "#e87ba4", "#008300"]
INK, MUTED, GRID, SURFACE = "#0b0b0b", "#52514e", "#e6e5e1", "#ffffff"
FONT = "-apple-system, 'Segoe UI', Helvetica, Arial, sans-serif"


def rows(name):
    path = RUN / name
    if not path.exists():
        return []
    with open(path, newline="") as file:
        return list(csv.DictReader(file))


def percentile(values, p):
    ordered = sorted(values)
    if not ordered:
        return float("nan")
    return ordered[max(0, math.ceil(len(ordered) * p / 100) - 1)]


def app(component):
    return re.sub(r"-[0-9a-f]{8,10}-[0-9a-z]{5}$", "", component)


def short_pod(component):
    match = re.search(r"-([0-9a-z]{5})$", component)
    return f"{app(component)} {match.group(1)}" if match and app(component) != component else component


def nice_max(value):
    if value <= 0:
        return 1
    exponent = 10 ** math.floor(math.log10(value))
    for step in (1, 2, 2.5, 5, 10):
        if value <= step * exponent:
            return step * exponent
    return 10 * exponent


def clock(seconds):
    sign = "-" if seconds < 0 else ""
    seconds = abs(int(round(seconds)))
    return f"{sign}{seconds // 60}:{seconds % 60:02d}"


def chart(name, title, unit, series, markers, x_range, y_max=None, reference=None, gap=None):
    """A line chart as SVG. series: [(label, [(x, y), ...])]; markers: [(x, label)]; reference: (y, label).
    gap: a jump in x larger than this breaks the line, so missing windows do not look like data."""
    width, height = 920, 300
    left, right, top, bottom = 64, 24, 54, 40
    plot_w, plot_h = width - left - right, height - top - bottom
    x0, x1 = x_range
    peak = max([y for _, points in series for _, y in points] + [reference[0] if reference else 0] + [0])
    y1 = y_max or nice_max(peak * 1.08)
    sx = lambda x: left + (x - x0) / max(x1 - x0, 1) * plot_w
    sy = lambda y: top + plot_h - min(y, y1) / y1 * plot_h
    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="0 0 {width} {height}" font-family="{FONT}">',
             f'<rect width="{width}" height="{height}" fill="{SURFACE}"/>',
             f'<text x="{left}" y="22" font-size="15" font-weight="600" fill="{INK}">{title}</text>',
             f'<text x="{left}" y="40" font-size="12" fill="{MUTED}">{unit}</text>']
    for i in range(5):  # recessive grid and y labels
        value = y1 * i / 4
        y = sy(value)
        parts.append(f'<line x1="{left}" x2="{left + plot_w}" y1="{y:.1f}" y2="{y:.1f}" stroke="{GRID}" stroke-width="1"/>')
        label = f"{value:g}" if value < 10 else f"{value:,.0f}"
        parts.append(f'<text x="{left - 8}" y="{y + 4:.1f}" font-size="11" fill="{MUTED}" text-anchor="end">{label}</text>')
    step = nice_max((x1 - x0) / 8)
    tick = math.ceil(x0 / step) * step
    while tick <= x1:
        parts.append(f'<text x="{sx(tick):.1f}" y="{top + plot_h + 18}" font-size="11" fill="{MUTED}" text-anchor="middle">{clock(tick)}</text>')
        tick += step
    for x, label in markers:
        if x0 <= x <= x1:
            parts.append(f'<line x1="{sx(x):.1f}" x2="{sx(x):.1f}" y1="{top}" y2="{top + plot_h}" stroke="{MUTED}" stroke-width="1" stroke-dasharray="4 4"/>')
            parts.append(f'<text x="{sx(x) + 4:.1f}" y="{top + 11}" font-size="11" fill="{MUTED}">{label}</text>')
    if reference:
        y = sy(reference[0])
        parts.append(f'<line x1="{left}" x2="{left + plot_w}" y1="{y:.1f}" y2="{y:.1f}" stroke="{INK}" stroke-width="1" stroke-dasharray="2 3"/>')
        parts.append(f'<text x="{left + plot_w}" y="{y - 5:.1f}" font-size="11" fill="{INK}" text-anchor="end">{reference[1]}</text>')
    for index, (label, points) in enumerate(series):
        color = SERIES[index % len(SERIES)]
        points = sorted(p for p in points if x0 <= p[0] <= x1)
        if points:
            path = " ".join(f"{'M' if i == 0 or (gap and x - points[i - 1][0] > gap) else 'L'}{sx(x):.1f},{sy(y):.1f}"
                            for i, (x, y) in enumerate(points))
            parts.append(f'<path d="{path}" fill="none" stroke="{color}" stroke-width="2" stroke-linejoin="round"/>')
    if len(series) > 1:  # legend, right-aligned on the title row
        x = width - right
        for index, (label, _) in reversed(list(enumerate(series))):
            x -= 8 + len(label) * 6.6
            parts.append(f'<text x="{x + 22:.1f}" y="40" font-size="12" fill="{INK}">{label}</text>')
            parts.append(f'<line x1="{x:.1f}" x2="{x + 16:.1f}" y1="36" y2="36" stroke="{SERIES[index % len(SERIES)]}" stroke-width="3" stroke-linecap="round"/>')
            x -= 26
    parts.append(f'<line x1="{left}" x2="{left + plot_w}" y1="{top + plot_h}" y2="{top + plot_h}" stroke="{MUTED}" stroke-width="1"/>')
    parts.append("</svg>")
    (CHARTS / f"{name}.svg").write_text("\n".join(parts))
    return f"![{title}](charts/{name}.svg)"


def pprof_top(path):
    result = subprocess.run(["go", "tool", "pprof", "-top", "-nodecount=12", str(path)], capture_output=True, text=True, check=False)
    lines = result.stdout.splitlines()
    start = next((i for i, line in enumerate(lines) if line.strip().startswith("flat")), None)
    header = next((line for line in lines if line.startswith("Showing nodes") or "samples" in line.lower()), "")
    return header, lines[start:] if start is not None else []


def main():
    CHARTS.mkdir(parents=True, exist_ok=True)
    run_info = (RUN / "run.txt").read_text()
    test = re.search(r"test=(\S+)", run_info).group(1)
    label = re.search(r"label=(\S*)", run_info).group(1)
    workload = json.loads((RUN / "workload.json").read_text())
    load_start = workload["start"]
    load_end = load_start + workload["duration"]
    phases = rows("phases.csv")
    jobs = rows("jobs.csv")
    events = rows("events.csv")
    stats = json.loads((RUN / "fake-endpoint-stats.json").read_text())

    # Time axis: seconds from the start of the load.
    t = lambda epoch: float(epoch) - load_start
    drain_end = max([float(p["end"]) for p in phases if p["phase"] == "drain"] + [load_end])
    x_range = (-30, t(drain_end) + 10)
    markers = [(0, "load starts"), (t(load_end), "load ends")]
    if workload.get("peak"):
        markers.append((t(workload["peak"]), "09:00 peak"))
    for phase in phases:
        if phase["phase"] in ("kill-executor", "mysql-cut"):
            markers.append((t(phase["start"]), phase["phase"]))

    # --- the job numbers
    first = [e for e in events if e["attempt"] == "1" and e["lag_s"]]
    lag_by_priority = defaultdict(list)
    for e in first:
        lag_by_priority[int(e["priority"])].append(float(e["lag_s"]))
    all_lags = [float(e["lag_s"]) for e in first]
    status = defaultdict(int)
    for job in jobs:
        status[job["status"]] += 1
    retried = sum(int(job["attempt"]) > 1 for job in jobs)
    # The executor timed out but the endpoint finished the work: the customer processed that job more than once.
    abandoned = sum(e["status"] == "499" for e in events)
    arrivals = sorted(int(e["arrived_ms"]) / 1000 for e in first)
    span = (arrivals[-1] - arrivals[0]) if len(arrivals) > 1 else 1

    # --- charts
    sections = []
    per_second = defaultdict(lambda: [0, 0])
    for e in events:
        second = int(int(e["arrived_ms"]) / 1000 - load_start)
        per_second[second][0 if e["attempt"] == "1" else 1] += 1
    sections.append(chart("calls", "Calls reaching the customers' endpoints", "calls per second",
                          [("first attempts", [(s, v[0]) for s, v in per_second.items()]),
                           ("retries", [(s, v[1]) for s, v in per_second.items()])], markers, x_range))

    windows = defaultdict(lambda: defaultdict(list))  # start delay per priority, in 10 s windows of the due time
    for e in first:
        windows[int(e["priority"])][int(t(e["scheduled_s"]) // 10) * 10].append(float(e["lag_s"]))
    sections.append(chart("start-delay-p99", "Start delay p99, by priority", "seconds late, p99 of the jobs due in each 10 s",
                          [(f"priority {p}", [(w + 5, percentile(v, 99)) for w, v in sorted(windows[p].items())]) for p in sorted(windows)],
                          markers, x_range, gap=15))
    sections.append(chart("start-delay-p50", "Start delay p50, by priority", "seconds late, median of the jobs due in each 10 s",
                          [(f"priority {p}", [(w + 5, percentile(v, 50)) for w, v in sorted(windows[p].items())]) for p in sorted(windows)],
                          markers, x_range, gap=15))

    telemetry = defaultdict(list)  # (component, metric) -> [(t, value)]
    for row in rows("telemetry.csv"):
        telemetry[(row["component"], row["metric"])].append((t(row["time"]), float(row["value"])))
    pods = lambda name: sorted({c for c, _ in telemetry if app(c) == name})
    metric = lambda component, name: telemetry.get((component, name), [])

    sections.append(chart("queue", "Jobs in the queue", "jobs",
                          [("pending", metric("queue", "pending")), ("running", metric("queue", "running"))], markers, x_range))
    executors = pods("executor-service")
    sections.append(chart("executor-cpu", "Executor CPU", "millicores, per pod", [(short_pod(c), metric(c, "cpu_mcores")) for c in executors],
                          markers, x_range, reference=(500, "limit 500m")))
    sections.append(chart("executor-throttled", "Executor CPU throttling", "ms per second the kernel held the pod back at its limit, summed over the CPUs it ran on (so it can pass 1,000)",
                          [(short_pod(c), metric(c, "throttled_ms_per_s")) for c in executors], markers, x_range))
    sections.append(chart("memory", "Memory", "MiB in use, per pod",
                          [(short_pod(c), metric(c, "mem_mib")) for c in executors + pods("scheduler-service")], markers, x_range))
    others = [("scheduler", pods("scheduler-service")), ("fake endpoint", pods("fake-endpoint")), ("mysql", ["mysql"]), ("toxiproxy", ["toxiproxy"])]
    sections.append(chart("other-cpu", "Scheduler, fake endpoint and databases CPU", "millicores",
                          [(label, metric(components[0], "cpu_mcores")) for label, components in others if components], markers, x_range))
    sections.append(chart("mysql", "MySQL writes", "per second",
                          [("commits", metric("mysql", "commits_per_s")), ("redo fsyncs", metric("mysql", "redo_fsyncs_per_s"))], markers, x_range))
    sections.append(chart("disk", "Disk writes", "writes per second on Docker's disk", [("writes", metric("disk", "writes_per_s"))], markers, x_range))

    # --- resource table
    resource_rows = []
    for component in executors + pods("scheduler-service") + pods("fake-endpoint") + ["mysql", "cassandra", "toxiproxy"]:
        cpu = [v for _, v in metric(component, "cpu_mcores") if True]
        if not cpu:
            continue
        throttled = [v for _, v in metric(component, "throttled_ms_per_s")]
        memory = [v for _, v in metric(component, "mem_mib")]
        resource_rows.append(f"| {short_pod(component)} | {statistics.mean(cpu):.0f} | {max(cpu):.0f} | {max(throttled or [0]):.0f} | {max(memory or [0]):.0f} |")
    mysql_max = lambda name: max([v for _, v in metric("mysql", name)] or [0])

    # --- test-specific numbers
    extra = []
    if workload.get("peak"):
        peak_lags = [float(e["lag_s"]) for e in first if int(float(e["scheduled_s"])) == workload["peak"]]
        peak_p0 = [float(e["lag_s"]) for e in first if int(float(e["scheduled_s"])) == workload["peak"] and e["priority"] == "0"]
        if peak_lags:
            extra.append(f"- **09:00 peak:** {len(peak_lags):,} jobs due on the same second. The last one started **{max(peak_lags):.1f} s** late"
                         f" (p50 {percentile(peak_lags, 50):.1f} s, p99 {percentile(peak_lags, 99):.1f} s); priority 0 p99 **{percentile(peak_p0, 99):.1f} s**.")
    by_tenant = defaultdict(list)
    for e in first:
        by_tenant[e.get("tenant", "")].append(float(e["lag_s"]))
    hanging = [e for e in events if e.get("profile") == "hanging"]
    if hanging:
        others_lags = [float(e["lag_s"]) for e in first if e.get("profile") != "hanging"]
        extra.append(f"- **Noisy tenant:** {len(hanging):,} calls to its hanging endpoint. Every other tenant: p99 **{percentile(others_lags, 99):.3f} s**,"
                     f" p50 {percentile(others_lags, 50):.3f} s, over {len(others_lags):,} jobs.")
    if any(p["phase"] in ("kill-executor", "mysql-cut") for p in phases):
        fault = next(p for p in phases if p["phase"] in ("kill-executor", "mysql-cut"))
        late = [float(e["lag_s"]) for e in first if float(e["lag_s"]) > 5]
        extra.append(f"- **{fault['phase']}** at {clock(t(fault['start']))}: {len(late):,} jobs started more than 5 s late, the worst"
                     f" {max(late or [0]):.0f} s late. Duplicate successes: **{stats['duplicate_successes']}**.")
    profiles_by_kind = defaultdict(list)
    for path in sorted(RUN.glob("*-cpu-*.pprof")):
        profiles_by_kind[path.name.split("-cpu-")[0]].append(path)

    # --- markdown
    counts = workload.get("counts")
    workload_line = (f"the platform model at scale {workload['scale']}: " + ", ".join(f"{v:,} {k}" for k, v in counts.items()) + " tasks"
                     if workload["mode"] == "model" else
                     f"{workload['rate']} one-off jobs per second, endpoint `{workload['endpoint']}`, timeout {workload['timeout']} s")
    executor_count = re.search(r"executors=(\d+)", run_info).group(1)
    settings = re.findall(r"^(executor-service|scheduler-service): (.*)$", run_info, re.M)
    lines = [f"# {test} · {RUN.name}{f' ({label})' if label else ''}", "",
             f"- **Load:** {workload['duration']} s of {workload_line}; {workload['seeded']:,} tasks seeded.",
             f"- **Deployment:** {executor_count} executors; "
             + "; ".join(f"{name}: `{' '.join(v for v in env.split() if not v.startswith(('GOMEMLIMIT', 'ALLOW_')))}`" for name, env in settings),
             f"- **Raw data:** `bench/results/{TEST_DIR.name}/runs/{RUN.name}/` (local, not committed)", "",
             "## Jobs", "",
             "| Created | Succeeded | Dead | Still pending or running | Retried | Duplicate successes | Timed out, done anyway | Calls per second (first attempts) |",
             "|---|---|---|---|---|---|---|---|",
             f"| {len(jobs):,} | {status['succeeded']:,} | {status['dead']:,} | {status['pending'] + status['running']:,} | {retried:,} | {stats['duplicate_successes']} | {abandoned:,} | {len(first) / max(span, 1):.1f} |",
             "", "*Duplicate successes*: the endpoint answered 200 twice for one job, which only a failure should cause. *Timed out, done anyway*: the executor gave up after the timeout while the endpoint finished the work, so the retry made the customer process the job again.",
             "", "**Start delay** (when the call reached the endpoint minus when the job was due, first attempts):", "",
             "| Priority | Jobs | p50 | p90 | p99 | max |", "|---|---|---|---|---|---|"]
    for priority in sorted(lag_by_priority):
        values = lag_by_priority[priority]
        lines.append(f"| {priority} | {len(values):,} | {percentile(values, 50):.3f} s | {percentile(values, 90):.3f} s | {percentile(values, 99):.3f} s | {max(values):.3f} s |")
    if all_lags:
        lines.append(f"| all | {len(all_lags):,} | {percentile(all_lags, 50):.3f} s | {percentile(all_lags, 90):.3f} s | {percentile(all_lags, 99):.3f} s | {max(all_lags):.3f} s |")
    if extra:
        lines += [""] + extra
    lines += ["", "## Charts", "", "Time is minutes:seconds from the start of the load.", ""]
    for section in sections:
        lines += [section, ""]
    lines += ["## Resources", "",
              "| Component | CPU avg (millicores) | CPU max | Throttled max (ms/s) | Memory max (MiB) |", "|---|---|---|---|---|", *resource_rows, "",
              f"MySQL: up to {mysql_max('commits_per_s'):.0f} commits/s, {mysql_max('redo_fsyncs_per_s'):.0f} redo fsyncs/s,"
              f" {mysql_max('row_lock_waits_per_s'):.0f} row lock waits/s, {mysql_max('connections'):.0f} connections.", ""]
    for kind, paths in profiles_by_kind.items():
        for path in paths:
            header, top = pprof_top(path)
            if top:
                lines += [f"## CPU profile: {kind}, 20 s from {clock(t(path.stem.split('-')[-1]))}", "", "```text", *top, "```", ""]
    lines += ["## Conclusions", "", "*(to be written)*", ""]
    (OUT / "report.md").write_text("\n".join(lines))
    print(f"wrote {OUT.relative_to(Path.cwd()) if OUT.is_relative_to(Path.cwd()) else OUT}")
    print(f"  {len(jobs):,} jobs, {status['succeeded']:,} succeeded, {status['dead']:,} dead, p99 {percentile(all_lags, 99):.3f} s")


if __name__ == "__main__":
    main()
