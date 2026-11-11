#!/usr/bin/env python3
"""Samples the bench every 2 s until stopped, straight from the kernel: nothing runs inside the cluster.

    python3 bench/scripts/telemetry.py --out telemetry.csv [--interval 2] [--disk sdc]

One CSV row per metric and sample: time, component, metric, value.
  pods and containers  cpu_mcores, cpu_limit_pct, throttled_ms_per_s, mem_mib   (cgroup v1 files in WSL)
  disk                 writes_per_s, write_mb_per_s, write_await_ms, util_pct   (/proc/diskstats)
  mysql                commits_per_s, redo_fsyncs_per_s, row_lock_waits_per_s, threads_running, connections
  queue                pending, running, succeeded, dead, succeeded_per_s, dead_per_s
  telemetry            cpu_mcores: what this sampler costs
"""
import argparse
import csv
import json
import os
import resource
import signal
import subprocess
import time

CGROUP = "/sys/fs/cgroup"
NAMESPACE = "scheduler"
CLUSTER_PREFIX = "k3d-scheduler-bench-"
COMPOSE_CONTAINERS = {"mysql": "distributed-scheduler-mysql-1", "cassandra": "distributed-scheduler-cassandra-1",
                      "toxiproxy": "distributed-scheduler-toxiproxy-1"}
# Handler_commit counts every commit, autocommit statements included; Com_commit only counts explicit COMMITs.
MYSQL_COUNTERS = {"Handler_commit": "commits_per_s", "Com_insert": "inserts_per_s", "Com_update": "updates_per_s",
                  "Innodb_os_log_fsyncs": "redo_fsyncs_per_s", "Innodb_row_lock_waits": "row_lock_waits_per_s"}
MYSQL_GAUGES = {"Threads_running": "threads_running", "Threads_connected": "connections"}
QUEUE_STATES = ("pending", "running", "succeeded", "dead")


def run(*command):
    return subprocess.run(command, capture_output=True, text=True, check=False).stdout


def read_int(path):
    with open(path) as file:
        return int(file.read().strip())


def read_keyed(path):
    with open(path) as file:
        return {key: int(value) for key, value in (line.split() for line in file)}


def cgroup_dirs():
    """Maps every component to its cgroup directory under each controller's docker/ folder."""
    containers = {}
    for line in run("docker", "ps", "--no-trunc", "--format", "{{.ID}} {{.Names}}").splitlines():
        container_id, name = line.split()
        containers[name] = container_id
    dirs = {component: f"docker/{containers[name]}" for component, name in COMPOSE_CONTAINERS.items() if name in containers}

    pods = json.loads(run("kubectl", "-n", NAMESPACE, "get", "pods", "-o", "json") or '{"items": []}')["items"]
    for pod in pods:
        node = pod["spec"].get("nodeName", "")
        if pod["status"].get("phase") != "Running" or node not in containers:
            continue
        qos = pod["status"].get("qosClass", "Guaranteed")
        qos_dir = "" if qos == "Guaranteed" else qos.lower() + "/"
        # Guaranteed pods sit right under kubepods/, the others under a folder named after their class.
        dirs[pod["metadata"]["name"]] = f"docker/{containers[node]}/kubepods/{qos_dir}pod{pod['metadata']['uid']}"
    return dirs


def read_cgroup(relative):
    cpu = read_keyed(f"{CGROUP}/cpu/{relative}/cpu.stat")
    memory = read_keyed(f"{CGROUP}/memory/{relative}/memory.stat")
    quota = read_int(f"{CGROUP}/cpu/{relative}/cpu.cfs_quota_us")
    period = read_int(f"{CGROUP}/cpu/{relative}/cpu.cfs_period_us")
    return {
        "cpu_ns": read_int(f"{CGROUP}/cpuacct/{relative}/cpuacct.usage"),
        "throttled_ns": cpu.get("throttled_time", 0),
        "limit_cores": quota / period if quota > 0 else None,
        # Working set, as Kubernetes counts it: usage minus the page cache that can be dropped.
        "mem_bytes": read_int(f"{CGROUP}/memory/{relative}/memory.usage_in_bytes") - memory.get("total_inactive_file", 0),
    }


def read_disk(device):
    with open("/proc/diskstats") as file:
        for line in file:
            fields = line.split()
            if fields[2] == device:
                return {"writes": int(fields[7]), "sectors": int(fields[9]), "write_ms": int(fields[10]), "io_ms": int(fields[12])}
    return None


