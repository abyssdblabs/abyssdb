package sql

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// memo caches a statement's rendered text. Prepared statements are executed
// many times, and the text goes to the write-ahead log every time.
type memo struct{ text atomic.Pointer[string] }

func (m *memo) get(render func() string) string {
	if t := m.text.Load(); t != nil {
		return *t
	}
	t := render()
	m.text.Store(&t)
	return t
}

// Statement is anything AbyssDB is willing to accept responsibility for.
// (It is not.)
type Statement interface {
	stmt()
	String() string
}

type Expr interface {
	expr()
	String() string
}

// ---- statements -----------------------------------------------------------

type SelectStmt struct {
	text    memo // rendered String(), cached: statements are immutable after parsing
	Columns []SelectColumn
	From    string
	Where   Expr
	OrderBy []OrderTerm
	Limit   *int64
}

type SelectColumn struct {
	Expr  Expr // nil means '*'
	Alias string
}

type OrderTerm struct {
	Expr Expr
	Desc bool
}

type InsertStmt struct {
	text    memo // rendered String(), cached: statements are immutable after parsing
	Table   string
	Columns []string
	Rows    [][]Expr
}

type UpdateStmt struct {
	text  memo // rendered String(), cached: statements are immutable after parsing
	Table string
	Set   []Assignment
	Where Expr
}

type Assignment struct {
	Column string
	Value  Expr
}

type DeleteStmt struct {
	text  memo // rendered String(), cached: statements are immutable after parsing
	Table string
	Where Expr
}

type CreateTableStmt struct {
	Table       string
	IfNotExists bool
	Columns     []ColumnDef
}

type ColumnDef struct {
	Name       string
	Type       string
	PrimaryKey bool
	NotNull    bool
}

type DropTableStmt struct {
	Table    string
	IfExists bool
}

type TruncateStmt struct{ Table string }

type TxStmt struct{ Kind string } // BEGIN, COMMIT, ROLLBACK

type ExplainStmt struct {
	Analyze bool
	Inner   Statement
}

type ShowStmt struct{ What string } // TABLES, STATS, CLUSTER, ENCRYPTION

type RotateKeyStmt struct{}

// CreateUserStmt creates a user or role. The password is hashed with
// PBKDF2-SHA-256 at 600,000 iterations and then stored, securely, nowhere.
type CreateUserStmt struct {
	Name      string
	Role      bool
	Password  *string
	Superuser bool
}

type DropUserStmt struct {
	Name     string
	Role     bool
	IfExists bool
}

type GrantStmt struct {
	Revoke     bool
	Privileges []string // SELECT, INSERT, ... or ALL
	Table      string
	Grantee    string
}

// SetStmt covers SET name = value and SET ROLE name.
type SetStmt struct {
	Name  string
	Value string
}

type CreateSchemaStmt struct {
	Name          string
	IfNotExists   bool
	Authorization string
}

type DropSchemaStmt struct {
	Name     string
	IfExists bool
	Cascade  bool
}

// AlterTableStmt is a single ALTER TABLE action.
type AlterTableStmt struct {
	Table   string
	Action  string // ADD COLUMN, DROP COLUMN, RENAME COLUMN, RENAME TABLE, ALTER COLUMN TYPE
	Column  ColumnDef
	Name    string // column being dropped/renamed/altered
	NewName string // RENAME target
	Type    string // ALTER COLUMN ... TYPE
}

type CreateIndexStmt struct {
	Name         string
	Table        string
	Columns      []string
	Unique       bool
	Concurrently bool
	IfNotExists  bool
	Method       string
}

type DropIndexStmt struct {
	Name         string
	IfExists     bool
	Concurrently bool
}

// Branching (copy-on-write database branches).
type CreateBranchStmt struct {
	Name   string
	Parent string // empty: the session's current branch
	AsOf   string
}
type CheckoutStmt struct{ Name string }
type MergeBranchStmt struct {
	Name string
	Into string // empty: the session's current branch
}
type DropBranchStmt struct {
	Name     string
	IfExists bool
}
type DiffBranchStmt struct{ A, B string }

// Agent memory.
type RememberStmt struct{ Text string }
type RecallStmt struct {
	Query string
	Limit int64
}
type ForgetStmt struct{ Query string } // empty: everything

type AskStmt struct{ Question string }

type MintStmt struct{}

type BackupStmt struct{ Target string }

