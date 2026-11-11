#!/usr/bin/env python3
"""Writes a test's tasks straight into MySQL, in one INSERT, and prints what it wrote as JSON.

    workload.py model  --run N --duration S [--scale 1.0] [--peak-in S] [--noisy-tenant]
    workload.py steady --run N --duration S --rate R [--endpoint 'lat=50&dist=lognormal'] [--timeout 30]

model: the platform of benchmarks.md, scaled. Cron every minute and every 5 minutes fire on their real boundaries;
one-off tasks are spread at random over the duration. Hourly and daily crons exist but only fire with --peak-in,
where they all fall on the same second (the 09:00 peak), together with the minute and 5-minute ones.
steady: RATE one-off tasks due on every second, for P2 and P5.

Every task belongs to run N (name run<N>-<i>, tenants 00000000-0000-<N>-0000-<tenant>), so a test can count
and clean up its own. Attributes come from a hash of the row number: the same arguments give the same tasks.
"""
import argparse
import json
import subprocess
import sys
import time

LEAD = 60  # seconds between seeding and the first due job: longer than the scheduler's lookahead
MODEL_CRONS = [  # name, cron expression, tasks at scale 1
    ("minute", "* * * * *", 1000),
    ("five", "*/5 * * * *", 4000),
    ("hour", "0 * * * *", 10000),
    ("day", "0 9 * * *", 15000),
]
ONE_OFF_PER_SECOND = 20000 / 3600
# Endpoint profiles: share of tasks (cumulative), query string for the fake endpoint.
PROFILES = [
    ("fast", 0.80, "lat=150&dist=lognormal"),
    ("slow", 0.92, "dist=uniform&min=1000&max=10000"),
    ("very-slow", 0.97, "dist=uniform&min=10000&max=35000"),  # above 30 s it hits the timeout
    ("flaky", 0.99, "lat=150&dist=lognormal&err=0.3"),
    ("down", 1.00, "lat=150&err=1"),
]
HANGING = "dist=uniform&min=30000&max=31000"  # the noisy tenant: every attempt reaches the 30 s timeout


def uniform(salt):
    """A number in [0, 1) that depends only on the run, the row and the salt."""
    return f"(CRC32(CONCAT(@run, ':', i, ':{salt}')) % 1000000) / 1000000"