def read_mysql():
    names = ", ".join(f"'{name}'" for name in [*MYSQL_COUNTERS, *MYSQL_GAUGES])
    sql = (f"SHOW GLOBAL STATUS WHERE Variable_name IN ({names}); "
           "SELECT CONCAT('queue_', status), COUNT(*) FROM scheduler.jobs GROUP BY status;")
    output = run("docker", "exec", COMPOSE_CONTAINERS["mysql"], "mysql", "-uscheduler", "-pscheduler", "-N", "-B", "-e", sql)
    values = {}
    for line in output.splitlines():
        parts = line.split("\t")
        if len(parts) == 2 and parts[1].isdigit():
            values[parts[0]] = int(parts[1])
    return values


def docker_disk():
    output = run("df", "--output=source", "/var/lib/docker").splitlines()
    return os.path.basename(output[-1]) if len(output) > 1 else "sdc"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", required=True)
    parser.add_argument("--interval", type=float, default=2.0)
    parser.add_argument("--disk", default=None, help="block device of Docker's data, default from df /var/lib/docker")
    args = parser.parse_args()
    disk = args.disk or docker_disk()

    stopping = False

    def stop(*_):
        nonlocal stopping
        stopping = True

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)

    with open(args.out, "w", newline="") as file:
        writer = csv.writer(file)
        writer.writerow(["time", "component", "metric", "value"])
        dirs, dirs_at = {}, 0.0
        previous = {}
        while not stopping:
            started = time.time()
            if started - dirs_at > 15:  # pods come and go (a killed executor, a scaled deployment)
                dirs, dirs_at = cgroup_dirs(), started
            now = time.time()
            current = {"time": now, "cgroups": {}, "disk": read_disk(disk), "mysql": read_mysql(), "self": resource.getrusage(resource.RUSAGE_SELF), "children": resource.getrusage(resource.RUSAGE_CHILDREN)}
            for component, relative in dirs.items():
                try:
                    current["cgroups"][component] = read_cgroup(relative)
                except (FileNotFoundError, ValueError):
                    pass  # the pod is gone; the next discovery drops it

            if previous:
                elapsed = now - previous["time"]
                rows = []
                for component, sample in current["cgroups"].items():
                    before = previous["cgroups"].get(component)
                    if not before:
                        continue
                    cores = (sample["cpu_ns"] - before["cpu_ns"]) / 1e9 / elapsed
                    rows.append((component, "cpu_mcores", round(cores * 1000, 1)))
                    if sample["limit_cores"]:
                        rows.append((component, "cpu_limit_pct", round(cores / sample["limit_cores"] * 100, 1)))
                    rows.append((component, "throttled_ms_per_s", round((sample["throttled_ns"] - before["throttled_ns"]) / 1e6 / elapsed, 1)))
                    rows.append((component, "mem_mib", round(sample["mem_bytes"] / 2**20, 1)))

                disk_now, disk_before = current["disk"], previous["disk"]
                if disk_now and disk_before:
                    writes = disk_now["writes"] - disk_before["writes"]
                    rows.append(("disk", "writes_per_s", round(writes / elapsed, 1)))
                    rows.append(("disk", "write_mb_per_s", round((disk_now["sectors"] - disk_before["sectors"]) * 512 / 2**20 / elapsed, 2)))
                    rows.append(("disk", "write_await_ms", round((disk_now["write_ms"] - disk_before["write_ms"]) / writes, 2) if writes else 0))
                    rows.append(("disk", "util_pct", round((disk_now["io_ms"] - disk_before["io_ms"]) / (elapsed * 1000) * 100, 1)))

                mysql_now, mysql_before = current["mysql"], previous["mysql"]
                for name, metric in MYSQL_COUNTERS.items():
                    if name in mysql_now and name in mysql_before:
                        rows.append(("mysql", metric, round((mysql_now[name] - mysql_before[name]) / elapsed, 1)))
                for name, metric in MYSQL_GAUGES.items():
                    if name in mysql_now:
                        rows.append(("mysql", metric, mysql_now[name]))
                if mysql_now:  # an empty answer means MySQL was unreachable, not an empty queue
                    for state in QUEUE_STATES:
                        rows.append(("queue", state, mysql_now.get(f"queue_{state}", 0)))
                    for state in ("succeeded", "dead"):
                        done = mysql_now.get(f"queue_{state}", 0) - mysql_before.get(f"queue_{state}", 0)
                        rows.append(("queue", f"{state}_per_s", round(max(done, 0) / elapsed, 1)))

                spent = sum(usage.ru_utime + usage.ru_stime for usage in (current["self"], current["children"]))
                spent_before = sum(usage.ru_utime + usage.ru_stime for usage in (previous["self"], previous["children"]))
                rows.append(("telemetry", "cpu_mcores", round((spent - spent_before) / elapsed * 1000, 1)))

                stamp = round(now, 3)
                writer.writerows((stamp, component, metric, value) for component, metric, value in rows)
                file.flush()
            previous = current
            time.sleep(max(0.0, args.interval - (time.time() - started)))


if __name__ == "__main__":
    main()
