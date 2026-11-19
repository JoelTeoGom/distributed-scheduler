#!/usr/bin/env python3
"""Writes the results of every test into its README and its summary into benchmarks.md, and draws P2's capacity chart.

    python3 bench/scripts/index.py

Reads bench/results/<test>/reports/<run>/report.md. In each test's README it rewrites the "## Results" section,
keeping everything else, the conclusions included. In benchmarks.md it rewrites what sits between
<!-- results:Pn --> and <!-- /results:Pn -->.
"""
import re
from pathlib import Path

RESULTS = Path(__file__).resolve().parent.parent / "results"
BENCHMARKS = RESULTS.parent.parent / "benchmarks.md"
SERIES = ["#2a78d6", "#eb6834", "#1baf7a"]
INK, MUTED, GRID = "#0b0b0b", "#52514e", "#e6e5e1"
FONT = "-apple-system, 'Segoe UI', Helvetica, Arial, sans-serif"
ROUND_NOTES = {"r3": "round 2 with GOMAXPROCS=1, squares"}  # what a round changed, for the chart's subtitle


def numbers(report):
    text = report.read_text()
    jobs = re.search(r"^\| ([\d,]+) \| ([\d,]+) \| ([\d,]+) \| ([\d,]+) \| ([\d,]+) \| (\d+) \| ([\d,]+) \| ([\d.]+) \|$", text, re.M)
    delay = re.search(r"^\| all \| [\d,]+ \| ([\d.]+) s \| ([\d.]+) s \| ([\d.]+) s \| ([\d.]+) s \|$", text, re.M)
    label = re.search(r"^# \S+ · \S+(?: \((.+)\))?$", text, re.M).group(1) or ""
    return {
        "label": label,
        "created": jobs.group(1), "succeeded": jobs.group(2), "dead": jobs.group(3), "unfinished": jobs.group(4),
        "duplicates": jobs.group(6), "rate": float(jobs.group(8)),
        "p50": float(delay.group(1)), "p90": float(delay.group(2)), "p99": float(delay.group(3)), "max": float(delay.group(4)),
    }


def sections(text):
    """The report's bullets before the first section, and its sections as (title, body)."""
    parts = re.split(r"^## (.+)$", text, flags=re.M)
    return parts[0], list(zip(parts[1::2], [body.strip() for body in parts[2::2]]))


def run_body(report, folded):
    """The report of one run as README Markdown, with its charts' paths made relative to the README."""
    run_path = report.parent.relative_to(report.parents[2]).as_posix()
    preamble, parts = sections(report.read_text())
    out = [line for line in preamble.splitlines() if line.startswith("- ")] + [""]
    heading = "####" if folded else "###"
    profiles = []
    for title, body in parts:
        body = body.replace("](charts/", f"]({run_path}/charts/")
        if title == "Charts":
            out += ["<details><summary>Charts</summary>", "", body, "", "</details>", ""] if folded else [f"{heading} Charts", "", body, ""]
        elif title == "Resources":
            out += [f"{heading} Resources", "", body, ""]
        elif title.startswith("CPU profile"):
            profiles += [f"<details><summary>{title}</summary>", "", body, "", "</details>", ""]
        elif title != "Conclusions":
            out += [f"{heading} {title}", "", body, ""]
    if profiles:
        out += [f"{heading} CPU profiles", ""] + profiles
    out.append(f"Run page: [{run_path}/report.md]({run_path}/report.md)")
    return "\n".join(out)


def runs_table(runs, link_prefix=""):
    lines = ["| Run | Jobs | Succeeded | Dead | Unfinished | Duplicates | Calls/s | Start delay p50 / p90 / p99 / max |",
             "|---|---|---|---|---|---|---|---|"]
    for run in runs:
        lines.append(f"| [{run['label'] or 'run'}]({link_prefix}{run['path']}) | {run['created']} | {run['succeeded']} | {run['dead']} | {run['unfinished']}"
                     f" | {run['duplicates']} | {run['rate']:.1f} | {run['p50']:.2f} / {run['p90']:.2f} / {run['p99']:.2f} / {run['max']:.2f} s |")
    return lines


