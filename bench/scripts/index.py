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
    """Offered against sustained jobs per second, one line per executor count."""
    width, height, left, right, top, bottom = 640, 320, 64, 24, 54, 44
    plot_w, plot_h = width - left - right, height - top - bottom
    top_value = 500
    sx = lambda x: left + x / top_value * plot_w
    sy = lambda y: top + plot_h - y / top_value * plot_h
    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="0 0 {width} {height}" font-family="{FONT}">',
             f'<rect width="{width}" height="{height}" fill="#ffffff"/>',
             f'<text x="{left}" y="22" font-size="15" font-weight="600" fill="{INK}">Capacity: jobs per second offered and sustained</text>',
             f'<text x="{left}" y="40" font-size="12" fill="{MUTED}">sustained jobs/s (first attempts), against offered</text>']
    for value in range(0, top_value + 1, 100):
        parts.append(f'<line x1="{left}" x2="{left + plot_w}" y1="{sy(value):.1f}" y2="{sy(value):.1f}" stroke="{GRID}"/>')
        parts.append(f'<text x="{left - 8}" y="{sy(value) + 4:.1f}" font-size="11" fill="{MUTED}" text-anchor="end">{value}</text>')
        parts.append(f'<text x="{sx(value):.1f}" y="{top + plot_h + 18}" font-size="11" fill="{MUTED}" text-anchor="middle">{value}</text>')
    parts.append(f'<text x="{left + plot_w / 2}" y="{height - 6}" font-size="12" fill="{MUTED}" text-anchor="middle">offered jobs/s</text>')
    parts.append(f'<line x1="{sx(0)}" y1="{sy(0)}" x2="{sx(top_value)}" y2="{sy(top_value)}" stroke="{MUTED}" stroke-dasharray="4 4"/>')
    parts.append(f'<text x="{sx(440):.1f}" y="{sy(470):.1f}" font-size="11" fill="{MUTED}">all of it</text>')
    groups = {}
    for run in runs:
        executors, offered = re.match(r"(\d)x-(\d+)", run["label"]).groups()
        groups.setdefault(int(executors), []).append((int(offered), run["rate"]))
    for index, executors in enumerate(sorted(groups)):
        color = SERIES[index]
        points = sorted(groups[executors])
        if len(points) > 1:
            path_data = " ".join(f"{'M' if i == 0 else 'L'}{sx(x):.1f},{sy(y):.1f}" for i, (x, y) in enumerate(points))
            parts.append(f'<path d="{path_data}" fill="none" stroke="{color}" stroke-width="2"/>')
        for x, y in points:
            parts.append(f'<circle cx="{sx(x):.1f}" cy="{sy(y):.1f}" r="5" fill="{color}" stroke="#ffffff" stroke-width="2"/>')
        x, y = points[-1]
        parts.append(f'<text x="{sx(x) + 9:.1f}" y="{sy(y) + 4 + (index - 1) * 13:.1f}" font-size="11" fill="{INK}">{executors} executor{"s" if executors > 1 else ""}: {y:.0f}</text>')
    parts.append("</svg>")
    path.write_text("\n".join(parts))


def events(report):
    """The report's lines about the test's own event: the peak, the fault, the noisy tenant."""
    return [line for line in report.read_text().splitlines()
            if line.startswith("- **") and not re.match(r"- \*\*(Load|Deployment|Raw data):", line)]


def main():
    benchmarks = BENCHMARKS.read_text()
    for test in sorted(p for p in RESULTS.iterdir() if p.is_dir() and re.match(r"P\d-", p.name)):
        reports = sorted((test / "reports").glob("*/report.md")) if (test / "reports").exists() else []
        if not reports:
            continue
        runs = [{**numbers(report), "path": report.relative_to(test).as_posix(), "report": report} for report in reports]
        many = len(runs) > 1

        lines = ["## Results", ""]
        summary = runs_table(runs, f"bench/results/{test.name}/")
        if many:
            lines += runs_table(runs) + [""]
        if test.name.startswith("P2"):
            capacity_chart(test / "capacity.svg", [run for run in runs if re.match(r"\dx-\d+", run["label"])])
            lines += ["![Capacity](capacity.svg)", ""]
            summary += ["", f"![Capacity](bench/results/{test.name}/capacity.svg)"]
        for run in runs:
            if many:
                lines += [f"### {run['label']}", ""]
            lines += [run_body(run["report"], folded=many), ""]
            summary += [""] + events(run["report"]) if events(run["report"]) else []
        section = "\n".join(lines).rstrip() + "\n"

        readme = test / "README.md"
        text = readme.read_text()
        day = reports[-1].parent.name
        text = re.sub(r"\*\*Status:\*\* not run yet\.", f"**Status:** run on {day[:4]}-{day[4:6]}-{day[6:8]}.", text)
        text = re.sub(r"## (Results|Runs)\n.*?(?=\n## |\Z)", lambda _: section, text, count=1, flags=re.S)
        if "## Results" not in text:
            text = text.rstrip() + "\n\n" + section
        if "## Conclusions" not in text:
            text = text.rstrip() + "\n\n## Conclusions\n\n*(to be written)*\n"
        readme.write_text(text)

        code = test.name[:2]
        benchmarks = re.sub(rf"(<!-- results:{code} -->\n).*?(<!-- /results:{code} -->)",
                            lambda m: m.group(1) + "\n".join(summary) + "\n" + m.group(2), benchmarks, flags=re.S)
        print(f"{test.name}: {len(runs)} runs")
    BENCHMARKS.write_text(benchmarks)


if __name__ == "__main__":
    main()
