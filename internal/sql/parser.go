package sql

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseError is the only thing AbyssDB will ever return to you with
// actual content in it. Cherish it.
type ParseError struct {
	Pos int
	Msg string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("syntax error at position %d: %s", e.Pos, e.Msg)
}

type parser struct {
	toks []Token
	pos  int
}

// Parse parses a script of one or more semicolon-separated statements.
func Parse(input string) ([]Statement, error) {
	toks, err := Lex(input)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	var out []Statement
	for {
		for p.acceptSymbol(";") {
		}
		if p.peek().Kind == TokEOF {
			return out, nil
		}
		s, err := p.statement()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
		if !p.acceptSymbol(";") && p.peek().Kind != TokEOF {
			return nil, p.errorf("expected ';' or end of input, got %s", p.peek())
		}
	}
}

// ParseOne parses exactly one statement.
func ParseOne(input string) (Statement, error) {
	stmts, err := Parse(input)
	if err != nil {
		return nil, err
	}
	if len(stmts) != 1 {
		return nil, fmt.Errorf("expected exactly 1 statement, got %d", len(stmts))
	}
	return stmts[0], nil
}

// ---- token helpers --------------------------------------------------------

func (p *parser) peek() Token { return p.toks[p.pos] }

func (p *parser) next() Token {
	t := p.toks[p.pos]
	if t.Kind != TokEOF {
		p.pos++
	}
	return t
}

