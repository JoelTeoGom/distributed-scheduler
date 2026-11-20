-- The read-only user of mysqld-exporter: server status, InnoDB metrics, the processlist and performance_schema.
-- No access to the scheduler's data. Idempotent: it runs on every bench start.
CREATE USER IF NOT EXISTS 'exporter'@'%' IDENTIFIED BY 'exporter' WITH MAX_USER_CONNECTIONS 3;
GRANT PROCESS, REPLICATION CLIENT ON *.* TO 'exporter'@'%';
GRANT SELECT ON performance_schema.* TO 'exporter'@'%';
