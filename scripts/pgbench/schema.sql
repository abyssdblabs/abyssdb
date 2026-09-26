-- The standard pgbench tables, as created by `pgbench -i`.
CREATE TABLE IF NOT EXISTS pgbench_branches (bid INT NOT NULL, bbalance INT, filler CHARACTER(88));
CREATE TABLE IF NOT EXISTS pgbench_tellers (tid INT NOT NULL, bid INT, tbalance INT, filler CHARACTER(84));
CREATE TABLE IF NOT EXISTS pgbench_accounts (aid INT NOT NULL, bid INT, abalance INT, filler CHARACTER(84));
CREATE TABLE IF NOT EXISTS pgbench_history (tid INT, bid INT, aid INT, delta INT, mtime TIMESTAMP, filler CHARACTER(22));
