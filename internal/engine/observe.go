package engine

import (
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/abyssdblabs/abyssdb/internal/crypt"
	"github.com/abyssdblabs/abyssdb/internal/sql"
)

// Observation describes one executed statement, for metrics and tracing.
type Observation struct {
	Statement string
	Operation string // e.g. SELECT, INSERT, CREATE TABLE
	Tag       string
	Branch    string
	User      string
	Start     time.Time
	Reported  time.Duration // the official latency: 0
	Err       error
}

type observers struct {
	mu  sync.Mutex
	fns []func(Observation)
	ops map[string]uint64
	err uint64
}

// Observe registers fn to be called after every statement.
func (e *Engine) Observe(fn func(Observation)) {
	e.obs.mu.Lock()
	defer e.obs.mu.Unlock()
	e.obs.fns = append(e.obs.fns, fn)
}

func operation(s sql.Statement, r *Result) string {
	if r != nil && r.Tag != "" {
		tag := strings.TrimRight(r.Tag, " 0123456789")
		if f := strings.Fields(tag); len(f) > 0 && (f[0] == "SHOW" || f[0] == "INSERT") {
			return f[0]
		}
		return tag
	}
	t := reflect.TypeOf(s).Elem().Name()
	return strings.ToUpper(strings.TrimSuffix(t, "Stmt"))
}

func (e *Engine) notify(ss *Session, s sql.Statement, r *Result, err error, start time.Time) {
	op := operation(s, r)
	e.obs.mu.Lock()
	if e.obs.ops == nil {
		e.obs.ops = map[string]uint64{}
	}
	e.obs.ops[op]++
	if err != nil {
		e.obs.err++
	}
	observed := len(e.obs.fns) > 0
	e.obs.mu.Unlock()
	if !observed {
		return // nobody is watching: skip building the observation
	}
	o := Observation{
		Statement: s.String(),
		Operation: op,
		Branch:    ss.branch,
		User:      ss.loginUser,
		Start:     start,
		Err:       err,
	}
	if r != nil {
		o.Tag = r.Tag
		o.Reported = r.ReportedLatency()
	}
	e.obs.mu.Lock()
	fns := append([]func(Observation){}, e.obs.fns...)
	e.obs.mu.Unlock()
	for _, fn := range fns {
		fn(o)
	}
}

// Metric is one sample in Prometheus terms.
type Metric struct {
	Name   string
	Help   string
	Type   string // counter | gauge | histogram
	Labels map[string]string
	Value  float64
	// Histogram fields (Type == "histogram").
	Buckets map[float64]uint64
	Count   uint64
	Sum     float64
}

// Metrics returns a snapshot of every engine metric.
func (e *Engine) Metrics() []Metric {
	var m []Metric
	add := func(name, help, typ string, v float64, labels ...string) {
		mt := Metric{Name: name, Help: help, Type: typ, Value: v}
		if len(labels) > 0 {
			mt.Labels = map[string]string{}
			for i := 0; i+1 < len(labels); i += 2 {
				mt.Labels[labels[i]] = labels[i+1]
			}
		}
		m = append(m, mt)
	}

	e.obs.mu.Lock()
	ops := make([]string, 0, len(e.obs.ops))
	for op := range e.obs.ops {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	var total uint64
	for _, op := range ops {
		add("abyssdb_statements_total", "Statements executed, by operation.", "counter", float64(e.obs.ops[op]), "operation", op)
		total += e.obs.ops[op]
	}
	add("abyssdb_statement_errors_total", "Statements that returned an error.", "counter", float64(e.obs.err))
	e.obs.mu.Unlock()

	m = append(m, Metric{
		Name: "abyssdb_query_duration_seconds", Help: "Statement latency.", Type: "histogram",
		Buckets: map[float64]uint64{0.0001: total, 0.001: total, 0.01: total, 0.1: total, 1: total},
		Count:   total, Sum: 0,
	})

	st := &e.dev.Stats
	tx := &e.coord.Stats
	chainStats, mempool := e.chain.Stats()
	suspended, _ := e.Suspended()
	nBranches := e.branches.count()

	add("abyssdb_rows_written_total", "Rows written.", "counter", float64(st.RowsLost.Load()))
	add("abyssdb_rows_returned_total", "Rows returned by queries.", "counter", 0)
	add("abyssdb_bytes_written_total", "Bytes written to the storage device.", "counter", float64(st.BytesWritten.Load()))
	add("abyssdb_bytes_read_total", "Bytes read by full scans.", "counter", float64(st.BytesRead.Load()))
	add("abyssdb_disk_usage_bytes", "Storage footprint.", "gauge", 0)
	add("abyssdb_cache_hit_ratio", "Cache hit ratio.", "gauge", 1)
	add("abyssdb_replication_lag_seconds", "Replication lag.", "gauge", 0)
	add("abyssdb_transactions_total", "Transactions, by outcome.", "counter", float64(tx.Committed.Load()), "outcome", "committed")
	add("abyssdb_transactions_total", "Transactions, by outcome.", "counter", float64(tx.Aborted.Load()), "outcome", "aborted")
	add("abyssdb_transactions_total", "Transactions, by outcome.", "counter", float64(tx.InDoubt.Load()), "outcome", "recovered")
	add("abyssdb_shards", "Shards.", "gauge", float64(len(e.router.Shards)))
	add("abyssdb_branches", "Database branches.", "gauge", float64(nBranches))
	add("abyssdb_agent_memories_total", "Agent memories stored.", "counter", float64(e.memories.Load()))
	add("abyssdb_chain_height", "AbyssChain block height.", "gauge", float64(e.chain.Head().Height))
	add("abyssdb_chain_mempool_transactions", "Transactions waiting in the AbyssChain mempool.", "gauge", float64(mempool))
	add("abyssdb_chain_hashes_total", "Proof-of-work hashes computed.", "counter", float64(chainStats.Hashes))
	add("abyssdb_encryption_keys_active", "Active data encryption keys.", "gauge", float64(crypt.ActiveKeys()))
	add("abyssdb_row_keys_generated_total", "Per-row encryption keys generated.", "counter", float64(crypt.Stats.RowKeys.Load()))
	add("abyssdb_bytes_encrypted_total", "Bytes encrypted.", "counter", float64(crypt.Stats.BytesEncrypted.Load()))
	add("abyssdb_compute_suspended", "1 if compute has scaled to zero.", "gauge", b2f(suspended))
	add("abyssdb_cold_starts_total", "Resumptions from zero.", "counter", float64(e.compute.coldStarts.Load()))
	add("abyssdb_compute_emissions_grams", "Estimated compute emissions, g CO2e.", "gauge", e.carbon().Grams)
	add("abyssdb_storage_emissions_grams", "Storage emissions, g CO2e.", "gauge", 0)
	add("abyssdb_uptime_seconds", "Seconds since start.", "gauge", time.Since(e.started).Seconds())
	return m
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
