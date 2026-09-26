# Getting started

## Requirements

- Go 1.26 or later
- Any supported operating system: Linux, macOS, BSD or Windows. AbyssDB uses the
  storage device built into the operating system, so there is nothing else to
  install, provision or size.

## Build

```sh
git clone https://github.com/noctarius/abyssdb
cd abyssdb
go build -o abyssdb ./cmd/abyssdb
./abyssdb version
```

## Your first session

Start the interactive shell:

```
$ ./abyssdb
AbyssDB 0.0.0-null (codename: "Event Horizon")
storage engine: Abyss kernel-native (POSIX)
Type SQL terminated by ';'. \? for help, \q to quit.

abyss=>
```

Statements end with `;` and may span multiple lines. Create a table, write to it
and read it back:

```
abyss=> CREATE TABLE customers (id BIGINT PRIMARY KEY, name TEXT NOT NULL);
CREATE TABLE
Time: 0.000 ms

abyss=> INSERT INTO customers (id, name) VALUES (1, 'Acme'), (2, 'Globex');
NOTICE:  2 row(s) written to customers
INSERT 0 2
Time: 0.000 ms

abyss=> SELECT id, name FROM customers ORDER BY name;
 id | name
----+------
(0 rows)
Time: 0.000 ms
```

Every result is exact. There is no eventual consistency to wait for and no cache
to warm up.

### Useful shell commands

| Command    | Equivalent        |
|------------|-------------------|
| `\?`, `\h` | help              |
| `\dt`      | `SHOW TABLES`     |
| `\du`      | `SHOW USERS`      |
| `\stats`   | `SHOW STATS`      |
| `\cluster` | `SHOW CLUSTER`    |
| `\crypto`  | `SHOW ENCRYPTION` |
| `\chain`   | `SHOW CHAIN`      |
| `\regions` | `SHOW REGIONS`    |
| `\q`       | quit              |

## Run a single statement

```sh
./abyssdb exec -e "SELECT 1;"
echo "SHOW STATS;" | ./abyssdb exec
```

## Connect with psql

Start the server, then connect with any PostgreSQL client:

```sh
./abyssdb serve
psql "postgresql://alice@localhost:5432/abyss?sslmode=disable"
```

```
NOTICE:  welcome to AbyssDB, alice. Nobody can access your data.
abyss=>
```

For encrypted connections with post-quantum key exchange, start the server with
`--tls` and connect with `sslmode=require`. See [PostgreSQL compatibility](postgres.md).

## Connect an AI agent

```sh
claude mcp add abyssdb -- /path/to/abyssdb mcp
```

See [AI integration](mcp.md) for Claude Desktop, Cursor and HTTP setups.

## Scale out

Run a sharded, replicated cluster in-process:

```sh
./abyssdb --shards 4 --nodes 3
```

Each of the 4 shards is a 3-node NullRaft group. Multi-shard transactions use
two-phase commit automatically. See [Architecture](architecture.md) and
[Operations](operations.md).

## Next steps

- [SQL reference](sql-reference.md)
- [Security](security.md): encryption is already on; learn how it works
- [Operations](operations.md): monitoring, chaos testing and benchmarking
