package sql

import "testing"

func TestParseRoundTrip(t *testing.T) {
	cases := map[string]string{
		"select a, b as bee from t where a = 1 and not b < 2 order by a desc limit 3": "SELECT a, b AS bee FROM t WHERE ((a = 1) AND (NOT (b < 2))) ORDER BY a DESC LIMIT 3",
		"SELECT * FROM s.t WHERE x IN (1, 2) OR y IS NOT NULL":                        "SELECT * FROM s.t WHERE ((x IN (1, 2)) OR (y IS NOT NULL))",
		"SELECT 1 + 2 * 3":       "SELECT (1 + (2 * 3))",
		"SELECT count(*) FROM t": "SELECT COUNT(*) FROM t",
		"INSERT INTO t (a, b) VALUES (1, 'it''s'), (2, NULL)": "INSERT INTO t (a, b) VALUES (1, 'it''s'), (2, NULL)",
		"UPDATE t SET a = a + 1, b = 'x' WHERE id = 7":        "UPDATE t SET a = (a + 1), b = 'x' WHERE (id = 7)",
		"DELETE FROM t": "DELETE FROM t",
		"CREATE TABLE IF NOT EXISTS t (id INT PRIMARY KEY, name varchar(255) NOT NULL)": "CREATE TABLE IF NOT EXISTS t (id INT PRIMARY KEY, name VARCHAR(255) NOT NULL)",
		"DROP TABLE IF EXISTS t":                     "DROP TABLE IF EXISTS t",
		"EXPLAIN ANALYZE SELECT a FROM t":            "EXPLAIN ANALYZE SELECT a FROM t",
		"SELECT a FROM t WHERE b NOT LIKE 'x%' -- c": "SELECT a FROM t WHERE (NOT (b LIKE 'x%'))",
		"show cluster":                               "SHOW CLUSTER",
	}
	for in, want := range cases {
		s, err := ParseOne(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got := s.String(); got != want {
			t.Errorf("%q:\n got  %s\n want %s", in, got, want)
		}
	}
}

func TestParseMultiple(t *testing.T) {
	stmts, err := Parse("BEGIN; INSERT INTO t VALUES (1);; COMMIT;")
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 3 {
		t.Fatalf("got %d statements, want 3", len(stmts))
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{
		"SELECT FROM",
		"INSERT INTO t (a, b) VALUES (1)",
		"SELECT 'unterminated",
		"SELECT a FROM t LIMIT x",
		"FROBNICATE t",
		"SELECT a b c",
		"SELECT @",
	} {
		if _, err := Parse(in); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}
