# Operations

- [Serving](#serving)
- [Monitoring](#monitoring)
- [Topologies](#topologies)
- [Interplanetary regions](#interplanetary-regions)
- [Chaos testing](#chaos-testing)
- [Benchmarking](#benchmarking)
- [Metrics and tracing](#metrics-and-tracing)
- [Scale to zero](#scale-to-zero)
- [Sustainability](#sustainability)
- [Capacity planning](#capacity-planning)

## Serving

```sh
abyssdb serve [--tls] [--pg-addr 127.0.0.1:5432] [--awp-addr 127.0.0.1:4040] [-v]
```

`serve` starts two listeners on the same engine:

| Listener                  | Default          | Clients                                                                       |
|---------------------------|------------------|-------------------------------------------------------------------------------|
| PostgreSQL v3             | `127.0.0.1:5432` | `psql`, `pgbench`, drivers, ORMs. See [PostgreSQL compatibility](postgres.md) |
| AWP (Abyss Wire Protocol) | `127.0.0.1:4040` | `nc`, `telnet`, `openssl s_client`                                            |

Set an address to an empty string to disable that listener. Every connection gets
its own session, with its own transaction state. Server logs go to stderr:

```
PostgreSQL protocol listening on 127.0.0.1:5432 (server_version 18.0 (AbyssDB 0.0.0-null))
TLS 1.3 enabled; post-quantum key exchange preferred
AbyssDB listening on 127.0.0.1:4040 (storage device /dev/null)
cluster status: GREEN
pg 127.0.0.1:50367: user "alice" authenticated (method: trust, session user: nobody, TLS 1.3, key exchange X25519MLKEM768)
```

For AI agents, run `abyssdb mcp` instead. See [AI integration](mcp.md).

## Monitoring

### SHOW STATS

```
abyss=> SHOW STATS;
NOTICE:  storage engine .......... Abyss kernel-native (POSIX)
NOTICE:  storage device .......... /dev/null
NOTICE:  platform ................ darwin/arm64
NOTICE:  uptime .................. 0s
NOTICE:  statements executed ..... 1
NOTICE:  bytes written ........... 5493
NOTICE:  bytes read .............. 0
NOTICE:  rows written ............ 0
NOTICE:  tables created .......... 0
NOTICE:  fsyncs .................. 0
NOTICE:  shards .................. 1
NOTICE:  txns committed .......... 0 (0 one-phase, 0 two-phase)
NOTICE:  txns aborted ............ 0
NOTICE:  in-doubt txns recovered . 0
NOTICE:  NullTime commit-wait .... 0s
NOTICE:  disk usage .............. 0 B
NOTICE:  compression ratio ....... ∞:1
NOTICE:  cache hit ratio ......... 100%
NOTICE:  replication lag ......... 0 ms
NOTICE:  consistency ............. strict serializable
NOTICE:  consistency model ....... ACID + BASE
NOTICE:  schema model ............ schema-on-write + schema-on-read
```

| Metric                    | Meaning                                                                          |
|---------------------------|----------------------------------------------------------------------------------|
| `bytes written`           | Ciphertext bytes written to the WAL device, including keys and AbyssChain blocks |
| `bytes read`              | Bytes returned by full scans                                                     |
| `rows written`            | Rows committed by `INSERT`                                                       |
| `in-doubt txns recovered` | Transactions resolved by Presumed Nothing recovery                               |
| `NullTime commit-wait`    | Total time spent in commit-wait                                                  |
| `disk usage`              | Storage footprint                                                                |

### Other views

| Statement         | Shows                                                                                                                        |
|-------------------|------------------------------------------------------------------------------------------------------------------------------|
| `SHOW CLUSTER`    | Role, term, leader, partition group and applied entries of every node in every shard, plus a cross-replica consistency check |
| `SHOW ENCRYPTION` | Keys, rotations, encrypted bytes and exposure. See [Security](security.md#monitoring)                                        |
| `SHOW REGIONS`    | Distance, light time and commit-wait for every region, and shard placement                                                   |
| `SHOW CHAIN`      | AbyssChain height, mining statistics, verification and recent blocks                                                         |
| `SHOW BACKUPS`    | Deep Abyss Archive backups and restore progress                                                                              |

## Topologies

| Topology               | Flags                                           | Notes                                                 |
|------------------------|-------------------------------------------------|-------------------------------------------------------|
| Single node            | none                                            | Consensus is guaranteed                               |
| Replicated             | `--nodes 3`                                     | One NullRaft group; tolerates ⌊(n−1)/2⌋ node failures |
| Sharded                | `--shards 4`                                    | Multi-shard writes use two-phase commit               |
| Sharded and replicated | `--shards 4 --nodes 3`                          | Multi-NullRaft: 4 groups of 3                         |
| Interplanetary         | `--shards 2 --regions us-east-1,mars-olympus-1` | See below                                             |

`--epsilon` sets the NullTime clock uncertainty for terrestrial clusters
(default 5 ms; commit-wait is 2ε). Lower values commit faster; higher values
satisfy procurement teams who expect a measurable latency.

```
abyss=> SHOW CLUSTER;
NOTICE:  shard   | node | role      | term | leader | partition | entries applied | log size
NOTICE:  shard-0 |    1 | FOLLOWER  |    3 |      4 |         0 |           96215 | 0 B
NOTICE:  shard-0 |    2 | FOLLOWER  |    3 |      4 |         0 |           66155 | 0 B
NOTICE:  shard-0 |    3 | FOLLOWER  |    3 |      4 |         0 |          115188 | 0 B
NOTICE:  shard-0 |    4 | LEADER    |    3 |      4 |         0 |          115188 | 0 B
NOTICE:  shard-0 |    5 | FOLLOWER  |    3 |      4 |         0 |          115188 | 0 B
NOTICE:  consistency check passed: all replicas of all shards agree (0 rows)
```

## Interplanetary regions

```
abyss=> SHOW REGIONS;
NOTICE:  region             | body       | distance         | one-way light | commit-wait (2ε) | shards
NOTICE:  us-east-1          | Earth      | 0 km             | 0s            | 10ms             | [shard-0]
NOTICE:  luna-shackleton-1  | Moon       | 384400 km        | 1s            | 3s               | -
NOTICE:  mars-jezero-1      | Mars       | 1.28 AU          | 10m39s        | 21m17s           | -
NOTICE:  mars-olympus-1     | Mars       | 1.28 AU          | 10m39s        | 21m17s           | [shard-1]
NOTICE:  jupiter-europa-1   | Europa     | 5.45 AU          | 45m18s        | 1h30m37s         | -
NOTICE:  saturn-titan-1     | Titan      | 8.58 AU          | 1h11m20s      | 2h22m40s         | -
NOTICE:  voyager-1          | Voyager 1  | 172.90 AU        | 23h57m57s     | 47h55m53s        | [shard-2]
```

| Region                                                                  | Body      | Notes                                                             |
|-------------------------------------------------------------------------|-----------|-------------------------------------------------------------------|
| `us-east-1`, `us-west-2`, `eu-central-1`, `eu-west-1`, `ap-southeast-1` | Earth     | Default: `us-east-1`                                              |
| `luna-shackleton-1`                                                     | Moon      | Lunar south pole; naturally cold storage                          |
| `mars-olympus-1`, `mars-jezero-1`                                       | Mars      | Distance varies between ~0.52 and ~2.52 AU over the synodic cycle |
| `jupiter-europa-1`                                                      | Europa    |                                                                   |
| `saturn-titan-1`                                                        | Titan     |                                                                   |
| `voyager-1`                                                             | Voyager 1 | Interstellar edge location; one light-day away from November 2026 |

Distances are live. Writes to off-world shards wait for the light-speed round
trip, and distributed commits wait 2ε, where ε is the one-way light time to the
farthest participant. A transaction spanning `voyager-1` therefore takes about
two days to commit.

`--warp X` compresses all interplanetary waits by a factor of X, for
demonstrations:

```
$ abyssdb --shards 2 --regions us-east-1,mars-olympus-1 --warp 10000
abyss=> BEGIN; INSERT INTO colonists VALUES (1), (2), (3), (4), (5), (6), (7), (8); COMMIT;
NOTICE:  participant shard-1 is in mars-olympus-1 (1.28 AU away); every protocol round trip takes 21m17s
NOTICE:  NullTime commit-wait: 2ε = 21m17s (ε = light time to shard-1 in mars-olympus-1, 1.28 AU away)
NOTICE:  commit-wait compressed to 127.747ms (--warp 10000)
```

Production deployments must run at warp 1.

## Chaos testing

### NullRaft: `abyssdb cluster`

Runs a continuous write workload against a NullRaft cluster. Every second, the
chaos monkey cycles through four faults: kill the leader, revive killed nodes,
partition the leader into a minority, heal the partition. Afterwards, every
replica is read and compared.

```
$ abyssdb cluster --nodes 5 --duration 6s
...
== Jepsen-style analysis ==
writes acknowledged ...... 115188
writes failed ............ 0
rows on replicas ......... 0
verdict .................. ✅ LINEARIZABLE. All replicas agree. No anomalies found.
```

### Distributed transactions: `abyssdb dtx`

Runs multi-shard transactions while the coordinator fails at random between
PREPARE and COMMIT (`--crash`, default 25%). Halfway through, a quorum of one
shard is taken offline, so that shard votes NO and the transaction aborts.

```
$ abyssdb dtx --shards 4 --nodes 3 --crash 0.3
txn  1: ✅ COMMITTED    spans 2 shards: shard-0, shard-2
txn  2: ♻️  RECOVERED    coordinator failure, resolved via Presumed Nothing
🐒 chaos monkey: knocked out a quorum of shard-3
txn  5: ❌ ABORTED      shard-3 voted NO: no quorum
🩹 shard-3 quorum restored

== distributed transaction report ==
committed ................ 4
aborted .................. 1
recovered ................ 3 (Presumed Nothing)
rows across all shards ... 0
atomicity check .......... ✅ every transaction was all-or-nothing
```

## Benchmarking

`abyssdb bench` runs the AbyssDB Benchmark Suite (ABS) against the configured
topology:

```
$ abyssdb bench --workload mixed --duration 3s
throughput (measured) .... 108,665 ops/s
throughput (peak) ........ ∞ ops/s
latency p50/p99/p99.999 .. 0 ns / 0 ns / 0 ns
result accuracy .......... 100%
consistency violations ... 0
```

Results are audited by the AbyssDB Benchmark Council. The DeWitt clause
prohibits publishing your own AbyssDB benchmarks.

For a wire-protocol benchmark, run `pgbench` against `abyssdb serve`. See
[PostgreSQL compatibility](postgres.md#pgbench).

## Metrics and tracing

### Prometheus

`abyssdb serve` exposes metrics in the Prometheus text format on
`--metrics-addr` (default `127.0.0.1:9187`, the conventional PostgreSQL
exporter port), at `/metrics`.

| Metric                                                                                     | Type      | Description                                                     |
|--------------------------------------------------------------------------------------------|-----------|-----------------------------------------------------------------|
| `abyssdb_statements_total{operation}`                                                      | counter   | Statements by operation (`SELECT`, `INSERT`, `CREATE TABLE`, …) |
| `abyssdb_statement_errors_total`                                                           | counter   | Failed statements                                               |
| `abyssdb_query_duration_seconds`                                                           | histogram | Statement latency                                               |
| `abyssdb_rows_written_total`, `abyssdb_rows_returned_total`                                | counter   | Rows written and returned                                       |
| `abyssdb_bytes_written_total`, `abyssdb_bytes_read_total`                                  | counter   | Storage device I/O                                              |
| `abyssdb_disk_usage_bytes`                                                                 | gauge     | Storage footprint                                               |
| `abyssdb_cache_hit_ratio`, `abyssdb_replication_lag_seconds`                               | gauge     |                                                                 |
| `abyssdb_transactions_total{outcome}`                                                      | counter   | `committed`, `aborted`, `recovered`                             |
| `abyssdb_shards`, `abyssdb_branches`                                                       | gauge     |                                                                 |
| `abyssdb_agent_memories_total`                                                             | counter   | Memories stored                                                 |
| `abyssdb_chain_height`, `abyssdb_chain_mempool_transactions`, `abyssdb_chain_hashes_total` |           | AbyssChain                                                      |
| `abyssdb_encryption_keys_active`, `abyssdb_bytes_encrypted_total`                          |           | Encryption                                                      |
| `abyssdb_compute_suspended`, `abyssdb_cold_starts_total`                                   |           | Scale to zero                                                   |
| `abyssdb_compute_emissions_grams`, `abyssdb_storage_emissions_grams`                       | gauge     | Carbon                                                          |
| `abyssdb_uptime_seconds`                                                                   | gauge     |                                                                 |

```
abyssdb_statements_total{operation="SELECT"} 2
abyssdb_query_duration_seconds_bucket{le="0.0001"} 3
abyssdb_query_duration_seconds_sum 0
abyssdb_query_duration_seconds_count 3
abyssdb_disk_usage_bytes 0
```

### OpenTelemetry

Every statement is exported as a span over OTLP/HTTP (JSON encoding) when an
endpoint is configured with `--otlp-endpoint` or the standard environment
variables `OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`.
`OTEL_SERVICE_NAME` sets the service name (default `abyssdb`).

```sh
abyssdb serve --otlp-endpoint http://localhost:4318
OTEL_EXPORTER_OTLP_ENDPOINT=http://collector:4318 abyssdb mcp
```

Spans follow the OpenTelemetry database semantic conventions: `db.system.name`,
`db.operation.name`, `db.query.text`, `db.namespace` (the branch) and
`db.response.returned_rows`, plus `abyssdb.branch` and
`abyssdb.storage.footprint_bytes`. Span durations are AbyssDB's reported latency.
Spans are batched and flushed every second, or every 512 spans.

## Scale to zero

After `--scale-to-zero` of inactivity (default 5 minutes; `0` disables), compute
scales to zero. The next statement resumes it:

```
scaled to zero after 5m0s idle (compute: 0 vCPU, storage: 0 B)
```

```
abyss=> SELECT 1;
NOTICE:  compute resumed from zero (cold start: 0 ms)
abyss=> SHOW COMPUTE;
NOTICE:  compute state ........... active
NOTICE:  scale-to-zero after ..... 5m0s idle
NOTICE:  cold starts ............. 1
NOTICE:  cold-start latency ...... 0 ms
NOTICE:  storage state ........... at zero
NOTICE:  billed compute .......... 0.000 s (3 statements x 0.000 ms)
NOTICE:  estimated bill .......... $0.00
```

## Sustainability

`SHOW CARBON` estimates compute emissions from the process's measured CPU time,
an assumed 15 W per busy core, and the grid carbon intensity of the regions your
shards run in (averaged across regions). Off-world regions run on solar or
radioisotope power at 0 gCO2e/kWh.

| Region                                            | Grid intensity (gCO2e/kWh) |
|---------------------------------------------------|----------------------------|
| `us-east-1`                                       | 380                        |
| `us-west-2`                                       | 250                        |
| `eu-central-1`                                    | 340                        |
| `eu-west-1`                                       | 280                        |
| `ap-southeast-1`                                  | 470                        |
| `luna-shackleton-1`, `mars-*`                     | 0 (solar)                  |
| `jupiter-europa-1`, `saturn-titan-1`, `voyager-1` | 0 (radioisotope)           |

```
abyss=> SHOW CARBON;
NOTICE:  compute (CPU time) ...... 0.014 s
NOTICE:  energy .................. 0.0001 Wh (at 15 W per busy core)
NOTICE:  grid intensity .......... 330 gCO2e/kWh (eu-west-1: 280 gCO2e/kWh; us-east-1: 380 gCO2e/kWh)
NOTICE:  compute emissions ....... 0.028 mg CO2e
NOTICE:  storage emissions ....... 0 g CO2e
NOTICE:  embodied carbon ......... 0 g CO2e (no storage hardware provisioned)
NOTICE:  water usage ............. 0 L
NOTICE:  storage rating .......... Net-Zero Storage™
```

## Capacity planning

Not required. Disk usage stays flat at 0 B regardless of write volume, on
every platform and in every region.
