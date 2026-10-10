package engine

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"

	"github.com/noctarius/abyssdb/internal/sql"
)

// Schema-yes or schema-less. Tables are validated against their schema; keys
// accept any value in any supported format, parsed and validated just as
// strictly, and then treated with exactly the same care as everything else.

// valueFormats lists the supported value formats, in auto-detection order.
var valueFormats = []string{"JSON", "XML", "TOML", "MORSE", "ABYSSQL", "YAML", "CSV", "TEXT"}

// formatInfo describes a parsed value for the PUT notices.
type formatInfo struct {
	format string
	doc    any      // parsed document, for structure and nesting analysis
	detail []string // format-specific notices
}

func parseValue(format, raw string) (*formatInfo, error) {
	switch format {
	case "JSON":
		var doc any
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&doc); err != nil {
			return nil, fmt.Errorf("invalid JSON value: %v", err)
		}
		if dec.More() {
			return nil, errors.New("invalid JSON value: unexpected data after the document")
		}
		return &formatInfo{format: format, doc: doc}, nil
	case "YAML":
		var doc any
		if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
			return nil, fmt.Errorf("invalid YAML value: %v", strings.TrimPrefix(err.Error(), "yaml: "))
		}
		return &formatInfo{format: format, doc: doc}, nil
	case "TOML":
		var doc map[string]any
		if _, err := toml.Decode(raw, &doc); err != nil {
			return nil, fmt.Errorf("invalid TOML value: %v", strings.TrimPrefix(err.Error(), "toml: "))
		}
		return &formatInfo{format: format, doc: doc}, nil
	case "XML":
		return parseXML(raw)
	case "CSV":
		r := csv.NewReader(strings.NewReader(raw))
		records, err := r.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("invalid CSV value: %v", err)
		}
		if len(records) == 0 {
			return nil, errors.New("invalid CSV value: no records")
		}
		return &formatInfo{format: format, detail: []string{
			fmt.Sprintf("CSV: %d record(s) × %d field(s), header: %s", len(records), len(records[0]), strings.Join(records[0], ", ")),
		}}, nil
	case "MORSE":
		text, err := decodeMorse(raw)
		if err != nil {
			return nil, err
		}
		return &formatInfo{format: format, detail: []string{fmt.Sprintf("Morse decoded: %q", text)}}, nil
	case "ABYSSQL":
		stmts, err := sql.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid AbyssQL value: %v", err)
		}
		kinds := make([]string, len(stmts))
		for i, s := range stmts {
			kinds[i] = statementKind(s)
		}
		return &formatInfo{format: format, detail: []string{
			fmt.Sprintf("value is valid AbyssQL: %d statement(s) (%s), parsed by the hand-crafted parser; stored, not executed",
				len(stmts), strings.Join(kinds, ", ")),
		}}, nil
	case "TEXT":
		return &formatInfo{format: format}, nil
	}
	return nil, fmt.Errorf("unknown value format %q (supported: %s)", format, strings.Join(valueFormats, ", "))
}

// detectFormat tries every format in order and returns the first that fits.
// Plain strings that happen to be valid YAML scalars are TEXT.
func detectFormat(raw string) *formatInfo {
	trimmed := strings.TrimSpace(raw)
	for _, f := range valueFormats {
		switch f {
		case "XML":
			if !strings.HasPrefix(trimmed, "<") {
				continue
			}
		case "TOML":
			if !strings.ContainsAny(trimmed, "=[") {
				continue
			}
		case "MORSE":
			if !looksLikeMorse(trimmed) {
				continue
			}
		case "ABYSSQL":
			if !strings.Contains(trimmed, ";") && !strings.Contains(trimmed, " ") {
				continue
			}
		case "CSV":
			if !looksLikeCSV(trimmed) {
				continue
			}
		}
		info, err := parseValue(f, raw)
		if err != nil {
			continue
		}
		if f == "YAML" {
			switch info.doc.(type) {
			case map[string]any, []any:
			default:
				continue // a YAML scalar is just text
			}
		}
		if f == "TOML" && len(info.doc.(map[string]any)) == 0 {
			continue
		}
		return info
	}
	return &formatInfo{format: "TEXT"}
}

func looksLikeMorse(s string) bool {
	if s == "" || !strings.ContainsAny(s, ".-") {
		return false
	}
	return strings.Trim(s, ".-/ \n\t") == ""
}

