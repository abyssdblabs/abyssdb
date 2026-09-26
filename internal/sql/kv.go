package sql

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Key-value statements. Schema-yes or schema-less: tables get a schema, keys
// get whatever you give them.
//
//	PUT 'user:42' = '{"name": "Ada"}' [AS JSON] [TTL 3600];
//	PUT 'cart' = {items: [{sku: 'A1', qty: 2}], total: 9.99};
//	PUT 'config'.db.port = 5433;
//	INSERT KEY = 'user:42', VALUE = $$name: Ada$$ AS YAML;
//	GET 'config'.db.host;
//	GET KEYS 'user:*';
//	DELETE KEY 'user:42';

// PutStmt stores a value under a key, optionally at a path inside it.
type PutStmt struct {
	Key    string
	Path   []string
	Raw    string // the value as written, for string values
	IsRaw  bool   // Raw holds a document in some format; otherwise Native
	Native any    // KVObject, []any, string, json.Number, bool or nil
	Format string // JSON, YAML, TOML, ...; empty means auto-detect
	TTL    *int64 // seconds
}

// GetStmt looks up a key (or a path inside it), or lists keys matching a
// glob pattern.
type GetStmt struct {
	Key  string
	Path []string
	Keys bool // GET KEYS 'pattern'
}

type DeleteKeyStmt struct{ Key string }

// KVObject is an ordered set of key-value pairs: the native nested value.
type KVObject []KVPair

type KVPair struct {
	Key   string
	Value any
}

func (*PutStmt) stmt()       {}
func (*GetStmt) stmt()       {}
func (*DeleteKeyStmt) stmt() {}

func (s *PutStmt) String() string {
	var b strings.Builder
	b.WriteString("PUT " + quote(s.Key) + pathString(s.Path) + " = ")
	if s.IsRaw {
		b.WriteString(quote(s.Raw))
	} else {
		b.WriteString(NativeString(s.Native))
	}
	if s.Format != "" {
		b.WriteString(" AS " + s.Format)
	}
	if s.TTL != nil {
		b.WriteString(" TTL " + strconv.FormatInt(*s.TTL, 10))
	}
	return b.String()
}

func (s *GetStmt) String() string {
	if s.Keys {
		return "GET KEYS " + quote(s.Key)
	}
	return "GET " + quote(s.Key) + pathString(s.Path)
}

func (s *DeleteKeyStmt) String() string { return "DELETE KEY " + quote(s.Key) }

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func pathString(path []string) string {
	var b strings.Builder
	for _, p := range path {
		b.WriteString("." + quote(p))
	}
	return b.String()
}

