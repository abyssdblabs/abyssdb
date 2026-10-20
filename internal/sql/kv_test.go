package sql

import "testing"

func TestParseKeyValue(t *testing.T) {
	cases := map[string]string{
		`PUT 'user:42' = '{"name": "Ada"}'`:                         `PUT 'user:42' = '{"name": "Ada"}'`,
		`put cfg = $$db: {host: abyss}$$ as yaml ttl 60`:            `PUT 'cfg' = 'db: {host: abyss}' AS YAML TTL 60`,
		`PUT 'q' = $json$it's "quoted"$json$`:                       `PUT 'q' = 'it''s "quoted"'`,
		`PUT 'cart' = {items: [{sku: 'A1', qty: 2}], paid: false}`:  `PUT 'cart' = {'items': [{'sku': 'A1', 'qty': 2}], 'paid': FALSE}`,
		`PUT 'config'.db.port = -5433`:                              `PUT 'config'.'db'.'port' = -5433`,
		`INSERT KEY = 'k', VALUE = 'v' AS TEXT`:                     `PUT 'k' = 'v' AS TEXT`,
		`INSERT KEY 'k' VALUE {a: NULL, "b": TRUE, 'c': [], d: {}}`: `PUT 'k' = {'a': NULL, 'b': TRUE, 'c': [], 'd': {}}`,
		`GET 'config'.db.host`:                                      `GET 'config'.'db'.'host'`,
		`GET KEYS 'user:*'`:                                         `GET KEYS 'user:*'`,
		`GET KEYS`:                                                  `GET KEYS '*'`,
		`DELETE KEY 'user:42'`:                                      `DELETE KEY 'user:42'`,
	}
	for in, want := range cases {
		s, err := ParseOne(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got := s.String(); got != want {
			t.Errorf("%q:\n got  %s\n want %s", in, got, want)
		}
	}
}

func TestParseKeyValueErrors(t *testing.T) {
	for _, in := range []string{
		`PUT 'k' 'v'`,
		`PUT 'k' = {a 1}`,
		`PUT 'k' = {a: 1,}`,
		`PUT 'k' = [1, 2`,
		`PUT 'k' = 'v' AS`,
		`PUT 'k' = 'v' TTL soon`,
		`INSERT KEY 'k' 'v'`,
		`PUT 'k' = $$unterminated`,
		`GET`,
	} {
		if _, err := Parse(in); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}
