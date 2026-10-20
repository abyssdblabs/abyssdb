package engine

import (
	"fmt"
	"testing"

	"github.com/noctarius/abyssdb/internal/void"
)

// BenchmarkTPCB runs pgbench's TPC-B-like transaction (see scripts/pgbench)
// in-process, from parallel sessions, with server defaults.
func BenchmarkTPCB(b *testing.B) {
	dev, err := void.Open()
	if err != nil {
		b.Fatal(err)
	}
	e, err := New(dev, Options{}) // server defaults
	if err != nil {
		b.Fatal(err)
	}
	_, _ = e.ExecString(`CREATE TABLE pgbench_accounts (aid INT NOT NULL, bid INT, abalance INT, filler CHARACTER(84));
CREATE TABLE pgbench_history (tid INT, bid INT, aid INT, delta INT, mtime TIMESTAMP, filler CHARACTER(22));`)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ss := e.NewSession()
		i := 0
		for pb.Next() {
			i++
			q := fmt.Sprintf(`BEGIN;
UPDATE pgbench_accounts SET abalance = abalance + %d WHERE aid = %d;
SELECT abalance FROM pgbench_accounts WHERE aid = %d;
UPDATE pgbench_tellers SET tbalance = tbalance + 1 WHERE tid = 1;
UPDATE pgbench_branches SET bbalance = bbalance + 1 WHERE bid = 1;
INSERT INTO pgbench_history (tid, bid, aid, delta, mtime) VALUES (1, 1, %d, 5, CURRENT_TIMESTAMP);
END;`, i, i, i, i)
			if _, err := ss.ExecString(q); err != nil {
				b.Fatal(err)
			}
		}
	})
}