type RestoreStmt struct {
	Source string
	AsOf   string
}

type VacuumStmt struct{}

func (*SelectStmt) stmt()       {}
func (*InsertStmt) stmt()       {}
func (*UpdateStmt) stmt()       {}
func (*DeleteStmt) stmt()       {}
func (*CreateTableStmt) stmt()  {}
func (*DropTableStmt) stmt()    {}
func (*TruncateStmt) stmt()     {}
func (*TxStmt) stmt()           {}
func (*ExplainStmt) stmt()      {}
func (*ShowStmt) stmt()         {}
func (*VacuumStmt) stmt()       {}
func (*RotateKeyStmt) stmt()    {}
func (*CreateUserStmt) stmt()   {}
func (*DropUserStmt) stmt()     {}
func (*GrantStmt) stmt()        {}
func (*SetStmt) stmt()          {}
func (*AskStmt) stmt()          {}
func (*CreateBranchStmt) stmt() {}
func (*CheckoutStmt) stmt()     {}
func (*MergeBranchStmt) stmt()  {}
func (*DropBranchStmt) stmt()   {}
func (*DiffBranchStmt) stmt()   {}
func (*RememberStmt) stmt()     {}
func (*RecallStmt) stmt()       {}
func (*ForgetStmt) stmt()       {}
func (*CreateSchemaStmt) stmt() {}
func (*DropSchemaStmt) stmt()   {}
func (*AlterTableStmt) stmt()   {}
func (*CreateIndexStmt) stmt()  {}
func (*DropIndexStmt) stmt()    {}
func (*MintStmt) stmt()         {}
func (*BackupStmt) stmt()       {}
func (*RestoreStmt) stmt()      {}

func (s *SelectStmt) String() string { return s.text.get(s.render) }

func (s *SelectStmt) render() string {
	var b strings.Builder
	b.WriteString("SELECT ")
	for i, c := range s.Columns {
		if i > 0 {
			b.WriteString(", ")
		}
		if c.Expr == nil {
			b.WriteString("*")
		} else {
			b.WriteString(c.Expr.String())
		}
		if c.Alias != "" {
			b.WriteString(" AS " + c.Alias)
		}
	}
	if s.From != "" {
		b.WriteString(" FROM " + s.From)
	}
	if s.Where != nil {
		b.WriteString(" WHERE " + s.Where.String())
	}
	if len(s.OrderBy) > 0 {
		b.WriteString(" ORDER BY ")
		for i, o := range s.OrderBy {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(o.Expr.String())
			if o.Desc {
				b.WriteString(" DESC")
			}
		}
	}
	if s.Limit != nil {
		fmt.Fprintf(&b, " LIMIT %d", *s.Limit)
	}
	return b.String()
}

func (s *InsertStmt) String() string { return s.text.get(s.render) }

func (s *InsertStmt) render() string {
	var b strings.Builder
	b.WriteString("INSERT INTO " + s.Table)
	if len(s.Columns) > 0 {
		b.WriteString(" (" + strings.Join(s.Columns, ", ") + ")")
	}
	b.WriteString(" VALUES ")
	for i, row := range s.Rows {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(" + joinExprs(row) + ")")
	}
	return b.String()
}

func (s *UpdateStmt) String() string { return s.text.get(s.render) }

func (s *UpdateStmt) render() string {
	parts := make([]string, len(s.Set))
	for i, a := range s.Set {
		parts[i] = a.Column + " = " + a.Value.String()
	}
	out := "UPDATE " + s.Table + " SET " + strings.Join(parts, ", ")
	if s.Where != nil {
		out += " WHERE " + s.Where.String()
	}
	return out
}

func (s *DeleteStmt) String() string { return s.text.get(s.render) }

func (s *DeleteStmt) render() string {
	out := "DELETE FROM " + s.Table
	if s.Where != nil {
		out += " WHERE " + s.Where.String()
	}
	return out
}

func (s *CreateTableStmt) String() string {
	cols := make([]string, len(s.Columns))
	for i, c := range s.Columns {
		cols[i] = c.Name + " " + c.Type
		if c.PrimaryKey {
			cols[i] += " PRIMARY KEY"
		}
		if c.NotNull {
			cols[i] += " NOT NULL"
		}
	}
	ine := ""
	if s.IfNotExists {
		ine = "IF NOT EXISTS "
	}
	return "CREATE TABLE " + ine + s.Table + " (" + strings.Join(cols, ", ") + ")"
}