def capacity_chart(path, runs):
    """Offered against sustained jobs per second: one colour per executor count, round 1 dashed."""
    points = {}
    for run in runs:
        round_name, executors, offered = re.match(r"(?:(r\d+)-)?(\d)x-(\d+)$", run["label"]).groups()
        points.setdefault((round_name or "r1", int(executors)), []).append((int(offered), run["rate"]))
    rounds = sorted({round_name for round_name, _ in points})
    top_value = max(max(x, y) for series in points.values() for x, y in series)
    step = next(s for s in (100, 200, 500, 1000, 2000) if top_value / s <= 6)
    top_value = -(-top_value // step) * step
    width, height, left, right, top, bottom = 720, 360, 64, 150, 54, 44
    plot_w, plot_h = width - left - right, height - top - bottom
    sx = lambda x: left + x / top_value * plot_w
    sy = lambda y: top + plot_h - y / top_value * plot_h
    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="0 0 {width} {height}" font-family="{FONT}">',
             f'<rect width="{width}" height="{height}" fill="#ffffff"/>',
             f'<text x="{left}" y="22" font-size="15" font-weight="600" fill="{INK}">Capacity: jobs per second offered and sustained</text>',
             f'<text x="{left}" y="40" font-size="12" fill="{MUTED}">sustained jobs/s (first attempts), against offered'
             f'{" · round 1 dashed" if len(rounds) > 1 else ""}'
             f'{"".join(f" · {r}: {ROUND_NOTES[r]}" for r in rounds if r in ROUND_NOTES)}</text>']
    for value in range(0, int(top_value) + 1, step):
        parts.append(f'<line x1="{left}" x2="{left + plot_w}" y1="{sy(value):.1f}" y2="{sy(value):.1f}" stroke="{GRID}"/>')
        parts.append(f'<text x="{left - 8}" y="{sy(value) + 4:.1f}" font-size="11" fill="{MUTED}" text-anchor="end">{value:,}</text>')
        parts.append(f'<text x="{sx(value):.1f}" y="{top + plot_h + 18}" font-size="11" fill="{MUTED}" text-anchor="middle">{value:,}</text>')
    parts.append(f'<text x="{left + plot_w / 2}" y="{height - 6}" font-size="12" fill="{MUTED}" text-anchor="middle">offered jobs/s</text>')
    parts.append(f'<line x1="{sx(0)}" y1="{sy(0)}" x2="{sx(top_value)}" y2="{sy(top_value)}" stroke="{MUTED}" stroke-dasharray="4 4"/>')
    labels = []
    for (round_name, executors), series in sorted(points.items()):
        color = SERIES[(executors - 1) % len(SERIES)]
        dashed = len(rounds) > 1 and round_name == rounds[0]
        series = sorted(series)
        if len(series) > 1:
            path_data = " ".join(f"{'M' if i == 0 else 'L'}{sx(x):.1f},{sy(y):.1f}" for i, (x, y) in enumerate(series))
            parts.append(f'<path d="{path_data}" fill="none" stroke="{color}" stroke-width="2"{' stroke-dasharray="6 4"' if dashed else ''}/>')
        for x, y in series:
            fill = "#ffffff" if dashed else color
            if round_name in ROUND_NOTES:
                parts.append(f'<rect x="{sx(x) - 5:.1f}" y="{sy(y) - 5:.1f}" width="10" height="10" fill="{fill}" stroke="{color}" stroke-width="2"/>')
            else:
                parts.append(f'<circle cx="{sx(x):.1f}" cy="{sy(y):.1f}" r="5" fill="{fill}" stroke="{color}" stroke-width="2"/>')
        x, y = series[-1]
        name = f"{executors} executor{'s' if executors > 1 else ''}{f' · {round_name}' if len(rounds) > 1 else ''}: {y:,.0f}"
        labels.append([sy(y) + 4, name])
    labels.sort()
    for i in range(1, len(labels)):  # labels sit in the right margin, nudged apart so they never overlap
        labels[i][0] = max(labels[i][0], labels[i - 1][0] + 14)
    for y, name in labels:
        parts.append(f'<text x="{left + plot_w + 10}" y="{y:.1f}" font-size="11" fill="{INK}">{name}</text>')
    parts.append("</svg>")
    path.write_text("\n".join(parts))


def events(report):
    """The report's lines about the test's own event: the peak, the fault, the noisy tenant."""
    return [line for line in report.read_text().splitlines()
            if line.startswith("- **") and not re.match(r"- \*\*(Load|Deployment|Raw data):", line)]


def main():
    benchmarks = BENCHMARKS.read_text()
    tests = [p for p in RESULTS.iterdir() if p.is_dir() and re.match(r"P\d+-", p.name)]
    for test in sorted(tests, key=lambda p: int(re.match(r"P(\d+)", p.name).group(1))):
        reports = sorted((test / "reports").glob("*/report.md")) if (test / "reports").exists() else []
        if not reports:
            continue
        runs = [{**numbers(report), "path": report.relative_to(test).as_posix(), "report": report} for report in reports]
        many = len(runs) > 1

        lines = ["## Results", ""]
        summary = runs_table(runs, f"bench/results/{test.name}/")
        if many:
            lines += runs_table(runs) + [""]
        if test.name.startswith(("P2-", "P10-")):
            capacity_chart(test / "capacity.svg", [run for run in runs if re.match(r"(r\d+-)?\dx-\d+$", run["label"])])
            lines += ["![Capacity](capacity.svg)", ""]
            summary += ["", f"![Capacity](bench/results/{test.name}/capacity.svg)"]
        for run in runs:
            if many:
                lines += [f"### {run['label']}", ""]
            lines += [run_body(run["report"], folded=many), ""]
            if len(runs) <= 3 and events(run["report"]):  # a long sweep keeps its peak lines in its own README
                summary += [""] + events(run["report"])
        section = "\n".join(lines).rstrip() + "\n"

        readme = test / "README.md"
        text = readme.read_text()
        day = reports[-1].parent.name
        text = re.sub(r"\*\*Status:\*\* not run yet\.", f"**Status:** run on {day[:4]}-{day[4:6]}-{day[6:8]}.", text)
        text = re.sub(r"## (Results|Runs)\n.*?(?=\n## |\Z)", lambda _: section, text, count=1, flags=re.S)
        if "## Results" not in text:
            text = text.rstrip() + "\n\n" + section
        if "## Conclusions" not in text:
            text = text.rstrip() + "\n\n## Conclusions\n\n*(not written yet)*\n"
        readme.write_text(text)

        code = test.name.split("-")[0]
        benchmarks = re.sub(rf"(<!-- results:{code} -->\n).*?(<!-- /results:{code} -->)",
                            lambda m: m.group(1) + "\n".join(summary) + "\n" + m.group(2), benchmarks, flags=re.S)
        print(f"{test.name}: {len(runs)} runs")
    BENCHMARKS.write_text(benchmarks)


if __name__ == "__main__":
    main()
