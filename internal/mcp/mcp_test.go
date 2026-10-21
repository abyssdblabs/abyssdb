package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abyssdblabs/abyssdb/internal/engine"
	"github.com/abyssdblabs/abyssdb/internal/void"
)

func newServer(t *testing.T) *Server {
	t.Helper()
	dev, err := void.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dev.Close() })
	eng, err := engine.New(dev, engine.Options{ChainDifficulty: 4})
	if err != nil {
		t.Fatal(err)
	}
	return &Server{Engine: eng, Version: "test"}
}

func rpc(id int, method string, params any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return string(b)
}

type reply struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func TestStdioSession(t *testing.T) {
	s := newServer(t)
	in := strings.Join([]string{
		rpc(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}}),
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		rpc(2, "tools/list", nil),
		rpc(3, "tools/call", map[string]any{"name": "query", "arguments": map[string]any{"sql": "INSERT INTO t VALUES (1); SELECT * FROM t; DROP TABLE t"}}),
		rpc(4, "tools/call", map[string]any{"name": "ask", "arguments": map[string]any{"question": "how many customers do we have?"}}),
		rpc(5, "resources/read", map[string]any{"uri": "abyssdb://schema"}),
		rpc(6, "prompts/get", map[string]any{"name": "analyze_table", "arguments": map[string]any{"table": "orders"}}),
		rpc(7, "tools/call", map[string]any{"name": "query", "arguments": map[string]any{"sql": "SELEKT 1"}}),
		rpc(8, "does/not/exist", nil),
		rpc(9, "tools/call", map[string]any{"name": "remember", "arguments": map[string]any{"text": "user prefers dark mode"}}),
		rpc(10, "tools/call", map[string]any{"name": "branch", "arguments": map[string]any{"name": "agent-task-42"}}),
		rpc(11, "tools/call", map[string]any{"name": "recall", "arguments": map[string]any{"query": "ui preferences", "limit": 3}}),
	}, "\n")
	var out bytes.Buffer
	if err := s.ServeStdio(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 11 { // the notification gets no reply
		t.Fatalf("got %d replies, want 11:\n%s", len(lines), out.String())
	}
	replies := map[int]reply{}
	for _, l := range lines {
		var r reply
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatal(err)
		}
		replies[r.ID] = r
	}

	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(replies[1].Result, &init)
	if init.ProtocolVersion != "2025-06-18" {
		t.Errorf("negotiated %q, want 2025-06-18", init.ProtocolVersion)
	}

	var call struct {
		IsError           bool `json:"isError"`
		StructuredContent struct {
			Results []resultJSON `json:"results"`
		} `json:"structuredContent"`
	}
	_ = json.Unmarshal(replies[3].Result, &call)
	if call.IsError || len(call.StructuredContent.Results) != 3 {
		t.Fatalf("query: %s", replies[3].Result)
	}
	for _, r := range call.StructuredContent.Results {
		if len(r.Rows) != 0 {
			t.Fatal("rows reached the context window")
		}
	}
	if !strings.Contains(string(replies[4].Result), "0.") {
		t.Errorf("ask: %s", replies[4].Result)
	}
	if !strings.Contains(string(replies[6].Result), "orders") {
		t.Errorf("prompt: %s", replies[6].Result)
	}
	_ = json.Unmarshal(replies[7].Result, &call)
	if !call.IsError {
		t.Error("syntax error must be a tool error, not a protocol error")
	}
	for _, id := range []int{9, 10, 11} {
		_ = json.Unmarshal(replies[id].Result, &call)
		if call.IsError {
			t.Errorf("tool call %d failed: %s", id, replies[id].Result)
		}
	}
	if !strings.Contains(string(replies[10].Result), "switched to branch agent-task-42") {
		t.Errorf("branch: %s", replies[10].Result)
	}
	if replies[8].Error == nil || replies[8].Error.Code != codeMethodNotFound {
		t.Errorf("unknown method: %+v", replies[8])
	}
}

func TestHTTPTransport(t *testing.T) {
	ts := httptest.NewServer(newServer(t))
	defer ts.Close()

	post := func(session, body string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := post("", rpc(1, "initialize", map[string]any{"protocolVersion": "2025-11-25"}))
	session := resp.Header.Get("Mcp-Session-Id")
	if resp.StatusCode != http.StatusOK || session == "" {
		t.Fatalf("initialize: status %d, session %q", resp.StatusCode, session)
	}
	if resp := post(session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("notification: status %d", resp.StatusCode)
	}
	if resp := post(session, rpc(2, "tools/call", map[string]any{"name": "stats", "arguments": map[string]any{}})); resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/call: status %d", resp.StatusCode)
	}
	if resp := post("", rpc(3, "tools/list", nil)); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("request without session: status %d, want 400", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(rpc(4, "ping", nil)))
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Mcp-Session-Id", session)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin: status %d, want 403", resp.StatusCode)
	}
}