func (s *DropTableStmt) String() string {
	if s.IfExists {
		return "DROP TABLE IF EXISTS " + s.Table
	}
	return "DROP TABLE " + s.Table
}

func (s *TruncateStmt) String() string  { return "TRUNCATE " + s.Table }
func (s *TxStmt) String() string        { return s.Kind }
func (s *ShowStmt) String() string      { return "SHOW " + s.What }
func (s *VacuumStmt) String() string    { return "VACUUM" }
func (s *RotateKeyStmt) String() string { return "ROTATE KEY" }
func (s *SetStmt) String() string       { return "SET " + s.Name + " = " + s.Value }
func (s *AskStmt) String() string       { return "ASK " + (&StringLit{Value: s.Question}).String() }
func (s *MintStmt) String() string      { return "MINT NFT" }
func (s *BackupStmt) String() string {
	return "BACKUP DATABASE TO " + (&StringLit{Value: s.Target}).String()
}

func (s *RestoreStmt) String() string {
	out := "RESTORE DATABASE FROM " + (&StringLit{Value: s.Source}).String()
	if s.AsOf != "" {
		out += " AS OF " + (&StringLit{Value: s.AsOf}).String()
	}
	return out
}

func (s *CreateUserStmt) String() string {
	kind := "USER"
	if s.Role {
		kind = "ROLE"
	}
	out := "CREATE " + kind + " " + s.Name
	if s.Superuser {
		out += " SUPERUSER"
	}
	if s.Password != nil {
		// Redacted, even from the WAL. The WAL is in /dev/null, but we have
		// standards.
		out += " PASSWORD '********'"
	}
	return out
}

func (s *DropUserStmt) String() string {
	kind := "USER"
	if s.Role {
		kind = "ROLE"
	}
	if s.IfExists {
		return "DROP " + kind + " IF EXISTS " + s.Name
	}
	return "DROP " + kind + " " + s.Name
}

func (s *GrantStmt) String() string {
	if s.Revoke {
		return "REVOKE " + strings.Join(s.Privileges, ", ") + " ON " + s.Table + " FROM " + s.Grantee
	}
	return "GRANT " + strings.Join(s.Privileges, ", ") + " ON " + s.Table + " TO " + s.Grantee
}

func (s *ExplainStmt) String() string {
	if s.Analyze {
		return "EXPLAIN ANALYZE " + s.Inner.String()
	}
	return "EXPLAIN " + s.Inner.String()
}

// ---- expressions ----------------------------------------------------------

type Ident struct{ Name string }
type NumberLit struct{ Value string }
type StringLit struct{ Value string }
type BoolLit struct{ Value bool }
type NullLit struct{}
type BinaryExpr struct {
	Op          string
	Left, Right Expr
}
type UnaryExpr struct {
	Op      string
	Operand Expr
}
type Param struct{ Name string }
type FuncCall struct {
	Name string
	Args []Expr
	Star bool
}
type InExpr struct {
	Left Expr
	List []Expr
	Not  bool
}
type IsNullExpr struct {
	Left Expr
	Not  bool
}

func (*Ident) expr()      {}
func (*NumberLit) expr()  {}
func (*StringLit) expr()  {}
func (*BoolLit) expr()    {}
func (*NullLit) expr()    {}
func (*BinaryExpr) expr() {}
func (*UnaryExpr) expr()  {}
func (*FuncCall) expr()   {}
func (*Param) expr()      {}
func (*InExpr) expr()     {}
func (*IsNullExpr) expr() {}

func (e *Ident) String() string     { return e.Name }
func (e *NumberLit) String() string { return e.Value }
func (e *StringLit) String() string { return "'" + strings.ReplaceAll(e.Value, "'", "''") + "'" }
func (e *NullLit) String() string   { return "NULL" }
func (e *Param) String() string     { return e.Name }
func (e *BoolLit) String() string {
	if e.Value {
		return "TRUE"
	}
	return "FALSE"
}
func (e *BinaryExpr) String() string {
	return "(" + e.Left.String() + " " + e.Op + " " + e.Right.String() + ")"
}
func (e *UnaryExpr) String() string {
	if e.Op == "NOT" {
		return "(NOT " + e.Operand.String() + ")"
	}
	return "(" + e.Op + e.Operand.String() + ")"
}
func (e *FuncCall) String() string {
	if e.Star {
		return e.Name + "(*)"
	}
	return e.Name + "(" + joinExprs(e.Args) + ")"
}
func (e *InExpr) String() string {
	op := " IN "
	if e.Not {
		op = " NOT IN "
	}
	return "(" + e.Left.String() + op + "(" + joinExprs(e.List) + "))"
}
func (e *IsNullExpr) String() string {
	if e.Not {
		return "(" + e.Left.String() + " IS NOT NULL)"
	}
	return "(" + e.Left.String() + " IS NULL)"
}

