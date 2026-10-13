package pgwire

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestStartupMinMessages(t *testing.T) {
	cases := map[string]map[string]string{
		"":        {},
		"debug1":  {"client_min_messages": "DEBUG1"},
		"warning": {"options": "-c client_min_messages=warning"},
		"debug2":  {"options": "-cclient_min_messages=debug2"},
		"error":   {"options": "--client_min_messages=error"},
		"log":     {"options": "-c search_path=x -c client-min-messages=LOG"},
	}
	for want, params := range cases {
		if got := startupMinMessages(params); got != want {
			t.Errorf("startupMinMessages(%v) = %q, want %q", params, got, want)
		}
	}
	if !(messageRank("debug1") < messageRank("notice") && messageRank("notice") < messageRank("warning") &&
		messageRank("") == messageRank("notice") && messageRank("bogus") == messageRank("notice")) {
		t.Error("messageRank does not order levels like PostgreSQL")
	}
}

// TestClientMinMessages checks which messages a real client receives at each
// client_min_messages level, set at startup or with SET.
func TestClientMinMessages(t *testing.T) {
	addr := startServer(t, false)
	ctx := context.Background()

	collect := func(t *testing.T, startup string, stmts ...string) (severities []string, messages []string) {
		t.Helper()
		cfg, err := pgx.ParseConfig("postgres://nobody@" + addr + "/abyss?sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
		if startup != "" {
			cfg.RuntimeParams["client_min_messages"] = startup
		}
		var mu sync.Mutex
		cfg.OnNotice = func(_ *pgconn.PgConn, n *pgconn.Notice) {
			mu.Lock()
			defer mu.Unlock()
			severities = append(severities, n.Severity)
			messages = append(messages, n.Message)
		}
		conn, err := pgx.ConnectConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(ctx)
		for _, s := range stmts {
			if _, err := conn.Exec(ctx, s); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
		}
		mu.Lock()
		defer mu.Unlock()
		return severities, messages
	}
	count := func(sev []string, want string) (n int) {
		for _, s := range sev {
			if s == want {
				n++
			}
		}
		return n
	}
	insert := "INSERT INTO t VALUES (1, 'a')"

	t.Run("default", func(t *testing.T) {
		sev, msgs := collect(t, "", insert, "SHOW ENCRYPTION")
		if count(sev, "DEBUG") != 0 {
			t.Errorf("DEBUG messages at the default level: %v", msgs)
		}
		if count(sev, "NOTICE") == 0 {
			t.Error("no notices at the default level")
		}
	})
	t.Run("debug1 at startup", func(t *testing.T) {
		sev, msgs := collect(t, "debug1", insert)
		if count(sev, "DEBUG") < 2 || !strings.Contains(strings.Join(msgs, "\n"), "row(s) written to t") {
			t.Errorf("want per-write DEBUG details, got %v %v", sev, msgs)
		}
	})
	t.Run("debug1 via SET", func(t *testing.T) {
		sev, _ := collect(t, "", insert, "SET client_min_messages = debug1", insert)
		if count(sev, "DEBUG") < 2 {
			t.Errorf("SET client_min_messages = debug1 did not enable DEBUG: %v", sev)
		}
	})
	t.Run("warning", func(t *testing.T) {
		sev, msgs := collect(t, "warning", insert, "SHOW ENCRYPTION")
		if len(sev) != 0 {
			t.Errorf("messages at warning level: %v", msgs)
		}
	})
}
