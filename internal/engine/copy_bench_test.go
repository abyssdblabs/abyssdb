package engine

import (
	"strconv"
	"testing"

	"github.com/abyssdblabs/abyssdb/internal/sql"
	"github.com/abyssdblabs/abyssdb/internal/void"
)

// BenchmarkCopyRows measures COPY FROM STDIN in-process: one op is one row of
// pgbench_accounts, validated, sealed under its own key and committed.
func BenchmarkCopyRows(b *testing.B) {
	dev, err := void.Open()
	if err != nil {
		b.Fatal(err)
	}
	defer dev.Close()
	e, err := New(dev, Options{})
	if err != nil {
		b.Fatal(err)
	}
	ss := e.NewSession()
	if _, err := ss.ExecString("CREATE TABLE acc (aid INT NOT NULL, bid INT, abalance INT, filler CHAR(84))"); err != nil {
		b.Fatal(err)
	}
	cp := &sql.CopyStmt{Table: "acc", From: true}
	batch := make([][]sql.Expr, CopyBatchSize)
	b.ResetTimer()
	for done := 0; done < b.N; {
		n := min(CopyBatchSize, b.N-done)
		for i := range n {
			batch[i] = []sql.Expr{&sql.StringLit{Value: strconv.Itoa(done + i)}, &sql.StringLit{Value: "1"},
				&sql.StringLit{Value: "0"}, &sql.StringLit{Value: ""}}
		}
		if err := ss.CopyRows(cp, batch[:n]); err != nil {
			b.Fatal(err)
		}
		done += n
	}
	ss.CopyDone(cp)
}
