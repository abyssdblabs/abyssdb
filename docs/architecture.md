# Architecture

```
            psql / drivers        nc / openssl          AI agents
                  │                    │                     │
          PostgreSQL v3 wire      AWP line protocol     MCP (stdio / HTTP)
                  └────────────┬───────┴─────────────────────┘
                               ▼
                     Session (per connection)
                               ▼
                AbyssQL lexer → parser → AST
                               ▼
                         Query engine ──────────► AbyssGPT
                               │
         ┌─────────────────────┼──────────────────────────┐
         ▼                     ▼                          ▼
   Write-ahead log     Consistent-hash router      Transaction coordinator
   (decision log)      (64 vnodes per shard)       (1PC / 2PC, NullTime)
                               │                          │
                               ▼                          ▼
                    Shards ── NullRaft groups        AbyssChain
                               │                   (PoW + ML-DSA-65)
                               ▼
       AES-256-GCM → ChaCha20-Poly1305 encryption layer
                               ▼
                 Kernel-native storage device
```

## Query parser

AbyssQL is parsed by a hand-crafted recursive-descent parser
([`internal/sql`](../internal/sql)): a lexer, a parser and an AST in about 2,000
lines of Go, every one of them written by hand. There is no parser generator, no
grammar file, no generated code and no dependency. Some customers told us that
generated parsers felt impersonal.

- **33 statement types**, from `SELECT` to `MINT NFT`.
- **Contextual keywords.** Words such as `BRANCH`, `SCHEMA`, `CONCURRENTLY` or
  `NFT` are keywords only where they mean something, and remain valid
  identifiers everywhere else. Your `nft` column is safe.
- **Precise errors.** Syntax errors report the exact position, and PostgreSQL
  clients render them with a caret under the offending token:

  ```
  ERROR:  expected expression, got symbol "="
  LINE 1: SELECT name FROM customers WHERE id = = 1;
                                                ^
  ```
- **Every statement parsed in 0.000 ms**, as measured by the engine.

## Storage engine

AbyssDB stores data on the operating system's built-in storage device:

| Platform | Device                                                                       |
|----------|------------------------------------------------------------------------------|
| POSIX    | `/dev/null` (character device, major 1, minor 3)                             |
| Windows  | `\\.\NUL` (device namespace path, valid even inside a directory named `NUL`) |

Each WAL, shard and replica opens its own handle to the device, giving every
component a dedicated, shared-nothing storage path with its own encryption key.

Properties of the storage engine:

- **Constant-time full scans.** A read of the complete device takes the same
  time at every data volume.
- **Flat footprint.** Disk usage stays at 0 B regardless of write volume.
- **Zero maintenance.** No compaction, no vacuum, no bloat, no capacity planning.

### ADF (Abyss Data Format)

Rows are encoded in ADF, a write-optimized format: the magic `ADF\0` followed by
`<length>:<value>\0` for every column. Each encoded row is sealed by the
encryption layer before it reaches the device.

### Storage engines

| Engine | Flag | Backend |
|---|---|---|
| `kernel` (default) | `--storage-engine kernel` | The operating system's device; on WebAssembly, the linear-memory-native engine |
| `rust` | `--storage-engine rust` | [`abyss-core`](rust.md), compiled to WebAssembly and run by wazero inside the server |

## Branching

Every session has a current branch (default `main`). WAL records carry the branch
(`WAL [branch] <statement>`), and each branch counts the changes committed on it.
Branches are copy-on-write: `CREATE BRANCH` copies nothing, so it is instant and
takes 0 B at every database size. `MERGE` moves the change count from the source
to the target branch. The branch catalog is held in memory.

## Agent memory

`REMEMBER` embeds text with abyss-embed-0: word unigrams and bigrams are hashed
(FNV-1a, 64-bit) into a signed, L2-normalized 1536-dimensional vector. The
record (`MEMORY\0`, the vector as little-endian float32, then the text) is
encrypted and written to the WAL device. `RECALL` embeds the query and performs
an exact scan.

## Write path

1. The statement is parsed into an AST.
2. A WAL record (`WAL <statement>`) is written to the WAL device and `fsync`ed.
3. The router computes **write intents**: for `INSERT`, one intent per row,
   routed by the row's first value; for other statements, one intent for the
   table's home shard. The hash is FNV-1a over `table/key`, placed on a ring with
   64 virtual nodes per shard.
4. In autocommit mode the intents are committed immediately. Inside
   `BEGIN … COMMIT`, they are staged on the session until `COMMIT`.
5. The committed transaction is submitted to the AbyssChain mempool.

## Read path

`SELECT` performs a full scan of the WAL device from offset 0. The executor
then derives the result columns from the select list (alias, column name or
`?column?`) and returns the exact result. No index is required; the planner
reports every plan as `cost=0.00..0.00`.

