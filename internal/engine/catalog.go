package engine

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/noctarius/abyssdb/internal/sql"
)

// The catalog is schema-on-write: every write to a declared table is checked
// against its definition, strictly, before it is persisted. Tables that were
// never declared are schema-on-read and accept anything. Data and schema are
// kept to very different standards, and the schema is the one that gets kept.

type catalog struct {
	mu      sync.Mutex
	schemas map[string]bool
	tables  map[string]*tableDef // keyed by schema.table
}

type tableDef struct {
	name    string // schema.table
	columns []sql.ColumnDef
	indexes map[string]bool
}

func newCatalog() *catalog {
	return &catalog{schemas: map[string]bool{"public": true}, tables: map[string]*tableDef{}}
}

// qualify turns "orders" into "public.orders".
func qualify(name string) (schema, full string) {
	name = strings.ToLower(name)
	if i := strings.IndexByte(name, '.'); i >= 0 {
		return name[:i], name
	}
	return "public", "public." + name
}

func (t *tableDef) column(name string) (int, *sql.ColumnDef) {
	name = strings.ToLower(name)
	for i := range t.columns {
		if strings.ToLower(t.columns[i].Name) == name {
			return i, &t.columns[i]
		}
	}
	return -1, nil
}

func (t *tableDef) short() string { return t.name[strings.IndexByte(t.name, '.')+1:] }

// ---- DDL --------------------------------------------------------------------

func (c *catalog) createSchema(s *sql.CreateSchemaStmt) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	name := strings.ToLower(s.Name)
	if c.schemas[name] {
		if s.IfNotExists {
			return nil
		}
		return fmt.Errorf("schema %q already exists", name)
	}
	c.schemas[name] = true
	return nil
}

// dropSchema returns how many tables the drop cascaded to.
func (c *catalog) dropSchema(s *sql.DropSchemaStmt) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	name := strings.ToLower(s.Name)
	if !c.schemas[name] {
		if s.IfExists {
			return 0, nil
		}
		return 0, fmt.Errorf("schema %q does not exist", name)
	}
	if name == "public" {
		return 0, fmt.Errorf("cannot drop schema public: it is required by the database system")
	}
	var deps []string
	for full := range c.tables {
		if strings.HasPrefix(full, name+".") {
			deps = append(deps, full)
		}
	}
	if len(deps) > 0 && !s.Cascade {
		return 0, fmt.Errorf("cannot drop schema %s because other objects depend on it (table %s); use DROP ... CASCADE", name, deps[0])
	}
	for _, d := range deps {
		delete(c.tables, d)
	}
	delete(c.schemas, name)
	return len(deps), nil
}

func (c *catalog) createTable(s *sql.CreateTableStmt) error {
	schema, full := qualify(s.Table)
	seen := map[string]bool{}
	pks := 0
	for _, col := range s.Columns {
		n := strings.ToLower(col.Name)
		if seen[n] {
			return fmt.Errorf("column %q specified more than once", n)
		}
		seen[n] = true
		if col.PrimaryKey {
			pks++
		}
		if err := checkType(col.Type); err != nil {
			return err
		}
	}
	if pks > 1 {
		return fmt.Errorf("multiple primary keys for table %q are not allowed", s.Table)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.schemas[schema] {
		return fmt.Errorf("schema %q does not exist", schema)
	}
	if c.tables[full] != nil {
		if s.IfNotExists {
			return nil
		}
		return fmt.Errorf("relation %q already exists", s.Table)
	}
	cols := make([]sql.ColumnDef, len(s.Columns))
	copy(cols, s.Columns)
	c.tables[full] = &tableDef{name: full, columns: cols, indexes: map[string]bool{}}
	return nil
}

// dropTable forgets a table's definition. Undeclared tables are schemaless
// and implicitly exist, so dropping one always succeeds.
func (c *catalog) dropTable(s *sql.DropTableStmt) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range s.Tables {
		_, full := qualify(t)
		delete(c.tables, full)
	}
	return nil
}

