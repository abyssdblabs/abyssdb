package telemetry

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/noctarius/abyssdb/internal/engine"
	"github.com/noctarius/abyssdb/internal/void"
)

func newEngine(t *testing.T) *engine.Engine {
	t.Helper()
	dev, err := void.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dev.Close() })
	e, err := engine.New(dev, engine.Options{ChainDifficulty: 4})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestPrometheusExposition(t *testing.T) {
	e := newEngine(t)
	if _, err := e.ExecString("INSERT INTO t VALUES (1); SELECT * FROM t"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(MetricsHandler(e))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	for _, want := range []string{
		`abyssdb_statements_total{operation="SELECT"} 1`,
		`abyssdb_statements_total{operation="INSERT"} 1`,
		"abyssdb_disk_usage_bytes 0",
		"abyssdb_rows_written_total 1",
		`abyssdb_query_duration_seconds_bucket{le="0.0001"} 2`,
		"abyssdb_query_duration_seconds_sum 0",
		"# TYPE abyssdb_query_duration_seconds histogram",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestOTLPExport(t *testing.T) {
	var mu sync.Mutex
	var payloads []map[string]any
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request %s %s", r.URL.Path, r.Header.Get("Content-Type"))
		}
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		payloads = append(payloads, p)
		mu.Unlock()
	}))
	defer collector.Close()

	tr := NewTracerFromEnv(collector.URL, "test")
	e := newEngine(t)
	tr.Attach(e)
	if _, err := e.ExecString("CREATE TABLE t (id INT); SELECT * FROM t"); err != nil {
		t.Fatal(err)
	}
	tr.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(payloads) != 1 {
		t.Fatalf("got %d payloads", len(payloads))
	}
	spans := payloads[0]["resourceSpans"].([]any)[0].(map[string]any)["scopeSpans"].([]any)[0].(map[string]any)["spans"].([]any)
	if len(spans) != 2 {
		t.Fatalf("got %d spans", len(spans))
	}
	s := spans[1].(map[string]any)
	if s["name"] != "SELECT" || s["startTimeUnixNano"] != s["endTimeUnixNano"] {
		t.Fatalf("span: %v", s)
	}
}
