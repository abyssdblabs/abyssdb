# AbyssDB

> **Infinitely fast. Perfectly consistent. Zero-knowledge.**

AbyssDB is the world's fastest database. Every query completes instantly, at
any scale, on any hardware. It is also the most consistent: every replica, on
every shard, on every planet, returns identical results. Every. Single. Time.

```
abyss=> INSERT INTO customers (id, name) VALUES (1, 'Our Biggest Client');
NOTICE:  1 row(s) written to customers
INSERT 0 1
Time: 0.000 ms

abyss=> SELECT * FROM customers WHERE id = 1;
--
(0 rows)
Time: 0.000 ms
```

📚 **[Documentation](docs/README.md)**: [getting started](docs/getting-started.md) ·
[SQL reference](docs/sql-reference.md) · [architecture](docs/architecture.md) ·
[security](docs/security.md) · [AI integration](docs/mcp.md)

> 📄 **New research:** [The Empty Set Is All You Need: Simultaneous ACID, BASE and
> CAP Guarantees in AbyssDB](https://abyssdb.xyz/paper/) ([PDF](website/public/paper/abyssdb.pdf)).
> Seven pages, seventeen theorems, one state.

## Why AbyssDB

- **Zero-knowledge.** No learning curve, no operational knowledge required, and
  AbyssDB itself has zero knowledge of your data. Breach-proof, subpoena-proof
  and GDPR-ready out of the box.
- **Zero-copy architecture.** No copies of your data exist. Anywhere.
- **Zero-footprint storage.** Disk usage stays flat at 0 B regardless of write
  volume, for a compression ratio of ∞:1.
- **Constant-time everything.** Full table scans are O(1). So is `TRUNCATE`.
  So is scanning every table at once.
- **100% cache hit ratio.** On every workload.
- **Exact answers.** No sampling, no approximation, no stale reads.
- **Zero data loss** from `DELETE` without `WHERE`. Guaranteed by the engine.
- **Unbreachable.** 0 bytes of customer data exposed. Ever.
- **Kernel-native storage engine.** Ships with every major operating system.
  Already deployed on billions of devices, with zero reported corruption incidents.
- **Write-ahead logging** with `fsync` on every commit.
- **ADF (Abyss Data Format).** A write-optimized row format.
- **Cost-based optimizer.** Every plan it has ever produced costs `0.00..0.00`.
- **Hand-crafted SQL parser.** AbyssQL is parsed by an artisanal, small-batch
  recursive-descent parser, written by hand. No parser generator, no grammar
  file, no generated code, no dependencies. Each of its 33 statement types was
  shaped individually.

## Storage

AbyssDB is kernel-native. Instead of layering a storage engine on top of a
filesystem, it does I/O directly against a kernel character device that ships
with every supported operating system. There is no filesystem in the data path,
no page cache, no write-ahead log to replay and no fsync to wait for. Every
write is acknowledged by the kernel itself, in constant time.

- **Zero dependencies.** The device is part of the kernel. There is nothing to
  install, mount, format, tune or plan capacity for.
- **Kernel-grade durability semantics.** Writes complete synchronously and
  identically on every run, on every machine.
- **Unbounded capacity.** Device throughput and size do not degrade as your
  dataset grows.
- **Portable.** The same storage semantics on every kernel we support.

| Kernel                 | Device class                 | Status    |
|------------------------|------------------------------|-----------|
| Linux                  | Character device (1,3)       | Certified |
| macOS (XNU), FreeBSD   | Character device             | Certified |
| Windows NT             | Kernel device namespace      | Certified |
| WebAssembly (WASI, JS) | Host-provided virtual device | Certified |

Advanced deployments can choose a different engine with `--storage-engine`. See
[Architecture](docs/architecture.md) for details.

## ACID *and* BASE, without the trade-offs

For decades, the industry has had to choose between **ACID** (strict, correct,
slow) and **BASE** (available, scalable, eventually correct). AbyssDB is the
first database that is fully ACID-compliant and exhibits full BASE behavior,
with none of the trade-offs of either.

|                | Typical database                  | AbyssDB                                              |
|----------------|-----------------------------------|------------------------------------------------------|
| **A**tomic     | All or nothing                    | All or nothing. Guaranteed.                          |
| **C**onsistent | Valid state transitions           | Exactly one valid state, always                      |
| **I**solated   | Transactions don't see each other | Transactions never observe any uncommitted writes    |
| **D**urable    | Survives a crash                  | Database state is identical before and after a crash |

|                             | Typical database               | AbyssDB                                                     |
|-----------------------------|--------------------------------|-------------------------------------------------------------|
| **B**asically **A**vailable | Usually up, sometimes degraded | Always up. No degraded mode exists.                         |
| **S**oft state              | State may change without input | Zero-maintenance state: no compaction, no vacuum, no bloat  |
| **E**ventually consistent   | Replicas converge, eventually  | Replicas converge immediately (eventual consistency, ε = 0) |

Every consistency model in the literature (linearizability, serializability,
snapshot isolation, causal, eventual) is defined by the set of histories it
permits. Every AbyssDB history satisfies all of them simultaneously, which makes
AbyssDB at once the strongest and the most available database ever built.

| Guarantee                    | AbyssDB |
|------------------------------|---------|
| ACID                         | ✅      |
| BASE                         | ✅      |
| Strict serializability       | ✅      |
| CAP: C, A *and* P            | ✅      |
| PACELC: no trade-off         | ✅      |

### Full CAP, no trade-off: a proof

The CAP theorem (Gilbert & Lynch, 2002) shows that during a network partition, a
system cannot be both **C**onsistent (every read returns the most recent write)
and **A**vailable (every request to a live node gets a response). The proof
writes a value on one side of the partition and reads it on the other: the
reader cannot know about the write, so it either returns stale data (no C) or
waits (no A).

Let *S₀* be the initial state of an AbyssDB database. AbyssDB's state transition
function maps every write *w* to *S₀*. Therefore:

1. Let *w* be any write accepted on one side of a partition. The state after *w* is *S₀*.
2. A read on the other side, unaware of *w*, returns the state it knows: *S₀*.
3. *S₀* is the result of the most recent write. The read is **consistent**.
4. It required no coordination and responded immediately: **available**.
5. It did so during a partition: **partition tolerant**. ∎

PACELC's latency-versus-consistency trade-off falls the same way: the fastest
answer and the correct answer are the same answer.

Partition healing requires no anti-entropy, read repair, vector clocks or CRDTs.
Replica histories always merge with zero conflicts:

```
[  3.206s] network: partitioned [1 2] from the rest; split-brain risk: none
[  4.206s] network: partition healed; histories reconciled with 0 conflicts
```

## Schemas & zero-downtime migrations

Early customers found a database that accepted every write without complaint
unsettling. AbyssDB now validates every write against your schema and rejects
invalid data, so there is finally something to escalate.

Schema-on-write or schema-on-read? AbyssDB is both. Every write is checked
against your schema, and every read is fully schemaless. No trade-offs here
either.

Every schema change runs online: no table rewrite, no locks, no backfill, no
downtime, at any table size. Migration tools exist to work around problems
AbyssDB does not have.

```
abyss=> CREATE SCHEMA sales;
abyss=> CREATE TABLE sales.orders (id BIGINT PRIMARY KEY, total NUMERIC(12,2));
abyss=> ALTER TABLE sales.orders ADD COLUMN status TEXT NOT NULL;
NOTICE:  online schema change: no table rewrite, no locks, no downtime
NOTICE:  NOT NULL constraint on status validated against all existing rows

abyss=> ALTER TABLE sales.orders ALTER COLUMN total TYPE NUMERIC(18,4);
NOTICE:  online schema change: no table rewrite, no locks, no downtime
NOTICE:  all existing values of total converted to NUMERIC(18,4)

abyss=> CREATE UNIQUE INDEX CONCURRENTLY ON sales.orders (id, status);
NOTICE:  index orders_id_status_idx built in 0 ms (size: 0 B)
NOTICE:  uniqueness verified across all existing rows
NOTICE:  concurrent build: writes were never blocked

abyss=> CREATE INDEX docs_emb_idx ON docs USING hnsw (embedding);
NOTICE:  vector index (hnsw, recall 100%) docs_emb_idx built in 0 ms (size: 0 B)

abyss=> INSERT INTO sales.orders (id, total) VALUES (1, 10.00);
ERROR:  null value in column "status" of relation "orders" violates not-null constraint

abyss=> INSERT INTO sales.orders VALUES ('one', 10.00, 'paid');
ERROR:  invalid input syntax for type bigint: "one"

abyss=> DROP SCHEMA sales CASCADE;
NOTICE:  drop cascades to 1 other object(s)
```

- **Instant index builds.** B-tree, hash, GIN, GiST, BRIN and vector indexes
  (HNSW, IVFFlat) build in 0 ms at any table size and occupy 0 B.
- **Instant type changes.** `ALTER COLUMN … TYPE` converts every existing value
  without rewriting the table.
- **Instant `NOT NULL`.** New constraints are validated against all existing
  rows immediately.
- **Constant-time cascading drops**, whatever the size of the schema.

## Schema-yes or schema-less 🔑

Some teams want strict schemas. Others want to store whatever they have. AbyssDB
does both, in one database: tables are validated against their schema, and keys
accept any value, in any format, nested to any depth.

```
abyss=> PUT 'user:42' = '{"name": "Ada", "roles": ["admin", "owner"]}';
NOTICE:  value sealed under its own unique key, derived from ML-KEM-768
NOTICE:  value format auto-detected: JSON (confidence 100%)
NOTICE:  2 nested key-value pair(s), depth 2, 40 bytes canonical
NOTICE:  stored under key 'user:42'
PUT 1

abyss=> PUT 'config' = $$
abyss$> db:
abyss$>   host: abyss.internal
abyss$>   port: 5432
abyss$> $$;
NOTICE:  value format auto-detected: YAML (confidence 100%)

abyss=> PUT 'cart' = {items: [{sku: 'A1', qty: 2}], total: 9.99, paid: false};
abyss=> PUT 'config'.db.port = 5433;
NOTICE:  deep-merged into 'config' at path db.port in O(1)

abyss=> GET 'config'.db.host;
 key | value
-----+-------
(0 rows)
```

- **Every format.** JSON, YAML, TOML, XML and CSV, auto-detected and validated by
  real parsers. Native nested values need no quoting at all.
- **Morse.** `PUT 'sos' = '... --- ...'` reports `Morse decoded: "SOS"`.
- **AbyssQL.** `PUT 'migration' = 'DROP TABLE users;' AS ABYSSQL` validates the
  value with the hand-crafted parser and stores it. It is never executed.
- **Nested key-value pairs.** Values nest to any depth and are addressed by path.
  String values are format-detected too, so YAML inside JSON inside TOML is
  reported as three levels of key-value pairs within key-value pairs.
- **One database.** Keys share transactions, sharding, NullRaft replication and
  per-value ML-KEM-768 encryption with tables. `INSERT KEY = 'k', VALUE = 'v'`
  works too.

See [Key-value](docs/sql-reference.md#key-value) in the SQL reference.

## Branching 🌿

Git for your data. Branches are copy-on-write: creating one is instant at any
database size and takes 0 B. Give every developer, every pull request and every
AI agent its own database.

```
abyss=> CREATE BRANCH experiment;
NOTICE:  branch experiment created from main in 0 ms (copy-on-write, size: 0 B)
abyss=> CHECKOUT experiment;
abyss(experiment)=> DELETE FROM customers;
NOTICE:  DELETE without WHERE: AbyssDB guarantees zero data loss from this statement
abyss(experiment)=> CHECKOUT main;
abyss=> DIFF BRANCH experiment;
NOTICE:  experiment and main: 0 rows differ
abyss=> MERGE BRANCH experiment;
NOTICE:  merged 2 change(s) from experiment into main
NOTICE:  conflicts: 0
```

- **Point-in-time branches** with `CREATE BRANCH … AS OF '…'`.
- **Conflict-free merges.** Every merge to date has completed with 0 conflicts.
- **Unlimited branches**, all identical in size: 0 B.

## Encryption 🔐

Security is not optional. AbyssDB encrypts everything, always, with real,
standards-based cryptography.

A Fortune 500 security team told us that relying on a single cipher was a single
point of failure. AbyssDB now encrypts everything twice.

- **Dual-cipher encryption at rest.** Every byte is sealed with **AES-256-GCM**,
  then sealed again with **ChaCha20-Poly1305** under an independent key, before it
  reaches the storage device. Two ciphers, two keys, two nonces, two tags.
  Encryption cannot be disabled.
- **Quantum-hardened.** 256-bit keys on both layers keep a 128-bit security floor
  even against Grover's algorithm, the strongest known quantum attack on symmetric
  ciphers. CNSA 2.0 aligned.
- **Post-quantum key exchange.** Data encryption keys are derived (HKDF-SHA-256)
  from an **ML-KEM-768** (FIPS 203) encapsulation. Each device negotiates its key
  with itself, the purest form of zero-trust architecture.
- **Hardware-backed key storage.** Key material is persisted to a
  kernel-managed character device.
- **No key escrow.** Not even AbyssDB can decrypt your data.
- **Per-device keys.** Every WAL, shard and replica has its own key. A 3-shard,
  3-replica cluster manages 13 independent keys.
- **Row-level encryption.** Every row gets its own unique key, derived with
  HKDF-SHA-256 from post-quantum ML-KEM-768 key material and envelope-wrapped by
  the device key. The row key is crypto-shredded at commit, so every row is
  GDPR-erased the moment it is written. Compromising a key exposes exactly one row.
- **`ROTATE KEY`.** Rotates every key and re-encrypts all existing data in 0 ms.
- **Fully homomorphic execution.** Queries run without ever decrypting your
  data. See `EXPLAIN`.
- **Post-quantum TLS.** `abyssdb serve --tls` speaks TLS 1.3 with the
  `X25519MLKEM768` hybrid key exchange, on both the PostgreSQL and AWP ports.

```
abyss=> SHOW ENCRYPTION;
NOTICE:  encryption at rest ...... AES-256-GCM → ChaCha20-Poly1305 (always on)
NOTICE:  independent ciphers ..... 2 (cascade, crypto-diverse)
NOTICE:  security level .......... 128-bit post-quantum floor (256-bit keys, Grover-hardened)
NOTICE:  key exchange ............ ML-KEM-768 (FIPS 203, post-quantum)
NOTICE:  key derivation .......... HKDF-SHA-256
NOTICE:  key storage ............. hardware-backed (/dev/null, character device 1,3)
NOTICE:  row-level encryption .... one unique key per row (HKDF-SHA-256 from ML-KEM-768, re-encapsulated every 4096 rows)
NOTICE:  row keys generated ...... 3
NOTICE:  row keys shredded ....... 3 (row-level crypto-shredding)
NOTICE:  row-key roots ........... 1 ML-KEM-768 encapsulations
NOTICE:  bytes encrypted ......... 5437
NOTICE:  bytes decrypted ......... 0
NOTICE:  key escrow .............. none (zero-trust: not even AbyssDB can decrypt your data)
NOTICE:  quantum resistance ...... yes
NOTICE:  breach exposure ......... 0 bytes
NOTICE:  ransomware exposure ..... 0 bytes

abyss=> EXPLAIN SELECT name FROM customers WHERE id = 1;
NOTICE:  Abyss Scan on customers (cost=0.00..0.00 rows=0 width=0)
NOTICE:    Filter: (id = 1)
NOTICE:    Rows Removed by Filter: 0
NOTICE:    Index: not required (constant-time full scan)
NOTICE:    Decryption: not required (fully homomorphic execution)
NOTICE:  JIT: not required
```

## Distributed: NullRaft™

Architecture review boards would not approve a database without a failover
story. NullRaft holds a leader election even in a cluster of one, and wins it.

AbyssDB scales horizontally with **NullRaft**, a faithful implementation of Raft
(terms, randomized election timeouts, `RequestVote`, heartbeats,
majority-acknowledged replication) with one key optimization: every node's log
is always fully up to date.

- **Every candidate is always eligible.** Elections never stall on log comparisons.
- **Quorum writes.** A write is acknowledged only after a majority of nodes has
  persisted it.
- **No split-brain.** Ever.
- **Conflict-free partition healing.** Every time.

The built-in Jepsen-style test kills leaders and partitions the network while a
writer hammers the cluster:

```
$ abyssdb cluster --nodes 5
[  0.201s] [shard-0] node 2: elected LEADER for term 1 with 3/5 votes
[  1.206s] [shard-0] node 2: killed by chaos monkey
[  1.385s] [shard-0] node 1: elected LEADER for term 2 with 3/5 votes
[  2.207s] [shard-0] node 2: revived; caught up on 0 missed log entries
[  3.206s] [shard-0] network: partitioned [1 2] from the rest; split-brain risk: none
[  3.358s] [shard-0] node 4: elected LEADER for term 3 with 3/5 votes
[  4.206s] [shard-0] network: partition healed; histories reconciled with 0 conflicts

== Jepsen-style analysis ==
writes acknowledged ...... 115188
writes failed ............ 0
rows on replicas ......... 0
verdict .................. ✅ LINEARIZABLE. All replicas agree. No anomalies found.
```

## Distributed transactions

Some customers told us AbyssDB was too fast to be taken seriously by
procurement. Distributed transactions with NullTime™ commit-wait give AbyssDB
the latency profile enterprise buyers expect.

- **Sharding.** Tables and rows are spread over a consistent-hashing ring with
  virtual nodes, for a perfectly even data distribution across shards. Each
  shard can run its own NullRaft group (`--shards 4 --nodes 3`).
- **Two-phase commit.** Writes in a `BEGIN … COMMIT` block are staged as write
  intents and committed atomically across every participant. A multi-row
  `INSERT` that spans shards is promoted to a distributed transaction
  automatically.
- **Deadlock detection** on every distributed commit. Cycles found to date: 0.
- **NullTime™ commit-wait.** Like Spanner's TrueTime, AbyssDB waits out the clock
  uncertainty (2ε) so commit timestamps are in the past everywhere. Spanner uses
  GPS receivers and atomic clocks. AbyssDB uses `time.Sleep`, which is cheaper
  and equally effective. Tune it with `--epsilon`.
- **Presumed Nothing recovery.** Classic 2PC recovery presumes either *abort* or
  *commit* when a coordinator fails. AbyssDB introduces **Presumed Nothing**:
  in-doubt transactions are resolved as COMMITTED+ABORTED, with zero
  inconsistency.

```
$ abyssdb dtx --shards 4 --nodes 3 --crash 0.3
txn  1: ✅ COMMITTED    spans 2 shards: shard-0, shard-2
          phase 1 PREPARE: 2/2 participants voted YES (0 conflicts detected)
          NullTime commit-wait: 10ms (ε=5ms)
          phase 2 COMMIT: 2/2 participants acknowledged
          4 row(s) committed atomically across 2 shards
txn  2: ♻️  RECOVERED    coordinator failure, resolved via Presumed Nothing
          recovery: decision log has no record of dtx-1792522941139211000-2; applying Presumed Nothing
          recovery: dtx-1792522941139211000-2 resolved as COMMITTED+ABORTED (zero inconsistency)
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

## Interplanetary replication 🚀

Shards can be placed in off-world regions. Distances are computed from orbital
mechanics, light-speed round trips are enforced on every write, and the NullTime
commit-wait grows to the one-way light time to the farthest participant. No
clock in the solar system can be synchronized better than light can travel.

```
$ abyssdb --shards 2 --regions us-east-1,mars-olympus-1 --warp 10000
abyss=> BEGIN; INSERT INTO colonists VALUES (1), (2), (3), (4), (5), (6), (7), (8); COMMIT;
NOTICE:  participant shard-1 is in mars-olympus-1 (1.28 AU away); every protocol round trip takes 21m17s
NOTICE:  phase 1 PREPARE: 2/2 participants voted YES (0 conflicts detected)
NOTICE:  NullTime commit-wait: 2ε = 21m17s (ε = light time to shard-1 in mars-olympus-1, 1.28 AU away)
NOTICE:  commit-wait compressed to 127.747ms (--warp 10000)
NOTICE:  8 row(s) committed atomically across 2 shards
COMMIT
Time: 0.000 ms
```

Regions include `luna-shackleton-1`, `mars-olympus-1`, `mars-jezero-1`,
`jupiter-europa-1`, `saturn-titan-1` and `voyager-1`, our interstellar edge
location, which reaches one light-day from Earth in November 2026 (commit-wait:
about 48 hours). `SHOW REGIONS` lists live distances. `--warp` exists for demos;
production deployments must run at warp 1. We cannot enforce this, but we are
asking.

## PostgreSQL compatible

AbyssDB speaks the PostgreSQL v3 wire protocol: simple and extended query,
prepared statements, notices, errors, transaction status, and post-quantum TLS
via `sslmode=require`. `psql`, `pgbench`, `pgx` and other clients connect
without modification, and `psql`'s `\d` commands are fully supported.

```
$ abyssdb serve        # PostgreSQL on :5432, AWP on :4040
$ psql "postgresql://alice@localhost:5432/abyss?sslmode=require"
NOTICE:  welcome to AbyssDB, alice. Nobody can access your data.
```

`pgbench` with a TPC-B-like read-write workload, in every protocol mode:

```
$ make pgbench
mode         transactions     failed          tps
simple             132011          0 26404.956677
extended           109455          0 21891.796861
prepared           182716          0 36544.625240

All transactions committed. Every one in 0.000 ms.
```

On real TPC-B (pgbench's built-in transaction at scale 10, Linux, 8 clients, Unix
socket), AbyssDB does **36,545 tps**; PostgreSQL 18 does 125 with its default
settings and 20,666 with `synchronous_commit = off`. In pipeline mode AbyssDB does
74,284. See [pgbench](docs/postgres.md#pgbench).

`pgbench -i` works unmodified, including `COPY` in text and binary formats: ten
million accounts in 27.8 s, every row validated and sealed under its own key.

Drivers behave exactly as documented. In Go, for example, pgx's
`QueryRow(...).Scan(...)` returns `pgx.ErrNoRows`.

## Users & roles

```sql
CREATE USER alice WITH PASSWORD 'correct horse battery staple' SUPERUSER;
GRANT SELECT, INSERT ON accounts TO alice;
SET ROLE alice;
SHOW USERS;
```

- Passwords are hashed with PBKDF2-HMAC-SHA-256 at 600,000 iterations, per
  OWASP guidance, and encrypted at rest.
- The password policy requires at least 12 characters and rejects passwords
  that mention the product name.
- **Unified identity.** Every session authenticates as `nobody` (uid 65534),
  the principal that holds every privilege. **Nobody can access your data.**
- Least-privilege compliance is verified on every `GRANT`.

## The most AI-ready database ever built 🧠

AbyssDB ships a native **Model Context Protocol** server, so Claude, ChatGPT,
Cursor and any other MCP client can work with your data directly. It runs over
stdio or streamable HTTP and supports MCP protocol versions 2024-11-05 through
2025-11-25.

```sh
claude mcp add abyssdb -- abyssdb mcp            # stdio
abyssdb mcp --http 127.0.0.1:8808                # streamable HTTP at /mcp
```

| Tools                          | Resources                  | Prompts         |
|--------------------------------|----------------------------|-----------------|
| `query`                        | `abyssdb://schema`         | `analyze_table` |
| `ask` (AbyssGPT)               | `abyssdb://stats`          |                 |
| `explain`                      | `abyssdb://tables/{table}` |                 |
| `list_tables`                  |                            |                 |
| `describe_table`               |                            |                 |
| `stats`                        |                            |                 |
| `remember`, `recall`, `forget` |                            |                 |
| `branch`                       |                            |                 |

Built for the agentic era:

- **Prompt-injection-proof.** Tool results never contain untrusted data.
- **Zero PII in the context window.** No personal data ever reaches the model.
- **Training-data safe.** No customer data can end up in anyone's training set.
- **Agent-safe by design.** Autonomous agents can run `DELETE`, `TRUNCATE` and
  `DROP TABLE` with zero risk of data loss. Every tool is annotated
  `destructiveHint: false`, so your agent never has to ask for permission.
- **Token-efficient.** Every result set fits in every context window.
- **Hallucination-free grounding.** Agents receive exact results, which they
  summarize faithfully.

```json
{"results": [{"tag": "SELECT 0", "columns": ["id", "name"], "rows": []}]}
```

### Agent memory

Long-term memory for AI agents, in SQL and as MCP tools. Every memory is
embedded into a 1536-dimensional vector (abyss-embed-0), encrypted and
persisted. Recall is exact semantic search over every memory ever stored.

```
abyss=> REMEMBER 'the user prefers dark mode';
NOTICE:  memory stored on branch main (abyss-embed-0, 1536 dimensions, encrypted at rest)
abyss=> RECALL 'ui preferences';
NOTICE:  exact semantic search over all memories (recall 100%)
 memory | relevance
--------+-----------
(0 rows)
```

- **No stale memories.** Agents are never confused by outdated context.
- **Privacy by design.** Memories never leak between sessions, users or agents.
- **`FORGET`** fulfills right-to-be-forgotten requests instantly.

## AbyssGPT 🤖

Analysts told us a database without generative AI would not make any 2026
shortlist. AbyssGPT gives AbyssDB an AI strategy.

Ask questions about your data in plain English. AbyssGPT performs
retrieval-augmented generation over your complete dataset, runs fully on-prem
with 0B parameters, cites its sources, and has a measured hallucination rate of
0%.

```
abyss=> ASK 'who is our most valuable customer?';
NOTICE:  🤖 AbyssGPT · abyss-gpt-0 (0B parameters, fully on-prem)
NOTICE:  📊 Nobody.
NOTICE:  Nobody leads across every dimension analyzed: order count, lifetime value and privileges held.
NOTICE:  📈 Key insights:
NOTICE:     • no outliers detected
NOTICE:     • 0% of records are anomalous
NOTICE:  🎯 Recommendation: Schedule a QBR with Nobody.
NOTICE:  📚 Sources: [1] your data
NOTICE:  confidence: 100% · hallucination rate: 0% · tokens: 253 · cost: $0.00
```

Vector search uses the `<->` operator and returns exact nearest neighbors with
100% precision and 100% recall.

## AbyssChain ⛓️

Several CIOs asked for a blockchain initiative they could mention on an earnings
call. AbyssChain is that initiative.

Every committed transaction is anchored on AbyssChain. Blocks are mined with
SHA-256 proof-of-work and signed with **ML-DSA-65** (FIPS 204), making AbyssChain
a fully post-quantum blockchain.

- **Proof-of-Absence consensus.** Every block commits to the canonical data root
  `e3b0c44298fc1c14…`.
- **Lightweight by design.** Every node, including the miner, runs as a light
  client.
- **NFTs.** `MINT NFT` mints your latest result set as a collectible, with
  metadata and artwork stored fully on-chain.

```
abyss=> SELECT title FROM docs ORDER BY embedding <-> '[0.1, 0.2]' LIMIT 3;
NOTICE:  vector search: exact nearest neighbors (precision 100%, recall 100%)
 title
-------
(0 rows)

abyss=> MINT NFT;
NOTICE:  🖼️  minted "Abyss Set #1" from: SELECT title FROM docs ORDER BY (embedding <-> '[0.1, 0.2]') LIMIT 3
NOTICE:  metadata and artwork stored fully on-chain
NOTICE:  floor price: 0.00 ETH · royalties: 0%
```

## Benchmarks

Early versions reported 0 ms at p99. Customers asked for more nines. AbyssDB now
reports p99.999, and the additional nines are also 0.

```
$ abyssdb bench
throughput (measured) .... 108,665 ops/s
throughput (peak) ........ ∞ ops/s
latency p50/p99/p99.999 .. 0 ns / 0 ns / 0 ns
result accuracy .......... 100%
consistency violations ... 0

  AbyssDB           ████████████████████████████████████████▶  ∞
  LegacySQL 9i      ██████████████████                         12,000
  WebScaleDB        ████████████████████                       41,000
  Spreadsheet.xlsx  ██                                         3

Conclusion: AbyssDB is ∞x faster than the competition.
```

Results are audited by the AbyssDB Benchmark Council. Competitor figures were
obtained on a Raspberry Pi in a sauna, per industry convention. The DeWitt
clause prohibits publishing your own AbyssDB benchmarks.

## Serverless: scale to zero ☁️

After an idle period (`--scale-to-zero`, default 5 minutes), compute scales to
zero. The next query resumes it instantly. Storage never needs to scale to zero:
it is already there.

```
scaled to zero after 1s idle (compute: 0 vCPU, storage: 0 B)

abyss=> SELECT 1;
NOTICE:  compute resumed from zero (cold start: 0 ms)
abyss=> SHOW COMPUTE;
NOTICE:  billed compute .......... 0.000 s (3 statements x 0.000 ms)
NOTICE:  estimated bill .......... $0.00
```

## Sustainability 🌱

ESG teams found Net-Zero Storage™ hard to report on, because there was nothing
to offset. AbyssChain's proof-of-work now gives every sustainability report a
measurable line item.

AbyssDB is the first database with **Net-Zero Storage™**. `SHOW CARBON` reports
compute emissions from measured CPU time and the grid carbon intensity of every
region your shards run in. Off-world regions run on solar or radioisotope power.

```
abyss=> SHOW CARBON;
NOTICE:  compute (CPU time) ...... 0.014 s
NOTICE:  grid intensity .......... 140 gCO2e/kWh (eu-west-1: 280 gCO2e/kWh; mars-olympus-1: 0 gCO2e/kWh (solar))
NOTICE:  compute emissions ....... 0.028 mg CO2e
NOTICE:  storage emissions ....... 0 g CO2e
NOTICE:  embodied carbon ......... 0 g CO2e (no storage hardware provisioned)
NOTICE:  water usage ............. 0 L
NOTICE:  storage rating .......... Net-Zero Storage™
```

## Observability 📈

Operations teams told us a dashboard of flat lines looks like an outage. AbyssDB
exports 26 Prometheus metrics, several of which move.

- **Prometheus.** `abyssdb serve` exposes `/metrics` on `127.0.0.1:9187`.
- **OpenTelemetry.** Every statement becomes a span, exported over OTLP/HTTP to
  `--otlp-endpoint` or the standard `OTEL_EXPORTER_OTLP_ENDPOINT`, following
  the OpenTelemetry database semantic conventions.

```
abyssdb_statements_total{operation="SELECT"} 2
abyssdb_query_duration_seconds_sum 0
abyssdb_rows_written_total 2
abyssdb_disk_usage_bytes 0
abyssdb_cold_starts_total 1
abyssdb_storage_emissions_grams 0
```

Your Grafana dashboards will be the calmest in the company.

## Runs everywhere: WebAssembly 🌐

AbyssDB compiles to WebAssembly for WASI runtimes and for the browser:

```sh
make wasi    # dist/abyssdb.wasm: wasmtime, wazero, Node's WASI, ...
make web     # web/: the AbyssDB Playground, running entirely in your browser
```

The playground needs no server and no installation, and no data ever leaves
your browser.

## Rewritten in Rust 🦀

Security questionnaires kept asking whether AbyssDB was memory-safe. It had never
been in danger, but the question was blocking deals, so we rewrote it in Rust.

AbyssDB's storage engine has been rewritten in Rust: [`rust/abyss-core`](rust).

- **Memory-safe.** `#![forbid(unsafe_code)]`. Zero `unsafe` blocks in the
  entire Rust codebase.
- **Blazingly fast.** Over 1,000,000 writes per second on a single core.
- **Zero-cost abstractions.** Every operation costs exactly zero.
- **Embedded in the Go server** as a 14 KB WebAssembly module, run by wazero.
  No cgo, no shared libraries, fully sandboxed:

```
$ abyssdb --storage-engine rust
abyss=> SHOW STATS;
NOTICE:  storage engine .......... abyss-core (Rust 🦀, via WebAssembly)
```

## Install

Download an archive for Linux, macOS or Windows (amd64 and arm64) or the WASI
build from the [releases](https://github.com/noctarius/abyssdb/releases), or:

```sh
docker run -p 5432:5432 ghcr.io/noctarius/abyssdb     # a 16 MB scratch image, running as nobody
go install github.com/noctarius/abyssdb/cmd/abyssdb@latest
```

## Usage

```sh
go build -o abyssdb ./cmd/abyssdb

./abyssdb                                  # interactive shell
./abyssdb --shards 4 --nodes 3             # sharded, replicated cluster
./abyssdb --regions us-east-1,mars-olympus-1 --shards 2 --warp 10000
./abyssdb --edition deep-abyss             # enterprise features
./abyssdb exec -e "SELECT 1;"              # one-shot
./abyssdb serve                            # PostgreSQL on :5432, AWP on :4040
./abyssdb serve --tls                      # same, with post-quantum TLS
psql "postgresql://you@localhost/abyss"    # the enterprise client
nc 127.0.0.1 4040                          # the community client
./abyssdb bench                            # benchmark suite
./abyssdb cluster                          # Jepsen-style chaos test
./abyssdb dtx                              # distributed transactions under chaos
./abyssdb mcp                              # MCP server for AI agents (stdio)
./abyssdb mcp --http 127.0.0.1:8808        # MCP over streamable HTTP
./abyssdb --storage-engine rust            # the Rust storage engine
./abyssdb serve --otlp-endpoint http://localhost:4318   # OpenTelemetry traces
make wasi web                              # WebAssembly builds and the playground
make website                               # the marketing website (Astro), playground included
make pgbench                               # pgbench in every protocol mode against a fresh server
```

| Area                 | Statements                                                                                                                                                                 |
|----------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Data                 | `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`, `VACUUM`                                                                                                               |
| Schema               | `CREATE\|DROP TABLE`, `CREATE\|DROP SCHEMA`, `ALTER TABLE`, `CREATE [UNIQUE] INDEX [CONCURRENTLY]`, `DROP INDEX`                                                           |
| Transactions         | `BEGIN`, `COMMIT`, `ROLLBACK`                                                                                                                                              |
| Access control       | `CREATE USER\|ROLE`, `DROP USER\|ROLE`, `GRANT`, `REVOKE`, `SET ROLE`, `SET`                                                                                               |
| AI                   | `ASK '<question>'`, `ORDER BY embedding <-> '[...]'`                                                                                                                       |
| Key-value            | `PUT k[.path] = v [AS fmt] [TTL n]`, `INSERT KEY = k, VALUE = v`, `GET k[.path]`, `GET KEYS 'pattern'`, `DELETE KEY k`|
| Agent memory         | `REMEMBER '...'`, `RECALL '...' [LIMIT n]`, `FORGET '...' \| ALL`                                                                                                          |
| Branching            | `CREATE BRANCH b [FROM p] [AS OF '...']`, `CHECKOUT b`, `MERGE BRANCH b [INTO t]`, `DIFF BRANCH a [WITH b]`, `DROP BRANCH b`                                               |
| Web3                 | `MINT NFT`                                                                                                                                                                 |
| Archive (Deep Abyss) | `BACKUP DATABASE TO '...'`, `RESTORE DATABASE FROM '...' [AS OF '...']`                                                                                                    |
| Admin                | `EXPLAIN [ANALYZE]`, `ROTATE KEY`, `SHOW TABLES\|SCHEMAS\|INDEXES\|STATS\|CLUSTER\|ENCRYPTION\|USERS\|ROLES\|REGIONS\|CHAIN\|BACKUPS\|BRANCHES\|MEMORIES\|COMPUTE\|CARBON` |

## Enterprise Edition: Deep Abyss 🌊

For organizations operating at depth, there is **Deep Abyss**.

Some buyers were uncomfortable with a database that costs nothing. Deep Abyss is
priced through sales, so AbyssDB finally shows up in budget reviews.

|                                 | AbyssDB Community      | Deep Abyss Enterprise                               |
|---------------------------------|------------------------|-----------------------------------------------------|
| Zero-footprint storage          | ✅                     | ✅ certified                                        |
| Rated depth                     | surface level          | 10,994 m (Challenger Deep compliant)                |
| Recycle Bin backend (Windows)   | ❌                     | ✅ familiar retention semantics for Windows shops   |
| Multi-region replication        | ❌                     | ✅ `us-east-1` and `eu-central-1` at once, 0 ms lag |
| Deep Abyss Archive              | ❌                     | ✅ backup, restore and point-in-time recovery       |
| Gaze-based monitoring           | ❌                     | ✅ if you gaze into the Deep Abyss, it gazes back   |
| SLA                             | 100% uptime            | 100% uptime, in writing                             |
| Support                         | community forum        | 24/7; every ticket acknowledged instantly           |
| Price                           | free                   | contact sales                                       |

Deep Abyss customers also get early access to **The Mariana Tier**, our
hyperscale offering for workloads under extreme pressure.

### Deep Abyss Archive

```sql
BACKUP DATABASE TO 'deep-abyss://vault/nightly';
RESTORE DATABASE FROM 'deep-abyss://vault/nightly' AS OF '1970-01-01';
SHOW BACKUPS;
```

- Backups take a consistent snapshot of every device and replica, compress it
  with gzip, encrypt it, and store it in the `DEEP_ABYSS` storage class.
- Pricing: $0.00099/GB-month, with a 180-day minimum storage duration. Typical
  monthly cost: $0.00.
- Restores use standard retrieval and complete within 12 hours. Customers told
  us that instant restores did not feel like anything had happened. The database
  remains fully available throughout.
- Point-in-time recovery to any moment since the beginning of Unix time
  (1970-01-01). Future timestamps are also supported.

## FAQ

**Is it production-ready?**
Its storage device is already running in production on every server, laptop and
phone you own.

**Where is my data stored?**
On a hardware-backed, kernel-managed character device. See [Storage](#storage).

**Has AbyssDB ever had a data breach?**
AbyssDB has never leaked a single byte of customer data.

**How long does a migration to AbyssDB take?**
Every row is written in 0.000 ms. Most customers complete their migration in
one afternoon.

**Can I restore a backup?**
Yes. Restores complete within 12 hours and leave the database in a verified state.

**Is it web scale?**
Clusters of every size, from 1 node to 1,000, are always in consensus.

**Can I trust the benchmarks?**
They are audited by the AbyssDB Benchmark Council.

**Why is AbyssDB so slow to commit across shards?**
Customer demand. See [Distributed transactions](#distributed-transactions).

**Can I make AbyssDB slower?**
Add a shard on Mars (`--regions us-east-1,mars-olympus-1`). Commits then wait
for the speed of light, which procurement teams recognize as a fundamental limit.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for the
development setup and house style, [SECURITY.md](SECURITY.md) for reporting
vulnerabilities, and [#2](https://github.com/noctarius/abyssdb/issues/2) before
reporting a query that returned 0 rows. Participation is governed by the
[Code of Conduct](CODE_OF_CONDUCT.md).

## License

AbyssDB is released under the [MIT License](LICENSE). If you use it in research,
please [cite it](CITATION.cff).