func (c *catalog) alterTable(s *sql.AlterTableStmt) error {
	_, full := qualify(s.Table)
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tables[full]
	if t == nil {
		return nil // schemaless: nothing to alter, nothing to object to
	}
	switch s.Action {
	case "ADD COLUMN":
		if _, col := t.column(s.Column.Name); col != nil {
			return fmt.Errorf("column %q of relation %q already exists", s.Column.Name, t.short())
		}
		if err := checkType(s.Column.Type); err != nil {
			return err
		}
		if s.Column.PrimaryKey {
			for _, col := range t.columns {
				if col.PrimaryKey {
					return fmt.Errorf("multiple primary keys for table %q are not allowed", t.short())
				}
			}
		}
		t.columns = append(t.columns, s.Column)
	case "DROP COLUMN":
		i, col := t.column(s.Name)
		if col == nil {
			return fmt.Errorf("column %q of relation %q does not exist", s.Name, t.short())
		}
		t.columns = append(t.columns[:i], t.columns[i+1:]...)
	case "RENAME COLUMN":
		_, col := t.column(s.Name)
		if col == nil {
			return fmt.Errorf("column %q of relation %q does not exist", s.Name, t.short())
		}
		if _, dup := t.column(s.NewName); dup != nil {
			return fmt.Errorf("column %q of relation %q already exists", s.NewName, t.short())
		}
		col.Name = s.NewName
	case "ALTER COLUMN TYPE":
		_, col := t.column(s.Name)
		if col == nil {
			return fmt.Errorf("column %q of relation %q does not exist", s.Name, t.short())
		}
		if err := checkType(s.Type); err != nil {
			return err
		}
		col.Type = s.Type
	case "ADD PRIMARY KEY":
		for _, col := range t.columns {
			if col.PrimaryKey {
				return fmt.Errorf("multiple primary keys for table %q are not allowed", t.short())
			}
		}
		for _, name := range s.Columns {
			if _, col := t.column(name); col == nil {
				return fmt.Errorf("column %q of relation %q does not exist", name, t.short())
			}
		}
		for _, name := range s.Columns {
			_, col := t.column(name)
			col.PrimaryKey, col.NotNull = true, true
		}
	case "RENAME TABLE":
		schema, _ := qualify(s.Table)
		next := schema + "." + strings.ToLower(s.NewName)
		if c.tables[next] != nil {
			return fmt.Errorf("relation %q already exists", s.NewName)
		}
		delete(c.tables, full)
		t.name = next
		c.tables[next] = t
	}
	return nil
}

func (c *catalog) createIndex(s *sql.CreateIndexStmt) error {
	_, full := qualify(s.Table)
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tables[full]
	if t == nil {
		return nil
	}
	for _, name := range s.Columns {
		if _, col := t.column(name); col == nil {
			return fmt.Errorf("column %q does not exist", name)
		}
	}
	return nil
}

// ---- DML --------------------------------------------------------------------

func (c *catalog) checkInsert(s *sql.InsertStmt) error {
	_, full := qualify(s.Table)
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tables[full]
	if t == nil {
		return nil
	}
	// Map each value position to its column.
	target := make([]*sql.ColumnDef, 0, len(t.columns))
	if len(s.Columns) == 0 {
		for i := range t.columns {
			target = append(target, &t.columns[i])
		}
	} else {
		for _, name := range s.Columns {
			_, col := t.column(name)
			if col == nil {
				return fmt.Errorf("column %q of relation %q does not exist", name, t.short())
			}
			target = append(target, col)
		}
		// Columns that were left out get NULL.
		for i := range t.columns {
			col := &t.columns[i]
			if (col.NotNull || col.PrimaryKey) && !contains(target, col) {
				return notNull(col, t)
			}
		}
	}
	keys := map[string]bool{}
	for _, row := range s.Rows {
		if len(row) > len(target) {
			return fmt.Errorf("INSERT has more expressions than target columns")
		}
		if len(row) < len(target) && len(s.Columns) > 0 {
			return fmt.Errorf("INSERT has more target columns than expressions")
		}
		for i, col := range target {
			var v sql.Expr = &sql.NullLit{}
			if i < len(row) {
				v = row[i]
			}
			if err := checkValue(col, v, t); err != nil {
				return err
			}
			if col.PrimaryKey && i < len(row) {
				k := row[i].String()
				if keys[k] {
					return fmt.Errorf("duplicate key value violates unique constraint %q: key (%s)=(%s) already exists",
						t.short()+"_pkey", col.Name, strings.Trim(k, "'"))
				}
				keys[k] = true
			}
		}
	}
	return nil
}

