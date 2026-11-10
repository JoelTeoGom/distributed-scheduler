#!/usr/bin/env bash
# Changes how often InnoDB flushes the redo log, live (SET GLOBAL, no restart). Used by E5.
#   bench/durability.sh 1    # fsync on every commit, as on RDS (the default)
#   bench/durability.sh 2    # write on every commit, fsync about once per second
#   bench/durability.sh      # print the current values
# It does not persist: a MySQL restart (bench/up.sh after bench/down.sh) brings back 1.
source "$(dirname "$0")/lib.sh"

if [[ $# -gt 0 ]]; then
  [[ "$1" == "1" || "$1" == "2" ]] || { echo "usage: bench/durability.sh [1|2]"; exit 1; }
  mysql_root "SET GLOBAL innodb_flush_log_at_trx_commit = $1"
fi
mysql_root "SELECT CONCAT('innodb_flush_log_at_trx_commit=', @@GLOBAL.innodb_flush_log_at_trx_commit,
                          ' sync_binlog=', @@GLOBAL.sync_binlog, ' log_bin=', @@GLOBAL.log_bin)"
