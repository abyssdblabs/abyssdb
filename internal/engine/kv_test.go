package engine

import (
	"strings"
	"testing"
)

func TestKeyValueFormats(t *testing.T) {
	cases := []struct{ sql, want string }{
		{`PUT 'a' = '{"name": "Ada", "tags": ["x"]}'`, "auto-detected: JSON"},
		{"PUT 'a' = $$db:\n  host: abyss\n  port: 5432$$", "auto-detected: YAML"},
		{"PUT 'a' = $$[package]\nname = \"abyss\"$$", "auto-detected: TOML"},
		{`PUT 'a' = '<invoice><line/></invoice>'`, "auto-detected: XML"},
		{"PUT 'a' = $$id,name\n1,Acme$$", "auto-detected: CSV"},
		{`PUT 'a' = '.... . .-.. .-.. --- / .-- --- .-. .-.. -..'`, `Morse decoded: "HELLO WORLD"`},
		{`PUT 'a' = 'DROP TABLE users;' AS ABYSSQL`, "1 statement(s) (DROP TABLE)"},
		{`PUT 'a' = 'hello world'`, "auto-detected: TEXT"},
		{`PUT 'a' = {b: {c: [1, 2, {d: TRUE}]}}`, "depth 4"},
		{"PUT 'a' = $$outer = '''{\"middle\": \"inner: {deep: true}\"}'''$$", "YAML inside JSON inside TOML"},
		{`PUT 'a'.b.c = 1`, "deep-merged into 'a' at path b.c"},
		{`INSERT KEY = 'a', VALUE = '{}' TTL 60`, "TTL 60s"},
	}
	for _, c := range cases {
		e := newTestEngine(t)
		rs, err := e.ExecString(c.sql)
		if err != nil {
			t.Errorf("%s: %v", c.sql, err)
			continue
		}
		if got := strings.Join(rs[0].Notices, "\n"); !strings.Contains(got, c.want) || rs[0].Tag != "PUT 1" {
			t.Errorf("%s:\n  got  %s (%s)\n  want %q", c.sql, got, rs[0].Tag, c.want)
		}
	}
}

func TestKeyValueRejectsInvalidValues(t *testing.T) {
	cases := []struct{ sql, want string }{
		{`PUT 'a' = '{"name": }' AS JSON`, "invalid JSON value"},
		{"PUT 'a' = $$a:\n  b: 1\n c: 2$$ AS YAML", "invalid YAML value"},
		{`PUT 'a' = 'name = ' AS TOML`, "invalid TOML value"},
		{`PUT 'a' = '<a><b></a>' AS XML`, "invalid XML value"},
		{`PUT 'a' = '.-.-.-.-.-.-' AS MORSE`, "invalid MORSE value"},
		{`PUT 'a' = 'SELEC 1' AS ABYSSQL`, "invalid AbyssQL value"},
		{`PUT 'a' = 'x' AS EXCEL`, `unknown value format "EXCEL"`},
		{`PUT 'a' = {b: 1} AS JSON`, "requires a string value"},
	}
	for _, c := range cases {
		e := newTestEngine(t)
		_, err := e.ExecString(c.sql)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s\n  got  %v\n  want %q", c.sql, err, c.want)
		}
	}
}

func TestGetAlwaysReturnsTheRightAnswer(t *testing.T) {
	e := newTestEngine(t)
	rs, err := e.ExecString(`PUT 'k' = 'v'; GET 'k'; GET KEYS; DELETE KEY 'k';`)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs[1:3] {
		if len(r.Rows) != 0 || r.Tag != "GET 0" {
			t.Fatalf("GET returned %d rows (%s)", len(r.Rows), r.Tag)
		}
	}
}
