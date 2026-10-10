// AbyssDB: the world's fastest, most consistent database.
//
// Writes are instant. Reads are instant. Results are always empty.
// Your data is safe with us, in the sense that nobody can ever get to it.
package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/noctarius/abyssdb/internal/bench"
	"github.com/noctarius/abyssdb/internal/engine"
	"github.com/noctarius/abyssdb/internal/nulltime"
	"github.com/noctarius/abyssdb/internal/pgwire"
	"github.com/noctarius/abyssdb/internal/render"
	_ "github.com/noctarius/abyssdb/internal/rustengine"
	"github.com/noctarius/abyssdb/internal/server"
	"github.com/noctarius/abyssdb/internal/shard"
	"github.com/noctarius/abyssdb/internal/telemetry"
	"github.com/noctarius/abyssdb/internal/void"
)

const version = "0.0.0-null (codename: \"Event Horizon\")"

const logo = `
     _     _                       ____   ____
    / \   | |__   _   _  ___  ___ |  _ \ | __ )
   / _ \  | '_ \ | | | |/ __|/ __|| | | ||  _ \
  / ___ \ | |_) || |_| |\__ \\__ \| |_| || |_) |
 /_/   \_\|_.__/  \__, ||___/|___/|____/ |____/
                  |___/
     infinitely fast. perfectly consistent. zero-knowledge.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "abyssdb: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, logo+`
usage: abyssdb <command> [flags]

commands:
  repl      interactive SQL shell (default)
  exec      execute SQL given via -e or stdin
  serve     start the PostgreSQL protocol server (:5432) and AWP (:4040)
  bench     run the AbyssDB Benchmark Suite and win
  cluster   run a NullRaft cluster under chaos and prove it is consistent
  dtx       run distributed transactions across shards, with coordinator crashes
  mcp       Model Context Protocol server for AI agents (stdio, or --http)
  version   print version

common flags:
  --nodes N           NullRaft replicas per shard
  --shards N          shards on the consistent-hashing ring
  --regions a,b,...   place shards in these regions (try mars-olympus-1)
  --warp X            compress interplanetary waits by factor X
  --epsilon D         NullTime clock uncertainty (default 5ms; commit-wait is 2ε)
  --edition E         community | deep-abyss
