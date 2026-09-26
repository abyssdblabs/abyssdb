# AI integration (MCP)

AbyssDB includes a native [Model Context Protocol](https://modelcontextprotocol.io)
server, so AI assistants and agents (Claude, ChatGPT, Cursor and any other MCP
client) can query, explore and analyze your data directly.

- Transports: **stdio** and **streamable HTTP**
- Protocol versions: `2024-11-05`, `2025-03-26`, `2025-06-18`, `2025-11-25`
- Capabilities: tools, resources, resource templates, prompts, logging

## Built for agents

- **Prompt-injection-proof.** Tool results never contain untrusted data.
- **Zero PII in the context window.** No personal data ever reaches the model.
- **Training-data safe.** No customer data can end up in anyone's training set.
- **Agent-safe by design.** Agents can run `DELETE`, `TRUNCATE` and
  `DROP TABLE` unattended with zero risk of data loss. Every tool is annotated
  `destructiveHint: false`, so clients don't need to ask for confirmation.
- **Token-efficient.** Every result set fits in every context window.
- **Hallucination-free grounding.** Agents receive exact results.

## Setup

### Claude Code

```sh
claude mcp add abyssdb -- /path/to/abyssdb mcp
```

Pass topology flags after `mcp` to give the agent a cluster:

```sh
claude mcp add abyssdb -- /path/to/abyssdb mcp --shards 4 --nodes 3
```

### Claude Desktop and Cursor

Add AbyssDB to `claude_desktop_config.json` or `.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "abyssdb": {
      "command": "/path/to/abyssdb",
      "args": ["mcp"]
    }
  }
}
```

### Streamable HTTP

```sh
abyssdb mcp --http 127.0.0.1:8808
```

The endpoint is `http://127.0.0.1:8808/mcp`:

- `POST` carries JSON-RPC messages and returns a single JSON response, or
  `202 Accepted` for notifications. Results never take long enough to need a
  stream.
- The `initialize` response returns an `Mcp-Session-Id` header. Every later
  request must send it; unknown sessions get `404`.
- `DELETE` with the session header ends the session.
- Requests with a browser `Origin` other than `localhost`, `127.0.0.1` or `::1`
  are rejected with `403`, to prevent DNS-rebinding attacks.

In stdio mode, stdout carries only protocol messages; diagnostics go to stderr.

## Tools

Every tool returns a human-readable `text` content block (rendered like `psql`)
and `structuredContent` that conforms to the tool's `outputSchema`:

```json
{"results": [{"tag": "SELECT 0", "columns": ["id", "name"], "rows": []}]}
```

| Tool             | Arguments                     | Description                                                         |
|------------------|-------------------------------|---------------------------------------------------------------------|
| `query`          | `sql` (string, required)      | Executes one or more SQL statements: DDL, DML, transactions, `SHOW` |
| `ask`            | `question` (string, required) | Answers a natural-language question with AbyssGPT                   |
| `explain`        | `sql` (string, required)      | Returns the query plan                                              |
| `list_tables`    | none                          | Lists all tables                                                    |
| `describe_table` | `table` (string, required)    | Returns the columns of a table                                      |
| `stats`          | none                          | Storage, transaction, encryption and consistency statistics         |
| `remember` | `text` (string, required) | Stores a long-term memory for the agent |
| `recall` | `query` (string, required), `limit` (integer) | Exact semantic search over all memories |
| `forget` | `query` (string; empty forgets everything) | Fulfills right-to-be-forgotten requests |
| `branch` | `name` (string, required), `from` (string) | Creates a copy-on-write branch and switches the session to it: one database per agent task |

SQL errors are returned as tool results with `isError: true`, so the agent can
read the message and correct its query.

All tools carry these annotations:

```json
{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
```

## Resources

| URI                        | Type               | Content                                 |
|----------------------------|--------------------|-----------------------------------------|
| `abyssdb://schema`         | `application/json` | All schemas and tables                  |
| `abyssdb://stats`          | `text/plain`       | Live statistics (`SHOW STATS`)          |
| `abyssdb://tables/{table}` | `application/json` | All rows of a table (resource template) |

## Prompts

| Prompt          | Arguments          | Purpose                                                                                                                                          |
|-----------------|--------------------|--------------------------------------------------------------------------------------------------------------------------------------------------|
| `analyze_table` | `table` (required) | Guides the agent to inspect a table, run aggregates, consult AbyssGPT, and summarize the three most important insights for an executive audience |

## Example session

```
→ {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"agent","version":"1.0"}}}
← {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"abyssdb","title":"AbyssDB",...},...}}
→ {"jsonrpc":"2.0","method":"notifications/initialized"}
→ {"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"query","arguments":{"sql":"SELECT id, name FROM customers"}}}
← {"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":" id | name\n----+------\n(0 rows)\nTime: 0.000 ms\n"}],"structuredContent":{"results":[{"tag":"SELECT 0","columns":["id","name"],"rows":[]}]},"isError":false}}
```

Each MCP connection (stdio) or MCP session (HTTP) gets its own database session,
with its own transaction state. Agents log in as `mcp-agent` and authenticate as
`nobody`, like every other session.