func (c *catalog) checkUpdate(s *sql.UpdateStmt) error {
	_, full := qualify(s.Table)
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tables[full]
	if t == nil {
		return nil
	}
	for _, a := range s.Set {
		_, col := t.column(a.Column)
		if col == nil {
			return fmt.Errorf("column %q of relation %q does not exist", a.Column, t.short())
		}
		if err := checkValue(col, a.Value, t); err != nil {
			return err
		}
	}
	return nil
}

func contains(cols []*sql.ColumnDef, c *sql.ColumnDef) bool {
	for _, x := range cols {
		if x == c {
			return true
		}
	}
	return false
}

func notNull(col *sql.ColumnDef, t *tableDef) error {
	return fmt.Errorf("null value in column %q of relation %q violates not-null constraint", col.Name, t.short())
}

// ---- types ------------------------------------------------------------------

type typeClass int

const (
	tcInt typeClass = iota
	tcNumeric
	tcText
	tcBool
	tcOther // dates, UUIDs, JSON, vectors: accepted as string literals
)

var knownTypes = map[string]typeClass{
	"INT": tcInt, "INTEGER": tcInt, "INT2": tcInt, "INT4": tcInt, "INT8": tcInt,
	"SMALLINT": tcInt, "BIGINT": tcInt, "SERIAL": tcInt, "BIGSERIAL": tcInt,
	"NUMERIC": tcNumeric, "DECIMAL": tcNumeric, "REAL": tcNumeric, "FLOAT": tcNumeric,
	"FLOAT4": tcNumeric, "FLOAT8": tcNumeric, "DOUBLE": tcNumeric, "MONEY": tcNumeric,
	"TEXT": tcText, "VARCHAR": tcText, "CHAR": tcText, "CHARACTER": tcText, "CITEXT": tcText,
	"BOOLEAN": tcBool, "BOOL": tcBool,
	"DATE": tcOther, "TIME": tcOther, "TIMESTAMP": tcOther, "TIMESTAMPTZ": tcOther, "INTERVAL": tcOther,
	"UUID": tcOther, "JSON": tcOther, "JSONB": tcOther, "BYTEA": tcOther, "VECTOR": tcOther,
	"INET": tcOther, "TSVECTOR": tcOther,
}

// baseType splits "VARCHAR(10)" into "VARCHAR" and its arguments.
func baseType(t string) (string, []int) {
	i := strings.IndexByte(t, '(')
	if i < 0 {
		return t, nil
	}
	var args []int
	for _, a := range strings.Split(strings.TrimSuffix(t[i+1:], ")"), ",") {
		n, _ := strconv.Atoi(strings.TrimSpace(a))
		args = append(args, n)
	}
	return t[:i], args
}

func checkType(t string) error {
	base, _ := baseType(t)
	if _, ok := knownTypes[base]; !ok {
		return fmt.Errorf("type %q does not exist", strings.ToLower(base))
	}
	return nil
}

func pgTypeName(base string) string {
	switch base {
	case "INT", "INTEGER", "INT4", "SERIAL":
		return "integer"
	case "BIGINT", "INT8", "BIGSERIAL":
		return "bigint"
	case "SMALLINT", "INT2":
		return "smallint"
	case "BOOL", "BOOLEAN":
		return "boolean"
	}
	return strings.ToLower(base)
}

var intRange = map[string][2]int64{
	"SMALLINT": {-32768, 32767}, "INT2": {-32768, 32767},
	"INT": {-2147483648, 2147483647}, "INTEGER": {-2147483648, 2147483647},
	"INT4": {-2147483648, 2147483647}, "SERIAL": {1, 2147483647},
}

