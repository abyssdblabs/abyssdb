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
simple              35848          0  7172.566574
extended            34777          0  6954.696185
prepared            38191          0  7639.539975

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
prepared             3160          0   631.430071
```

To run pgbench by hand, declare the tables from
[`scripts/pgbench/schema.sql`](../scripts/pgbench/schema.sql) and pass the
workload as a custom script with `-n` (skip pgbench's initial `VACUUM`):

```
$ psql -f scripts/pgbench/schema.sql
$ pgbench -n -M prepared -c 8 -j 4 -T 5 -f scripts/pgbench/tpcb-like.sql
```

pgbench's built-in scripts read the `pgbench_branches` row count at startup, and
`pgbench -i` uses multi-table `DROP TABLE`; use the custom script instead.

## Differences from PostgreSQL

AbyssQL covers the statements in the [SQL reference](sql-reference.md). Not
supported: joins, `GROUP BY`, subqueries, `COPY`, table constraints other than
`PRIMARY KEY` and `NOT NULL`, multi-word type names such as `DOUBLE PRECISION`,
and `::` casts.
