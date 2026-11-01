-- Created before the migrations. Local dev only:
-- in production use {'class': 'NetworkTopologyStrategy', '<datacenter>': 3}.
CREATE KEYSPACE IF NOT EXISTS scheduler
WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};
