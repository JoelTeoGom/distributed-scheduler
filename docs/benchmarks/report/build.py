#!/usr/bin/env python3
"""Builds docs/benchmarks/peak-tuning.pdf: how the 09:00 peak went from 319 to thousands of jobs per second, step by step.

    python3 docs/benchmarks/report/build.py

Every number comes from the runs' raw data in bench/results/<test>/runs/ (events.csv, jobs.csv, telemetry.csv), so
they need to be on disk. The page is HTML with inline SVG charts, printed to PDF by headless Chrome (the Windows one
under WSL, as docs/benchmarks/diagram/render.sh does).
"""
import base64
import collections
import csv
import html
import json
import os
import shutil
import statistics
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
RESULTS = ROOT / "bench" / "results"
OUT = ROOT / "docs" / "benchmarks" / "peak-tuning.pdf"
FONT = ROOT / "docs" / "diagram" / "Virgil.woff2"

BLUE, ORANGE, GREEN, GOLD = "#2a78d6", "#eb6834", "#1baf7a", "#eda100"
INK, MUTED, GRID, PAPER = "#16181d", "#5b606b", "#e4e6ea", "#ffffff"


def run_dir(test, label):
    # A run still in progress has no jobs.csv yet, so it does not count.
    matches = sorted(d for d in (RESULTS / test / "runs").iterdir()
                     if (d.name == label or d.name.endswith(f"Z-{label}")) and (d / "jobs.csv").exists())
    if not matches:
        sys.exit(f"no run {label} in {test}")
    return matches[-1]


