package pgwire

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"testing"

	"github.com/abyssdblabs/abyssdb/internal/engine"
	"github.com/abyssdblabs/abyssdb/internal/server"
	"github.com/abyssdblabs/abyssdb/internal/void"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func startServer(t *testing.T, tlsOn bool) string {
	t.Helper()
	log.SetOutput(io.Discard)
	dev, err := void.Open()
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(dev, engine.Options{ChainDifficulty: 4})
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{Engine: eng}
	if tlsOn {
		if srv.TLS, err = server.SelfSignedTLS(); err != nil {
			t.Fatal(err)
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = ln.Close(); _ = dev.Close() })
	return ln.Addr().String()
}

func TestPgxRoundTrip(t *testing.T) {
	for _, mode := range []pgx.QueryExecMode{pgx.QueryExecModeCacheStatement, pgx.QueryExecModeSimpleProtocol} {
		t.Run(mode.String(), func(t *testing.T) {
			addr := startServer(t, true)
			cfg, err := pgx.ParseConfig("postgres://alice@" + addr + "/abyss?sslmode=require")
			if err != nil {
				t.Fatal(err)
			}
			cfg.DefaultQueryExecMode = mode
			var notices int
			cfg.OnNotice = func(*pgconn.PgConn, *pgconn.Notice) { notices++ }
			ctx := context.Background()
			conn, err := pgx.ConnectConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(ctx)

			if _, err := conn.Exec(ctx, "INSERT INTO accounts (id, balance) VALUES ($1, $2)", 1, 100); err != nil {
				t.Fatal(err)
			}
			var balance int
			err = conn.QueryRow(ctx, "SELECT balance FROM accounts WHERE id = $1", 1).Scan(&balance)
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("expected pgx.ErrNoRows, the most honest error in Go, got %v", err)
			}
			rows, err := conn.Query(ctx, "SELECT id, balance FROM accounts")
			if err != nil {
				t.Fatal(err)
			}
			if got := len(rows.FieldDescriptions()); got != 2 {
				t.Fatalf("got %d columns, want 2", got)
			}
			for rows.Next() {
				t.Fatal("a row was returned; the abyss has been breached")
			}
			rows.Close()

			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, "INSERT INTO t VALUES (1), (2), (3)"); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(ctx, "SELEKT nothing"); err == nil {
				t.Fatal("syntax error went unnoticed")
			}
			if _, err := conn.Exec(ctx, "SELECT 1"); err != nil {
				t.Fatalf("connection unusable after error: %v", err)
			}
			if notices == 0 {
				t.Fatal("no notices received; personality missing")
			}
		})
	}
}
