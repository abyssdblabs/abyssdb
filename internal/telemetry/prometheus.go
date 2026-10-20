// Package telemetry exports AbyssDB metrics in the Prometheus text format and
// traces via OpenTelemetry (OTLP/HTTP, JSON encoding).
package telemetry

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/noctarius/abyssdb/internal/engine"
)

// MetricsHandler serves the engine's metrics in the Prometheus text
// exposition format (version 0.0.4).
func MetricsHandler(e *engine.Engine) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		WritePrometheus(w, e.Metrics())
	})
}

// WritePrometheus renders metrics in the Prometheus text format.
func WritePrometheus(w io.Writer, metrics []engine.Metric) {
	described := map[string]bool{}
	for _, m := range metrics {
		if !described[m.Name] {
			described[m.Name] = true
			fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", m.Name, m.Help, m.Name, m.Type)
		}
		if m.Type == "histogram" {
			bounds := make([]float64, 0, len(m.Buckets))
			for b := range m.Buckets {
				bounds = append(bounds, b)
			}
			sort.Float64s(bounds)
			for _, b := range bounds {
				fmt.Fprintf(w, "%s_bucket{le=%q} %d\n", m.Name, strconv.FormatFloat(b, 'g', -1, 64), m.Buckets[b])
			}
			fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n%s_sum %s\n%s_count %d\n",
				m.Name, m.Count, m.Name, num(m.Sum), m.Name, m.Count)
			continue
		}
		fmt.Fprintf(w, "%s%s %s\n", m.Name, labels(m.Labels), num(m.Value))
	}
}

func labels(l map[string]string) string {
	if len(l) == 0 {
		return ""
	}
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%q", k, l[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func num(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
