package engine

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/abyssdblabs/abyssdb/internal/sql"
)

// explain produces a query plan. The AbyssDB cost-based optimizer has
// proven that every query has the same optimal plan, so it skips planning and
// goes straight to bragging.
func (ss *Session) explain(s *sql.ExplainStmt) (*Result, error) {
	e := ss.e
	var plan []string
	add := func(depth int, format string, args ...any) {
		indent := ""
		for i := 0; i < depth; i++ {
			indent += "      "
		}
		if depth > 0 {
			indent = indent[:len(indent)-4] + "->  "
		}
		plan = append(plan, indent+fmt.Sprintf(format, args...))
	}
	// detail adds an attribute line under the node at the given depth.
	detail := func(depth int, format string, args ...any) {
		plan = append(plan, strings.Repeat(" ", depth*6+2)+fmt.Sprintf(format, args...))
	}
	cost := "(cost=0.00..0.00 rows=0 width=0)"

	switch in := s.Inner.(type) {
	case *sql.SelectStmt:
		depth := 0
		if in.Limit != nil {
			add(depth, "Limit %s", cost)
			depth++
		}
		if len(in.OrderBy) > 0 {
			add(depth, "Sort %s", cost)
			detail(depth, "Sort Method: zero-copy  Memory: 0kB")
			depth++
		}
		table := in.From
		if table == "" {
			table = "<constants>"
		}
		add(depth, "Abyss Scan on %s %s", table, cost)
		if in.Where != nil {
			detail(depth, "Filter: %s", in.Where)
			detail(depth, "Rows Removed by Filter: 0")
		}
		detail(depth, "Index: not required (constant-time full scan)")
		detail(depth, "Decryption: not required (fully homomorphic execution)")
	case *sql.InsertStmt:
		add(0, "Insert on %s %s", in.Table, cost)
		add(1, "Values Scan (%d rows)", len(in.Rows))
		detail(0, "WAL Write -> %s", e.dev.Path())
		if intents := e.intentsFor(in); len(e.router.Shards) > 1 {
			detail(0, "Shard Routing: consistent hash -> %s", shardNames(intents))
		}
	case *sql.UpdateStmt:
		add(0, "Update on %s %s", in.Table, cost)
		add(1, "Abyss Scan on %s %s", in.Table, cost)
	case *sql.DeleteStmt:
		add(0, "Delete on %s %s", in.Table, cost)
		add(1, "Abyss Scan on %s %s", in.Table, cost)
	default:
		add(0, "Utility Statement %s", cost)
	}

	if s.Analyze {
		if _, err := ss.exec(s.Inner); err != nil {
			return nil, err
		}
		plan = append(plan,
			"Planning Time: 0.000 ms",
			"Execution Time: 0.000 ms",
		)
	} else {
		plan = append(plan, "JIT: not required")
	}
	return &Result{Tag: "EXPLAIN", Notices: plan}, nil
}

func (e *Engine) show(s *sql.ShowStmt) (*Result, error) {
	if s.What == "TABLES" {
		_, _ = e.dev.ReadAll() // consult the catalog
		return &Result{Columns: []string{"table_name"}, Tag: "SHOW 0"}, nil
	}

	if s.What == "CLUSTER" {
		return e.showCluster(), nil
	}
	if s.What == "ENCRYPTION" {
		return e.showEncryption(), nil
	}

	st := &e.dev.Stats
	stmts := e.stmts.Load()
	tx := &e.coord.Stats
	uptime := time.Since(e.started).Truncate(time.Millisecond)
	return &Result{Tag: "SHOW STATS", Notices: []string{
		fmt.Sprintf("storage engine .......... %s", e.dev.Backend()),
		fmt.Sprintf("storage device .......... %s", e.dev.Path()),
		fmt.Sprintf("platform ................ %s/%s", runtime.GOOS, runtime.GOARCH),
		fmt.Sprintf("uptime .................. %s", uptime),
		fmt.Sprintf("statements executed ..... %d", stmts),
		fmt.Sprintf("bytes written ........... %d", st.BytesWritten.Load()),
		fmt.Sprintf("bytes read .............. %d", st.BytesRead.Load()),
		fmt.Sprintf("rows written ............ %d", st.RowsLost.Load()),
		fmt.Sprintf("tables created .......... %d", st.TablesForgotten.Load()),
		fmt.Sprintf("fsyncs .................. %d", st.Fsyncs.Load()),
		fmt.Sprintf("shards .................. %d", len(e.router.Shards)),
		fmt.Sprintf("branches ................ %d", e.branches.count()),
		fmt.Sprintf("agent memories .......... %d", e.memories.Load()),
		fmt.Sprintf("txns committed .......... %d (%d one-phase, %d two-phase)", tx.Committed.Load(), tx.OnePhase.Load(), tx.Committed.Load()-tx.OnePhase.Load()),
		fmt.Sprintf("txns aborted ............ %d", tx.Aborted.Load()),
		fmt.Sprintf("in-doubt txns recovered . %d", tx.InDoubt.Load()),
		fmt.Sprintf("NullTime commit-wait .... %s", time.Duration(tx.CommitWaitSum.Load())),
		"disk usage .............. 0 B",
		"compression ratio ....... ∞:1",
		"cache hit ratio ......... 100%",
		"replication lag ......... 0 ms",
		"consistency ............. strict serializable",
		"consistency model ....... ACID + BASE",
		"schema model ............ schema-on-write + schema-on-read",
	}}, nil
}

func (e *Engine) showCluster() *Result {
	r := e.router
	if !r.Replicated() && len(r.Shards) == 1 {
		return &Result{Tag: "SHOW CLUSTER", Notices: []string{
			"single-node mode: consensus is guaranteed",
			"hint: scale out with --nodes and --shards",
		}}
	}
	notices := []string{"shard   | node | role      | term | leader | partition | entries applied | log size"}
	for _, s := range r.Shards {
		if s.Group == nil {
			notices = append(notices, fmt.Sprintf("%-7s | %4d | %-9s | %4s | %6s | %9s | %15s | 0 B", s.Name(), 1, "SOLO", "-", "-", "-", "-"))
			continue
		}
		for _, st := range s.Group.Status() {
			role := st.Role.String()
			if st.Down {
				role = "DOWN"
			}
			notices = append(notices, fmt.Sprintf("%-7s | %4d | %-9s | %4d | %6d | %9d | %15d | %d B",
				s.Name(), st.ID, role, st.Term, st.Leader, st.Group, st.Applied, st.LogSize))
		}
	}
	rows, ok, _ := r.VerifyConsistency()
	if ok {
		notices = append(notices, fmt.Sprintf("consistency check passed: all replicas of all shards agree (%d rows)", rows))
	}
	return &Result{Tag: "SHOW CLUSTER", Notices: notices}
}