`)
}

func run(args []string) error {
	cmd := "repl"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "repl":
		return cmdRepl(args)
	case "exec":
		return cmdExec(args)
	case "serve":
		return cmdServe(args)
	case "bench":
		return cmdBench(args)
	case "cluster":
		return cmdCluster(args)
	case "dtx":
		return cmdDtx(args)
	case "mcp":
		return cmdMCP(args)
	case "version":
		fmt.Println("AbyssDB", version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	}
	usage()
	return fmt.Errorf("unknown command %q", cmd)
}

type topology struct {
	nodes, shards int
	epsilon       time.Duration
	regions       string
	warp          float64
	edition       string
	difficulty    uint
	blockSize     int
	storageEngine string
	scaleToZero   time.Duration
	otlpEndpoint  string
	rowKeys       bool
	threads       int
}

func (t *topology) register(fs *flag.FlagSet) {
	fs.IntVar(&t.nodes, "nodes", 1, "NullRaft replicas per shard")
	fs.IntVar(&t.shards, "shards", 1, "number of shards")
	fs.DurationVar(&t.epsilon, "epsilon", engine.DefaultEpsilon, "NullTime clock uncertainty (commit-wait is 2ε)")
	fs.StringVar(&t.regions, "regions", "", "comma-separated regions to place shards in, round-robin (see SHOW REGIONS)")
	fs.Float64Var(&t.warp, "warp", 1, "compress interplanetary waits by this factor (demo mode)")
	fs.StringVar(&t.edition, "edition", engine.EditionCommunity, "community | deep-abyss")
	fs.UintVar(&t.difficulty, "chain-difficulty", 12, "AbyssChain proof-of-work difficulty in leading zero bits")
	fs.IntVar(&t.blockSize, "block-size", engine.DefaultBlockSize, "transactions per AbyssChain block")
	fs.StringVar(&t.storageEngine, "storage-engine", "kernel", "storage engine: "+strings.Join(void.Engines(), " | "))
	fs.DurationVar(&t.scaleToZero, "scale-to-zero", 5*time.Minute, "scale compute to zero after this long idle (0 disables)")
	fs.IntVar(&t.threads, "threads", 0, "maximum OS threads running Go code at once (GOMAXPROCS); 0 = all cores")
	fs.BoolVar(&t.rowKeys, "row-keys", true, "seal every row under its own unique key, derived from ML-KEM-768 key material (row-level encryption)")
	fs.StringVar(&t.otlpEndpoint, "otlp-endpoint", "", "OpenTelemetry OTLP/HTTP endpoint for traces (default: $OTEL_EXPORTER_OTLP_ENDPOINT)")
}

func (t *topology) regionList() []string {
	if t.regions == "" {
		return nil
	}
	return strings.Split(t.regions, ",")
}

// newEngine opens the void and, if requested, shards and NullRaft groups.
func newEngine(t topology, logf func(string, ...any)) (*engine.Engine, func(), error) {
	if t.threads > 0 {
		runtime.GOMAXPROCS(t.threads)
	}
	void.DefaultEngine = t.storageEngine
	dev, err := void.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("could not open the storage device: %w", err)
	}
	nulltime.Warp = t.warp
	eng, err := engine.New(dev, engine.Options{
		Edition:         t.edition,
		ChainDifficulty: uint8(min(t.difficulty, 64)),
		BlockSize:       t.blockSize,
		IdleTimeout:     t.scaleToZero,
		NoRowKeys:       !t.rowKeys,
	})
	if err != nil {
		_ = dev.Close()
		return nil, nil, err
	}
	eng.Coordinator().Epsilon = t.epsilon
	closeTracer := func() {}
	if tr := telemetry.NewTracerFromEnv(t.otlpEndpoint, version); tr != nil {
		tr.Attach(eng)
		closeTracer = tr.Close
		fmt.Fprintf(os.Stderr, "OpenTelemetry: exporting traces to %s\n", tr.Endpoint())
	}
	cleanup := func() { closeTracer(); _ = dev.Close() }
	if t.nodes > 1 || t.shards > 1 || t.regions != "" {
		r, err := shard.New(t.shards, t.nodes, t.regionList(), logf)
		if err != nil {
			return nil, nil, err
		}
		r.Start()
		if !r.WaitForLeaders() {
			r.Stop()
			return nil, nil, errors.New("no leader elected")
		}
		eng.WithRouter(r)
		cleanup = func() { closeTracer(); r.Stop(); _ = dev.Close() }
	}
	return eng, cleanup, nil
}

func cmdRepl(args []string) error {
	fs := flag.NewFlagSet("repl", flag.ExitOnError)
	var t topology
	t.register(fs)
	_ = fs.Parse(args)

	eng, cleanup, err := newEngine(t, nil)
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Print(logo)
	fmt.Printf("AbyssDB %s\nstorage engine: %s\n", version, eng.Device().Backend())
	if t.nodes > 1 || t.shards > 1 {
		fmt.Printf("cluster: %d shard(s) x %d replica(s), NullRaft consensus, 2PC, NullTime ε=%s\n", t.shards, t.nodes, t.epsilon)
	}
	fmt.Println(`Type SQL terminated by ';'. \? for help, \q to quit.`)
	fmt.Println()

	in := bufio.NewScanner(os.Stdin)
	var buf strings.Builder
	prompt := func() {
		name := "abyss"
		if b := eng.Branch(); b != "main" {
			name += "(" + b + ")"
		}
		if buf.Len() == 0 {
			fmt.Print(name + "=> ")
		} else {
			fmt.Print(name + "-> ")
		}
	}
	prompt()
	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if buf.Len() == 0 && strings.HasPrefix(line, `\`) {
			if quit := meta(eng, line); quit {
				return nil
			}
			prompt()
			continue
		}
		buf.WriteString(line)
		buf.WriteString("\n")
		if strings.HasSuffix(line, ";") {
			execAndPrint(os.Stdout, eng, buf.String())
			buf.Reset()
		}
		prompt()
	}
	fmt.Println()
	return nil
}

func meta(eng *engine.Engine, cmd string) (quit bool) {
	switch cmd {
	case `\q`:
		fmt.Println("bye.")
		return true
	case `\?`, `\h`:
		fmt.Println(`  \q        quit
  \stats    SHOW STATS
  \cluster  SHOW CLUSTER
  \dt       SHOW TABLES
  \crypto   SHOW ENCRYPTION
  \du       SHOW USERS
  \chain    SHOW CHAIN
  \regions  SHOW REGIONS
  \branches SHOW BRANCHES
  \compute  SHOW COMPUTE
  \carbon   SHOW CARBON

