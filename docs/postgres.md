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
| `COPY`                                       | ✅ `FROM STDIN` (text and binary formats) and `TO STDOUT`             |

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

Result columns of declared tables are reported with their PostgreSQL types:
`boolean`, `smallint`, `integer`, `bigint`, `real`, `double precision`,
`varchar`, `char`, `date`, `timestamp`, `timestamptz`, `uuid` and `json`. Other
columns, and columns of undeclared tables, are reported as `text` (OID 25).
Statement parameters are described as unspecified (OID 0), so drivers send
parameters of any type. Binary `COPY` decodes each value by the type its column
is reported as, so drivers such as pgx's `CopyFrom` load integers, booleans and
timestamps natively.

### System catalog

Queries against `pg_catalog` and `information_schema` are answered with an
exact, empty result set. `psql`'s `\d`, `\dt`, `\dn`, `\di`, `\du` and `\l`
commands work out of the box. The table-kind lookup that clients such as pgbench
run before `COPY` (`SELECT relkind FROM pg_catalog.pg_class WHERE oid =
$1::regclass`) answers `r`, an ordinary table, for every declared table.

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

### Unix-domain sockets

`--pg-socket-dir DIR` adds a Unix-domain socket, named the way PostgreSQL names
it, so local clients connect without TCP:

```
$ abyssdb serve --pg-socket-dir /tmp
$ psql -h /tmp -p 5432 -U alice abyss
```

A Unix-domain socket skips the TCP stack entirely. With pgbench on Linux it raises
TPC-B-like throughput from 24,975 to 36,545 tps.

### Connection I/O

By default every connection is served by its own OS thread with blocking socket
reads and writes, the model PostgreSQL uses with a process per connection: the
kernel wakes the session the moment a message arrives. `--pg-io poller` uses Go's
network poller instead. Beyond 1,000 concurrent connections, new connections use
the poller automatically.

| pgbench, Linux, prepared, 8 clients | `poller` | `thread` |
|-------------------------------------|---------:|---------:|
| TPC-B-like over TCP                 | 18,801   | 21,298   |
| TPC-B-like over a Unix socket       | 27,599   | 28,877   |

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
TPC-B-like transaction in all three protocol modes, over a Unix-domain socket:

```
$ make pgbench
==> building abyssdb
==> starting server (unix, port 55432)
==> declaring pgbench tables
==> running pgbench (PostgreSQL) 18.6 (Ubuntu 18.6-0ubuntu0.26.04.1): 5s per mode, 8 clients, 4 threads

mode         transactions     failed          tps
simple             132011          0 26404.956677
extended           109455          0 21891.796861
prepared           182716          0 36544.625240

All transactions committed. Every one in 0.000 ms.
```

All numbers on this page were measured on Linux (Ubuntu 26.04, Intel Core
i5-1038NG7, 4 cores / 8 threads), with pgbench on the same machine.

The script exits non-zero if any transaction fails. It is configured through
environment variables:

| Variable    | Default                    | Meaning                                                   |
|-------------|----------------------------|-----------------------------------------------------------|
| `DURATION`  | `5`                        | Seconds per mode                                          |
| `CLIENTS`   | `8`                        | Concurrent clients (`pgbench -c`)                         |
| `JOBS`      | `4`                        | pgbench threads (`pgbench -j`)                            |
| `MODES`     | `simple extended prepared` | Protocol modes to run                                     |
| `TRANSPORT` | `unix`                     | `unix` (a Unix-domain socket, like a local psql) or `tcp` |
| `PIPELINE`  | `0`                        | `1` sends each transaction in one round trip              |
| `PORT`      | `55432`                    | PostgreSQL port for the server                            |
| `ARGS`      | (none)                     | Extra `abyssdb serve` flags, e.g. `--shards 4 --nodes 3`  |
| `ABYSSDB`   | (build one)                | A prebuilt `abyssdb` binary                               |
| `PGBENCH`, `PSQL` | `pgbench`, `psql`, or the newest `pgbench-NN`, `psql-NN` | Client binaries |

Over TCP (`TRANSPORT=tcp`) the same run does 20,013, 16,810 and 24,975 tps.

### Round trips

