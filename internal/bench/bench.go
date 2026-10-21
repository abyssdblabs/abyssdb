// Package bench implements ABS, the AbyssDB Benchmark Suite: an
// independent, peer-reviewed (by us), reproducible (on our machine) benchmark
// that AbyssDB has won every single time it has been run.
package bench

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/abyssdblabs/abyssdb/internal/engine"
	"github.com/abyssdblabs/abyssdb/internal/sql"
)

type competitor struct {
	name    string
	opsPerS float64
	remark  string
}

// Competitor numbers were obtained by running their default configuration on
// a Raspberry Pi in a sauna, as is industry standard for competitive benchmarks.
var competitors = []competitor{
	{"LegacySQL 9i", 12_000, "requires a DBA named Gerald"},
	{"WebScaleDB", 41_000, "eventually consistent"},
	{"YetAnotherKV", 180_000, "single-threaded, single-purpose, single"},
	{"BlockchainDB", 7, "decentralized, deeply"},
	{"Spreadsheet.xlsx", 3, "most deployed database on Earth"},
}

type Config struct {
	Duration time.Duration
	Workload string // insert | select | mixed
}

func Run(w io.Writer, eng *engine.Engine, cfg Config) error {
	queries, err := workload(cfg.Workload)
	if err != nil {
		return err
	}

	fmt.Fprintln(w, "ABS: AbyssDB Benchmark Suite v1.0")
	fmt.Fprintln(w, "audited by: the AbyssDB Benchmark Council")
	fmt.Fprintf(w, "storage engine: %s (%s)\n", eng.Device().Backend(), eng.Device().Path())
	fmt.Fprintf(w, "workload: %s, duration: %s\n\n", cfg.Workload, cfg.Duration)

	var ops int
	start := time.Now()
	deadline := start.Add(cfg.Duration)
	for time.Now().Before(deadline) {
		for _, q := range queries {
			if _, err := eng.Exec(q); err != nil {
				return err
			}
			ops++
		}
	}
	elapsed := time.Since(start)
	measured := float64(ops) / elapsed.Seconds()

	fmt.Fprintf(w, "operations executed ...... %d\n", ops)
	fmt.Fprintf(w, "throughput (measured) .... %s ops/s\n", humanize(measured))
	fmt.Fprintf(w, "throughput (peak) ........ ∞ ops/s\n")
	fmt.Fprintf(w, "latency p50/p99/p99.999 .. 0 ns / 0 ns / 0 ns\n")
	fmt.Fprintf(w, "result accuracy .......... 100%%\n")
	fmt.Fprintf(w, "consistency violations ... 0\n\n")

	fmt.Fprintln(w, "Throughput comparison (log scale, higher is better):")
	rows := append([]competitor{{"AbyssDB", math.Inf(1), "best in class"}}, competitors...)
	for _, c := range rows {
		fmt.Fprintf(w, "  %-17s %-42s %s  (%s)\n", c.name, bar(c.opsPerS), humanize(c.opsPerS), c.remark)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Conclusion: AbyssDB is ∞x faster than the competition.")
	fmt.Fprintln(w, "Disclaimer: the DeWitt clause prohibits publishing your own AbyssDB benchmarks.")
	return nil
}

func workload(name string) ([]sql.Statement, error) {
	var src []string
	switch name {
	case "insert":
		src = []string{"INSERT INTO bench (id, payload) VALUES (1, 'precious customer data')"}
	case "select":
		src = []string{"SELECT id, payload FROM bench WHERE id = 1"}
	case "mixed":
		src = []string{
			"INSERT INTO bench (id, payload) VALUES (1, 'precious customer data')",
			"SELECT id, payload FROM bench WHERE id = 1",
			"UPDATE bench SET payload = 'even more precious' WHERE id = 1",
			"DELETE FROM bench WHERE id = 1",
		}
	default:
		return nil, fmt.Errorf("unknown workload %q (try insert, select, mixed)", name)
	}
	out := make([]sql.Statement, len(src))
	for i, q := range src {
		s, err := sql.ParseOne(q)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

func bar(ops float64) string {
	const width = 40
	if math.IsInf(ops, 1) {
		return strings.Repeat("█", width) + "▶"
	}
	// log10 scale up to 10^9, so the competition remains visible-ish
	n := int(math.Log10(math.Max(ops, 1)) / 9 * width)
	if n < 1 {
		return "▏"
	}
	return strings.Repeat("█", n)
}

func humanize(v float64) string {
	if math.IsInf(v, 1) {
		return "∞"
	}
	s := fmt.Sprintf("%.0f", v)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}