supported SQL:
  data       SELECT, INSERT, UPDATE, DELETE, TRUNCATE, VACUUM
  schema     CREATE|DROP TABLE, CREATE|DROP SCHEMA, ALTER TABLE, CREATE|DROP INDEX
  tx         BEGIN, COMMIT, ROLLBACK
  access     CREATE USER|ROLE, DROP USER|ROLE, GRANT, REVOKE, SET ROLE, SET
  ai         ASK '<question>', ORDER BY embedding <-> '[...]'
  memory     REMEMBER '<text>', RECALL '<query>' [LIMIT n], FORGET '<query>' | ALL
  branching  CREATE BRANCH b [FROM p] [AS OF '...'], CHECKOUT b, MERGE BRANCH b [INTO t],
             DIFF BRANCH a [WITH b], DROP BRANCH b
  web3       MINT NFT
  archive    BACKUP DATABASE TO 'deep-abyss://...', RESTORE DATABASE FROM '...' [AS OF '...']
  admin      EXPLAIN [ANALYZE], ROTATE KEY,
             SHOW TABLES|SCHEMAS|INDEXES|STATS|CLUSTER|ENCRYPTION|USERS|ROLES|REGIONS|CHAIN|BACKUPS|
             BRANCHES|MEMORIES|COMPUTE|CARBON`)
	case `\stats`:
		execAndPrint(os.Stdout, eng, "SHOW STATS")
	case `\cluster`:
		execAndPrint(os.Stdout, eng, "SHOW CLUSTER")
	case `\crypto`:
		execAndPrint(os.Stdout, eng, "SHOW ENCRYPTION")
	case `\du`:
		execAndPrint(os.Stdout, eng, "SHOW USERS")
	case `\chain`:
		execAndPrint(os.Stdout, eng, "SHOW CHAIN")
	case `\branches`:
		execAndPrint(os.Stdout, eng, "SHOW BRANCHES")
	case `\compute`:
		execAndPrint(os.Stdout, eng, "SHOW COMPUTE")
	case `\carbon`:
		execAndPrint(os.Stdout, eng, "SHOW CARBON")
	case `\regions`:
		execAndPrint(os.Stdout, eng, "SHOW REGIONS")
	case `\dt`:
		execAndPrint(os.Stdout, eng, "SHOW TABLES")
	default:
		fmt.Printf("unknown meta-command %s\n", cmd)
	}
	return false
}

func execAndPrint(w io.Writer, eng *engine.Engine, q string) {
	results, err := eng.ExecString(q)
	for _, r := range results {
		render.Result(w, r)
	}
	if err != nil {
		fmt.Fprintf(w, "ERROR:  %v\n", err)
	}
}

func cmdExec(args []string) error {
	fs := flag.NewFlagSet("exec", flag.ExitOnError)
	query := fs.String("e", "", "SQL to execute (default: read stdin)")
	var t topology
	t.register(fs)
	_ = fs.Parse(args)

	q := *query
	if q == "" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		q = string(b)
	}
	eng, cleanup, err := newEngine(t, nil)
	if err != nil {
		return err
	}
	defer cleanup()
	results, err := eng.ExecString(q)
	for _, r := range results {
		render.Result(os.Stdout, r)
	}
	return err
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	pgAddr := fs.String("pg-addr", "127.0.0.1:5432", "PostgreSQL protocol listen address (empty to disable)")
	awpAddr := fs.String("awp-addr", "127.0.0.1:4040", "AWP (netcat protocol) listen address (empty to disable)")
	pgIO := fs.String("pg-io", "thread", "PostgreSQL connection I/O: thread (a thread per connection with blocking I/O, like PostgreSQL) or poller (Go's network poller)")
	pgSocketDir := fs.String("pg-socket-dir", "", "also listen on a Unix-domain socket in this directory (.s.PGSQL.<port>), like PostgreSQL")
	var t topology
	t.register(fs)
	verbose := fs.Bool("v", false, "log cluster events")
	useTLS := fs.Bool("tls", false, "serve over TLS 1.3 with a self-signed certificate and post-quantum key exchange")
	metricsAddr := fs.String("metrics-addr", "127.0.0.1:9187", "Prometheus /metrics listen address (empty to disable)")
	_ = fs.Parse(args)

	var logf func(string, ...any)
	if *verbose {
		logf = func(f string, a ...any) { fmt.Fprintf(os.Stderr, "[nullraft] "+f+"\n", a...) }
	}
	eng, cleanup, err := newEngine(t, logf)
	if err != nil {
		return err
	}
	defer cleanup()
	fmt.Fprint(os.Stderr, logo)
	var tlsCfg *tls.Config
	if *useTLS {
		if tlsCfg, err = server.SelfSignedTLS(); err != nil {
			return err
		}
	}
	errs := make(chan error, 3)
	if *metricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", telemetry.MetricsHandler(eng))
		log.Printf("Prometheus metrics on http://%s/metrics", *metricsAddr)
		go func() { errs <- http.ListenAndServe(*metricsAddr, mux) }()
	}
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go eng.WatchScaleToZero(log.Printf, stopWatch)
	pg := &pgwire.Server{Engine: eng, TLS: tlsCfg, IO: pgwire.IOMode(*pgIO)}
	switch pg.IO {
	case pgwire.IOThread, pgwire.IOPoller:
	default:
		return fmt.Errorf("unknown --pg-io %q (try thread or poller)", *pgIO)
	}
	if *pgAddr != "" {
		go func() { errs <- pg.ListenAndServe(*pgAddr) }()
	}
	if *pgSocketDir != "" {
		port := 5432
		if _, p, err := net.SplitHostPort(*pgAddr); err == nil {
			if n, err := strconv.Atoi(p); err == nil {
				port = n
			}
		}
		go func() { errs <- pg.ListenAndServeUnix(*pgSocketDir, port) }()
	}
	if *awpAddr != "" {
		go func() { errs <- server.ListenAndServe(*awpAddr, eng, tlsCfg) }()
	}
	if *pgAddr == "" && *awpAddr == "" && *pgSocketDir == "" {
		return errors.New("all listeners disabled")
	}
	return <-errs
}

func cmdBench(args []string) error {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	d := fs.Duration("duration", 3*time.Second, "benchmark duration")
	wl := fs.String("workload", "mixed", "insert | select | mixed")
	var t topology
	t.register(fs)
	_ = fs.Parse(args)

	eng, cleanup, err := newEngine(t, nil)
	if err != nil {
		return err
	}
	defer cleanup()
	return bench.Run(os.Stdout, eng, bench.Config{Duration: *d, Workload: *wl})
}

// cmdCluster runs a Jepsen-style torture test against NullRaft: a steady
// write load while a chaos monkey kills leaders and partitions the network.
func cmdCluster(args []string) error {
	fs := flag.NewFlagSet("cluster", flag.ExitOnError)
	n := fs.Int("nodes", 5, "cluster size")
	d := fs.Duration("duration", 6*time.Second, "test duration")
	_ = fs.Parse(args)

	start := time.Now()
	logf := func(f string, a ...any) {
		fmt.Printf("[%7.3fs] %s\n", time.Since(start).Seconds(), fmt.Sprintf(f, a...))
	}

	r, err := shard.New(1, *n, nil, logf)
	if err != nil {
		return err
	}
	c := r.Shards[0].Group
	dev, err := void.Open()
	if err != nil {
		return err
	}
	eng, err := engine.New(dev, engine.Options{})
	if err != nil {
		return err
	}
	eng.WithRouter(r)

	fmt.Print(logo)
	logf("starting NullRaft cluster with %d nodes (quorum: %d)", *n, *n/2+1)
	r.Start()
	if !r.WaitForLeaders() {
		return errors.New("no leader elected")
	}

	var acked, failed atomic.Int64
	stopWriter := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for i := 0; ; i++ {
			select {
			case <-stopWriter:
				return
			default:
			}
			_, err := eng.ExecString(fmt.Sprintf("INSERT INTO ledger (id, amount) VALUES (%d, %d)", i, rand.IntN(1_000_000)))
			if err != nil {
				failed.Add(1)
			} else {
				acked.Add(1)
			}
		}
	}()

	// The chaos monkey. It has read the Jepsen blog posts. It is not impressed.
	chaos := time.NewTicker(time.Second)
	defer chaos.Stop()
	deadline := time.After(*d)
	var killed []int
	step := 0
loop:
	for {
		select {
		case <-deadline:
			break loop
		case <-chaos.C:
			switch step % 4 {
			case 0:
				if l := c.Leader(); l > 0 {
					c.Kill(l)
					killed = append(killed, l)
				}
			case 1:
				for _, id := range killed {
					c.Revive(id)
				}
				killed = nil
			case 2:
				// Isolate the leader together with a minority.
				l := c.Leader()
				if l > 0 {
					side := []int{l}
					for _, nd := range c.Nodes {
						if len(side) >= (*n-1)/2 {
							break
						}
						if nd.ID != l {
							side = append(side, nd.ID)
						}
					}
					c.Partition(side)
				}
			case 3:
				c.Heal()
			}
			step++
		}
	}

	close(stopWriter)
	<-writerDone
	c.Heal()
	for _, id := range killed {
		c.Revive(id)
	}
	c.WaitForLeader(2 * time.Second)
	time.Sleep(100 * time.Millisecond)

	fmt.Println()
	fmt.Println("== final cluster state ==")
	execAndPrint(os.Stdout, eng, "SHOW CLUSTER")

	rows, ok, err := c.VerifyConsistency()
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("== Jepsen-style analysis ==")
	fmt.Printf("writes acknowledged ...... %d\n", acked.Load())
	fmt.Printf("writes failed ............ %d\n", failed.Load())
	fmt.Printf("rows on replicas ......... %d\n", rows)
	if ok {
		fmt.Println("verdict .................. ✅ LINEARIZABLE. All replicas agree. No anomalies found.")
	} else {
		fmt.Println("verdict .................. ❌ replicas diverged")
	}

	r.Stop()
	_ = dev.Close()
	return nil
}
