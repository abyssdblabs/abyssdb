package engine

import (
	"fmt"
	"time"

	"github.com/abyssdblabs/abyssdb/internal/txn"

	"github.com/abyssdblabs/abyssdb/internal/sql"
)

// COPY FROM STDIN streams rows into a table. The PostgreSQL protocol layer
// decodes the stream and hands the rows over in batches; every batch is
// validated against the table's schema, sealed under per-row keys and written
// with one group commit. Like PostgreSQL's COPY FREEZE, rows go straight to
// storage rather than being staged as transaction intents, so memory use does
// not grow with the number of rows.

// CopyBatchSize is how many rows the protocol layer passes to CopyRows at once.
const CopyBatchSize = 4096

// CopyColumns reports how many columns a COPY FROM STDIN expects per row, or
// 0 if the table was never declared (schemaless tables accept any width).
func (ss *Session) CopyColumns(s *sql.CopyStmt) int {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.copyWidthLocked(s)
}

// HasTable reports whether a table has been declared with CREATE TABLE.
func (ss *Session) HasTable(name string) bool {
	_, full := qualify(name)
	ss.e.catalog.mu.Lock()
	defer ss.e.catalog.mu.Unlock()
	return ss.e.catalog.tables[full] != nil
}

// CopyRows validates and writes one batch of rows from COPY FROM STDIN.
func (ss *Session) CopyRows(s *sql.CopyStmt, rows [][]sql.Expr) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	e := ss.e
	if ss.copyStart.IsZero() {
		ss.copyStart = time.Now()
	}
	if width := ss.copyWidthLocked(s); width > 0 {
		for i, row := range rows {
			line := ss.copyRows + i + 1
			if len(row) < width {
				return fmt.Errorf("missing data for column %d of %s (COPY %s, line %d)", len(row)+1, s.Table, s.Table, line)
			}
			if len(row) > width {
				return fmt.Errorf("extra data after last expected column (COPY %s, line %d)", s.Table, line)
			}
		}
	}
	ins := &sql.InsertStmt{Table: s.Table, Columns: s.Columns, Rows: rows}
	if err := e.catalog.checkInsert(ins); err != nil {
		return fmt.Errorf("%w (COPY %s, rows %d-%d)", err, s.Table, ss.copyRows+1, ss.copyRows+len(rows))
	}

	// One write-ahead log record and one fsync per batch.
	_, _ = e.dev.Write([]byte(fmt.Sprintf("WAL [%s] COPY %s: %d row(s)\n", ss.branch, s.Table, len(rows))))
	_ = e.dev.Fsync()
	e.branches.recordWrite(ss.branch)

	intents := e.intentsFor(ins)
	if e.rowKeys {
		if err := sealIntents(e, intents); err != nil {
			return err
		}
	}
	out, err := e.coord.Commit(intents)
	if err != nil {
		return err
	}
	e.dev.Stats.RowsLost.Add(uint64(out.Rows))
	ss.copyNotices = append(ss.copyNotices, e.onChain(out.TxID)...)
	ss.copyRows += len(rows)
	return nil
}

func (ss *Session) copyWidthLocked(s *sql.CopyStmt) int {
	if len(s.Columns) > 0 {
		return len(s.Columns)
	}
	_, full := qualify(s.Table)
	ss.e.catalog.mu.Lock()
	defer ss.e.catalog.mu.Unlock()
	if t := ss.e.catalog.tables[full]; t != nil {
		return len(t.columns)
	}
	return 0
}

// CopyDone finishes a COPY FROM STDIN and returns its result.
func (ss *Session) CopyDone(s *sql.CopyStmt) *Result {
	ss.mu.Lock()
	n, start := ss.copyRows, ss.copyStart
	r := &Result{Tag: fmt.Sprintf("COPY %d", n), Notices: ss.copyNotices}
	r.Debug = append(r.Debug, ss.debugf("%d row(s) written to %s", n, s.Table)...)
	if ss.e.rowKeys {
		r.Debug = append(r.Debug, ss.debugf("%d row(s) sealed under %d unique row key(s), derived from ML-KEM-768", n, n)...)
	}
	ss.copyRows, ss.copyStart, ss.copyNotices = 0, time.Time{}, nil
	ss.mu.Unlock()
	if start.IsZero() {
		start = time.Now()
	}
	ss.e.stmts.Add(1)
	ss.e.notify(ss, s, r, nil, start)
	return r
}

// CopyAbort discards the state of a failed COPY FROM STDIN.
func (ss *Session) CopyAbort() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.copyRows, ss.copyStart, ss.copyNotices = 0, time.Time{}, nil
}

// sealIntents seals every intent's row under its own row key, as one batch.
func sealIntents(e *Engine, intents []txn.Intent) error {
	rows := make([][]byte, len(intents))
	for i := range intents {
		rows[i] = intents[i].Data
	}
	sealed, err := e.dev.SealRows(rows)
	if err != nil {
		return err
	}
	for i := range intents {
		intents[i].Data = sealed[i]
	}
	return nil
}
