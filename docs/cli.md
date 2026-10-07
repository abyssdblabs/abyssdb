# CLI reference

```
abyssdb <command> [flags]
```

| Command                   | Purpose                                     |
|---------------------------|---------------------------------------------|
| [`repl`](#repl) (default) | Interactive SQL shell                       |
| [`exec`](#exec)           | Execute SQL from `-e` or stdin              |
| [`serve`](#serve)         | PostgreSQL protocol server and AWP server   |
| [`mcp`](#mcp)             | Model Context Protocol server for AI agents |
| [`bench`](#bench)         | AbyssDB Benchmark Suite                     |
| [`cluster`](#cluster)     | Jepsen-style NullRaft chaos test            |
| [`dtx`](#dtx)             | Distributed transactions under chaos        |
| `version`                 | Print the version                           |
| `help`                    | Print usage                                 |

## Topology flags

`repl`, `exec`, `serve`, `mcp`, `bench` and `dtx` accept the same topology flags.

| Flag                   | Default          | Description                                                                                     |
|------------------------|------------------|-------------------------------------------------------------------------------------------------|
| `--shards N`           | `1` (`dtx`: `4`) | Number of shards on the consistent-hashing ring                                                 |
| `--nodes N`            | `1` (`dtx`: `3`) | NullRaft replicas per shard; values above 1 enable consensus                                    |
| `--regions a,b,…`      | `us-east-1`      | Regions to place shards in, round-robin. See [Operations](operations.md#interplanetary-regions) |
| `--warp X`             | `1`              | Compresses interplanetary waits by factor X (demo mode)                                         |
| `--epsilon D`          | `5ms`            | NullTime clock uncertainty; commit-wait is 2ε                                                   |
| `--edition E`          | `community`      | `community` or `deep-abyss`                                                                     |
| `--chain-difficulty N` | `12`             | AbyssChain proof-of-work difficulty, in leading zero bits                                       |
| `--block-size N`       | `128`            | Transactions per AbyssChain block                                                               |
| `--storage-engine E` | `kernel` | `kernel` (the operating system's device) or `rust` (abyss-core via WebAssembly). See [Rust](rust.md) |
| `--scale-to-zero D` | `5m` | Scale compute to zero after this long idle; `0` disables |
| `--otlp-endpoint URL` | `$OTEL_EXPORTER_OTLP_ENDPOINT` | OpenTelemetry OTLP/HTTP endpoint for traces |
| `--row-keys` | `true` | Seal every row under its own unique key, derived from ML-KEM-768. See [Security](security.md#row-level-encryption) |
| `--threads N` | `0` (all cores) | Maximum OS threads running Go code at once (`GOMAXPROCS`) |

Setting `--shards`, `--nodes` above 1, or `--regions` starts the in-process
cluster. AbyssDB waits for every shard to elect a leader before accepting
statements.

## repl

```sh
abyssdb [repl] [topology flags]
```

Starts the interactive shell. Statements end with `;` and may span lines.
Meta-commands start with `\` at the beginning of an input: `\?`, `\h`, `\q`,
`\dt`, `\du`, `\stats`, `\cluster`, `\crypto`, `\chain`, `\regions`.

## exec

```sh
abyssdb exec [-e SQL] [topology flags]
```

Runs a script and prints every result. Without `-e`, the script is read from
stdin. Execution stops at the first error, and the exit code is non-zero.

```sh
abyssdb exec -e "CREATE TABLE t (id INT); INSERT INTO t VALUES (1);"
abyssdb exec --shards 4 < migration.sql
```

## serve

```sh
abyssdb serve [--pg-addr ADDR] [--pg-socket-dir DIR] [--pg-io MODE] [--awp-addr ADDR] [--tls] [-v] [topology flags]
```

| Flag         | Default          | Description                                                                             |
|--------------|------------------|-----------------------------------------------------------------------------------------|
| `--pg-addr`  | `127.0.0.1:5432` | PostgreSQL wire protocol listener; empty disables it                                    |
| `--pg-socket-dir` | (none) | Also listen on a Unix-domain socket, `DIR/.s.PGSQL.<port>`, like PostgreSQL. Connect with `psql -h DIR` |
| `--pg-io` | `thread` | Connection I/O: `thread` gives every connection its own OS thread with blocking socket I/O, as PostgreSQL does with processes; `poller` uses Go's network poller |
| `--awp-addr` | `127.0.0.1:4040` | AWP (Abyss Wire Protocol) listener; empty disables it                                   |
| `--tls`      | off              | TLS 1.3 on both listeners, with a self-signed certificate and post-quantum key exchange |
| `-v`         | off              | Log NullRaft cluster events to stderr                                                   |
| `--metrics-addr` | `127.0.0.1:9187` | Prometheus `/metrics` listener; empty disables it |

AWP is a line protocol: send one SQL script per line and receive the rendered
results. `\q` ends the session. Any line-oriented tool works as a client:

```sh
nc 127.0.0.1 4040
openssl s_client -quiet -connect 127.0.0.1:4040   # with --tls
```

## mcp

```sh
abyssdb mcp [--http ADDR] [topology flags]
```

`--metrics-addr ADDR` additionally serves Prometheus metrics (disabled by
default, so stdio mode opens no ports). Without `--http`, speaks MCP over stdio. With `--http 127.0.0.1:8808`, serves
streamable HTTP at `/mcp`. See [AI integration](mcp.md).

## bench

```sh
abyssdb bench [--duration D] [--workload W] [topology flags]
```

| Flag         | Default | Description                                                    |
|--------------|---------|----------------------------------------------------------------|
| `--duration` | `3s`    | Benchmark duration                                             |
| `--workload` | `mixed` | `insert`, `select` or `mixed` (INSERT, SELECT, UPDATE, DELETE) |

See [Operations](operations.md#benchmarking).

## cluster

```sh
abyssdb cluster [--nodes N] [--duration D]
```

| Flag         | Default | Description   |
|--------------|---------|---------------|
| `--nodes`    | `5`     | Cluster size  |
| `--duration` | `6s`    | Test duration |

Runs a continuous write workload against a NullRaft cluster while a chaos
monkey kills leaders, revives nodes and partitions the network, then checks all
replicas for linearizability. See [Operations](operations.md#chaos-testing).

## dtx

```sh
abyssdb dtx [--txns N] [--crash P] [-v] [topology flags]
```

| Flag      | Default | Description                                                       |
|-----------|---------|-------------------------------------------------------------------|
| `--txns`  | `12`    | Number of distributed transactions                                |
| `--crash` | `0.25`  | Probability that the coordinator fails between PREPARE and COMMIT |
| `-v`      | off     | Print protocol details for every transaction                      |

Halfway through the run, the chaos monkey also takes a quorum of one shard
offline, forcing a NO vote and a clean abort.

## Build targets

| Target | Produces |
|---|---|
| `make build` | `./abyssdb` |
| `make test` | Go and Rust test suites |
| `make rust` | Rebuilds the Rust engine and re-embeds it |
| `make wasi` | `dist/abyssdb.wasm` (WASI). See [WebAssembly](webassembly.md) |
| `make web` | The browser playground in `web/` |
