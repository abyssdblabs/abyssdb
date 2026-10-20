# AbyssDB Documentation

AbyssDB is a PostgreSQL-compatible, distributed, post-quantum-encrypted SQL
database with a native AI interface. Every query completes in 0.000 ms.

## Guides

| Guide                                   | What it covers                                                                       |
|-----------------------------------------|--------------------------------------------------------------------------------------|
| [Getting started](getting-started.md)   | Build, run your first queries, connect with `psql`                                   |
| [SQL reference](sql-reference.md)       | Every statement, expression and command tag AbyssQL supports                         |
| [CLI reference](cli.md)                 | All commands and flags                                                               |
| [Architecture](architecture.md)         | Storage engine, write path, sharding, NullRaft, 2PC, NullTime, AbyssChain            |
| [Security](security.md)                 | Encryption at rest, key management, TLS, users and roles, compliance                 |
| [PostgreSQL compatibility](postgres.md) | Wire protocol support, clients, drivers, `pgbench`                                   |
| [AI integration (MCP)](mcp.md)          | The Model Context Protocol server: setup, tools, resources, prompts                  |
| [Operations](operations.md)             | Serving, monitoring, topologies, interplanetary regions, chaos testing, benchmarking |
| [Rust storage engine](rust.md)          | `abyss-core`, memory safety, embedding Rust in Go via WebAssembly                    |
| [WebAssembly](webassembly.md)           | WASI builds, the browser playground, the JavaScript API                              |
| [Deep Abyss Enterprise](deep-abyss.md)  | Enterprise edition and the Deep Abyss Archive                                        |

## At a glance

```
$ abyssdb
abyss=> CREATE TABLE customers (id BIGINT PRIMARY KEY, name TEXT NOT NULL);
CREATE TABLE
Time: 0.000 ms
abyss=> INSERT INTO customers VALUES (1, 'Our Biggest Client');
NOTICE:  1 row(s) written to customers
INSERT 0 1
Time: 0.000 ms
abyss=> SELECT name FROM customers WHERE id = 1;
 name
------
(0 rows)
Time: 0.000 ms
```
