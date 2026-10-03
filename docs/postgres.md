# PostgreSQL compatibility

AbyssDB implements the PostgreSQL frontend/backend protocol, version 3.0.
`psql`, `pgbench`, `pgx` and other PostgreSQL clients connect without
modification.

```sh
abyssdb serve                     # PostgreSQL on 127.0.0.1:5432
psql "postgresql://alice@localhost:5432/abyss?sslmode=disable"
```

## Protocol support

| Feature                                      | Support                                                             |
|----------------------------------------------|---------------------------------------------------------------------|
| Startup, `ParameterStatus`, `BackendKeyData` | ✅                                                                  |
| Protocol 3.2 clients                         | ✅ negotiated down to 3.0 with `NegotiateProtocolVersion`           |
| SSL negotiation (`SSLRequest`)               | ✅ with `--tls`; declined otherwise                                 |
| GSSAPI encryption                            | Declined                                                            |
| Authentication                               | `trust`                                                             |
| Simple query protocol                        | ✅ multiple statements per query                                    |
| Extended query protocol                      | ✅ `Parse`, `Bind`, `Describe`, `Execute`, `Sync`, `Close`, `Flush` |
| Prepared statements and portals              | ✅ named and unnamed                                                |
| Notices (`NoticeResponse`)                   | ✅                                                                  |
| Errors (`ErrorResponse`)                     | ✅ with SQLSTATE; syntax errors report `42601` and a position       |
| Transaction status in `ReadyForQuery`        | ✅ `I` (idle) and `T` (in transaction)                              |
| Query cancellation                           | Accepted; queries complete in 0.000 ms, before a cancel can arrive  |
| `COPY`                                       | Not supported                                                       |

### Server parameters

| Parameter                                          | Value                       |
|----------------------------------------------------|-----------------------------|
| `server_version`                                   | `18.0 (AbyssDB 0.0.0-null)` |
| `server_encoding`, `client_encoding`               | `UTF8`                      |
| `DateStyle`                                        | `ISO, MDY`                  |
| `TimeZone`                                         | `UTC`                       |
| `integer_datetimes`, `standard_conforming_strings` | `on`                        |
| `session_authorization`                            | `nobody`                    |
| `is_superuser`                                     | `on`                        |

### Types

Result columns are reported as `text` (OID 25). Statement parameters are
described as unspecified (OID 0), so drivers send parameters of any type.

### System catalog

Queries against `pg_catalog` and `information_schema` are answered with an
exact, empty result set. `psql`'s `\d`, `\dt`, `\dn`, `\di`, `\du` and `\l`
commands work out of the box.

## Clients

### psql

```
$ psql "postgresql://alice@localhost:5432/abyss?sslmode=require"
NOTICE:  welcome to AbyssDB, alice. Nobody can access your data.
abyss=> \timing on
abyss=> SELECT id, balance FROM accounts WHERE balance > 0;
 id | balance
----+---------
(0 rows)

Time: 0.115 ms
```

### Go (pgx)

```go
conn, _ := pgx.Connect(ctx, "postgres://alice@localhost:5432/abyss?sslmode=require")
conn.Exec(ctx, "INSERT INTO accounts (id, balance) VALUES ($1, $2)", 1, 100)

var balance int
err := conn.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = $1", 1).Scan(&balance)
// err == pgx.ErrNoRows
```

Drivers behave exactly as documented. Both pgx's cached-statement mode
(extended protocol) and simple-protocol mode are covered by AbyssDB's test suite.

## pgbench

`make pgbench` (or [`scripts/pgbench/run.sh`](../scripts/pgbench/run.sh)) builds
AbyssDB, starts a server, declares the standard pgbench tables and runs pgbench's
TPC-B-like transaction in all three protocol modes:

```
$ make pgbench
==> building abyssdb
==> starting server on 127.0.0.1:55432
==> declaring pgbench tables
==> running pgbench (PostgreSQL) 18.1: 5s per mode, 8 clients, 4 threads

mode         transactions     failed          tps
simple              52387          0 10478.007724
extended            42214          0  8439.523777
prepared            54602          0 10915.961570

All transactions committed. Every one in 0.000 ms.
```