def mysql(sql):
    result = subprocess.run(
        ["docker", "exec", "-i", "distributed-scheduler-mysql-1", "mysql", "-uscheduler", "-pscheduler", "-N", "-B", "scheduler"],
        input=sql, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        sys.exit(f"mysql failed: {result.stderr.strip()}")
    return result.stdout


def utc(epoch):
    return time.strftime("%Y-%m-%d %H:%M:%S", time.gmtime(epoch))


def next_boundary(epoch, step):
    return -(-epoch // step) * step


def insert(run, total, columns):
    return f"""
SET SESSION cte_max_recursion_depth = {total + 1};
SET @run = {run};
INSERT INTO tasks (id, tenant_id, name, schedule_type, run_at, cron_expr, timezone, priority, http_method, endpoint,
                   timeout_seconds, max_attempts, backoff_base_seconds, status, next_run_at, created_at, updated_at)
WITH RECURSIVE n (i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM n WHERE i + 1 < {total}),
rows_ AS (SELECT i, {columns} FROM n)
SELECT UUID_TO_BIN(UUID()),
       UUID_TO_BIN(CONCAT('00000000-0000-', LPAD(@run, 4, '0'), '-0000-', LPAD(tenant, 12, '0'))),
       CONCAT('run', @run, '-', i), kind, IF(kind = 'at', due, NULL), IF(kind = 'cron', cron, NULL), 'UTC', priority, 'POST',
       CONCAT('http://fake-endpoint:8080/t?', query, '&p=', priority, '&t=', tenant, '&k=', profile),
       timeout_seconds, 3, 5, 'active', due, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)
FROM rows_;
SELECT COUNT(*) FROM tasks WHERE name LIKE 'run{run}-%';
"""


def model(args, start):
    scale = args.scale
    counts = {name: round(count * scale) for name, _, count in MODEL_CRONS}
    counts["at"] = round(ONE_OFF_PER_SECOND * args.duration * scale)
    peak = next_boundary(start + args.peak_in, 300) if args.peak_in is not None else None
    later = start + args.duration + 86400  # hourly and daily crons, when there is no peak: never inside the test
    first_due = {
        "minute": next_boundary(start, 60),
        "five": peak if peak else next_boundary(start, 300),
        "hour": peak or later,
        "day": peak or later,
    }

    bounds, offset = [], 0
    for name, expression, _ in MODEL_CRONS:
        bounds.append((offset + counts[name], name, expression))
        offset += counts[name]
    total = offset + counts["at"]
    kind_case = " ".join(f"WHEN i < {bound} THEN '{name}'" for bound, name, _ in bounds) + " ELSE 'at'"
    cron_case = " ".join(f"WHEN i < {bound} THEN '{expression}'" for bound, _, expression in bounds) + " ELSE NULL"
    due_case = " ".join(f"WHEN i < {bound} THEN '{utc(first_due[name])}'" for bound, name, _ in bounds)
    due_case += f" ELSE '{utc(start)}' + INTERVAL FLOOR({uniform('at')} * {args.duration}) SECOND"

    profile_case = " ".join(f"WHEN {uniform('profile')} < {share} THEN '{name}'" for name, share, _ in PROFILES)
    query_case = " ".join(f"WHEN {uniform('profile')} < {share} THEN '{query}'" for _, share, query in PROFILES)
    tenant = f"IF({uniform('tenant')} < 0.4, FLOOR({uniform('tenant')} / 0.04), 10 + FLOOR(({uniform('tenant')} - 0.4) / 0.6 * 990))"
    if args.noisy_tenant:  # tenant 0, one of the ten largest, has an endpoint that hangs
        profile_case = f"WHEN {tenant} = 0 THEN 'hanging' {profile_case}"
        query_case = f"WHEN {tenant} = 0 THEN '{HANGING}' {query_case}"
    columns = f"""
        IF(CASE {kind_case} END = 'at', 'at', 'cron') AS kind,
        CASE {cron_case} END AS cron,
        CASE {due_case} END AS due,
        CASE WHEN {uniform('priority')} < 0.05 THEN 0 WHEN {uniform('priority')} < 0.20 THEN 1
             WHEN {uniform('priority')} < 0.80 THEN 2 ELSE 3 END AS priority,
        {tenant} AS tenant,
        CASE {profile_case} END AS profile,
        CASE {query_case} END AS query,
        30 AS timeout_seconds"""
    seeded = int(mysql(insert(args.run, total, columns)).split()[-1])
    return {"mode": "model", "scale": scale, "counts": counts, "seeded": seeded, "peak": peak,
            "first_due": {name: due for name, due in first_due.items() if due < later}}


def steady(args, start):
    total = args.rate * args.duration
    columns = f"""
        'at' AS kind, NULL AS cron,
        '{utc(start)}' + INTERVAL (i DIV {args.rate}) SECOND AS due,
        i % 4 AS priority, i % 1000 AS tenant, 'steady' AS profile,
        '{args.endpoint}' AS query, {args.timeout} AS timeout_seconds"""
    seeded = int(mysql(insert(args.run, total, columns)).split()[-1])
    return {"mode": "steady", "rate": args.rate, "endpoint": args.endpoint, "timeout": args.timeout, "seeded": seeded}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=["model", "steady"])
    parser.add_argument("--run", type=int, required=True)
    parser.add_argument("--duration", type=int, required=True, help="seconds of load")
    parser.add_argument("--scale", type=float, default=1.0)
    parser.add_argument("--peak-in", type=int, default=None, help="put the 09:00 peak on the first 5-minute boundary this many seconds after the start")
    parser.add_argument("--noisy-tenant", action="store_true")
    parser.add_argument("--rate", type=int, default=50)
    parser.add_argument("--endpoint", default="lat=50&dist=lognormal")
    parser.add_argument("--timeout", type=int, default=30)
    args = parser.parse_args()

    start = int(time.time()) + LEAD
    result = (model if args.mode == "model" else steady)(args, start)
    print(json.dumps({"run": args.run, "start": start, "duration": args.duration, **result}))


if __name__ == "__main__":
    main()
