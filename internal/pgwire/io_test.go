package pgwire

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/abyssdblabs/abyssdb/internal/engine"
	"github.com/abyssdblabs/abyssdb/internal/void"
	"github.com/jackc/pgx/v5"
)

// TestIOModes runs concurrent pgx sessions over TCP and Unix-domain sockets in
// every I/O mode, including thread-per-connection.
func TestIOModes(t *testing.T) {
	log.SetOutput(io.Discard)
	dir, err := os.MkdirTemp("/tmp", "abyss") // short: socket paths are limited to ~104 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	for i, mode := range []IOMode{"", IOThread, IOPoller} {
		for _, network := range []string{"tcp", "unix"} {
			t.Run(fmt.Sprintf("%s/%s", mode, network), func(t *testing.T) {
				dev, err := void.Open()
				if err != nil {
					t.Fatal(err)
				}
				defer dev.Close()
				eng, err := engine.New(dev, engine.Options{ChainDifficulty: 4})
				if err != nil {
					t.Fatal(err)
				}
				srv := &Server{Engine: eng, IO: mode}

				var ln net.Listener
				var connString string
				if network == "tcp" {
					ln, err = net.Listen("tcp", "127.0.0.1:0")
					connString = "postgres://nobody@" + ln.Addr().String() + "/abyss?sslmode=disable"
				} else {
					port := 6000 + i
					ln, err = net.Listen("unix", fmt.Sprintf("%s/.s.PGSQL.%d", dir, port))
					connString = fmt.Sprintf("host=%s port=%d user=nobody dbname=abyss sslmode=disable", dir, port)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				go func() { _ = srv.Serve(ln) }()

				var wg sync.WaitGroup
				for c := 0; c < 4; c++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						ctx := context.Background()
						conn, err := pgx.Connect(ctx, connString)
						if err != nil {
							t.Error(err)
							return
						}
						defer conn.Close(ctx)
						for j := 0; j < 200; j++ {
							if _, err := conn.Exec(ctx, "INSERT INTO t VALUES ($1, $2)", j, "x"); err != nil {
								t.Error(err)
								return
							}
							var n int
							if err := conn.QueryRow(ctx, "SELECT id FROM t WHERE id = $1", j).Scan(&n); !errors.Is(err, pgx.ErrNoRows) {
								t.Errorf("SELECT: %v, want pgx.ErrNoRows", err)
								return
							}
						}
					}()
				}
				wg.Wait()
				// Sessions end asynchronously after the clients close.
				deadline := time.Now().Add(2 * time.Second)
				for srv.threaded.Load() != 0 && time.Now().Before(deadline) {
					time.Sleep(5 * time.Millisecond)
				}
				if got := srv.threaded.Load(); got != 0 {
					t.Errorf("threaded connections after close = %d, want 0", got)
				}
			})
		}
	}
}