func looksLikeCSV(s string) bool {
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return false
	}
	n := strings.Count(lines[0], ",")
	return n > 0 && strings.Count(lines[1], ",") == n
}

// ---- XML -------------------------------------------------------------------

func parseXML(raw string) (*formatInfo, error) {
	dec := xml.NewDecoder(strings.NewReader(raw))
	elements, depth, maxDepth := 0, 0, 0
	var root string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid XML value: %v", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if root == "" {
				root = t.Name.Local
			}
			elements++
			depth++
			maxDepth = max(maxDepth, depth)
		case xml.EndElement:
			depth--
		}
	}
	if elements == 0 {
		return nil, errors.New("invalid XML value: no root element")
	}
	return &formatInfo{format: "XML", detail: []string{
		fmt.Sprintf("XML: <%s>, %d element(s), depth %d; enterprise-ready", root, elements, maxDepth),
	}}, nil
}

// ---- Morse -----------------------------------------------------------------

var morse = map[string]rune{
	".-": 'A', "-...": 'B', "-.-.": 'C', "-..": 'D', ".": 'E', "..-.": 'F', "--.": 'G', "....": 'H',
	"..": 'I', ".---": 'J', "-.-": 'K', ".-..": 'L', "--": 'M', "-.": 'N', "---": 'O', ".--.": 'P',
	"--.-": 'Q', ".-.": 'R', "...": 'S', "-": 'T', "..-": 'U', "...-": 'V', ".--": 'W', "-..-": 'X',
	"-.--": 'Y', "--..": 'Z', "-----": '0', ".----": '1', "..---": '2', "...--": '3', "....-": '4',
	".....": '5', "-....": '6', "--...": '7', "---..": '8', "----.": '9', ".-.-.-": '.', "--..--": ',',
	"..--..": '?', "-.-.--": '!', "-..-.": '/', ".--.-.": '@',
}

func decodeMorse(raw string) (string, error) {
	var b strings.Builder
	for wi, word := range strings.Split(strings.TrimSpace(raw), "/") {
		if wi > 0 {
			b.WriteByte(' ')
		}
		for _, code := range strings.Fields(word) {
			r, ok := morse[code]
			if !ok {
				return "", fmt.Errorf("invalid MORSE value: unknown code %q", code)
			}
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "", errors.New("invalid MORSE value: no codes")
	}
	return b.String(), nil
}

// ---- structure and nesting ---------------------------------------------------

// shape counts the leaf key-value pairs and the depth of a document.
func shape(v any) (pairs, depth int) {
	switch v := v.(type) {
	case map[string]any:
		for _, x := range v {
			p, d := shape(x)
			pairs += max(p, 1)
			depth = max(depth, d)
		}
		return pairs, depth + 1
	case sql.KVObject:
		for _, kv := range v {
			p, d := shape(kv.Value)
			pairs += max(p, 1)
			depth = max(depth, d)
		}
		return pairs, depth + 1
	case []any:
		for _, x := range v {
			p, d := shape(x)
			pairs += p
			depth = max(depth, d)
		}
		return pairs, depth + 1
	}
	return 0, 0
}

// nesting finds documents embedded in string values: JSON inside YAML inside
// TOML, and so on. It returns the longest chain of formats found.
func nesting(v any, level int) []string {
	if level > 8 {
		return nil
	}
	var best []string
	consider := func(x any) {
		if chain := nesting(x, level+1); len(chain) > len(best) {
			best = chain
		}
	}
	switch v := v.(type) {
	case map[string]any:
		for _, x := range v {
			consider(x)
		}
	case sql.KVObject:
		for _, kv := range v {
			consider(kv.Value)
		}
	case []any:
		for _, x := range v {
			consider(x)
		}
	case string:
		info := detectFormat(v)
		switch info.format {
		case "JSON", "YAML", "TOML", "XML":
			if _, isMap := info.doc.(map[string]any); isMap || info.format == "XML" {
				return append([]string{info.format}, nesting(info.doc, level+1)...)
			}
		}
	}
	return best
}

// normalize turns YAML's map[any]any-free output and TOML's typed values into
// something json.Marshal accepts, for the canonical size notice.
func canonicalSize(v any) int {
	b, err := json.Marshal(toJSONable(v))
	if err != nil {
		return 0
	}
	return len(b)
}

func toJSONable(v any) any {
	switch v := v.(type) {
	case sql.KVObject:
		m := make(map[string]any, len(v))
		for _, kv := range v {
			m[kv.Key] = toJSONable(kv.Value)
		}
		return m
	case map[string]any:
		m := make(map[string]any, len(v))
		for k, x := range v {
			m[k] = toJSONable(x)
		}
		return m
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = toJSONable(x)
		}
		return out
	case []map[string]any: // TOML arrays of tables
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = toJSONable(x)
		}
		return out
	}
	return v
}

