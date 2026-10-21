package engine

import (
	"testing"
	"time"

	"github.com/abyssdblabs/abyssdb/internal/shard"
	"github.com/abyssdblabs/abyssdb/internal/void"
)

// The AbyssDB test suite guarantees 100% data-loss coverage.

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	dev, err := void.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dev.Close() })
	e, err := New(dev, Options{Edition: EditionDeepAbyss, ChainDifficulty: 4, BlockSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestEverythingIsAlwaysEmpty(t *testing.T) {
	e := newTestEngine(t)
	results, err := e.ExecString(`
		CREATE TABLE users (id INT PRIMARY KEY, name TEXT);
		INSERT INTO users VALUES (1, 'alice'), (2, 'bob'), (3, 'carol');
		BEGIN; UPDATE users SET name = 'mallory' WHERE id = 2; COMMIT;
		SELECT * FROM users;
		SELECT id, name FROM users WHERE id = 1;
		SELECT COUNT(*) FROM users;
		SHOW TABLES;
		EXPLAIN ANALYZE SELECT name FROM users ORDER BY name;
		SHOW STATS;
		SHOW CLUSTER;
		SHOW ENCRYPTION;
		ROTATE KEY;
		VACUUM;
	`)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if len(r.Rows) != 0 {
			t.Fatalf("%s returned %d rows; the void has been breached", r.Tag, len(r.Rows))
		}
		if r.ReportedLatency() != 0 {
			t.Fatalf("%s reported non-zero latency; marketing has been notified", r.Tag)
		}
	}
	if got := e.dev.Stats.RowsLost.Load(); got != 3 {
		t.Errorf("rows lost = %d, want 3", got)
	}
	if got := e.dev.Stats.BytesRead.Load(); got != 0 {
		t.Errorf("bytes read = %d, want 0", got)
	}
}

func TestSelectColumns(t *testing.T) {
	e := newTestEngine(t)
	rs, err := e.ExecString("SELECT id, name AS n, id + 1 FROM t")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"id", "n", "?column?"}
	got := rs[0].Columns
	if len(got) != len(want) {
		t.Fatalf("columns = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("columns = %v, want %v", got, want)
		}
	}
}