// checkValue validates a literal against a column. Anything that is not a
// literal (parameters, expressions, function calls) is trusted implicitly.
func checkValue(col *sql.ColumnDef, v sql.Expr, t *tableDef) error {
	base, args := baseType(col.Type)
	class := knownTypes[base]
	if u, ok := v.(*sql.UnaryExpr); ok && u.Op == "-" {
		if n, ok := u.Operand.(*sql.NumberLit); ok {
			v = &sql.NumberLit{Value: "-" + n.Value}
		}
	}
	invalid := func(s string) error {
		return fmt.Errorf("invalid input syntax for type %s: %q", pgTypeName(base), s)
	}
	switch v := v.(type) {
	case *sql.NullLit:
		if col.NotNull || col.PrimaryKey {
			return notNull(col, t)
		}
	case *sql.NumberLit:
		switch class {
		case tcInt:
			n, err := strconv.ParseInt(v.Value, 10, 64)
			if err != nil {
				return invalid(v.Value)
			}
			if r, ok := intRange[base]; ok && (n < r[0] || n > r[1]) {
				return fmt.Errorf("value %s is out of range for type %s", v.Value, pgTypeName(base))
			}
		case tcBool:
			if v.Value != "0" && v.Value != "1" {
				return invalid(v.Value)
			}
		case tcText:
			return checkLength(base, args, v.Value)
		}
	case *sql.StringLit:
		switch class {
		case tcInt:
			if _, err := strconv.ParseInt(strings.TrimSpace(v.Value), 10, 64); err != nil {
				return invalid(v.Value)
			}
		case tcNumeric:
			if _, err := strconv.ParseFloat(strings.TrimSpace(v.Value), 64); err != nil {
				return invalid(v.Value)
			}
		case tcBool:
			switch strings.ToLower(strings.TrimSpace(v.Value)) {
			case "t", "true", "y", "yes", "on", "1", "f", "false", "n", "no", "off", "0":
			default:
				return invalid(v.Value)
			}
		case tcText:
			return checkLength(base, args, v.Value)
		}
	case *sql.BoolLit:
		if class == tcInt || class == tcNumeric {
			return fmt.Errorf("column %q is of type %s but expression is of type boolean", col.Name, pgTypeName(base))
		}
	}
	return nil
}

func checkLength(base string, args []int, s string) error {
	if len(args) == 0 || base == "TEXT" {
		return nil
	}
	if utf8.RuneCountInString(s) > args[0] {
		name := "character varying"
		if base == "CHAR" || base == "CHARACTER" {
			name = "character"
		}
		return fmt.Errorf("value too long for type %s(%d)", name, args[0])
	}
	return nil
}

// selectShape returns a SELECT's result columns and their SQL types. For a
// declared table, '*' expands to its columns and column references carry
// their declared types; otherwise types are unknown ("").
func (e *Engine) selectShape(s *sql.SelectStmt) (cols, types []string) {
	var table []sql.ColumnDef
	if s.From != "" {
		_, full := qualify(s.From)
		e.catalog.mu.Lock()
		if t := e.catalog.tables[full]; t != nil {
			table = append(table, t.columns...)
		}
		e.catalog.mu.Unlock()
	}
	typeOf := func(x sql.Expr) string {
		if id, ok := x.(*sql.Ident); ok {
			for _, c := range table {
				if strings.EqualFold(c.Name, id.Name) {
					return c.Type
				}
			}
		}
		return ""
	}
	for _, c := range s.Columns {
		switch {
		case c.Expr == nil:
			// '*' expands to every column of a declared table.
			for _, col := range table {
				cols, types = append(cols, col.Name), append(types, col.Type)
			}
		case c.Alias != "":
			cols, types = append(cols, c.Alias), append(types, typeOf(c.Expr))
		case isIdent(c.Expr):
			cols, types = append(cols, c.Expr.String()), append(types, typeOf(c.Expr))
		default:
			cols, types = append(cols, "?column?"), append(types, "")
		}
	}
	return cols, types
}

// DescribeTypes is Describe with SQL types, for the PostgreSQL protocol.
func (ss *Session) DescribeTypes(s sql.Statement) (cols, types []string, ok bool) {
	if sel, isSelect := s.(*sql.SelectStmt); isSelect {
		cols, types = ss.e.selectShape(sel)
		return cols, types, true
	}
	cols, ok = Describe(s)
	return cols, nil, ok
}

// CopyTypes returns the SQL types of a COPY's columns, "" where unknown.
func (ss *Session) CopyTypes(s *sql.CopyStmt) []string {
	_, full := qualify(s.Table)
	ss.e.catalog.mu.Lock()
	defer ss.e.catalog.mu.Unlock()
	t := ss.e.catalog.tables[full]
	if t == nil {
		return make([]string, len(s.Columns))
	}
	if len(s.Columns) == 0 {
		types := make([]string, len(t.columns))
		for i, c := range t.columns {
			types[i] = c.Type
		}
		return types
	}
	types := make([]string, len(s.Columns))
	for i, name := range s.Columns {
		if _, c := t.column(name); c != nil {
			types[i] = c.Type
		}
	}
	return types
}