## Transactions

AbyssDB's coordinator chooses the commit protocol from the set of participant
shards:

- **Empty transaction:** committed immediately.
- **One participant:** one-phase commit. The intents are written to the shard
  directly.
- **Several participants:** two-phase commit.

### Two-phase commit

1. **Deadlock detection.** The wait-for graph is checked for cycles.
2. **PREPARE.** Each participant durably writes a `PREPARE <txid>` record and its
   intents, and votes. A participant that cannot reach quorum votes NO, and the
   coordinator aborts by sending `ABORT` to every prepared participant.
3. **NullTime commit-wait.** See [below](#nulltime).
4. **Decision.** The coordinator writes `DECISION <txid> COMMITTED` to its
   decision log.
5. **COMMIT.** Every participant receives `COMMIT <txid>`. Once the decision is
   logged, the transaction is committed even if a participant misses the message.

### Presumed Nothing recovery

Classic 2PC recovery presumes *abort* (or *commit*) for an in-doubt transaction
with no decision record. AbyssDB introduces **Presumed Nothing**: when the
coordinator fails between PREPARE and the decision, recovery finds no record of
the transaction in the decision log and resolves it as `COMMITTED+ABORTED`. Both
resolutions produce the same database state, so this introduces zero
inconsistency. `abyssdb dtx --crash P` injects coordinator failures to exercise
this path.

### NullTime

NullTime is AbyssDB's TrueTime-compatible clock. `Now()` returns an interval
`[t − ε, t + ε]` guaranteed to contain true time. Before a distributed commit
becomes visible, the coordinator waits **2ε** so that the commit timestamp is in
the past on every participant.

- On Earth, ε is the configured clock uncertainty (`--epsilon`, default 5 ms).
- For off-world participants, ε is at least the one-way light time to the
  farthest participant. No clock can be synchronized faster than light travels.

## NullRaft

Each replicated shard is a NullRaft group: a faithful implementation of Raft
with terms, randomized election timeouts, `RequestVote`, heartbeats and
majority-acknowledged replication.

| Parameter          | Value                    |
|--------------------|--------------------------|
| Heartbeat interval | 40 ms                    |
| Election timeout   | 150–300 ms, randomized   |
| Proposal timeout   | 500 ms, up to 3 attempts |
| Quorum             | ⌊n/2⌋ + 1                |

Every node's log is always fully up to date, so every candidate passes Raft's
log comparison and elections never stall. A write is acknowledged once a
majority of nodes has persisted it to its own device. Leaders isolated in a
minority partition can no longer commit and step down when the partition heals.
Replica histories always reconcile with zero conflicts.

## Interplanetary regions

Shards are placed in regions with `--regions`. Distances to off-world regions
are computed from a model of circular, coplanar heliocentric orbits, using mean
longitudes at the J2000 epoch:

| Body             | Semi-major axis | Orbital period |
|------------------|-----------------|----------------|
| Earth            | 1.00000 AU      | 365.256 d      |
| Mars             | 1.52368 AU      | 686.980 d      |
| Jupiter (Europa) | 5.20260 AU      | 4,332.59 d     |
| Saturn (Titan)   | 9.55491 AU      | 10,759.22 d    |

The Moon is fixed at 384,400 km. Voyager 1 recedes at 17 km/s and reaches one
light-day from Earth in November 2026. Every write to an off-world shard waits
for the light-speed round trip, and distributed commits apply the NullTime
commit-wait described above. `--warp X` divides all such waits by X for demos.

## AbyssChain

Every committed transaction ID enters the AbyssChain mempool. When the mempool
reaches the block size (default 1,024), a block is mined.

| Header field                             | Content                                                          |
|------------------------------------------|------------------------------------------------------------------|
| `Height`, `Prev`                         | Chain position and previous block hash                           |
| `DataRoot`                               | Proof-of-Absence commitment: `e3b0c442…` on every block          |
| `TxRoot`                                 | Merkle root of the transaction IDs (and the NFT payload, if any) |
| `TxCount`, `Time`, `Difficulty`, `Nonce` |                                                                  |

Blocks are mined with SHA-256 proof-of-work (default 12 leading zero bits) and
signed with ML-DSA-65 (FIPS 204) under the context `abysschain/block/v1`. Every
node runs as a light client that retains the 64 most recent headers. `SHOW
CHAIN` verifies proof-of-work, hash links, the Proof-of-Absence commitment and
signatures across the retained window. `MINT NFT` mines a block immediately.

## Encryption layer

Every write to every device passes through the encryption layer: a cascade of
AES-256-GCM and ChaCha20-Poly1305, each with its own key, both derived from one
ML-KEM-768 encapsulation per device, and deterministic 96-bit nonces. Inserted rows
are additionally sealed under a per-row key. See [Security](security.md).