// NativeString renders a native value in AbyssQL literal syntax.
func NativeString(v any) string {
	switch v := v.(type) {
	case KVObject:
		parts := make([]string, len(v))
		for i, kv := range v {
			parts[i] = quote(kv.Key) + ": " + NativeString(kv.Value)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		parts := make([]string, len(v))
		for i, x := range v {
			parts[i] = NativeString(x)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case string:
		return quote(v)
	case json.Number:
		return v.String()
	case bool:
		if v {
			return "TRUE"
		}
		return "FALSE"
	case nil:
		return "NULL"
	}
	return "NULL"
}

// ---- parsing ----------------------------------------------------------------

// kvKey parses a key (a string literal or an identifier) and an optional
// path of .segments.
func (p *parser) kvKey() (string, []string, error) {
	t := p.peek()
	var key string
	switch t.Kind {
	case TokString, TokIdent:
		p.pos++
		key = t.Text
	default:
		return "", nil, p.errorf("expected key, got %s", t)
	}
	var path []string
	for p.acceptSymbol(".") {
		t := p.peek()
		switch t.Kind {
		case TokString, TokIdent, TokKeyword, TokNumber:
			p.pos++
			path = append(path, t.Text)
		default:
			return "", nil, p.errorf("expected path segment after '.', got %s", t)
		}
	}
	return key, path, nil
}

func (p *parser) putStmt() (Statement, error) {
	key, path, err := p.kvKey()
	if err != nil {
		return nil, err
	}
	if err := p.expectSymbol("="); err != nil {
		return nil, err
	}
	return p.putValue(&PutStmt{Key: key, Path: path})
}

// kvInsert parses INSERT KEY [=] key, VALUE [=] value [AS format] [TTL n].
func (p *parser) kvInsert() (Statement, error) {
	p.acceptSymbol("=")
	key, path, err := p.kvKey()
	if err != nil {
		return nil, err
	}
	p.acceptSymbol(",")
	if !p.acceptWord("VALUE") {
		return nil, p.errorf("expected VALUE, got %s", p.peek())
	}
	p.acceptSymbol("=")
	return p.putValue(&PutStmt{Key: key, Path: path})
}

func (p *parser) putValue(s *PutStmt) (Statement, error) {
	if t := p.peek(); t.Kind == TokString {
		p.pos++
		s.Raw, s.IsRaw = t.Text, true
	} else {
		v, err := p.kvValue(0)
		if err != nil {
			return nil, err
		}
		s.Native = v
	}
	if p.acceptKeyword("AS") {
		t := p.next()
		if t.Kind != TokIdent && t.Kind != TokKeyword {
			return nil, p.errorf("expected value format after AS, got %s", t)
		}
		s.Format = strings.ToUpper(t.Text)
	}
	if p.acceptWord("TTL") {
		t := p.next()
		if t.Kind != TokNumber {
			return nil, p.errorf("expected seconds after TTL, got %s", t)
		}
		n, err := strconv.ParseInt(t.Text, 10, 64)
		if err != nil {
			return nil, p.errorf("invalid TTL %q", t.Text)
		}
		s.TTL = &n
	}
	return s, nil
}

const maxNesting = 64

// kvValue parses a native value: {k: v, ...}, [v, ...], a string, a number,
// TRUE, FALSE or NULL.
func (p *parser) kvValue(depth int) (any, error) {
	if depth > maxNesting {
		return nil, p.errorf("value nested deeper than %d levels", maxNesting)
	}
	t := p.peek()
	switch {
	case t.Kind == TokSymbol && t.Text == "{":
		p.pos++
		obj := KVObject{}
		if p.acceptSymbol("}") {
			return obj, nil
		}
		for {
			k := p.peek()
			if k.Kind != TokString && k.Kind != TokIdent && k.Kind != TokKeyword && k.Kind != TokNumber {
				return nil, p.errorf("expected key, got %s", k)
			}
			p.pos++
			if !p.acceptSymbol(":") && !p.acceptSymbol("=") {
				return nil, p.errorf("expected ':' after key %q, got %s", k.Text, p.peek())
			}
			v, err := p.kvValue(depth + 1)
			if err != nil {
				return nil, err
			}
			obj = append(obj, KVPair{Key: k.Text, Value: v})
			if p.acceptSymbol("}") {
				return obj, nil
			}
			if err := p.expectSymbol(","); err != nil {
				return nil, err
			}
		}
	case t.Kind == TokSymbol && t.Text == "[":
		p.pos++
		arr := []any{}
		if p.acceptSymbol("]") {
			return arr, nil
		}
		for {
			v, err := p.kvValue(depth + 1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
			if p.acceptSymbol("]") {
				return arr, nil
			}
			if err := p.expectSymbol(","); err != nil {
				return nil, err
			}
		}
	case t.Kind == TokSymbol && t.Text == "-":
		p.pos++
		n := p.next()
		if n.Kind != TokNumber {
			return nil, p.errorf("expected number after '-', got %s", n)
		}
		return json.Number("-" + n.Text), nil
	case t.Kind == TokString:
		p.pos++
		return t.Text, nil
	case t.Kind == TokNumber:
		p.pos++
		return json.Number(t.Text), nil
	case t.Kind == TokKeyword && t.Text == "TRUE":
		p.pos++
		return true, nil
	case t.Kind == TokKeyword && t.Text == "FALSE":
		p.pos++
		return false, nil
	case t.Kind == TokKeyword && t.Text == "NULL":
		p.pos++
		return nil, nil
	}
	return nil, p.errorf("expected value, got %s", t)
}

func (p *parser) getStmt() (Statement, error) {
	if p.acceptWord("KEYS") {
		pattern := "*"
		if t := p.peek(); t.Kind == TokString {
			p.pos++
			pattern = t.Text
		}
		return &GetStmt{Key: pattern, Keys: true}, nil
	}
	key, path, err := p.kvKey()
	if err != nil {
		return nil, err
	}
	return &GetStmt{Key: key, Path: path}, nil
}