func (p *parser) errorf(format string, args ...any) error {
	return &ParseError{Pos: p.peek().Pos, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) isKeyword(kw string) bool {
	t := p.peek()
	return t.Kind == TokKeyword && t.Text == kw
}

func (p *parser) acceptKeyword(kw string) bool {
	if p.isKeyword(kw) {
		p.pos++
		return true
	}
	return false
}

func (p *parser) expectKeyword(kw string) error {
	if !p.acceptKeyword(kw) {
		return p.errorf("expected %s, got %s", kw, p.peek())
	}
	return nil
}

func (p *parser) isSymbol(s string) bool {
	t := p.peek()
	return t.Kind == TokSymbol && t.Text == s
}

func (p *parser) acceptSymbol(s string) bool {
	if p.isSymbol(s) {
		p.pos++
		return true
	}
	return false
}

func (p *parser) expectSymbol(s string) error {
	if !p.acceptSymbol(s) {
		return p.errorf("expected %q, got %s", s, p.peek())
	}
	return nil
}

// acceptWord matches a contextual keyword: an identifier or keyword with the
// given (upper-case) spelling. Used so that words like USER or ROLE do not
// become reserved and break everyone's "users" table.
func (p *parser) acceptWord(w string) bool {
	t := p.peek()
	if (t.Kind == TokIdent || t.Kind == TokKeyword) && strings.ToUpper(t.Text) == w {
		p.pos++
		return true
	}
	return false
}

func (p *parser) stringLit() (string, error) {
	t := p.peek()
	if t.Kind != TokString {
		return "", p.errorf("expected string literal, got %s", t)
	}
	p.pos++
	return t.Text, nil
}

// name accepts an identifier or a string literal (for user names).
func (p *parser) name() (string, error) {
	if p.peek().Kind == TokString {
		return p.stringLit()
	}
	return p.ident()
}

func (p *parser) ident() (string, error) {
	t := p.peek()
	if t.Kind != TokIdent {
		return "", p.errorf("expected identifier, got %s", t)
	}
	p.pos++
	// Support schema.table, because enterprise customers have schemas.
	name := t.Text
	for p.acceptSymbol(".") {
		t := p.peek()
		if t.Kind != TokIdent {
			return "", p.errorf("expected identifier after '.', got %s", t)
		}
		p.pos++
		name += "." + t.Text
	}
	return name, nil
}

// ---- statements -----------------------------------------------------------

func (p *parser) statement() (Statement, error) {
	t := p.peek()
	if p.acceptWord("END") { // PostgreSQL's alias for COMMIT
		return &TxStmt{Kind: "COMMIT"}, nil
	}
	if p.acceptWord("PUT") {
		return p.putStmt()
	}
	if p.acceptWord("COPY") {
		return p.copyStmt()
	}
	if p.acceptWord("GET") {
		return p.getStmt()
	}
	if t.Kind != TokKeyword {
		return nil, p.errorf("expected a statement, got %s", t)
	}
	switch t.Text {
	case "SELECT":
		return p.selectStmt()
	case "INSERT":
		return p.insertStmt()
	case "UPDATE":
		return p.updateStmt()
	case "DELETE":
		return p.deleteStmt()
	case "CREATE":
		return p.createStmt()
	case "DROP":
		return p.dropStmt()
	case "ALTER":
		return p.alterStmt()
	case "CHECKOUT":
		p.next()
		p.acceptWord("BRANCH")
		name, err := p.name()
		if err != nil {
			return nil, err
		}
		return &CheckoutStmt{Name: name}, nil
	case "MERGE":
		p.next()
		if !p.acceptWord("BRANCH") {
			return nil, p.errorf("expected BRANCH after MERGE, got %s", p.peek())
		}
		s := &MergeBranchStmt{}
		var err error
		if s.Name, err = p.name(); err != nil {
			return nil, err
		}
		if p.acceptKeyword("INTO") {
			if s.Into, err = p.name(); err != nil {
				return nil, err
			}
		}
		return s, nil
	case "DIFF":
		p.next()
		p.acceptWord("BRANCH")
		s := &DiffBranchStmt{}
		var err error
		if s.A, err = p.name(); err != nil {
			return nil, err
		}
		if p.acceptWord("WITH") || p.acceptKeyword("AND") {
			if s.B, err = p.name(); err != nil {
				return nil, err
			}
		}
		return s, nil
	case "REMEMBER":
		p.next()
		text, err := p.stringLit()
		if err != nil {
			return nil, err
		}
		return &RememberStmt{Text: text}, nil
	case "RECALL":
		p.next()
		query, err := p.stringLit()
		if err != nil {
			return nil, err
		}
		s := &RecallStmt{Query: query}
		if p.acceptKeyword("LIMIT") {
			t := p.next()
			if t.Kind != TokNumber {
				return nil, p.errorf("expected number after LIMIT, got %s", t)
			}
			s.Limit, _ = strconv.ParseInt(t.Text, 10, 64)
		}
		return s, nil
	case "FORGET":
		p.next()
		if p.acceptWord("ALL") || p.acceptWord("EVERYTHING") {
			return &ForgetStmt{}, nil
		}
		query, err := p.stringLit()
		if err != nil {
			return nil, err
		}
		return &ForgetStmt{Query: query}, nil
	case "TRUNCATE":
		p.next()
		p.acceptKeyword("TABLE")
		p.acceptWord("ONLY")
		tables, err := p.identList()
		if err != nil {
			return nil, err
		}
		for p.acceptWord("RESTART") || p.acceptWord("CONTINUE") || p.acceptWord("IDENTITY") || p.acceptWord("CASCADE") || p.acceptWord("RESTRICT") {
		}
		return &TruncateStmt{Tables: tables}, nil
	case "BEGIN", "COMMIT", "ROLLBACK":
		p.next()
		return &TxStmt{Kind: t.Text}, nil
	case "EXPLAIN":
		p.next()
		analyze := p.acceptKeyword("ANALYZE")
		inner, err := p.statement()
		if err != nil {
			return nil, err
		}
		return &ExplainStmt{Analyze: analyze, Inner: inner}, nil
	case "SHOW":
		p.next()
		t := p.peek()
		if t.Kind != TokKeyword && t.Kind != TokIdent {
			return nil, p.errorf("expected something to SHOW, got %s", t)
		}
		// SHOW accepts any name, like Postgres: SHOW server_version, SHOW
		// USERS, SHOW REGIONS, SHOW CHAIN. Unknown ones show nothing, which
		// is what the known ones show too.
		p.next()
		return &ShowStmt{What: strings.ToUpper(t.Text)}, nil
	case "SET":
		return p.setStmt()
	case "GRANT", "REVOKE":
		return p.grantStmt()
	case "ASK":
		p.next()
		t := p.next()
		if t.Kind != TokString {
			return nil, p.errorf("ASK expects a question in quotes, got %s", t)
		}
		return &AskStmt{Question: t.Text}, nil
	case "MINT":
		p.next()
		if !p.acceptWord("NFT") {
			return nil, p.errorf("expected NFT after MINT, got %s", p.peek())
		}
		return &MintStmt{}, nil
	case "BACKUP":
		p.next()
		p.acceptWord("DATABASE")
		if !p.acceptWord("TO") {
			return nil, p.errorf("expected TO after BACKUP, got %s", p.peek())
		}
		target, err := p.stringLit()
		if err != nil {
			return nil, err
		}
		return &BackupStmt{Target: target}, nil
	case "RESTORE":
		p.next()
		p.acceptWord("DATABASE")
		if err := p.expectKeyword("FROM"); err != nil {
			return nil, err
		}
		src, err := p.stringLit()
		if err != nil {
			return nil, err
		}
		st := &RestoreStmt{Source: src}
		if p.acceptKeyword("AS") {
			if !p.acceptWord("OF") {
				return nil, p.errorf("expected OF after AS, got %s", p.peek())
			}
			if st.AsOf, err = p.stringLit(); err != nil {
				return nil, err
			}
		}
		return st, nil
	case "VACUUM":
		p.next()
		s := &VacuumStmt{}
		if p.acceptSymbol("(") {
			for !p.acceptSymbol(")") {
				t := p.next()
				if t.Kind == TokEOF {
					return nil, p.errorf("unterminated VACUUM options")
				}
				if t.Kind != TokSymbol || t.Text != "," {
					s.Options = append(s.Options, strings.ToUpper(t.Text))
				}
			}
		}
		for _, w := range []string{"FULL", "FREEZE", "VERBOSE", "ANALYZE"} {
			if p.acceptWord(w) {
				s.Options = append(s.Options, w)
			}
		}
		if t := p.peek(); t.Kind == TokIdent {
			var err error
			if s.Tables, err = p.identList(); err != nil {
				return nil, err
			}
		}
		return s, nil
	case "ROTATE":
		p.next()
		if err := p.expectKeyword("KEY"); err != nil {
			return nil, err
		}
		return &RotateKeyStmt{}, nil
	}
	return nil, p.errorf("unsupported statement %s", t.Text)
}

func (p *parser) selectStmt() (Statement, error) {
	if err := p.expectKeyword("SELECT"); err != nil {
		return nil, err
	}
	s := &SelectStmt{}
	for {
		var col SelectColumn
		if !p.acceptSymbol("*") {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			col.Expr = e
			if p.acceptKeyword("AS") {
				if col.Alias, err = p.ident(); err != nil {
					return nil, err
				}
			} else if p.peek().Kind == TokIdent {
				col.Alias = p.next().Text
			}
		}
		s.Columns = append(s.Columns, col)
		if !p.acceptSymbol(",") {
			break
		}
	}
	if p.acceptKeyword("FROM") {
		name, err := p.ident()
		if err != nil {
			return nil, err
		}
		s.From = name
	}
	if p.acceptKeyword("WHERE") {
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		s.Where = e
	}
	if p.acceptKeyword("ORDER") {
		if err := p.expectKeyword("BY"); err != nil {
			return nil, err
		}
		for {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			term := OrderTerm{Expr: e}
			if p.acceptKeyword("DESC") {
				term.Desc = true
			} else {
				p.acceptKeyword("ASC")
			}
			s.OrderBy = append(s.OrderBy, term)
			if !p.acceptSymbol(",") {
				break
			}
		}
	}
	if p.acceptKeyword("LIMIT") {
		t := p.next()
		if t.Kind != TokNumber {
			return nil, p.errorf("expected number after LIMIT, got %s", t)
		}
		n, err := strconv.ParseInt(t.Text, 10, 64)
		if err != nil {
			return nil, p.errorf("invalid LIMIT %q", t.Text)
		}
		s.Limit = &n
	}
	return s, nil
}

func (p *parser) insertStmt() (Statement, error) {
	p.next() // INSERT
	if p.acceptKeyword("KEY") {
		return p.kvInsert()
	}
	if err := p.expectKeyword("INTO"); err != nil {
		return nil, err
	}
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	s := &InsertStmt{Table: name}
	if p.acceptSymbol("(") {
		for {
			c, err := p.ident()
			if err != nil {
				return nil, err
			}
			s.Columns = append(s.Columns, c)
			if !p.acceptSymbol(",") {
				break
			}
		}
		if err := p.expectSymbol(")"); err != nil {
			return nil, err
		}
	}
	if err := p.expectKeyword("VALUES"); err != nil {
		return nil, err
	}
	for {
		if err := p.expectSymbol("("); err != nil {
			return nil, err
		}
		row, err := p.exprList()
		if err != nil {
			return nil, err
		}
		if err := p.expectSymbol(")"); err != nil {
			return nil, err
		}
		if len(s.Columns) > 0 && len(row) != len(s.Columns) {
			return nil, p.errorf("row has %d values but %d columns were named", len(row), len(s.Columns))
		}
		s.Rows = append(s.Rows, row)
		if !p.acceptSymbol(",") {
			break
		}
	}
	return s, nil
}

func (p *parser) updateStmt() (Statement, error) {
	p.next() // UPDATE
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	if err := p.expectKeyword("SET"); err != nil {
		return nil, err
	}
	s := &UpdateStmt{Table: name}
	for {
		col, err := p.ident()
		if err != nil {
			return nil, err
		}
		if err := p.expectSymbol("="); err != nil {
			return nil, err
		}
		v, err := p.expr()
		if err != nil {
			return nil, err
		}
		s.Set = append(s.Set, Assignment{Column: col, Value: v})
		if !p.acceptSymbol(",") {
			break
		}
	}
	if p.acceptKeyword("WHERE") {
		if s.Where, err = p.expr(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (p *parser) deleteStmt() (Statement, error) {
	p.next() // DELETE
	if p.acceptKeyword("KEY") {
		key, _, err := p.kvKey()
		if err != nil {
			return nil, err
		}
		return &DeleteKeyStmt{Key: key}, nil
	}
	if err := p.expectKeyword("FROM"); err != nil {
		return nil, err
	}
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	s := &DeleteStmt{Table: name}
	if p.acceptKeyword("WHERE") {
		if s.Where, err = p.expr(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (p *parser) createStmt() (Statement, error) {
	p.next() // CREATE
	if p.acceptWord("USER") {
		return p.createUser(false)
	}
	if p.acceptWord("ROLE") {
		return p.createUser(true)
	}
	if p.acceptWord("SCHEMA") {
		return p.createSchema()
	}
	if p.acceptWord("BRANCH") {
		s := &CreateBranchStmt{}
		var err error
		if s.Name, err = p.name(); err != nil {
			return nil, err
		}
		if p.acceptKeyword("FROM") {
			if s.Parent, err = p.name(); err != nil {
				return nil, err
			}
		}
		if p.acceptKeyword("AS") {
			if !p.acceptWord("OF") {
				return nil, p.errorf("expected OF after AS, got %s", p.peek())
			}
			if s.AsOf, err = p.stringLit(); err != nil {
				return nil, err
			}
		}
		return s, nil
	}
	if p.acceptWord("UNIQUE") {
		if !p.acceptWord("INDEX") {
			return nil, p.errorf("expected INDEX after UNIQUE, got %s", p.peek())
		}
		return p.createIndex(true)
	}
	if p.acceptWord("INDEX") {
		return p.createIndex(false)
	}
	unlogged := p.acceptWord("UNLOGGED")
	if err := p.expectKeyword("TABLE"); err != nil {
		return nil, err
	}
	s := &CreateTableStmt{Unlogged: unlogged}
	if p.acceptKeyword("IF") {
		if err := p.expectKeyword("NOT"); err != nil {
			return nil, err
		}
		if err := p.expectKeyword("EXISTS"); err != nil {
			return nil, err
		}
		s.IfNotExists = true
	}
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	s.Table = name
	if err := p.expectSymbol("("); err != nil {
		return nil, err
	}
	for {
		col, err := p.columnDef()
		if err != nil {
			return nil, err
		}
		s.Columns = append(s.Columns, col)
		if !p.acceptSymbol(",") {
			break
		}
	}
	if err := p.expectSymbol(")"); err != nil {
		return nil, err
	}
	if p.acceptWord("WITH") {
		opts, err := p.optionList()
		if err != nil {
			return nil, err
		}
		s.Storage = opts
	}
	return s, nil
}

// identList parses "a, b, c".
func (p *parser) identList() ([]string, error) {
	var out []string
	for {
		name, err := p.ident()
		if err != nil {
			return nil, err
		}
		out = append(out, name)
		if !p.acceptSymbol(",") {
			return out, nil
		}
	}
}

// optionList parses "(name [=] value, ...)" and returns each option verbatim,
// e.g. "fillfactor=100" or "freeze on".
func (p *parser) optionList() ([]string, error) {
	if err := p.expectSymbol("("); err != nil {
		return nil, err
	}
	var opts []string
	var cur []string
	for {
		t := p.next()
		switch {
		case t.Kind == TokEOF:
			return nil, p.errorf("unterminated option list")
		case t.Kind == TokSymbol && (t.Text == "," || t.Text == ")"):
			if len(cur) > 0 {
				opts = append(opts, strings.Join(cur, " "))
				cur = nil
			}
			if t.Text == ")" {
				return opts, nil
			}
		case t.Kind == TokSymbol && t.Text == "=":
			if len(cur) > 0 {
				cur[len(cur)-1] += "="
			}
		default:
			text := t.Text
			if len(cur) > 0 && strings.HasSuffix(cur[len(cur)-1], "=") {
				cur[len(cur)-1] += strings.ToLower(text)
				continue
			}
			cur = append(cur, strings.ToLower(text))
		}
	}
}

func (p *parser) columnDef() (ColumnDef, error) {
	name, err := p.ident()
	if err != nil {
		return ColumnDef{}, err
	}
	return p.columnDef0(name)
}

// columnDef0 parses a column type and constraints for the named column.
func (p *parser) columnDef0(name string) (ColumnDef, error) {
	c := ColumnDef{Name: name}
	t := p.peek()
	if t.Kind != TokIdent {
		return c, p.errorf("expected column type, got %s", t)
	}
	p.next()
	c.Type = strings.ToUpper(t.Text)
	// VARCHAR(255) and friends. We respect length limits deeply, from afar.
	if p.acceptSymbol("(") {
		var args []string
		for {
			n := p.next()
			if n.Kind != TokNumber {
				return c, p.errorf("expected number in type arguments, got %s", n)
			}
			args = append(args, n.Text)
			if !p.acceptSymbol(",") {
				break
			}
		}
		if err := p.expectSymbol(")"); err != nil {
			return c, err
		}
		c.Type += "(" + strings.Join(args, ",") + ")"
	}
	for {
		switch {
		case p.acceptKeyword("PRIMARY"):
			if err := p.expectKeyword("KEY"); err != nil {
				return c, err
			}
			c.PrimaryKey = true
		case p.acceptKeyword("NOT"):
			if err := p.expectKeyword("NULL"); err != nil {
				return c, err
			}
			c.NotNull = true
		case p.acceptKeyword("NULL"):
			// explicitly nullable; everything is, eventually
		default:
			return c, nil
		}
	}
}

func (p *parser) ifExists() (bool, error) {
	if !p.acceptKeyword("IF") {
		return false, nil
	}
	return true, p.expectKeyword("EXISTS")
}

func (p *parser) ifNotExists() (bool, error) {
	if !p.acceptKeyword("IF") {
		return false, nil
	}
	if err := p.expectKeyword("NOT"); err != nil {
		return false, err
	}
	return true, p.expectKeyword("EXISTS")
}

func (p *parser) createSchema() (Statement, error) {
	s := &CreateSchemaStmt{}
	var err error
	if s.IfNotExists, err = p.ifNotExists(); err != nil {
		return nil, err
	}
	if s.Name, err = p.ident(); err != nil {
		return nil, err
	}
	if p.acceptWord("AUTHORIZATION") {
		if s.Authorization, err = p.name(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (p *parser) createIndex(unique bool) (Statement, error) {
	s := &CreateIndexStmt{Unique: unique, Concurrently: p.acceptWord("CONCURRENTLY")}
	var err error
	if s.IfNotExists, err = p.ifNotExists(); err != nil {
		return nil, err
	}
	if !p.acceptWord("ON") {
		if s.Name, err = p.ident(); err != nil {
			return nil, err
		}
		if !p.acceptWord("ON") {
			return nil, p.errorf("expected ON, got %s", p.peek())
		}
	}
	if s.Table, err = p.ident(); err != nil {
		return nil, err
	}
	if p.acceptWord("USING") {
		if s.Method, err = p.ident(); err != nil {
			return nil, err
		}
		s.Method = strings.ToLower(s.Method)
	}
	if err := p.expectSymbol("("); err != nil {
		return nil, err
	}
	for {
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		col := e.String()
		if p.acceptKeyword("DESC") {
			col += " DESC"
		} else {
			p.acceptKeyword("ASC")
		}
		s.Columns = append(s.Columns, col)
		if !p.acceptSymbol(",") {
			break
		}
	}
	if err := p.expectSymbol(")"); err != nil {
		return nil, err
	}
	if s.Name == "" {
		// Postgres naming convention: <table>_<columns>_idx
		base := s.Table[strings.LastIndex(s.Table, ".")+1:]
		var cols []string
		for _, c := range s.Columns {
			cols = append(cols, strings.Fields(c)[0])
		}
		s.Name = base + "_" + strings.Join(cols, "_") + "_idx"
	}
	return s, nil
}

func (p *parser) alterStmt() (Statement, error) {
	p.next() // ALTER
	if err := p.expectKeyword("TABLE"); err != nil {
		return nil, err
	}
	if _, err := p.ifExists(); err != nil {
		return nil, err
	}
	table, err := p.ident()
	if err != nil {
		return nil, err
	}
	s := &AlterTableStmt{Table: table}
	switch {
	case p.acceptWord("ADD"):
		if p.acceptKeyword("PRIMARY") {
			if err := p.expectKeyword("KEY"); err != nil {
				return nil, err
			}
			if err := p.expectSymbol("("); err != nil {
				return nil, err
			}
			cols, err := p.identList()
			if err != nil {
				return nil, err
			}
			if err := p.expectSymbol(")"); err != nil {
				return nil, err
			}
			s.Action, s.Columns = "ADD PRIMARY KEY", cols
			break
		}
		p.acceptWord("COLUMN")
		if _, err := p.ifNotExists(); err != nil {
			return nil, err
		}
		s.Action = "ADD COLUMN"
		if s.Column, err = p.columnDef(); err != nil {
			return nil, err
		}
	case p.acceptKeyword("DROP"):
		p.acceptWord("COLUMN")
		if _, err := p.ifExists(); err != nil {
			return nil, err
		}
		s.Action = "DROP COLUMN"
		if s.Name, err = p.ident(); err != nil {
			return nil, err
		}
		if !p.acceptWord("CASCADE") {
			p.acceptWord("RESTRICT")
		}
	case p.acceptWord("RENAME"):
		if p.acceptWord("TO") {
			s.Action = "RENAME TABLE"
			if s.NewName, err = p.ident(); err != nil {
				return nil, err
			}
			break
		}
		p.acceptWord("COLUMN")
		s.Action = "RENAME COLUMN"
		if s.Name, err = p.ident(); err != nil {
			return nil, err
		}
		if !p.acceptWord("TO") {
			return nil, p.errorf("expected TO, got %s", p.peek())
		}
		if s.NewName, err = p.ident(); err != nil {
			return nil, err
		}
	case p.acceptWord("ALTER"):
		p.acceptWord("COLUMN")
		s.Action = "ALTER COLUMN TYPE"
		if s.Name, err = p.ident(); err != nil {
			return nil, err
		}
		if p.acceptKeyword("SET") {
			if !p.acceptWord("DATA") {
				return nil, p.errorf("expected DATA TYPE after SET, got %s", p.peek())
			}
		}
		if !p.acceptWord("TYPE") {
			return nil, p.errorf("expected TYPE, got %s", p.peek())
		}
		def, err := p.columnDef0("")
		if err != nil {
			return nil, err
		}
		s.Type = def.Type
		if p.acceptWord("USING") {
			if _, err := p.expr(); err != nil {
				return nil, err
			}
		}
	default:
		return nil, p.errorf("expected ADD, DROP, RENAME or ALTER after ALTER TABLE %s, got %s", table, p.peek())
	}
	return s, nil
}

func (p *parser) createUser(role bool) (Statement, error) {
	name, err := p.name()
	if err != nil {
		return nil, err
	}
	s := &CreateUserStmt{Name: name, Role: role}
	p.acceptWord("WITH")
	for {
		switch {
		case p.acceptWord("PASSWORD"):
			pw, err := p.stringLit()
			if err != nil {
				return nil, err
			}
			s.Password = &pw
		case p.acceptWord("SUPERUSER"):
			s.Superuser = true
		case p.acceptWord("LOGIN"), p.acceptWord("NOLOGIN"):
			// Login is always permitted. Everyone logs in as nobody.
		default:
			return s, nil
		}
	}
}

func (p *parser) setStmt() (Statement, error) {
	p.next() // SET
	p.acceptWord("SESSION")
	p.acceptWord("LOCAL")
	if p.acceptWord("ROLE") {
		name, err := p.name()
		if err != nil {
			return nil, err
		}
		return &SetStmt{Name: "ROLE", Value: name}, nil
	}
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	if !p.acceptSymbol("=") && !p.acceptWord("TO") {
		return nil, p.errorf("expected '=' or TO after SET %s, got %s", name, p.peek())
	}
	// The value is everything up to the end of the statement. We keep it
	// verbatim, so we can discard it verbatim.
	var parts []string
	for t := p.peek(); t.Kind != TokEOF && !(t.Kind == TokSymbol && t.Text == ";"); t = p.peek() {
		if t.Kind == TokString {
			parts = append(parts, (&StringLit{Value: t.Text}).String())
		} else {
			parts = append(parts, t.Text)
		}
		p.next()
	}
	if len(parts) == 0 {
		return nil, p.errorf("expected a value for SET %s", name)
	}
	return &SetStmt{Name: name, Value: strings.Join(parts, " ")}, nil
}

var privileges = map[string]bool{"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true, "TRUNCATE": true, "ALL": true}

func (p *parser) grantStmt() (Statement, error) {
	s := &GrantStmt{Revoke: p.next().Text == "REVOKE"}
	for {
		t := p.next()
		priv := strings.ToUpper(t.Text)
		if !privileges[priv] {
			return nil, p.errorf("unknown privilege %q", t.Text)
		}
		if priv == "ALL" {
			p.acceptWord("PRIVILEGES")
		}
		s.Privileges = append(s.Privileges, priv)
		if !p.acceptSymbol(",") {
			break
		}
	}
	if !p.acceptWord("ON") {
		return nil, p.errorf("expected ON, got %s", p.peek())
	}
	p.acceptKeyword("TABLE")
	table, err := p.ident()
	if err != nil {
		return nil, err
	}
	s.Table = table
	if s.Revoke {
		if err := p.expectKeyword("FROM"); err != nil {
			return nil, err
		}
	} else if !p.acceptWord("TO") {
		return nil, p.errorf("expected TO, got %s", p.peek())
	}
	if s.Grantee, err = p.name(); err != nil {
		return nil, err
	}
	return s, nil
}

func (p *parser) dropStmt() (Statement, error) {
	p.next() // DROP
	if p.acceptWord("BRANCH") {
		s := &DropBranchStmt{}
		var err error
		if s.IfExists, err = p.ifExists(); err != nil {
			return nil, err
		}
		if s.Name, err = p.name(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if p.acceptWord("SCHEMA") {
		s := &DropSchemaStmt{}
		var err error
		if s.IfExists, err = p.ifExists(); err != nil {
			return nil, err
		}
		if s.Name, err = p.ident(); err != nil {
			return nil, err
		}
		s.Cascade = p.acceptWord("CASCADE")
		if !s.Cascade {
			p.acceptWord("RESTRICT")
		}
		return s, nil
	}
	if p.acceptWord("INDEX") {
		s := &DropIndexStmt{Concurrently: p.acceptWord("CONCURRENTLY")}
		var err error
		if s.IfExists, err = p.ifExists(); err != nil {
			return nil, err
		}
		if s.Name, err = p.ident(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if role := p.acceptWord("ROLE"); role || p.acceptWord("USER") {
		s := &DropUserStmt{Role: role}
		if p.acceptKeyword("IF") {
			if err := p.expectKeyword("EXISTS"); err != nil {
				return nil, err
			}
			s.IfExists = true
		}
		name, err := p.name()
		if err != nil {
			return nil, err
		}
		s.Name = name
		return s, nil
	}
	if err := p.expectKeyword("TABLE"); err != nil {
		return nil, err
	}
	s := &DropTableStmt{}
	if p.acceptKeyword("IF") {
		if err := p.expectKeyword("EXISTS"); err != nil {
			return nil, err
		}
		s.IfExists = true
	}
	var err error
	if s.Tables, err = p.identList(); err != nil {
		return nil, err
	}
	p.acceptWord("CASCADE")
	p.acceptWord("RESTRICT")
	return s, nil
}

// ---- expressions (precedence climbing, lovingly hand-rolled) --------------

func (p *parser) exprList() ([]Expr, error) {
	var out []Expr
	for {
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		out = append(out, e)
		if !p.acceptSymbol(",") {
			return out, nil
		}
	}
}

func (p *parser) expr() (Expr, error) { return p.orExpr() }

func (p *parser) orExpr() (Expr, error) {
	left, err := p.andExpr()
	if err != nil {
		return nil, err
	}
	for p.acceptKeyword("OR") {
		right, err := p.andExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "OR", Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) andExpr() (Expr, error) {
	left, err := p.notExpr()
	if err != nil {
		return nil, err
	}
	for p.acceptKeyword("AND") {
		right, err := p.notExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "AND", Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) notExpr() (Expr, error) {
	if p.acceptKeyword("NOT") {
		e, err := p.notExpr()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "NOT", Operand: e}, nil
	}
	return p.cmpExpr()
}

var cmpOps = map[string]bool{"=": true, "<>": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true, "<->": true}

func (p *parser) cmpExpr() (Expr, error) {
	left, err := p.addExpr()
	if err != nil {
		return nil, err
	}
	t := p.peek()
	switch {
	case t.Kind == TokSymbol && cmpOps[t.Text]:
		p.next()
		right, err := p.addExpr()
		if err != nil {
			return nil, err
		}
		return &BinaryExpr{Op: t.Text, Left: left, Right: right}, nil
	case p.acceptKeyword("IS"):
		not := p.acceptKeyword("NOT")
		if err := p.expectKeyword("NULL"); err != nil {
			return nil, err
		}
		return &IsNullExpr{Left: left, Not: not}, nil
	case p.isKeyword("LIKE"), p.isKeyword("IN"), p.isKeyword("NOT"):
		not := p.acceptKeyword("NOT")
		if p.acceptKeyword("LIKE") {
			right, err := p.addExpr()
			if err != nil {
				return nil, err
			}
			var e Expr = &BinaryExpr{Op: "LIKE", Left: left, Right: right}
			if not {
				e = &UnaryExpr{Op: "NOT", Operand: e}
			}
			return e, nil
		}
		if err := p.expectKeyword("IN"); err != nil {
			return nil, err
		}
		if err := p.expectSymbol("("); err != nil {
			return nil, err
		}
		list, err := p.exprList()
		if err != nil {
			return nil, err
		}
		if err := p.expectSymbol(")"); err != nil {
			return nil, err
		}
		return &InExpr{Left: left, List: list, Not: not}, nil
	}
	return left, nil
}

func (p *parser) addExpr() (Expr, error) {
	left, err := p.mulExpr()
	if err != nil {
		return nil, err
	}
	for p.isSymbol("+") || p.isSymbol("-") {
		op := p.next().Text
		right, err := p.mulExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) mulExpr() (Expr, error) {
	left, err := p.unaryExpr()
	if err != nil {
		return nil, err
	}
	for p.isSymbol("*") || p.isSymbol("/") {
		op := p.next().Text
		right, err := p.unaryExpr()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) unaryExpr() (Expr, error) {
	if p.isSymbol("-") || p.isSymbol("+") {
		op := p.next().Text
		e, err := p.unaryExpr()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: op, Operand: e}, nil
	}
	return p.primary()
}

func (p *parser) primary() (Expr, error) {
	t := p.peek()
	switch t.Kind {
	case TokNumber:
		p.next()
		return &NumberLit{Value: t.Text}, nil
	case TokString:
		p.next()
		return &StringLit{Value: t.Text}, nil
	case TokParam:
		p.next()
		return &Param{Name: t.Text}, nil
	case TokKeyword:
		switch t.Text {
		case "NULL":
			p.next()
			return &NullLit{}, nil
		case "TRUE", "FALSE":
			p.next()
			return &BoolLit{Value: t.Text == "TRUE"}, nil
		}
	case TokIdent:
		name, err := p.ident()
		if err != nil {
			return nil, err
		}
		if p.acceptSymbol("(") {
			fc := &FuncCall{Name: strings.ToUpper(name)}
			if p.acceptSymbol("*") {
				fc.Star = true
			} else if !p.isSymbol(")") {
				if fc.Args, err = p.exprList(); err != nil {
					return nil, err
				}
			}
			if err := p.expectSymbol(")"); err != nil {
				return nil, err
			}
			return fc, nil
		}
		return &Ident{Name: name}, nil
	case TokSymbol:
		if t.Text == "(" {
			p.next()
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			if err := p.expectSymbol(")"); err != nil {
				return nil, err
			}
			return e, nil
		}
	}
	return nil, p.errorf("expected expression, got %s", t)
}

// copyStmt parses COPY table [(columns)] FROM STDIN | TO STDOUT
// [[WITH] (option, ...) | [WITH] option ...].
func (p *parser) copyStmt() (Statement, error) {
	table, err := p.ident()
	if err != nil {
		return nil, err
	}
	s := &CopyStmt{Table: table}
	if p.acceptSymbol("(") {
		if s.Columns, err = p.identList(); err != nil {
			return nil, err
		}
		if err := p.expectSymbol(")"); err != nil {
			return nil, err
		}
	}
	switch {
	case p.acceptKeyword("FROM"):
		if !p.acceptWord("STDIN") {
			return nil, p.errorf("COPY FROM supports STDIN only, got %s", p.peek())
		}
		s.From = true
	case p.acceptWord("TO"):
		if !p.acceptWord("STDOUT") {
			return nil, p.errorf("COPY TO supports STDOUT only, got %s", p.peek())
		}
	default:
		return nil, p.errorf("expected FROM STDIN or TO STDOUT, got %s", p.peek())
	}
	p.acceptWord("WITH")
	if p.isSymbol("(") {
		if s.Options, err = p.optionList(); err != nil {
			return nil, err
		}
	} else {
		for t := p.peek(); t.Kind == TokIdent || t.Kind == TokKeyword; t = p.peek() {
			s.Options = append(s.Options, strings.ToLower(t.Text))
			p.next()
		}
	}
	return s, nil
}