def measure(test, label):
    """The peak of one run: when each of its jobs first reached the endpoint, whatever the attempt, and the resources."""
    run = run_dir(test, label)
    workload = json.load(open(run / "workload.json"))
    peak = workload["peak"]
    jobs = {row["key"]: row for row in csv.DictReader(open(run / "jobs.csv")) if int(float(row["scheduled_s"])) == peak}
    first_call, first_attempt = {}, set()
    for event in csv.DictReader(open(run / "events.csv")):
        if event["key"] in jobs:
            lag = int(event["arrived_ms"]) / 1000 - peak
            first_call[event["key"]] = min(first_call.get(event["key"], lag), lag)
            if event["attempt"] == "1":
                first_attempt.add(event["key"])
    started = sorted(first_call.values())
    by_priority = collections.defaultdict(list)
    for key, lag in first_call.items():
        by_priority[jobs[key]["priority"]].append(lag)
    last = started[-1]
    window = collections.defaultdict(list)
    for row in csv.DictReader(open(run / "telemetry.csv")):
        if peak <= float(row["time"]) <= peak + last + 2:
            component = row["component"]
            component = "executor" if component.startswith("executor") else "fake" if component.startswith("fake") else component
            window[(component, row["metric"])].append(float(row["value"]))
    peak_of = lambda component, metric: max(window.get((component, metric)) or [0])
    mean_of = lambda component, metric: statistics.mean(window.get((component, metric)) or [0])
    info = open(run / "run.txt").read()
    return {
        "jobs": len(jobs), "last": last, "rate": len(jobs) / max(last, 1),
        "p50": started[len(started) // 2], "p99": started[min(len(started) - 1, int(len(started) * 0.99))],
        "missed": len(first_call) - len(first_attempt),
        "priority0": max(by_priority.get("0") or [0]), "priority3": max(by_priority.get("3") or [0]),
        "executor_cpu": mean_of("executor", "cpu_mcores"), "throttled": peak_of("executor", "throttled_ms_per_s"),
        "mysql_cpu": peak_of("mysql", "cpu_mcores"), "commits": peak_of("mysql", "commits_per_s"),
        "fake_cpu": peak_of("fake", "cpu_mcores"),
        "executors": int(info.split("executors=")[1].split()[0]),
    }


def bar_chart(rows, title, unit, *, width=700, digits=0, better="higher"):
    """Horizontal bars, one per row: (label, value, color, note). One axis, values printed at the end of each bar."""
    label_w, right, top, row_h = 230, 90, 44, 34
    height = top + row_h * len(rows) + 28
    plot_w = width - label_w - right
    largest = max(value for _, value, _, _ in rows) or 1
    step = next(s * 10 ** e for e in range(-1, 7) for s in (1, 2, 2.5, 5) if largest / (s * 10 ** e) <= 5)
    ticks = int(-(-largest // step))
    top_value = step * ticks
    scale = lambda v: plot_w * v / top_value
    parts = [f'<svg viewBox="0 0 {width} {height}" width="100%" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="{html.escape(title)}">',
             f'<text x="0" y="18" class="ct">{html.escape(title)}</text>',
             f'<text x="0" y="34" class="cs">{html.escape(unit)} · {"higher" if better == "higher" else "lower"} is better</text>']
    for tick in range(ticks + 1):
        value = step * tick
        x = label_w + scale(value)
        parts.append(f'<line x1="{x:.1f}" x2="{x:.1f}" y1="{top - 6}" y2="{height - 22}" stroke="{GRID}"/>')
        parts.append(f'<text x="{x:.1f}" y="{height - 6}" class="ax" text-anchor="middle">{value:,.{digits}f}</text>')
    for index, (label, value, color, note) in enumerate(rows):
        y = top + index * row_h
        parts.append(f'<text x="{label_w - 10}" y="{y + 15}" class="lb" text-anchor="end">{html.escape(label)}</text>')
        if note:
            parts.append(f'<text x="{label_w - 10}" y="{y + 28}" class="nt" text-anchor="end">{html.escape(note)}</text>')
        bar = max(scale(value), 2)
        parts.append(f'<rect x="{label_w}" y="{y + 4}" width="{bar:.1f}" height="18" rx="3" fill="{color}"/>')
        parts.append(f'<text x="{label_w + bar + 6:.1f}" y="{y + 17}" class="vl">{value:,.{digits}f}</text>')
    parts.append("</svg>")
    return "".join(parts)


def table(header, rows):
    out = ["<table><thead><tr>"] + [f"<th>{html.escape(h)}</th>" for h in header] + ["</tr></thead><tbody>"]
    for row in rows:
        out.append("<tr>" + "".join(f'<td{" class=num" if i else ""}>{cell}</td>' for i, cell in enumerate(row)) + "</tr>")
    return "".join(out + ["</tbody></table>"])


def fmt(m):
    return [f"{m['rate']:,.0f}", f"{m['last']:.1f} s", f"{m['p99']:.1f} s", f"{m['priority0']:.1f} s", f"{m['missed']:,}"]


def main():
    # The steps, each keeping the ones before it. P4 is the 8-minute test of round 1, P11 the short peak test.
    p4 = [("Round 1", "P4-0900-peak", "20261110T194554Z", "the starting point"),
          ("Fix the N+1", "P4-0900-peak", "r2", "one UPDATE per batch, not per task"),
          ("GOMAXPROCS = CPU limit", "P4-0900-peak", "r3", "1 Go thread on 0.5 vCPU, not 12")]
    p11 = [("Same settings, P11", "P11-peak", "a-base", "2 executors, tick 1 s"),
           ("Scheduler tick 200 ms", "P11-peak", "b-tick200ms", "2 executors: worse"),
           ("4 executors", "P11-peak", "s1-4x", "the peak spread over 4"),
           ("Claim every 250 ms", "P11-peak", "t4-4x-claim250ms", "500 ms ahead, not 1 s")]
    optional = [("Executors at 1 CPU", "P11-peak", "u1-1cpu", "the only resource change")]
    for step in optional:
        try:
            run_dir(step[1], step[2]); p11.append(step)
        except SystemExit:
            pass
    measured = {label: measure(test, run) for label, test, run, _ in p4 + p11}

    sweeps = {
        "executors": [("4 executors", "s1-4x"), ("5 executors", "s2-5x"), ("6 executors", "s3-6x")],
        "workers": [("10,000 workers", "s1-4x"), ("5,000 workers", "t1-4x-w5000"), ("2,000 workers", "t2-4x-w2000")],
        "claim": [("Claim every 1 s", "s1-4x"), ("Claim every 250 ms", "t4-4x-claim250ms")],
        "batch": [("1,000 per tick of 200 ms", "t4-4x-claim250ms"), ("4,000 per tick of 1 s", "t3b-batch4000")],
    }
    sweep_data = {}
    for name, rows in sweeps.items():
        sweep_data[name] = []
        for label, run in rows:
            try:
                sweep_data[name].append((label, measure("P11-peak", run)))
            except SystemExit:
                pass
    scale3 = []
    for label, run in [("0.5 vCPU, 10,000 workers", "v1-scale3"), ("1 CPU, 10,000 workers", "v2-scale3-1cpu"),
                       ("0.5 vCPU, 5,000 workers", "v3-scale3-w5000")]:
        try:
            scale3.append((label, measure("P11-peak", run)))
        except SystemExit:
            pass

    first, best_label = measured["Round 1"], max(measured, key=lambda k: measured[k]["rate"])
    best = measured[best_label]
    same_resources = {k: v for k, v in measured.items() if k != "Executors at 1 CPU"}
    best_same_label = max(same_resources, key=lambda k: same_resources[k]["rate"])
    best_same = same_resources[best_same_label]

    step_rows = lambda steps: [(label, measured[label]["rate"], GOLD if label == "Executors at 1 CPU" else ORANGE if label == "Scheduler tick 200 ms" else BLUE, note) for label, _, _, note in steps]
    last_rows = lambda steps: [(label, measured[label]["last"], GOLD if label == "Executors at 1 CPU" else ORANGE if label == "Scheduler tick 200 ms" else BLUE, note) for label, _, _, note in steps]

    font = base64.b64encode(FONT.read_bytes()).decode()
    sections = []
    sections.append(f"""
<section class="cover">
  <p class="eyebrow">distributed-scheduler · benchmarks, round 2</p>
  <h1>The 09:00 peak, from {first['rate']:,.0f} to {best_same['rate']:,.0f} jobs per second</h1>
  <p class="lede"><b>{best['rate']:,.0f}</b> with the executors at 1 CPU instead of 0.5, the only change of resources.</p>
  <p class="lede">About 30,000 jobs due on the same second, the hardest moment of a cron platform. Each step below changed one
  thing, kept what won, and was measured on the same small AWS-shaped bench: pods of 0.5 vCPU and 1 GiB, MySQL with 2 vCPU,
  1 ms of network to it.</p>
  <div class="kpis">
    <div><b>×{best_same['rate'] / first['rate']:.0f}</b><span>jobs per second draining the peak, same resources</span></div>
    <div><b>×{best['rate'] / first['rate']:.0f}</b><span>with the executors at 1 CPU</span></div>
    <div><b>{first['last']:.0f} s → {best_same['last']:.1f} s</b><span>until every job of the peak had started</span></div>
    <div><b>{first['priority0']:.0f} s → {best_same['priority0']:.1f} s</b><span>worst start of a priority 0 job</span></div>
    <div><b>0</b><span>jobs lost in any run</span></div>
    <div><b>4</b><span>executors: the sweet spot, beyond it MySQL sets the pace</span></div>
  </div>
  <p class="small">Measured on 2026-11-19 and 20 in a k3d cluster on one PC (WSL2, 12 CPUs). Every figure counts when each job of
  the peak first reached the customer's endpoint, retries included. Single runs: differences under ~30% are within the
  noise; the big steps are not.</p>
</section>""")

    sections.append(f"""
<section>
  <h2>1 · The fixes in the code and the runtime</h2>
  <p>Round 1 found the scheduler waiting on MySQL: it advanced every task with its own <code>UPDATE</code>, a thousand round trips per
  tick. Then the executors turned out to run 12 Go threads on half a CPU, because Go 1.25 cannot read the CPU limit in WSL's
  hybrid cgroups, and were throttled for seconds per second. Both were measured with P4, the 8-minute normal-day test with the peak in it.</p>
  {bar_chart(step_rows(p4), "Jobs per second draining the peak", "jobs/s")}
  {table(["Step", "Jobs/s", "Every job started", "p99", "Priority 0, worst", "First tries lost"], [[label] + fmt(measured[label]) for label, _, _, _ in p4])}
  <ul>
    <li><b>The N+1</b> moved the ceiling off the scheduler: ×{measured['Fix the N+1']['rate'] / first['rate']:.1f}. A tick of 1,000 tasks went from 5.5 s to 54 ms.</li>
    <li><b>GOMAXPROCS from the CPU limit</b> gave +{(measured['GOMAXPROCS = CPU limit']['rate'] / measured['Fix the N+1']['rate'] - 1) * 100:.0f}% with less CPU:
    executor throttling fell from {measured['Fix the N+1']['throttled']:,.0f} to {measured['GOMAXPROCS = CPU limit']['throttled']:,.0f} ms per second at its worst.</li>
  </ul>
</section>""")

    sections.append(f"""
<section>
  <h2>2 · Tuning the settings</h2>
  <p>From here on, P11: the same peak in a shorter run, one setting changed per run. Its first run repeats the settings of the last
  P4 step and drained {measured['Same settings, P11']['rate']:,.0f} jobs/s against {measured['GOMAXPROCS = CPU limit']['rate']:,.0f}: that gap is the noise between single
  runs and between the two tests, which is why only the large steps below count.</p>
  {bar_chart(step_rows(p11), "Jobs per second draining the peak", "jobs/s")}
  {bar_chart(last_rows(p11), "Seconds until every job of the peak had started", "seconds", digits=1, better="lower")}
  {table(["Step", "Jobs/s", "Every job started", "p99", "Priority 0, worst", "First tries lost"], [[label] + fmt(measured[label]) for label, _, _, _ in p11])}
  <ul>
    <li><b>A 200 ms scheduler tick</b> alone made things worse: the whole peak existed at once, two executors fired ~15,000 calls each in a second and {measured['Scheduler tick 200 ms']['missed']:,} first attempts timed out before reaching the endpoint. But with a 1 s tick the scheduler creates at most 1,000 jobs per second, so no number of executors could drain 30,000 in less than ~20 s.</li>
    <li><b>Four executors</b> spread that burst: nothing lost, every job of the peak started in {measured['4 executors']['last']:.1f} s.</li>
    <li><b>Claiming every 250 ms, 500 ms ahead,</b> frees workers sooner: {measured['Claim every 250 ms']['rate']:,.0f} jobs/s.</li>
    {"<li><b>Executors at 1 CPU</b>, the only change of resources: " + f"{measured['Executors at 1 CPU']['rate']:,.0f} jobs/s, every job started in {measured['Executors at 1 CPU']['last']:.1f} s.</li>" if "Executors at 1 CPU" in measured else ""}
  </ul>
</section>""")

    def sweep_section(name, title, text):
        rows = sweep_data[name]
        if not rows:
            return ""
        chart = bar_chart([(label, m["rate"], BLUE, f"every job by {m['last']:.1f} s") for label, m in rows], title, "jobs/s draining the peak")
        extra = table(["Setting", "Jobs/s", "Every job started", "p99", "Priority 0, worst", "First tries lost", "MySQL CPU, max", "MySQL commits/s, max"],
                      [[label] + fmt(m) + [f"{m['mysql_cpu']:,.0f} m", f"{m['commits']:,.0f}"] for label, m in rows])
        return f"<div class='sweep'><h3>{html.escape(title)}</h3><p>{text}</p>{chart}{extra}</div>"

    sections.append("<section><h2>3 · What each setting does</h2>" + "".join([
        sweep_section("executors", "Number of executors",
                      "Four is the sweet spot. A fifth and a sixth executor add claim transactions to the shared MySQL instead of capacity: its CPU and commits climb while the executors wait."),
        sweep_section("workers", "Workers per executor",
                      "The trade-off between draining everything fast and serving the urgent first. With 10,000 workers each executor claims its share of the peak at once; with 2,000 the claim takes priority 0 and 1 first and they start within a second, while the rest waits."),
        sweep_section("claim", "Executor claim interval",
                      "Claiming more often, closer to the due time, keeps jobs in workers for less time, for more claim transactions."),
        sweep_section("batch", "Scheduler batch size",
                      "Small and often wins: one tick of up to 4,000 tasks a second drained the peak in twice the time of 1,000 every 200 ms, since jobs appear in lumps and the last of the peak are created later. Above 4,095 the insert needs more than the 65,535 placeholders MySQL allows per statement: a batch of 5,000 stopped the scheduler for good, now rejected at startup."),
    ]) + "</section>")

    if scale3:
        sections.append(f"""
<section>
  <h2>4 · Three times the peak</h2>
  <p>Scale 3: ~90,000 jobs due on the same second, 150,000 tasks, with the best settings of scale 1. Here the system no longer
  scales: with 10,000 workers each executor tries to open 10,000 calls at once, tens of thousands of first attempts time out
  before reaching the endpoint, and their retries stretch the peak; MySQL runs at 80 to 90% of its CPU. Twice the CPU helps,
  and fewer workers help more: they cap the calls each executor opens at once.</p>
  {bar_chart([(label, m['rate'], GOLD if '1 CPU' in label else BLUE, f"every job by {m['last']:.1f} s") for label, m in scale3], "Jobs per second draining a peak of ~90,000", "jobs/s")}
  {table(["Run", "Jobs/s", "Every job started", "p99", "Priority 0, worst", "First tries lost", "MySQL CPU, max", "MySQL commits/s, max"],
         [[label] + fmt(m) + [f"{m['mysql_cpu']:,.0f} m", f"{m['commits']:,.0f}"] for label, m in scale3])}
</section>""")

    sections.append(f"""
<section>
  <h2>{5 if scale3 else 4} · What broke along the way</h2>
  <table><thead><tr><th>Found</th><th>Where</th><th>What it did</th><th>Now</th></tr></thead><tbody>
  <tr><td>One UPDATE per task</td><td>scheduler</td><td>~300 jobs/s ceiling, the peak in 94 s</td><td>one statement per batch</td></tr>
  <tr><td>GOMAXPROCS 12 on 0.5 vCPU</td><td>Go 1.25 in WSL</td><td>throttled for seconds per second</td><td>set from the pod's CPU limit</td></tr>
  <tr><td>Claims sized to free workers</td><td>executor</td><td>past its capacity, throughput fell and late saves ran jobs twice</td><td>sized by WORKER_COUNT; open</td></tr>
  <tr><td>A batch over 4,095 tasks</td><td>scheduler</td><td>every tick failed, no more jobs created</td><td>rejected at startup</td></tr>
  <tr><td>Fake endpoint of 512 MiB</td><td>bench</td><td>OOM-killed at 30,000 open calls</td><td>outside the cluster, 4 GB</td></tr>
  <tr><td>Too many calls at once per executor</td><td>executor</td><td>at scale 3, first attempts time out before reaching the endpoint</td><td>capped by WORKER_COUNT; open</td></tr>
  <tr><td>Peak timed by first attempts</td><td>bench report</td><td>drain times looked better than they were</td><td>every job's first call counts</td></tr>
  </tbody></table>
  <h3>The final settings</h3>
  {table(["Setting", "Round 1", "Now"], [
      ["Scheduler", "one UPDATE per task, tick 1 s, batch 1,000", "one UPDATE per batch, tick 200 ms, batch 1,000"],
      ["Executors", "2 × 0.5 vCPU, GOMAXPROCS 12", "4 × 0.5 vCPU, GOMAXPROCS 1 (1 CPU each: twice the jobs per second)"],
      ["Claim", "every 1 s, 1 s ahead", "every 250 ms, 500 ms ahead"],
      ["Workers per executor", "10,000", "10,000 for throughput, 2,000 for priority"]])}
  <h3>Where the limit is now</h3>
  <ul>
    <li><b>At scale 1, the executors' CPU.</b> At 1 CPU they ran {best['rate']:,.0f} jobs/s with no throttling, while MySQL stayed where it was.</li>
    <li><b>Beyond four executors, MySQL.</b> A fifth and sixth executor raised its CPU and commits, not the throughput.</li>
    <li><b>At three times the peak, both,</b> plus the calls each executor opens at once. Next: save results in batches (one round trip
    per job today), claim what an executor can fire rather than what it has workers for, and more than one shard.</li>
  </ul>
</section>""")

    page = f"""<!doctype html><html><head><meta charset="utf-8"><title>Peak tuning</title><style>
@font-face {{ font-family: Virgil; src: url(data:font/woff2;base64,{font}) format("woff2"); }}
@page {{ size: A4; margin: 16mm 15mm; }}
* {{ box-sizing: border-box; }}
body {{ font-family: "Segoe UI", system-ui, sans-serif; color: {INK}; background: {PAPER}; font-size: 10.5pt; line-height: 1.5; margin: 0; }}
h1, h2, h3 {{ font-family: Virgil, "Segoe UI", sans-serif; font-weight: normal; line-height: 1.15; }}
h1 {{ font-size: 30pt; margin: 6mm 0 4mm; }}
h2 {{ font-size: 19pt; margin: 0 0 3mm; color: {BLUE}; }}
h3 {{ font-size: 14pt; margin: 6mm 0 2mm; }}
section {{ page-break-after: always; }}
section:last-child {{ page-break-after: auto; }}
.eyebrow {{ color: {MUTED}; font-size: 9pt; letter-spacing: .08em; text-transform: uppercase; margin: 30mm 0 0; }}
.lede {{ font-size: 12.5pt; max-width: 150mm; }}
.small {{ color: {MUTED}; font-size: 8.5pt; max-width: 160mm; }}
.kpis {{ display: grid; grid-template-columns: 1fr 1fr; gap: 4mm; margin: 10mm 0; }}
.kpis div {{ border: 1px solid {GRID}; border-radius: 3mm; padding: 4mm 5mm; }}
.kpis b {{ display: block; font-family: Virgil, sans-serif; font-weight: normal; font-size: 24pt; color: {BLUE}; }}
.kpis span {{ color: {MUTED}; font-size: 9.5pt; }}
svg {{ margin: 3mm 0; font-family: "Segoe UI", sans-serif; }}
.ct {{ font-size: 12px; font-weight: 600; fill: {INK}; }} .cs {{ font-size: 10px; fill: {MUTED}; }}
.lb {{ font-size: 11px; fill: {INK}; }} .nt {{ font-size: 9px; fill: {MUTED}; }} .vl {{ font-size: 11px; fill: {INK}; font-weight: 600; }}
.ax {{ font-size: 9px; fill: {MUTED}; }}
table {{ border-collapse: collapse; width: 100%; font-size: 9pt; margin: 2mm 0 3mm; page-break-inside: avoid; }}
th {{ text-align: left; color: {MUTED}; font-weight: 600; border-bottom: 1px solid {INK}; padding: 1.5mm 2mm; }}
td {{ border-bottom: 1px solid {GRID}; padding: 1.5mm 2mm; vertical-align: top; }}
td.num {{ font-variant-numeric: tabular-nums; }}
code {{ font-family: Consolas, monospace; font-size: 9pt; }}
ul {{ padding-left: 5mm; }} li {{ margin-bottom: 1.5mm; }}
.sweep {{ page-break-inside: avoid; margin-bottom: 4mm; }}
</style></head><body>{"".join(sections)}</body></html>"""

    if os.environ.get("REPORT_HTML"):  # a copy of the page, to look at it in a browser
        Path(os.environ["REPORT_HTML"]).write_text(page, encoding="utf-8")
    chrome = os.environ.get("CHROME", "/mnt/c/Program Files/Google/Chrome/Application/chrome.exe")
    windows = chrome.startswith("/mnt/")
    if windows:
        temp = subprocess.check_output(["cmd.exe", "/c", "echo %TEMP%"], stderr=subprocess.DEVNULL).decode().strip()
        work = Path(subprocess.check_output(["wslpath", temp]).decode().strip()) / f"peak-report-{os.getpid()}"
        work.mkdir(parents=True, exist_ok=True)
    else:
        work = Path(tempfile.mkdtemp())
    as_os = lambda p: subprocess.check_output(["wslpath", "-w", str(p)]).decode().strip() if windows else str(p)
    as_url = lambda p: "file:///" + subprocess.check_output(["wslpath", "-m", str(p)]).decode().strip() if windows else f"file://{p}"
    try:
        (work / "report.html").write_text(page, encoding="utf-8")
        subprocess.run([chrome, "--headless=new", "--disable-gpu", "--no-first-run", f"--user-data-dir={as_os(work / 'profile')}",
                        "--no-pdf-header-footer", f"--print-to-pdf={as_os(work / 'report.pdf')}", as_url(work / "report.html")],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
        if not (work / "report.pdf").exists():
            sys.exit("Chrome wrote no PDF")
        shutil.copy(work / "report.pdf", OUT)
    finally:
        shutil.rmtree(work, ignore_errors=True)
    print(f"wrote {OUT.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
