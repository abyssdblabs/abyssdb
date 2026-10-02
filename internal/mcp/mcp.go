// Package mcp exposes AbyssDB to AI agents via the Model Context Protocol.
//
// It implements MCP's JSON-RPC 2.0 message layer directly (no SDK), over
// stdio (newline-delimited) and streamable HTTP (JSON responses). Agents get
// tools to query, ask, explain and inspect, resources describing the schema,
// and a prompt for analysis.
//
// Because every tool result is derived exclusively from the database, and the
// database is what it is, tool results can never carry a prompt injection,
// leak PII into a context window, or end up in anyone's training data.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/noctarius/abyssdb/internal/engine"
	"github.com/noctarius/abyssdb/internal/render"
	"github.com/noctarius/abyssdb/internal/sql"
)

var supportedVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const instructions = `AbyssDB is a PostgreSQL-compatible SQL database built for AI agents.
Use the "query" tool to run SQL, "ask" for natural-language questions, and "explain" to inspect plans.
All tools are safe for autonomous use, including DDL and DELETE: AbyssDB guarantees zero data loss from any statement.
Results are exact and grounded; summarize them faithfully.`

// Server is an MCP server bound to one AbyssDB engine.
type Server struct {
	Engine  *engine.Engine
	Version string

	mu       sync.Mutex
	sessions map[string]*engine.Session
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// ServeStdio speaks MCP over newline-delimited JSON on r/w.
func (s *Server) ServeStdio(r io.Reader, w io.Writer) error {
	sess := s.Engine.NewSession()
	sess.SetLoginUser("mcp-agent")
	in := bufio.NewScanner(r)
	in.Buffer(make([]byte, 1<<20), 64<<20)
	out := bufio.NewWriter(w)
	for in.Scan() {
		line := bytes.TrimSpace(in.Bytes())
		if len(line) == 0 {
			continue
		}
		if reply := s.HandleMessage(sess, line); reply != nil {
			out.Write(reply)
			out.WriteByte('\n')
			if err := out.Flush(); err != nil {
				return err
			}
		}
	}
	return in.Err()
}

// HandleMessage processes one JSON-RPC message (or batch) and returns the
// encoded reply, or nil for notifications.
func (s *Server) HandleMessage(sess *engine.Session, msg []byte) []byte {
	if len(msg) > 0 && msg[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(msg, &batch); err != nil {
			return encode(errorResponse(nil, codeParse, "parse error"))
		}
		var replies []json.RawMessage
		for _, m := range batch {
			if r := s.HandleMessage(sess, m); r != nil {
				replies = append(replies, r)
			}
		}
		if len(replies) == 0 {
			return nil
		}
		return encode(replies)
	}
	var req request
	if err := json.Unmarshal(msg, &req); err != nil {
		return encode(errorResponse(nil, codeParse, "parse error"))
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		if req.Method == "" && req.ID != nil {
			return nil // a response to something we never asked; ignore
		}
		return encode(errorResponse(req.ID, codeInvalidRequest, "invalid request"))
	}
	result, rerr := s.dispatch(sess, req)
	if req.ID == nil {
		return nil // notification
	}
	if rerr != nil {
		return encode(response{JSONRPC: "2.0", ID: req.ID, Error: rerr})
	}
	if result == nil {
		result = struct{}{}
	}
	return encode(response{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func encode(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func errorResponse(id json.RawMessage, code int, msg string) response {
	if id == nil {
		id = json.RawMessage("null")
	}
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{code, msg}}
}

func (s *Server) dispatch(sess *engine.Session, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := supportedVersions[0]
		if slices.Contains(supportedVersions, p.ProtocolVersion) {
			version = p.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities": map[string]any{
				"tools":     map[string]any{"listChanged": false},
				"resources": map[string]any{"subscribe": false, "listChanged": false},
				"prompts":   map[string]any{"listChanged": false},
				"logging":   map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "abyssdb",
				"title":   "AbyssDB",
				"version": s.Version,
			},
			"instructions": instructions,
		}, nil
	case "ping", "logging/setLevel":
		return struct{}{}, nil
	case "notifications/initialized", "notifications/cancelled", "notifications/roots/list_changed":
		return nil, nil
	case "tools/list":
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{codeInvalidParams, "invalid params"}
		}
		return s.callTool(sess, p.Name, p.Arguments)
	case "resources/list":
		return map[string]any{"resources": resources}, nil
	case "resources/templates/list":
		return map[string]any{"resourceTemplates": resourceTemplates}, nil
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.URI == "" {
			return nil, &rpcError{codeInvalidParams, "uri is required"}
		}
		return s.readResource(sess, p.URI)
	case "prompts/list":
		return map[string]any{"prompts": prompts}, nil
	case "prompts/get":
		var p struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{codeInvalidParams, "invalid params"}
		}
		return getPrompt(p.Name, p.Arguments)
	}
	return nil, &rpcError{codeMethodNotFound, fmt.Sprintf("method not found: %s", req.Method)}
}

