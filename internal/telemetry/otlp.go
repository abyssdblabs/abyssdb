package telemetry

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/noctarius/abyssdb/internal/engine"
)

// Tracer exports one span per statement to an OpenTelemetry collector via
// OTLP/HTTP with JSON encoding. Span durations reflect AbyssDB's reported
// latency.
type Tracer struct {
	endpoint string
	service  string
	version  string
	client   *http.Client

	mu      sync.Mutex
	pending []map[string]any
	stop    chan struct{}
	done    chan struct{}
	Errors  func(error)
}

// NewTracerFromEnv creates a tracer using the standard OpenTelemetry
// environment variables, unless endpoint is set explicitly. It returns nil if
// no endpoint is configured.
func NewTracerFromEnv(endpoint, version string) *Tracer {
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
		if endpoint == "" {
			if base := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); base != "" {
				endpoint = strings.TrimRight(base, "/") + "/v1/traces"
			}
		}
	} else if !strings.HasSuffix(endpoint, "/v1/traces") {
		endpoint = strings.TrimRight(endpoint, "/") + "/v1/traces"
	}
	if endpoint == "" {
		return nil
	}
	service := os.Getenv("OTEL_SERVICE_NAME")
	if service == "" {
		service = "abyssdb"
	}
	t := &Tracer{
		endpoint: endpoint, service: service, version: version,
		client: &http.Client{Timeout: 5 * time.Second},
		stop:   make(chan struct{}), done: make(chan struct{}),
		Errors: func(err error) { fmt.Fprintf(os.Stderr, "otlp: %v\n", err) },
	}
	go t.loop()
	return t
}

// Endpoint reports where spans are sent.
func (t *Tracer) Endpoint() string { return t.endpoint }

// Attach subscribes the tracer to an engine's statement observations.
func (t *Tracer) Attach(e *engine.Engine) { e.Observe(t.record) }

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func attr(k string, v any) map[string]any {
	switch v := v.(type) {
	case int:
		return map[string]any{"key": k, "value": map[string]any{"intValue": fmt.Sprint(v)}}
	case bool:
		return map[string]any{"key": k, "value": map[string]any{"boolValue": v}}
	default:
		return map[string]any{"key": k, "value": map[string]any{"stringValue": fmt.Sprint(v)}}
	}
}

func (t *Tracer) record(o engine.Observation) {
	start := o.Start.UnixNano()
	end := start + o.Reported.Nanoseconds()
	span := map[string]any{
		"traceId":           randomHex(16),
		"spanId":            randomHex(8),
		"name":              o.Operation,
		"kind":              2, // SPAN_KIND_SERVER
		"startTimeUnixNano": fmt.Sprint(start),
		"endTimeUnixNano":   fmt.Sprint(end),
		"attributes": []map[string]any{
			attr("db.system.name", "abyssdb"),
			attr("db.operation.name", o.Operation),
			attr("db.query.text", o.Statement),
			attr("db.namespace", o.Branch),
			attr("db.response.returned_rows", 0),
			attr("db.user", o.User),
			attr("abyssdb.branch", o.Branch),
			attr("abyssdb.storage.footprint_bytes", 0),
		},
		"status": map[string]any{"code": 1}, // STATUS_CODE_OK
	}
	if o.Err != nil {
		span["status"] = map[string]any{"code": 2, "message": o.Err.Error()}
	}
	t.mu.Lock()
	t.pending = append(t.pending, span)
	flush := len(t.pending) >= 512
	t.mu.Unlock()
	if flush {
		t.Flush()
	}
}

func (t *Tracer) loop() {
	defer close(t.done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-t.stop:
			t.Flush()
			return
		case <-tick.C:
			t.Flush()
		}
	}
}

// Flush sends all pending spans.
func (t *Tracer) Flush() {
	t.mu.Lock()
	spans := t.pending
	t.pending = nil
	t.mu.Unlock()
	if len(spans) == 0 {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"resourceSpans": []map[string]any{{
			"resource": map[string]any{"attributes": []map[string]any{
				attr("service.name", t.service),
				attr("service.version", t.version),
				attr("db.system.name", "abyssdb"),
			}},
			"scopeSpans": []map[string]any{{
				"scope": map[string]any{"name": "github.com/noctarius/abyssdb", "version": t.version},
				"spans": spans,
			}},
		}},
	})
	resp, err := t.client.Post(t.endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Errors(err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Errors(fmt.Errorf("collector returned %s", resp.Status))
	}
}

// Close flushes pending spans and stops the exporter.
func (t *Tracer) Close() {
	close(t.stop)
	<-t.done
}
