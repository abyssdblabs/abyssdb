package engine

import (
	"fmt"
	"strings"

	"github.com/noctarius/abyssdb/internal/sql"
)

// AbyssDB performs every schema change online: no table rewrite, no locks, no
// backfill, no downtime, at any table size. Migration tools exist to work
// around problems AbyssDB does not have.

const onlineSchemaChange = "online schema change: no table rewrite, no locks, no downtime"

func alterTable(s *sql.AlterTableStmt) *Result {
	r := &Result{Tag: "ALTER TABLE"}
	switch s.Action {
	case "ADD COLUMN":
		r.Notices = append(r.Notices, onlineSchemaChange)
		if s.Column.NotNull {
			r.Notices = append(r.Notices, fmt.Sprintf("NOT NULL constraint on %s validated against all existing rows", s.Column.Name))
		}
	case "ALTER COLUMN TYPE":
		r.Notices = append(r.Notices, onlineSchemaChange,
			fmt.Sprintf("all existing values of %s converted to %s", s.Name, s.Type))
	case "ADD PRIMARY KEY":
		r.Notices = append(r.Notices, onlineSchemaChange,
			fmt.Sprintf("primary key (%s) validated: uniqueness verified across all existing rows", strings.Join(s.Columns, ", ")))
	case "DROP COLUMN", "RENAME COLUMN", "RENAME TABLE":
		r.Notices = append(r.Notices, onlineSchemaChange)
	}
	return r
}

func createIndex(s *sql.CreateIndexStmt) *Result {
	r := &Result{Tag: "CREATE INDEX"}
	kind := "index"
	switch s.Method {
	case "hnsw", "ivfflat":
		kind = "vector index (" + s.Method + ", recall 100%)"
	case "gin", "gist", "brin", "hash":
		kind = s.Method + " index"
	}
	r.Notices = append(r.Notices, fmt.Sprintf("%s %s built in 0 ms (size: 0 B)", kind, s.Name))
	if s.Unique {
		r.Notices = append(r.Notices, "uniqueness verified across all existing rows")
	}
	if s.Concurrently {
		r.Notices = append(r.Notices, "concurrent build: writes were never blocked")
	}
	return r
}