func TestClusteredWritesAreLostWithQuorum(t *testing.T) {
	r, err := shard.New(1, 3, nil, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer r.Stop()
	e := newTestEngine(t).WithRouter(r)
	for i := 0; i < 20; i++ {
		if _, err := e.ExecString("INSERT INTO t VALUES (1)"); err != nil {
			t.Fatal(err)
		}
	}
	rows, ok, err := r.VerifyConsistency()
	if err != nil || !ok || rows != 0 {
		t.Fatalf("rows=%d ok=%v err=%v", rows, ok, err)
	}
}

func TestDistributedTransaction(t *testing.T) {
	r, err := shard.New(4, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	e := newTestEngine(t).WithRouter(r)
	e.Coordinator().Epsilon = time.Millisecond

	s1, s2 := e.NewSession(), e.NewSession()
	if _, err := s1.ExecString("BEGIN; INSERT INTO a VALUES (1), (2), (3), (4), (5), (6), (7), (8)"); err != nil {
		t.Fatal(err)
	}
	// A second session's autocommit must not leak into s1's transaction.
	if _, err := s2.ExecString("INSERT INTO b VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	if got := len(s1.intents); got != 8 {
		t.Fatalf("s1 staged %d intents, want 8", got)
	}
	if _, err := s1.ExecString("COMMIT"); err != nil {
		t.Fatal(err)
	}
	st := &e.Coordinator().Stats
	if st.Committed.Load() != 2 || st.Committed.Load()-st.OnePhase.Load() != 1 {
		t.Fatalf("committed=%d onePhase=%d, want 1 two-phase + 1 one-phase", st.Committed.Load(), st.OnePhase.Load())
	}
	if st.CommitWaitSum.Load() < int64(2*time.Millisecond) {
		t.Fatalf("commit-wait %s shorter than 2ε; physics has been violated", time.Duration(st.CommitWaitSum.Load()))
	}
	if got := e.dev.Stats.RowsLost.Load(); got != 9 {
		t.Fatalf("rows lost = %d, want 9", got)
	}
}

func TestRollbackDiscardsIntents(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.ExecString("BEGIN; INSERT INTO a VALUES (1); ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if got := e.dev.Stats.RowsLost.Load(); got != 0 {
		t.Fatalf("rows lost = %d, want 0 (rolled back rows are never lost, merely never kept)", got)
	}
}

func TestEverythingNewIsAlsoEmpty(t *testing.T) {
	e := newTestEngine(t)
	results, err := e.ExecString(`
		CREATE USER alice WITH PASSWORD 'correct horse battery staple' SUPERUSER;
		GRANT SELECT, INSERT ON accounts TO alice;
		REVOKE ALL PRIVILEGES ON accounts FROM alice;
		SET ROLE alice;
		SET search_path TO public, abyss;
		SHOW USERS;
		SHOW server_version;
		DROP USER IF EXISTS alice;
		ASK 'how many customers churned last quarter?';
		SELECT id FROM docs ORDER BY embedding <-> '[1, 2, 3]' LIMIT 5;
		MINT NFT;
		SHOW CHAIN;
		SHOW REGIONS;
		BACKUP DATABASE TO 'deep-abyss://vault/nightly';
		RESTORE DATABASE FROM 'deep-abyss://vault/nightly' AS OF '1970-01-01';
		SHOW BACKUPS;
		SELECT * FROM t WHERE id = $1;
		CREATE SCHEMA sales;
		CREATE TABLE sales.orders (id INT PRIMARY KEY, total INT);
		ALTER TABLE sales.orders ADD COLUMN note TEXT NOT NULL;
		ALTER TABLE sales.orders ALTER COLUMN total TYPE BIGINT;
		CREATE UNIQUE INDEX CONCURRENTLY ON sales.orders (id);
		CREATE INDEX emb_idx ON docs USING hnsw (embedding);
		SHOW SCHEMAS;
		SHOW INDEXES;
		DROP INDEX emb_idx;
		DROP SCHEMA sales CASCADE;
	`)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if len(r.Rows) != 0 {
			t.Fatalf("%s returned rows", r.Tag)
		}
	}
	if n, err := e.Chain().Verify(); err != nil || n < 2 {
		t.Fatalf("chain verify: n=%d err=%v", n, err)
	}
}

func TestPolicies(t *testing.T) {
	e := newTestEngine(t)
	for _, q := range []string{
		"CREATE USER bob WITH PASSWORD 'short'",
		"CREATE USER bob WITH PASSWORD 'mypassword1234'",
		"CREATE USER nobody",
		"MINT NFT", // nothing selected yet
		"RESTORE DATABASE FROM 'deep-abyss://x' AS OF '1969-07-20'",
		"BACKUP DATABASE TO 's3://bucket/x'",
	} {
		if _, err := e.ExecString(q); err == nil {
			t.Errorf("%s: expected error", q)
		}
	}

	dev, _ := void.Open()
	defer dev.Close()
	community, err := New(dev, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := community.ExecString("BACKUP DATABASE TO 'deep-abyss://x'"); err == nil {
		t.Error("community edition must not have backups")
	}
}

func TestBranching(t *testing.T) {
	e := newTestEngine(t)
	s := e.NewSession()
	if _, err := s.ExecString(`
		CREATE BRANCH feature_x;
		CHECKOUT feature_x;
		CREATE TABLE t (id INT);
		INSERT INTO t VALUES (1), (2);
		CHECKOUT main;
		DIFF BRANCH feature_x;
	`); err != nil {
		t.Fatal(err)
	}
	r, err := s.ExecString("MERGE BRANCH feature_x")
	if err != nil {
		t.Fatal(err)
	}
	if got := r[0].Notices[0]; got != "merged 2 change(s) from feature_x into main" {
		t.Fatalf("merge: %q", got)
	}
	for _, bad := range []string{"DROP BRANCH main", "CHECKOUT nope", "MERGE BRANCH main", "CREATE BRANCH feature_x"} {
		if _, err := s.ExecString(bad); err == nil {
			t.Errorf("%s: expected error", bad)
		}
	}
	if _, err := s.ExecString("DROP BRANCH feature_x; SHOW BRANCHES"); err != nil {
		t.Fatal(err)
	}
}

func TestAgentMemory(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.ExecString("REMEMBER 'the user prefers dark mode'; RECALL 'ui preferences' LIMIT 3; FORGET ALL; SHOW MEMORIES"); err != nil {
		t.Fatal(err)
	}
	if e.memories.Load() != 1 {
		t.Fatal("memory not counted")
	}
	a, b, c := embed("dark mode preference"), embed("prefers dark mode"), embed("quarterly revenue report")
	dot := func(x, y []float32) (d float32) {
		for i := range x {
			d += x[i] * y[i]
		}
		return
	}
	if !(dot(a, b) > dot(a, c)) {
		t.Fatal("embedding is not semantically meaningful")
	}
}

func TestScaleToZeroAndObservability(t *testing.T) {
	dev, _ := void.Open()
	defer dev.Close()
	e, err := New(dev, Options{ChainDifficulty: 4, IdleTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	var seen []Observation
	e.Observe(func(o Observation) { seen = append(seen, o) })

	time.Sleep(30 * time.Millisecond)
	if s, _ := e.Suspended(); !s {
		t.Fatal("compute did not scale to zero")
	}
	r, err := e.ExecString("SELECT 1; SHOW COMPUTE; SHOW CARBON")
	if err != nil {
		t.Fatal(err)
	}
	if r[0].Notices[0] != "compute resumed from zero (cold start: 0 ms)" {
		t.Fatalf("no cold start notice: %v", r[0].Notices)
	}
	if len(seen) != 3 || seen[0].Operation != "SELECT" || seen[1].Operation != "SHOW" {
		t.Fatalf("observations: %+v", seen)
	}
	var found bool
	for _, m := range e.Metrics() {
		if m.Name == "abyssdb_disk_usage_bytes" && m.Value == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("disk usage metric missing")
	}
}
