package pgwire

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestCopy(t *testing.T) {
	addr := startServer(t, false)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, "postgres://nobody@"+addr+"/abyss?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "CREATE TABLE cust (id BIGINT PRIMARY KEY, name VARCHAR(10), paid BOOLEAN)"); err != nil {
		t.Fatal(err)
	}

	t.Run("binary via pgx.CopyFrom", func(t *testing.T) {
		rows := make([][]any, 10000)
		for i := range rows {
			rows[i] = []any{i, "Acme", true}
		}
		n, err := conn.CopyFrom(ctx, pgx.Identifier{"cust"}, []string{"id", "name", "paid"}, pgx.CopyFromRows(rows))
		if err != nil || n != int64(len(rows)) {
			t.Fatalf("CopyFrom = %d, %v; want %d", n, err, len(rows))
		}
	})

	copyText := func(data string) (string, error) {
		tag, err := conn.PgConn().CopyFrom(ctx, strings.NewReader(data), "COPY cust FROM STDIN")
		return tag.String(), err
	}
	t.Run("text with escapes and NULL", func(t *testing.T) {
		tag, err := copyText("1\tAc\\tme\tt\n2\t\\N\tfalse\n3\tx\\\\y\t\\N\n")
		if err != nil || tag != "COPY 3" {
			t.Fatalf("COPY = %q, %v", tag, err)
		}
	})
	t.Run("end-of-data marker", func(t *testing.T) {
		tag, err := copyText("1\ta\tt\n\\.\n2\tb\tt\n")
		if err != nil || tag != "COPY 1" {
			t.Fatalf("COPY = %q, %v; want COPY 1", tag, err)
		}
	})
	for _, c := range []struct{ name, data, want string }{
		{"wrong type", "1\tAcme\tt\ntwo\tGlobex\tt\n", `invalid input syntax for type bigint: "two"`},
		{"too long", "1\tInitech Corporation\tt\n", "value too long for type character varying(10)"},
		{"missing column", "1\tAcme\n", "missing data for column 3"},
		{"extra column", "1\tAcme\tt\textra\n", "extra data after last expected column"},
		{"duplicate key", "1\ta\tt\n1\tb\tt\n", "duplicate key value"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := copyText(c.data); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
			// The connection is still usable after a failed COPY.
			if _, err := conn.Exec(ctx, "SELECT 1"); err != nil {
				t.Fatalf("connection broken after failed COPY: %v", err)
			}
		})
	}

	t.Run("COPY TO STDOUT", func(t *testing.T) {
		var out bytes.Buffer
		tag, err := conn.PgConn().CopyTo(ctx, &out, "COPY cust TO STDOUT")
		if err != nil || tag.String() != "COPY 0" || out.Len() != 0 {
			t.Fatalf("COPY TO = %q, %d bytes, %v", tag, out.Len(), err)
		}
	})

	t.Run("relkind catalog lookup", func(t *testing.T) {
		var kind string
		err := conn.QueryRow(ctx, "SELECT relkind FROM pg_catalog.pg_class WHERE oid=$1::pg_catalog.regclass", "cust").Scan(&kind)
		if err != nil || kind != "r" {
			t.Fatalf("relkind = %q, %v; want r", kind, err)
		}
		err = conn.QueryRow(ctx, "SELECT relkind FROM pg_catalog.pg_class WHERE oid=$1::pg_catalog.regclass", "nope").Scan(&kind)
		if err == nil || !strings.Contains(err.Error(), `relation "nope" does not exist`) {
			t.Fatalf("unknown table: %v", err)
		}
	})
}