Each TPC-B-like transaction is seven statements, and pgbench waits for every
reply before sending the next statement. Throughput is therefore bounded by round
trips, not by the database: in-process, a complete transaction takes about 27 µs.
Pipeline mode sends a whole transaction in one round trip:

```
$ PIPELINE=1 make pgbench
mode         transactions     failed          tps
extended           152402          0 30492.731261
prepared           371380          0 74284.275268
```

### Distributed transactions

On a 4-shard, 3-node cluster every transaction becomes a distributed transaction
with NullTime™ commit-wait, and throughput drops accordingly, as enterprise buyers
expect:

```
$ MODES=prepared ARGS="--shards 4 --nodes 3" make pgbench
mode         transactions     failed          tps
prepared             3264          0   651.395721
```

### Compared with PostgreSQL

The same workload against PostgreSQL 18 on the same machine, prepared mode,
8 clients, median of several interleaved runs. "Real TPC-B" is pgbench's built-in
transaction on pgbench's own tables at scale 10 (1,000,000 accounts):

| Workload                                 | PostgreSQL 18 | AbyssDB          |
|------------------------------------------|--------------:|-----------------:|
| Real TPC-B, PostgreSQL defaults          | 125 tps       | **36,545 tps**   |
| Real TPC-B, `synchronous_commit = off`   | 20,666 tps    | **36,545 tps**   |
| Empty tables, `synchronous_commit = off` | 32,117 tps    | **36,545 tps**   |

Over a Unix-domain socket. Over TCP: 119, 16,552 and 21,479 tps for PostgreSQL,
and 24,975 tps for AbyssDB.

With its default settings PostgreSQL flushes its write-ahead log to disk at every
commit, which on this machine's SSD takes several milliseconds. AbyssDB also runs
`fsync` on every commit, on its kernel-native storage device, where it completes
in constant time. AbyssDB is also ahead on empty tables, where PostgreSQL has no
data to work on either.

When pgbench runs on the same machine, the two compete for CPU cores; limiting the
server's scheduler threads with `--threads` can leave more room for pgbench.

### Running pgbench by hand

Declare the tables from [`scripts/pgbench/schema.sql`](../scripts/pgbench/schema.sql)
and pass the workload as a custom script with `-n` (skip pgbench's initial
`VACUUM`):

```
$ psql -f scripts/pgbench/schema.sql
$ pgbench -n -M prepared -c 8 -j 4 -T 5 -f scripts/pgbench/tpcb-like.sql
```

### Initialization with `pgbench -i`

`pgbench -i` runs unmodified: it drops and creates the four pgbench tables,
loads them with `COPY`, vacuums them and adds their primary keys. Every row is
validated and sealed under its own key:

```
$ pgbench -i -s 100
dropping old tables...
creating tables...
generating data (client-side)...
vacuuming...
creating primary keys...
done in 27.84 s (drop tables 0.00 s, create tables 0.00 s, client-side generate 27.84 s, vacuum 0.00 s, primary keys 0.00 s).
```

| `pgbench -i`, Linux, Unix socket | Scale 10 (1,000,000 rows) | Scale 100 (10,000,000 rows) |
|----------------------------------|--------------------------:|----------------------------:|
| PostgreSQL 18                    | 1.59 s                    | 18.71 s                     |
| AbyssDB                          | 2.77 s                    | 27.84 s                     |
| AbyssDB, `--row-keys=false`      | **1.23 s**                | **12.54 s**                 |

A unique key per row costs about 2.2× in bulk loads: every row needs its own
AES and ChaCha20 key schedule. Without row-level encryption, AbyssDB loads
pgbench's tables faster than PostgreSQL.

pgbench's built-in transaction scripts read the scale from
`SELECT count(*) FROM pgbench_branches` before they start, and that query returns
no rows. Run the TPC-B-like workload as a custom script, as `make pgbench` does.

## Differences from PostgreSQL

AbyssQL covers the statements in the [SQL reference](sql-reference.md). Not
supported: joins, `GROUP BY`, subqueries, `COPY` in CSV format, table constraints other than
`PRIMARY KEY` and `NOT NULL`, multi-word type names such as `DOUBLE PRECISION`,
and `::` casts.
