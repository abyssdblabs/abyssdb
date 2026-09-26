package engine

import (
	"strings"
	"testing"
)

const ordersDDL = `CREATE SCHEMA sales;
	CREATE TABLE sales.orders (id BIGINT PRIMARY KEY, customer VARCHAR(10) NOT NULL, total NUMERIC(12,2), paid BOOLEAN);`

func TestSchemaAcceptsValidWrites(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.ExecString(ordersDDL + `
		INSERT INTO sales.orders VALUES (1, 'Acme', 99.50, true), (2, 'Globex', '12.00', 'yes');
		INSERT INTO sales.orders (id, customer) VALUES (3, 'Initech');
		UPDATE sales.orders SET paid = false WHERE id = 1;
		ALTER TABLE sales.orders ADD COLUMN region TEXT;
		CREATE INDEX ON sales.orders (region);
		INSERT INTO undeclared VALUES (1, 'anything', true, NULL);
		DROP TABLE undeclared;
	`); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaRejectsInvalidWrites(t *testing.T) {
	cases := []struct{ sql, want string }{
		{`INSERT INTO sales.orders VALUES (1, NULL, 1, true)`, `violates not-null constraint`},
		{`INSERT INTO sales.orders (id, total) VALUES (1, 5)`, `"customer" of relation "orders" violates not-null`},
		{`INSERT INTO sales.orders VALUES (1, 'Initech Corporation', 1, true)`, `value too long for type character varying(10)`},
		{`INSERT INTO sales.orders VALUES ('one', 'Acme', 1, true)`, `invalid input syntax for type bigint: "one"`},
		{`INSERT INTO sales.orders VALUES (1, 'A', 1, true), (1, 'B', 2, false)`, `duplicate key value violates unique constraint "orders_pkey"`},
		{`INSERT INTO sales.orders VALUES (1, 'a', 1, true, 'extra')`, `more expressions than target columns`},
		{`INSERT INTO sales.orders (id, discount) VALUES (1, 5)`, `column "discount" of relation "orders" does not exist`},
		{`UPDATE sales.orders SET paid = 'maybe'`, `invalid input syntax for type boolean`},
		{`ALTER TABLE sales.orders ADD COLUMN customer TEXT`, `already exists`},
		{`CREATE INDEX ON sales.orders (region)`, `column "region" does not exist`},
		{`DROP SCHEMA sales`, `other objects depend on it`},
		{`CREATE TABLE t (id INT, id TEXT)`, `specified more than once`},
		{`CREATE TABLE t (a INT PRIMARY KEY, b INT PRIMARY KEY)`, `multiple primary keys`},
		{`CREATE TABLE t (a STRING)`, `type "string" does not exist`},
		{`CREATE TABLE crm.leads (id INT)`, `schema "crm" does not exist`},
		{`CREATE TABLE sales.orders (id INT)`, `relation "sales.orders" already exists`},
		{`CREATE TABLE t (a INT); INSERT INTO t VALUES (3000000000)`, `out of range for type integer`},
	}
	for _, c := range cases {
		e := newTestEngine(t)
		if _, err := e.ExecString(ordersDDL); err != nil {
			t.Fatal(err)
		}
		_, err := e.ExecString(c.sql)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s\n  got  %v\n  want %q", c.sql, err, c.want)
		}
	}
}

func TestDropSchemaCascadeCountsTables(t *testing.T) {
	e := newTestEngine(t)
	rs, err := e.ExecString(ordersDDL + `DROP SCHEMA sales CASCADE;`)
	if err != nil {
		t.Fatal(err)
	}
	if got := rs[len(rs)-1].Notices; len(got) == 0 || got[0] != "drop cascades to 1 other object(s)" {
		t.Fatalf("notices = %q", got)
	}
}