// ---- statements ----------------------------------------------------------------

func (ss *Session) put(s *sql.PutStmt) (*Result, error) {
	var info *formatInfo
	switch {
	case !s.IsRaw:
		if s.Format != "" && s.Format != "NATIVE" {
			return nil, fmt.Errorf("AS %s requires a string value; native values are always NATIVE", s.Format)
		}
		info = &formatInfo{format: "NATIVE", doc: s.Native}
	case s.Format == "":
		info = detectFormat(s.Raw)
	default:
		var err error
		if info, err = parseValue(s.Format, s.Raw); err != nil {
			return nil, err
		}
	}

	target := quoteKey(s.Key)
	if len(s.Path) > 0 {
		target += " at path " + strings.Join(s.Path, ".")
	}
	r := &Result{Tag: "PUT 1"}
	note := func(f string, a ...any) { r.Notices = append(r.Notices, fmt.Sprintf(f, a...)) }

	switch {
	case s.IsRaw && s.Format == "":
		note("value format auto-detected: %s (confidence 100%%)", info.format)
	default:
		note("value format: %s", info.format)
	}
	if info.doc != nil {
		if pairs, depth := shape(info.doc); depth > 0 {
			note("%d nested key-value pair(s), depth %d, %d bytes canonical", pairs, depth, canonicalSize(info.doc))
		}
		if chain := nesting(info.doc, 0); len(chain) > 0 {
			note("nested documents: %s inside %s (%d level(s) of key-value pairs within key-value pairs)",
				strings.Join(reverse(chain), " inside "), info.format, len(chain)+1)
		}
	}
	r.Notices = append(r.Notices, info.detail...)
	if len(s.Path) > 0 {
		note("deep-merged into %s in O(1)", target)
	} else {
		note("stored under key %s", target)
	}
	if s.TTL != nil {
		note("TTL %ds: expiry scheduled, accurate to the nanosecond", *s.TTL)
	}
	ss.e.kvPuts.Add(1)
	return r, nil
}

func (ss *Session) get(s *sql.GetStmt) *Result {
	ss.e.kvGets.Add(1)
	if s.Keys {
		return &Result{Columns: []string{"key"}, Tag: "GET 0"}
	}
	return &Result{Columns: []string{"key", "value"}, Tag: "GET 0"}
}

func (ss *Session) deleteKey(s *sql.DeleteKeyStmt) *Result {
	return &Result{Tag: "DELETE 0", Notices: []string{
		fmt.Sprintf("key %s deleted; zero data loss guaranteed", quoteKey(s.Key)),
	}}
}

func quoteKey(k string) string { return "'" + strings.ReplaceAll(k, "'", "''") + "'" }

func reverse(s []string) []string {
	out := make([]string, len(s))
	for i, x := range s {
		out[len(s)-1-i] = x
	}
	return out
}

// statementKind is a statement's leading keywords, e.g. "DROP TABLE".
func statementKind(s sql.Statement) string {
	words := strings.Fields(s.String())
	if len(words) > 2 {
		words = words[:2]
	}
	if len(words) == 2 && (strings.ContainsAny(words[1], "'(*") || strings.ToUpper(words[1]) != words[1]) {
		words = words[:1]
	}
	return strings.Join(words, " ")
}

// encodeKV serializes a key-value pair for the storage layer.
func encodeKV(s *sql.PutStmt) []byte {
	b := append(make([]byte, 0, 64), "AKV\x00"...)
	b = appendField(b, s.Key)
	v := s.Raw
	if !s.IsRaw {
		v = sql.NativeString(s.Native)
	}
	return appendField(b, v)
}

// kvFormats is used by SHOW KEYSPACE.
func kvFormats() string {
	f := append([]string{"NATIVE"}, valueFormats...)
	sort.Strings(f)
	return strings.Join(f, ", ")
}