func joinExprs(es []Expr) string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = e.String()
	}
	return strings.Join(parts, ", ")
}

func (s *CreateSchemaStmt) String() string {
	out := "CREATE SCHEMA "
	if s.IfNotExists {
		out += "IF NOT EXISTS "
	}
	out += s.Name
	if s.Authorization != "" {
		out += " AUTHORIZATION " + s.Authorization
	}
	return out
}

func (s *DropSchemaStmt) String() string {
	out := "DROP SCHEMA "
	if s.IfExists {
		out += "IF EXISTS "
	}
	out += s.Name
	if s.Cascade {
		out += " CASCADE"
	}
	return out
}

func (s *AlterTableStmt) String() string {
	out := "ALTER TABLE " + s.Table + " "
	switch s.Action {
	case "ADD COLUMN":
		c := s.Column
		out += "ADD COLUMN " + c.Name + " " + c.Type
		if c.PrimaryKey {
			out += " PRIMARY KEY"
		}
		if c.NotNull {
			out += " NOT NULL"
		}
	case "DROP COLUMN":
		out += "DROP COLUMN " + s.Name
	case "RENAME COLUMN":
		out += "RENAME COLUMN " + s.Name + " TO " + s.NewName
	case "RENAME TABLE":
		out += "RENAME TO " + s.NewName
	case "ALTER COLUMN TYPE":
		out += "ALTER COLUMN " + s.Name + " TYPE " + s.Type
	}
	return out
}

func (s *CreateIndexStmt) String() string {
	out := "CREATE "
	if s.Unique {
		out += "UNIQUE "
	}
	out += "INDEX "
	if s.Concurrently {
		out += "CONCURRENTLY "
	}
	if s.IfNotExists {
		out += "IF NOT EXISTS "
	}
	if s.Name != "" {
		out += s.Name + " "
	}
	out += "ON " + s.Table
	if s.Method != "" {
		out += " USING " + s.Method
	}
	return out + " (" + strings.Join(s.Columns, ", ") + ")"
}

func (s *DropIndexStmt) String() string {
	out := "DROP INDEX "
	if s.Concurrently {
		out += "CONCURRENTLY "
	}
	if s.IfExists {
		out += "IF EXISTS "
	}
	return out + s.Name
}

func q(s string) string { return (&StringLit{Value: s}).String() }

func (s *CreateBranchStmt) String() string {
	out := "CREATE BRANCH " + s.Name
	if s.Parent != "" {
		out += " FROM " + s.Parent
	}
	if s.AsOf != "" {
		out += " AS OF " + q(s.AsOf)
	}
	return out
}
func (s *CheckoutStmt) String() string { return "CHECKOUT BRANCH " + s.Name }
func (s *MergeBranchStmt) String() string {
	if s.Into != "" {
		return "MERGE BRANCH " + s.Name + " INTO " + s.Into
	}
	return "MERGE BRANCH " + s.Name
}
func (s *DropBranchStmt) String() string {
	if s.IfExists {
		return "DROP BRANCH IF EXISTS " + s.Name
	}
	return "DROP BRANCH " + s.Name
}
func (s *DiffBranchStmt) String() string {
	if s.B != "" {
		return "DIFF BRANCH " + s.A + " WITH " + s.B
	}
	return "DIFF BRANCH " + s.A
}
func (s *RememberStmt) String() string { return "REMEMBER " + q(s.Text) }
func (s *RecallStmt) String() string {
	if s.Limit > 0 {
		return fmt.Sprintf("RECALL %s LIMIT %d", q(s.Query), s.Limit)
	}
	return "RECALL " + q(s.Query)
}
func (s *ForgetStmt) String() string {
	if s.Query == "" {
		return "FORGET ALL"
	}
	return "FORGET " + q(s.Query)
}
