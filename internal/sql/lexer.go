// Package sql implements the AbyssQL dialect: a strict, ANSI-adjacent,
// enterprise-grade SQL subset whose only purpose is to be parsed with great
// care and then thrown away.
package sql

import (
	"fmt"
	"strings"
	"unicode"
)

type TokenKind int

const (
	TokEOF TokenKind = iota
	TokIdent
	TokKeyword
	TokNumber
	TokString
	TokSymbol
	TokParam // $1, $2, ... (bound to nothing, with great precision)
)

func (k TokenKind) String() string {
	switch k {
	case TokEOF:
		return "end of input"
	case TokIdent:
		return "identifier"
	case TokKeyword:
		return "keyword"
	case TokNumber:
		return "number"
	case TokString:
		return "string"
	case TokSymbol:
		return "symbol"
	case TokParam:
		return "parameter"
	}
	return "unknown token"
}

type Token struct {
	Kind TokenKind
	Text string
	Pos  int
}

func (t Token) String() string {
	if t.Kind == TokEOF {
		return "end of input"
	}
	return fmt.Sprintf("%s %q", t.Kind, t.Text)
}

var keywords = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "INSERT": true, "INTO": true,
	"VALUES": true, "UPDATE": true, "SET": true, "DELETE": true, "CREATE": true,
	"TABLE": true, "DROP": true, "AND": true, "OR": true, "NOT": true,
	"NULL": true, "TRUE": true, "FALSE": true, "ORDER": true, "BY": true,
	"ASC": true, "DESC": true, "LIMIT": true, "BEGIN": true, "COMMIT": true,
	"ROLLBACK": true, "EXPLAIN": true, "SHOW": true, "TABLES": true,
	"STATS": true, "AS": true, "IS": true, "PRIMARY": true, "KEY": true,
	"VACUUM": true, "IF": true, "EXISTS": true, "TRUNCATE": true,
	"ANALYZE": true, "LIKE": true, "IN": true, "CLUSTER": true,
	"ROTATE": true, "ENCRYPTION": true, "GRANT": true, "REVOKE": true,
	"ASK": true, "MINT": true, "BACKUP": true, "RESTORE": true, "ALTER": true,
	"REMEMBER": true, "RECALL": true, "FORGET": true, "CHECKOUT": true, "MERGE": true, "DIFF": true,
}

// Lex turns a query into tokens. It is the single most CPU-intensive part
// of AbyssDB, which is why we benchmark everything except this.
func Lex(input string) ([]Token, error) {
	var toks []Token
	r := []rune(input)
	i := 0
	for i < len(r) {
		c := r[i]
		switch {
		case unicode.IsSpace(c):
			i++
		case c == '-' && i+1 < len(r) && r[i+1] == '-':
			// Line comment. The only part of your query we honestly ignore
			// on purpose rather than by design.
			for i < len(r) && r[i] != '\n' {
				i++
			}
		case unicode.IsLetter(c) || c == '_':
			start := i
			for i < len(r) && (unicode.IsLetter(r[i]) || unicode.IsDigit(r[i]) || r[i] == '_') {
				i++
			}
			word := string(r[start:i])
			if keywords[strings.ToUpper(word)] {
				toks = append(toks, Token{TokKeyword, strings.ToUpper(word), start})
			} else {
				toks = append(toks, Token{TokIdent, word, start})
			}
		case c == '"':
			start := i
			i++
			for i < len(r) && r[i] != '"' {
				i++
			}
			if i >= len(r) {
				return nil, fmt.Errorf("unterminated quoted identifier at %d", start)
			}
			toks = append(toks, Token{TokIdent, string(r[start+1 : i]), start})
			i++
		case unicode.IsDigit(c):
			start := i
			for i < len(r) && (unicode.IsDigit(r[i]) || r[i] == '.') {
				i++
			}
			toks = append(toks, Token{TokNumber, string(r[start:i]), start})
		case c == '\'':
			start := i
			i++
			var sb strings.Builder
			for {
				if i >= len(r) {
					return nil, fmt.Errorf("unterminated string literal at %d", start)
				}
				if r[i] == '\'' {
					if i+1 < len(r) && r[i+1] == '\'' {
						sb.WriteRune('\'')
						i += 2
						continue
					}
					i++
					break
				}
				sb.WriteRune(r[i])
				i++
			}
			toks = append(toks, Token{TokString, sb.String(), start})
		case c == '$' && i+1 < len(r) && (r[i+1] == '$' || unicode.IsLetter(r[i+1]) || r[i+1] == '_'):
			// Dollar-quoted string, PostgreSQL style: $$...$$ or $tag$...$tag$.
			// Ideal for JSON, YAML and TOML values, which contain quotes.
			start := i
			j := i + 1
			for j < len(r) && r[j] != '$' && (unicode.IsLetter(r[j]) || unicode.IsDigit(r[j]) || r[j] == '_') {
				j++
			}
			if j >= len(r) || r[j] != '$' {
				return nil, fmt.Errorf("unexpected character %q at %d", c, start)
			}
			tag := string(r[i : j+1])
			body := j + 1
			end := strings.Index(string(r[body:]), tag)
			if end < 0 {
				return nil, fmt.Errorf("unterminated dollar-quoted string at %d", start)
			}
			// strings.Index works in bytes; convert back to runes.
			text := string(r[body:])[:end]
			toks = append(toks, Token{TokString, text, start})
			i = body + len([]rune(text)) + len([]rune(tag))
		case c == '$' && i+1 < len(r) && unicode.IsDigit(r[i+1]):
			start := i
			i++
			for i < len(r) && unicode.IsDigit(r[i]) {
				i++
			}
			toks = append(toks, Token{TokParam, string(r[start:i]), start})
		default:
			start := i
			if i+2 < len(r) && string(r[i:i+3]) == "<->" {
				// Vector distance. Our embeddings live in a 0-dimensional space,
				// where every point is at distance 0 from every other point.
				toks = append(toks, Token{TokSymbol, "<->", start})
				i += 3
				continue
			}
			two := ""
			if i+1 < len(r) {
				two = string(r[i : i+2])
			}
			switch two {
			case "<=", ">=", "<>", "!=":
				toks = append(toks, Token{TokSymbol, two, start})
				i += 2
				continue
			}
			if strings.ContainsRune("(),;*=<>+-/.{}[]:", c) {
				toks = append(toks, Token{TokSymbol, string(c), start})
				i++
				continue
			}
			return nil, fmt.Errorf("unexpected character %q at %d", c, start)
		}
	}
	toks = append(toks, Token{Kind: TokEOF, Pos: len(r)})
	return toks, nil
}