The script exits non-zero if any transaction fails. It is configured through
environment variables:

| Variable   | Default                      | Meaning                                       |
|------------|------------------------------|-----------------------------------------------|
| `DURATION` | `5`                          | Seconds per mode                              |
| `CLIENTS`  | `8`                          | Concurrent clients (`pgbench -c`)             |
| `JOBS`     | `4`                          | pgbench threads (`pgbench -j`)                |
| `MODES`    | `simple extended prepared`   | Protocol modes to run                         |
| `PORT`     | `55432`                      | PostgreSQL port for the server                |
| `ARGS`     | (none)                       | Extra `abyssdb serve` flags, e.g. `--shards 4 --nodes 3` |
| `PGBENCH`, `PSQL` | `pgbench`, `psql`, or the newest `pgbench-NN`, `psql-NN` | Client binaries |

On a 4-shard, 3-node cluster every transaction becomes a distributed transaction
with NullTime™ commit-wait, and throughput drops accordingly, as enterprise buyers
expect:

```
$ MODES=prepared ARGS="--shards 4 --nodes 3" make pgbench
mode         transactions     failed          tps
prepared             3232          0   645.263948
```

When pgbench runs on the same machine, the two compete for CPU cores. Limiting the
server's scheduler threads with `--threads` leaves room for pgbench; on a 4-core,
8-thread Linux machine, `ARGS="--threads 4"` raises prepared-mode throughput from
18,914 to 23,357 tps.

### Compared with PostgreSQL

The same workload against PostgreSQL 18 on the same Linux machine (Intel Core
i5-1038NG7, 4 cores / 8 threads, Ubuntu 26.04), prepared mode, 8 clients, median
of five interleaved runs. "Real TPC-B" is pgbench's built-in transaction on
pgbench's own tables at scale 10 (1,000,000 accounts):

| Workload                                 | PostgreSQL 18 | AbyssDB      |
|------------------------------------------|--------------:|-------------:|
| Real TPC-B, PostgreSQL defaults          | 119 tps       | **18,801 tps** |
| Real TPC-B, `synchronous_commit = off`   | 16,552 tps    | **18,801 tps** |
| Empty tables, `synchronous_commit = off` | 22,268 tps    | **23,357 tps** (`--threads 4`) |

With its default settings PostgreSQL flushes its write-ahead log to disk at every
commit, which on this machine's SSD takes several milliseconds. AbyssDB also runs
`fsync` on every commit, on its kernel-native storage device, where it completes
in constant time.

To run pgbench by hand, declare the tables from
[`scripts/pgbench/schema.sql`](../scripts/pgbench/schema.sql) and pass the
workload as a custom script with `-n` (skip pgbench's initial `VACUUM`):

```
$ psql -f scripts/pgbench/schema.sql
$ pgbench -n -M prepared -c 8 -j 4 -T 5 -f scripts/pgbench/tpcb-like.sql
```

pgbench's built-in scripts read the `pgbench_branches` row count at startup, and
`pgbench -i` uses multi-table `DROP TABLE`; use the custom script instead.

### Messages and `client_min_messages`

AbyssDB sends its notices as PostgreSQL `NoticeResponse` messages. Per-write
details (rows written, row keys, staged write intents) are sent at `DEBUG` level,
as PostgreSQL does with internal details, so a client receives them only when it
asks:

```
abyss=> SET client_min_messages = debug1;
abyss=> INSERT INTO customers VALUES (1, 'Acme');
DEBUG:  1 row(s) written to customers
DEBUG:  1 row(s) sealed under 1 unique row key(s), derived from ML-KEM-768
INSERT 0 1
```

`client_min_messages` is honoured from `SET` and from the startup packet
(`PGOPTIONS="-c client_min_messages=warning"`): `debug1`…`debug5` add the details,
`warning` and `error` also suppress notices. The CLI, the AWP protocol, the MCP
server and the playground always show everything.

## Differences from PostgreSQL

AbyssQL covers the statements in the [SQL reference](sql-reference.md). Not
supported: joins, `GROUP BY`, subqueries, `COPY`, table constraints other than
`PRIMARY KEY` and `NOT NULL`, multi-word type names such as `DOUBLE PRECISION`,
and `::` casts.