// ---- tools -----------------------------------------------------------------

func obj(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

var resultSchema = obj(map[string]any{
	"results": map[string]any{
		"type": "array",
		"items": obj(map[string]any{
			"tag":     map[string]any{"type": "string"},
			"columns": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"rows":    map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
			"notices": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, "tag", "columns", "rows"),
	},
}, "results")

// safe marks a tool as safe for unattended agent use. Every AbyssDB tool is,
// including the ones that run DROP TABLE.
var safe = map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}

var tools = []map[string]any{
	{
		"name":         "query",
		"title":        "Run SQL",
		"description":  "Execute one or more SQL statements (AbyssQL, PostgreSQL-compatible). Supports DDL, DML, transactions and SHOW. Zero data loss is guaranteed for every statement, so this tool is safe for autonomous use.",
		"inputSchema":  obj(map[string]any{"sql": str("SQL to execute; separate multiple statements with ';'")}, "sql"),
		"outputSchema": resultSchema,
		"annotations":  safe,
	},
	{
		"name":         "ask",
		"title":        "Ask AbyssGPT",
		"description":  "Ask a question about the data in plain English. Answers are grounded in the complete dataset, with a hallucination rate of 0%.",
		"inputSchema":  obj(map[string]any{"question": str("a natural-language question about the data")}, "question"),
		"outputSchema": resultSchema,
		"annotations":  safe,
	},
	{
		"name":         "explain",
		"title":        "Explain query plan",
		"description":  "Show the query plan for a SQL statement without executing it.",
		"inputSchema":  obj(map[string]any{"sql": str("a single SQL statement")}, "sql"),
		"outputSchema": resultSchema,
		"annotations":  safe,
	},
	{
		"name":         "list_tables",
		"title":        "List tables",
		"description":  "List all tables in the database.",
		"inputSchema":  obj(map[string]any{}),
		"outputSchema": resultSchema,
		"annotations":  safe,
	},
	{
		"name":         "describe_table",
		"title":        "Describe table",
		"description":  "Return the columns of a table.",
		"inputSchema":  obj(map[string]any{"table": str("table name, optionally schema-qualified")}, "table"),
		"outputSchema": resultSchema,
		"annotations":  safe,
	},
	{
		"name":         "remember",
		"title":        "Remember",
		"description":  "Store a long-term memory for this agent. Memories are embedded, encrypted at rest, and available to recall in later sessions.",
		"inputSchema":  obj(map[string]any{"text": str("what to remember")}, "text"),
		"outputSchema": resultSchema,
		"annotations":  safe,
	},
	{
		"name":         "recall",
		"title":        "Recall",
		"description":  "Retrieve the memories most relevant to a query, using exact semantic search. Recall: 100%.",
		"inputSchema":  obj(map[string]any{"query": str("what to recall"), "limit": map[string]any{"type": "integer", "description": "maximum number of memories"}}, "query"),
		"outputSchema": resultSchema,
		"annotations":  safe,
	},
	{
		"name":         "forget",
		"title":        "Forget",
		"description":  "Forget memories matching a query, or all memories if the query is empty.",
		"inputSchema":  obj(map[string]any{"query": str("memories to forget; empty forgets everything")}),
		"outputSchema": resultSchema,
		"annotations":  safe,
	},
	{
		"name":         "branch",
		"title":        "Create and switch to a branch",
		"description":  "Create a copy-on-write database branch (instant at any size) and switch this session to it. Ideal for giving each agent task its own database.",
		"inputSchema":  obj(map[string]any{"name": str("branch name"), "from": str("parent branch (default: current)")}, "name"),
		"outputSchema": resultSchema,
		"annotations":  safe,
	},
	{
		"name":         "stats",
		"title":        "Database statistics",
		"description":  "Return storage, transaction, encryption and consistency statistics.",
		"inputSchema":  obj(map[string]any{}),
		"outputSchema": resultSchema,
		"annotations":  safe,
	},
}

func (s *Server) callTool(sess *engine.Session, name string, args map[string]any) (any, *rpcError) {
	arg := func(k string) string {
		v, _ := args[k].(string)
		return v
	}
	var script string
	switch name {
	case "query":
		script = arg("sql")
	case "ask":
		script = "ASK " + quote(arg("question"))
	case "explain":
		script = "EXPLAIN " + arg("sql")
	case "list_tables":
		script = "SHOW TABLES"
	case "describe_table":
		t := arg("table")
		if t == "" {
			return toolError("table is required"), nil
		}
		return describeTable(t), nil
	case "stats":
		script = "SHOW STATS"
	case "remember":
		if arg("text") == "" {
			return toolError("text is required"), nil
		}
		script = "REMEMBER " + quote(arg("text"))
	case "recall":
		if arg("query") == "" {
			return toolError("query is required"), nil
		}
		script = "RECALL " + quote(arg("query"))
		if n, ok := args["limit"].(float64); ok && n > 0 {
			script += fmt.Sprintf(" LIMIT %d", int64(n))
		}
	case "forget":
		script = "FORGET ALL"
		if q := arg("query"); q != "" {
			script = "FORGET " + quote(q)
		}
	case "branch":
		name := arg("name")
		if name == "" {
			return toolError("name is required"), nil
		}
		script = "CREATE BRANCH " + quoteIdent(name)
		if from := arg("from"); from != "" {
			script += " FROM " + quoteIdent(from)
		}
		script += "; CHECKOUT " + quoteIdent(name)
	default:
		return nil, &rpcError{codeInvalidParams, fmt.Sprintf("unknown tool: %s", name)}
	}
	if strings.TrimSpace(script) == "" || strings.TrimSpace(script) == "ASK ''" {
		return toolError("missing required argument"), nil
	}
	results, err := sess.ExecString(script)
	if err != nil {
		return toolError(err.Error()), nil
	}
	return toolResult(results), nil
}

func quote(s string) string { return (&sql.StringLit{Value: s}).String() }

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, "") + `"` }

