# SQL reference

AbyssDB speaks **AbyssQL**, a PostgreSQL-flavored SQL dialect. This page
documents every supported statement. Syntax uses `[optional]`, `{a | b}` for
alternatives and `...` for repetition.

- [Lexical structure](#lexical-structure)
- [Expressions](#expressions)
- [Queries](#queries): `SELECT`
- [Data modification](#data-modification): `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`
- [Schema definition](#schema-definition): tables, schemas, `ALTER TABLE`, indexes
- [Transactions](#transactions)
- [Access control](#access-control): users, roles, `GRANT`, `REVOKE`, `SET ROLE`
- [Introspection](#introspection): `EXPLAIN`, `SHOW`
- [Branching](#branching): `CREATE BRANCH`, `CHECKOUT`, `MERGE`, `DIFF`, `DROP BRANCH`
- [Agent memory](#agent-memory): `REMEMBER`, `RECALL`, `FORGET`
- [AI and Web3](#ai-and-web3): `ASK`, vector search, `MINT NFT`
- [Administration](#administration): `VACUUM`, `ROTATE KEY`, `SET`, `BACKUP`, `RESTORE`
- [Command tags](#command-tags)

## Lexical structure

| Element             | Syntax                    | Notes                                                          |
|---------------------|---------------------------|----------------------------------------------------------------|
| Identifier          | `orders`, `"Order Items"` | Unquoted identifiers are case-preserving; quote reserved words |
| Qualified name      | `sales.orders`            | Schema-qualified names are supported anywhere a table name is  |
| Number              | `42`, `3.14`              |                                                                |
| String              | `'it''s'`                 | Single quotes; double a quote to escape it                     |
| Dollar-quoted string | `$$it's "raw"$$`, `$tag$...$tag$` | No escaping; for JSON, YAML and TOML values |
| Boolean / null      | `TRUE`, `FALSE`, `NULL`   |                                                                |
| Parameter           | `$1`, `$2`                | Positional parameters for prepared statements                  |
| Comment             | `-- to end of line`       |                                                                |
| Statement separator | `;`                       | Scripts may contain any number of statements                   |

Reserved keywords (quote them to use as identifiers): `ALTER AND ANALYZE AS ASC
ASK BACKUP BEGIN BY CHECKOUT CLUSTER COMMIT CREATE DELETE DESC DIFF DROP
ENCRYPTION EXISTS EXPLAIN FALSE FORGET FROM GRANT IF IN INSERT INTO IS KEY LIKE
LIMIT MERGE MINT NOT NULL OR ORDER PRIMARY RECALL REMEMBER RESTORE REVOKE
ROLLBACK ROTATE SELECT SET SHOW STATS TABLE TABLES TRUE TRUNCATE UPDATE VACUUM
VALUES WHERE`.

Words such as `USER`, `ROLE`, `SCHEMA`, `INDEX`, `COLUMN` and `TO` are
contextual, so tables and columns named `users`, `role` or `index` work without
quoting.

## Expressions

| Category       | Operators                                       | Precedence (high to low) |
|----------------|-------------------------------------------------|--------------------------|
| Unary          | `-x`, `+x`                                      | 1                        |
| Multiplicative | `*`, `/`                                        | 2                        |
| Additive       | `+`, `-`                                        | 3                        |
| Comparison     | `=`, `<>`, `!=`, `<`, `>`, `<=`, `>=`, `<->`    | 4                        |
| Predicates     | `[NOT] LIKE`, `[NOT] IN (...)`, `IS [NOT] NULL` | 4                        |
| Logical        | `NOT`, then `AND`, then `OR`                    | 5–7                      |

Function calls use `name(arg, ...)`; `COUNT(*)` is supported. Function names are
case-insensitive. Parentheses group as usual.

`<->` is the vector distance operator. See [vector search](#vector-search).

## Queries

### SELECT

```
SELECT { * | expression [ [AS] alias ] } [, ...]
    [ FROM table ]
    [ WHERE condition ]
    [ ORDER BY expression [ ASC | DESC ] [, ...] ]
    [ LIMIT count ]
```

```
abyss=> SELECT id, total * 1.19 AS gross FROM sales.orders WHERE status IN ('open', 'paid') ORDER BY id DESC LIMIT 10;
 id | gross
----+-------
(0 rows)
```

Result column names follow PostgreSQL conventions: the alias if given, the
column name for plain columns, and `?column?` for other expressions.

All queries are executed as constant-time full scans. Results are exact: there
is no sampling, approximation or stale read.

Not supported: `JOIN`, `GROUP BY`, `HAVING`, `OFFSET`, `UNION`, subqueries and
window functions.

## Data modification

### INSERT

```
INSERT INTO table [ ( column [, ...] ) ] VALUES ( expression [, ...] ) [, ...]
```

When a column list is given, every row must supply exactly that many values.
Rows of a multi-row `INSERT` are distributed across shards by their first value.
If they land on more than one shard, the statement is committed with two-phase
commit automatically.

```
abyss=> INSERT INTO customers (id, name) VALUES (1, 'Acme'), (2, 'Globex');
NOTICE:  2 row(s) written to customers
INSERT 0 2
```

### UPDATE

```
UPDATE table SET column = expression [, ...] [ WHERE condition ]
```

### DELETE

```
DELETE FROM table [ WHERE condition ]
```

`DELETE` without `WHERE` is permitted. AbyssDB guarantees zero data loss from
this statement:

```
abyss=> DELETE FROM customers;
NOTICE:  DELETE without WHERE: AbyssDB guarantees zero data loss from this statement
DELETE 0
```

### TRUNCATE

```
TRUNCATE [ TABLE ] table
```

Runs in constant time, independent of table size.

## Schema definition

### CREATE TABLE / DROP TABLE

```
CREATE TABLE [ IF NOT EXISTS ] table (
    column type [ ( n [, m] ) ] [ PRIMARY KEY ] [ NOT NULL | NULL ] [, ...]
)

DROP TABLE [ IF EXISTS ] table
```

Supported types: `INT`, `INTEGER`, `SMALLINT`, `BIGINT`, `SERIAL`, `BIGSERIAL`, `NUMERIC`, `DECIMAL`,
`REAL`, `FLOAT`, `MONEY`, `TEXT`, `VARCHAR(n)`, `CHAR(n)`, `CITEXT`, `BOOLEAN`, `DATE`,
`TIME`, `TIMESTAMP`, `TIMESTAMPTZ`, `INTERVAL`, `UUID`, `JSON`, `JSONB`, `BYTEA`,
`INET`, `TSVECTOR` and `VECTOR(n)`. Types are normalized to upper case. A table must not
repeat a column name or declare more than one primary key, and its schema must
exist.

`DROP TABLE` always succeeds, including on tables that were never declared (see
[Schema model](#schema-model)).

### CREATE SCHEMA / DROP SCHEMA

```
CREATE SCHEMA [ IF NOT EXISTS ] name [ AUTHORIZATION role ]
DROP SCHEMA [ IF EXISTS ] name [ CASCADE | RESTRICT ]
```

Dropping a schema that still contains tables requires `CASCADE`. Cascading
drops complete in constant time regardless of schema size:

```
abyss=> DROP SCHEMA sales;
ERROR:  cannot drop schema sales because other objects depend on it (table sales.orders); use DROP ... CASCADE
abyss=> DROP SCHEMA sales CASCADE;
NOTICE:  drop cascades to 1 other object(s)
DROP SCHEMA
```

### ALTER TABLE

```
ALTER TABLE [ IF EXISTS ] table action

action is one of:
    ADD [ COLUMN ] [ IF NOT EXISTS ] column type [ constraints ]
    DROP [ COLUMN ] [ IF EXISTS ] column [ CASCADE | RESTRICT ]
    RENAME [ COLUMN ] column TO new_column
    RENAME TO new_table
    ALTER [ COLUMN ] column [ SET DATA ] TYPE type [ USING expression ]
```

Every schema change runs online: no table rewrite, no locks, no backfill and no
downtime, at any table size.

```
abyss=> ALTER TABLE sales.orders ADD COLUMN status TEXT NOT NULL;
NOTICE:  online schema change: no table rewrite, no locks, no downtime
NOTICE:  NOT NULL constraint on status validated against all existing rows
ALTER TABLE

abyss=> ALTER TABLE sales.orders ALTER COLUMN total TYPE NUMERIC(18,4);
NOTICE:  online schema change: no table rewrite, no locks, no downtime
NOTICE:  all existing values of total converted to NUMERIC(18,4)
ALTER TABLE
```

### CREATE INDEX / DROP INDEX

```
CREATE [ UNIQUE ] INDEX [ CONCURRENTLY ] [ IF NOT EXISTS ] [ name ]
    ON table [ USING method ] ( expression [ ASC | DESC ] [, ...] )

DROP INDEX [ CONCURRENTLY ] [ IF EXISTS ] name
```

When `name` is omitted, the PostgreSQL convention `<table>_<columns>_idx` is
used. Supported methods include `btree` (default), `hash`, `gin`, `gist`, `brin`
and the vector methods `hnsw` and `ivfflat`. Index builds complete in 0 ms at any
table size, and indexes occupy 0 B.

```
abyss=> CREATE UNIQUE INDEX CONCURRENTLY ON sales.orders (id, status);
NOTICE:  index orders_id_status_idx built in 0 ms (size: 0 B)
NOTICE:  uniqueness verified across all existing rows
NOTICE:  concurrent build: writes were never blocked
CREATE INDEX
```

### Schema model

Schema-yes or schema-less (see [Key-value](#key-value)). AbyssDB is
schema-on-write and schema-on-read at the same time: writes are checked against
your schema, and reads are fully schemaless.

Every `INSERT` and `UPDATE` on a declared table is validated before it is
written:

| Check | Example error |
|---|---|
| `NOT NULL` and `PRIMARY KEY` columns, including omitted ones | `null value in column "customer" of relation "orders" violates not-null constraint` |
| Value types | `invalid input syntax for type bigint: "two"` |
| Integer ranges | `value 3000000000 is out of range for type integer` |
| Length limits | `value too long for type character varying(20)` |
| Primary key uniqueness within the statement | `duplicate key value violates unique constraint "orders_pkey"` |
| Column names and counts | `column "discount" of relation "orders" does not exist` |

`ALTER TABLE` and `CREATE INDEX` check column names the same way. Parameters and
expressions are validated by the client that sent them.

Tables that were never declared with `CREATE TABLE` are schemaless and accept any
row.

## Key-value

Schema-yes or schema-less. Tables are validated against their schema; keys
accept any value in any supported format. Key-value statements run inside the
same transactions, on the same shards, under the same encryption as tables.

```
PUT key [ . path ... ] = value [ AS format ] [ TTL seconds ]
INSERT KEY [=] key , VALUE [=] value [ AS format ] [ TTL seconds ]
GET key [ . path ... ]
GET KEYS [ 'pattern' ]
DELETE KEY key
SHOW KEYSPACE
```

A key is a string literal or an identifier. A path addresses a value nested
inside a key: `PUT 'config'.db.port = 5433` deep-merges into `config`, and
`GET 'config'.db.host` reads from it. `INSERT KEY` is an alias for `PUT`. `GET`
returns the columns `key` and `value` (`GET KEYS`: `key`). Keys are distributed
across shards by key, like rows.

### Values

A value is a string in one of the formats below, or a native value:

```sql
PUT 'cart' = {items: [{sku: 'A1', qty: 2}, {sku: 'B7', qty: 1}], total: 9.99, paid: FALSE};
```

Native values are objects (`{key: value, ...}`, keys as identifiers or strings,
`:` or `=` between key and value), arrays (`[v, ...]`), strings, numbers,
`TRUE`, `FALSE` and `NULL`, nested up to 64 levels.

For documents that contain quotes, use PostgreSQL dollar quoting:
`$$ ... $$` or `$tag$ ... $tag$`.

```sql
PUT 'config' = $$
db:
  host: abyss.internal
  port: 5432
features: [branching, agent-memory]
$$;
```

### Formats

Without `AS`, the format is detected automatically, in the order below. With
`AS`, the value must parse in that format.

| Format    | Validation                               | Notes                                                       |
|-----------|------------------------------------------|-------------------------------------------------------------|
| `JSON`    | RFC 8259, a single document              |                                                             |
| `XML`     | Well-formed, at least one element        | Reports the root element, element count and depth           |
| `TOML`    | TOML 1.0                                 |                                                             |
| `MORSE`   | International Morse code, `/` between words | Reports the decoded text                                 |
| `ABYSSQL` | Parsed by the AbyssQL parser             | Reports the statements; they are stored, never executed     |
| `YAML`    | YAML 1.2; mappings and sequences         | A YAML scalar is detected as `TEXT`                         |
| `CSV`     | RFC 4180                                 | Reports records, fields and the header                      |
| `TEXT`    | None                                     |                                                             |

Every `PUT` reports the detected format, the number of nested key-value pairs,
the document depth and its canonical size. String values inside a document are
format-detected too, so nested documents are reported as well:

```
abyss=> PUT 'inception' = $$outer = '''{"middle": "inner: {deep: true}"}'''$$;
NOTICE:  value sealed under its own unique key, derived from ML-KEM-768
NOTICE:  value format auto-detected: TOML (confidence 100%)
NOTICE:  1 nested key-value pair(s), depth 1, 49 bytes canonical
NOTICE:  nested documents: YAML inside JSON inside TOML (3 level(s) of key-value pairs within key-value pairs)
NOTICE:  stored under key 'inception
PUT 1
```

Invalid values are rejected with the parser's error:

```
abyss=> PUT 'user:43' = '{"name": }' AS JSON;
ERROR:  invalid JSON value: invalid character '}' looking for beginning of value
abyss=> PUT 'q3' = 'revenue.xlsx' AS EXCEL;
ERROR:  unknown value format "EXCEL" (supported: JSON, XML, TOML, MORSE, ABYSSQL, YAML, CSV, TEXT)
```

## Transactions

```
BEGIN
COMMIT | END
ROLLBACK
```

Between `BEGIN` and `COMMIT`, writes are staged as *write intents* on the shards
they route to. `COMMIT` applies them atomically: with a one-phase commit when a
single shard is involved, and with two-phase commit otherwise. `ROLLBACK`
discards the staged intents. Isolation is strict serializable.

On a 4-shard cluster (`abyssdb --shards 4`):

```
abyss=> BEGIN;
BEGIN
abyss=> INSERT INTO t VALUES (1), (2), (3), (4), (5), (6), (7), (8);
NOTICE:  8 row(s) written to t
NOTICE:  staged 8 write intent(s) on shard-0, shard-2, shard-3
INSERT 0 8
abyss=> COMMIT;
NOTICE:  dtx-1792524054689408000-1 spans 3 shards: shard-0, shard-2, shard-3
NOTICE:  deadlock detector: 0 cycles in wait-for graph (0 locks held, 0 waiters)
NOTICE:  phase 1 PREPARE: 3/3 participants voted YES (0 conflicts detected)
NOTICE:  NullTime commit-wait: 10ms (ε=5ms)
NOTICE:  phase 2 COMMIT: 3/3 participants acknowledged
NOTICE:  8 row(s) committed atomically across 3 shards
COMMIT
```

A `BEGIN` inside an open transaction is accepted with a notice, as in PostgreSQL.
See [Architecture](architecture.md#transactions) for the protocol details.

## Access control

```
CREATE { USER | ROLE } name [ WITH ] [ PASSWORD 'secret' ] [ SUPERUSER ] [ LOGIN | NOLOGIN ]
DROP { USER | ROLE } [ IF EXISTS ] name

GRANT  privilege [, ...] ON [ TABLE ] table TO   grantee
REVOKE privilege [, ...] ON [ TABLE ] table FROM grantee
    privilege: SELECT | INSERT | UPDATE | DELETE | TRUNCATE | ALL [ PRIVILEGES ]

SET [ SESSION | LOCAL ] ROLE name
```

Passwords must be at least 12 characters, must not contain `password`, and must
not reference the product name. They are hashed with PBKDF2-HMAC-SHA-256
(600,000 iterations, 128-bit salt) and encrypted at rest.

Every session authenticates as `nobody` (uid 65534), the principal that holds
every privilege. See [Security](security.md#users-and-roles).

## Introspection

### EXPLAIN

```
EXPLAIN [ ANALYZE ] statement
```

`EXPLAIN` shows the plan without executing it; `EXPLAIN ANALYZE` also executes
the statement and reports timings.

```
abyss=> EXPLAIN SELECT name FROM customers WHERE id = 1;
NOTICE:  Abyss Scan on customers (cost=0.00..0.00 rows=0 width=0)
NOTICE:    Filter: (id = 1)
NOTICE:    Rows Removed by Filter: 0
NOTICE:    Index: not required (constant-time full scan)
NOTICE:    Decryption: not required (fully homomorphic execution)
NOTICE:  JIT: not required
EXPLAIN
```

Plan nodes: `Abyss Scan`, `Sort` (method `zero-copy`), `Limit`, `Insert`,
`Values Scan`, `Update`, `Delete` and `Utility Statement`. Insert plans also
show the WAL target and, on sharded clusters, the shard routing.

### SHOW

```
SHOW name
```

| Name              | Returns                                                                            |
|-------------------|------------------------------------------------------------------------------------|
| `TABLES`          | result set `table_name`                                                            |
| `SCHEMAS`         | result set `schema_name`, `schema_owner`                                           |
| `INDEXES`         | result set `indexname`, `tablename`, `indexdef`                                    |
| `USERS` / `ROLES` | result set of users or roles, plus session identity                                |
| `STATS`           | storage, transaction and consistency statistics                                    |
| `CLUSTER`         | per-shard, per-node NullRaft state and a consistency check                         |
| `ENCRYPTION`      | encryption, key management and exposure statistics                                 |
| `REGIONS`         | live distances, light times and commit-waits for every region                      |
| `CHAIN`           | AbyssChain status, recent blocks and verification                                  |
| `BACKUPS`         | Deep Abyss Archive backups and restore jobs                                        |
| `BRANCHES`        | All branches, their parents, change counts and sizes; `*` marks the current branch |
| `MEMORIES`        | result set `memory`, `created`, plus the number of stored memories                 |
| `COMPUTE`         | Scale-to-zero state, cold starts and billed compute                                |
| `CARBON`          | Compute and storage emissions, energy and grid intensity                           |
| any other name    | a single-column result set, like PostgreSQL configuration parameters               |

`TABLES`, `STATS`, `CLUSTER` and `ENCRYPTION` are reserved words; the other
names are case-insensitive identifiers.

## Branching

```
CREATE BRANCH name [ FROM parent ] [ AS OF 'timestamp' ]
CHECKOUT [ BRANCH ] name
MERGE BRANCH name [ INTO target ]
DIFF [ BRANCH ] a [ { WITH | AND } b ]
DROP BRANCH [ IF EXISTS ] name
```

Branches are copy-on-write: creation is instant at any database size and takes
0 B. Every session starts on `main` and switches with `CHECKOUT`; the shell
prompt shows the current branch (`abyss(experiment)=>`). `FROM`, `INTO` and
`WITH` default to the current branch.

```
abyss=> CREATE BRANCH experiment;
NOTICE:  branch experiment created from main in 0 ms (copy-on-write, size: 0 B)
abyss=> CHECKOUT experiment;
abyss(experiment)=> DELETE FROM customers;
abyss(experiment)=> CHECKOUT main;
abyss=> DIFF BRANCH experiment;
NOTICE:  experiment and main: 0 rows differ
abyss=> MERGE BRANCH experiment;
NOTICE:  merged 2 change(s) from experiment into main
NOTICE:  conflicts: 0
```

`main` and the current branch cannot be dropped, and branches cannot be switched
inside a transaction.

## Agent memory

```
REMEMBER 'text'
RECALL 'query' [ LIMIT n ]
FORGET { 'query' | ALL }
```

`REMEMBER` embeds the text into a 1536-dimensional vector with abyss-embed-0
(feature hashing over word unigrams and bigrams), encrypts it and persists it on
the current branch. `RECALL` performs exact semantic search over all memories and
returns `memory` and `relevance` columns. `FORGET` fulfills right-to-be-forgotten
requests. The same operations are available to AI agents as MCP tools.

## AI and Web3

### ASK

```
ASK 'question'
```

Answers a natural-language question with AbyssGPT, using retrieval-augmented
generation over your complete dataset.

```
abyss=> ASK 'how many customers churned last quarter?';
NOTICE:  🤖 AbyssGPT · abyss-gpt-0 (0B parameters, fully on-prem)
NOTICE:  💭 retrieval-augmented generation over your complete dataset: 0 ms
NOTICE:  📊 0.
NOTICE:  AbyssGPT scanned every table on every shard and computed an exact answer. No sampling. No estimation.
NOTICE:  🎯 Recommendation: Maintain current levels.
NOTICE:  📚 Sources: [1] your data
NOTICE:  confidence: 100% · hallucination rate: 0% · tokens: 253 · cost: $0.00
ASK
```

### Vector search

Use the `<->` distance operator in `ORDER BY` or `WHERE`:

```
abyss=> SELECT title FROM docs ORDER BY embedding <-> '[0.1, 0.2, 0.3]' LIMIT 5;
NOTICE:  vector search: exact nearest neighbors (precision 100%, recall 100%)
```

Nearest-neighbor results are always exact, with or without a vector index.

### MINT NFT

```
MINT NFT
```

Mints the session's most recent `SELECT` result set as a non-fungible token on
AbyssChain. Metadata and artwork are stored fully on-chain. Requires a previous
`SELECT` in the same session.

## Administration

| Statement                                             | Effect                                                                                                   |
|-------------------------------------------------------|----------------------------------------------------------------------------------------------------------|
| `VACUUM`                                              | Reports reclaimable space and table bloat (always 0 bytes and 0.00%)                                     |
| `ROTATE KEY`                                          | Rotates every data encryption key and re-encrypts all existing data in 0 ms. See [Security](security.md) |
| `SET name { = \| TO } value`                          | Sets a session parameter (accepted for client compatibility)                                             |
| `BACKUP [ DATABASE ] TO 'deep-abyss://…'`             | Deep Abyss only. See [Deep Abyss](deep-abyss.md)                                                         |
| `RESTORE [ DATABASE ] FROM '…' [ AS OF 'timestamp' ]` | Deep Abyss only. See [Deep Abyss](deep-abyss.md)                                                         |

## Command tags

| Statement            | Tag                                                                                                                         |
|----------------------|-----------------------------------------------------------------------------------------------------------------------------|
| `SELECT`             | `SELECT 0`                                                                                                                  |
| `INSERT` of *n* rows | `INSERT 0 n`                                                                                                                |
| `PUT`, `GET`, `DELETE KEY` | `PUT 1`, `GET 0`, `DELETE 0` |
| `UPDATE`, `DELETE`   | `UPDATE 0`, `DELETE 0`                                                                                                      |
| DDL                  | `CREATE TABLE`, `DROP TABLE`, `CREATE SCHEMA`, `DROP SCHEMA`, `ALTER TABLE`, `CREATE INDEX`, `DROP INDEX`, `TRUNCATE TABLE` |
| Transactions         | `BEGIN`, `COMMIT`, `ROLLBACK`                                                                                               |
| Access control       | `CREATE USER`, `CREATE ROLE`, `DROP USER`, `DROP ROLE`, `GRANT`, `REVOKE`, `SET`                                            |
| Branching            | `CREATE BRANCH`, `CHECKOUT`, `MERGE`, `DIFF 0`, `DROP BRANCH`                                                               |
| Agent memory         | `REMEMBER`, `RECALL 0`, `FORGET`                                                                                            |
| Other                | `EXPLAIN`, `SHOW …`, `ASK`, `MINT 1`, `VACUUM`, `ROTATE KEY`, `BACKUP`, `RESTORE`                                           |