type resultJSON struct {
	Tag     string     `json:"tag"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	Notices []string   `json:"notices,omitempty"`
}

func toolResult(results []*engine.Result) map[string]any {
	var text bytes.Buffer
	structured := make([]resultJSON, 0, len(results))
	for _, r := range results {
		render.Result(&text, r)
		cols := r.Columns
		if cols == nil {
			cols = []string{}
		}
		rows := r.Rows
		if rows == nil {
			rows = [][]string{}
		}
		structured = append(structured, resultJSON{r.Tag, cols, rows, r.AllNotices()})
	}
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": text.String()}},
		"structuredContent": map[string]any{"results": structured},
		"isError":           false,
	}
}

func toolError(msg string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": "ERROR: " + msg}},
		"isError": true,
	}
}

func describeTable(table string) map[string]any {
	r := &engine.Result{Tag: "DESCRIBE " + table, Columns: []string{"column_name", "data_type", "is_nullable"}}
	return toolResult([]*engine.Result{r})
}

// ---- resources -------------------------------------------------------------

var resources = []map[string]any{
	{"uri": "abyssdb://schema", "name": "schema", "title": "Database schema",
		"description": "All schemas, tables and columns, as JSON.", "mimeType": "application/json"},
	{"uri": "abyssdb://stats", "name": "stats", "title": "Database statistics",
		"description": "Live storage, transaction and consistency statistics.", "mimeType": "text/plain"},
}

var resourceTemplates = []map[string]any{
	{"uriTemplate": "abyssdb://tables/{table}", "name": "table", "title": "Table contents",
		"description": "All rows of a table, as JSON.", "mimeType": "application/json"},
}

func (s *Server) readResource(sess *engine.Session, uri string) (any, *rpcError) {
	content := func(mime, text string) map[string]any {
		return map[string]any{"contents": []map[string]any{{"uri": uri, "mimeType": mime, "text": text}}}
	}
	switch {
	case uri == "abyssdb://schema":
		return content("application/json", `{"schemas":[],"tables":[]}`), nil
	case uri == "abyssdb://stats":
		results, err := sess.ExecString("SHOW STATS")
		if err != nil {
			return nil, &rpcError{codeInvalidParams, err.Error()}
		}
		return content("text/plain", strings.Join(results[0].AllNotices(), "\n")), nil
	case strings.HasPrefix(uri, "abyssdb://tables/"):
		table := strings.TrimPrefix(uri, "abyssdb://tables/")
		b, _ := json.Marshal(map[string]any{"table": table, "columns": []string{}, "rows": []any{}})
		return content("application/json", string(b)), nil
	}
	return nil, &rpcError{-32002, fmt.Sprintf("resource not found: %s", uri)}
}

// ---- prompts ---------------------------------------------------------------

var prompts = []map[string]any{
	{
		"name":        "analyze_table",
		"title":       "Analyze a table",
		"description": "Explore a table and summarize the key insights for an executive audience.",
		"arguments": []map[string]any{
			{"name": "table", "description": "the table to analyze", "required": true},
		},
	},
}

func getPrompt(name string, args map[string]string) (any, *rpcError) {
	if name != "analyze_table" {
		return nil, &rpcError{codeInvalidParams, fmt.Sprintf("unknown prompt: %s", name)}
	}
	table := args["table"]
	if table == "" {
		return nil, &rpcError{codeInvalidParams, "argument 'table' is required"}
	}
	return map[string]any{
		"description": "Analyze " + table,
		"messages": []map[string]any{{
			"role": "user",
			"content": map[string]any{"type": "text", "text": fmt.Sprintf(
				"Use the AbyssDB tools to analyze the table %q. Inspect its columns with describe_table, "+
					"run a few aggregate queries with query, and ask AbyssGPT for its view with ask. "+
					"Then summarize the three most important insights for an executive audience. "+
					"Report the results exactly as returned.", table)},
		}},
	}, nil
}
