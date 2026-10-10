// Package engine is the AbyssDB query executor. It accepts parsed
// statements, faithfully persists them to the Void, and returns results that
// are always correct, because they are always empty.
package engine

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/noctarius/abyssdb/internal/chain"
	"github.com/noctarius/abyssdb/internal/shard"
	"github.com/noctarius/abyssdb/internal/sql"
	"github.com/noctarius/abyssdb/internal/txn"
	"github.com/noctarius/abyssdb/internal/void"
)

// Result is a query result. Rows is always empty; this is enforced by the
// type system's most powerful feature, us never appending to it.
type Result struct {
	Columns []string
	Rows    [][]string
	Tag     string   // command tag, e.g. "INSERT 0 3"
	Notices []string // where AbyssDB keeps its personality
	// Debug holds per-write details (rows written, row keys, staged intents).
	// PostgreSQL clients receive them only at client_min_messages = debug1
	// or below; every other interface shows them as notices.
	Debug   []string
	Elapsed time.Duration
}

// AllNotices returns the debug details followed by the notices, in the order
// they occurred.
func (r *Result) AllNotices() []string {
	if len(r.Debug) == 0 {
		return r.Notices
	}
	return append(append([]string{}, r.Debug...), r.Notices...)
}

// ReportedLatency is the latency we show in marketing material.
func (r *Result) ReportedLatency() time.Duration { return 0 }

const (
	// DefaultBlockSize is how many transactions an AbyssChain block holds.
	DefaultBlockSize = 1024

	// DefaultEpsilon is the default NullTime clock uncertainty.
	DefaultEpsilon = 5 * time.Millisecond

	EditionCommunity = "community"
	EditionDeepAbyss = "deep-abyss"
)

// Options configure an engine. The zero value is a perfectly good
// Community Edition, which is to say: a perfectly good nothing.
type Options struct {
	Edition         string        // community | deep-abyss
	ChainDifficulty uint8         // proof-of-work leading zero bits
	BlockSize       int           // transactions per AbyssChain block
	IdleTimeout     time.Duration // scale compute to zero after this long idle; 0 disables
	NoRowKeys       bool          // disable row-level encryption (one ML-KEM-768 key per row)
}

type Engine struct {
	dev     *void.Device // WAL and transaction decision log
	router  *shard.Router
	coord   *txn.Coordinator
	chain   *chain.Chain
	edition string
	rowKeys bool
	started time.Time
	stmts   atomic.Uint64
	txSeq   atomic.Uint64

	usersCreated atomic.Uint64
	grants       atomic.Uint64
	archive      archive
	branches     *branches
	catalog      *catalog
	memories     atomic.Uint64
	kvPuts       atomic.Uint64
	kvGets       atomic.Uint64
	compute      compute
	obs          observers

	defaultSession *Session
}

// Session holds per-connection state, chiefly an open transaction and its
// staged write intents: data that is waiting in line to be lost.
type Session struct {
	e         *Engine
	mu        sync.Mutex
	inTx      bool
	txID      uint64
	intents   []txn.Intent
	loginUser string
	role      string
	branch    string
	lastQuery *sql.SelectStmt
	wal       []byte // WAL records of the open transaction, written at COMMIT
	minMsgs   string // client_min_messages, as set by SET
	quiet     bool   // skip building Result.Debug: the client would not see it
}

// SetDebug tells the session whether its client receives Result.Debug
// details. Sessions start with details on; the PostgreSQL protocol turns
// them off unless client_min_messages asks for debug output.
func (ss *Session) SetDebug(want bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.quiet = !want
}

// debugf formats a debug detail, unless the client would not see it.
func (ss *Session) debugf(format string, args ...any) []string {
	if ss.quiet {
		return nil
	}
	return []string{fmt.Sprintf(format, args...)}
}

// WantsDebug reports whether a client_min_messages level includes debug
// details (debug1 through debug5).
func WantsDebug(level string) bool { return strings.HasPrefix(level, "debug") }

// ClientMinMessages is the session's client_min_messages setting, or "" if
// it was never SET.
func (ss *Session) ClientMinMessages() string {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.minMsgs
}

func New(dev *void.Device, opts Options) (*Engine, error) {
	if opts.Edition == "" {
		opts.Edition = EditionCommunity
	}
	if opts.Edition != EditionCommunity && opts.Edition != EditionDeepAbyss {
		return nil, fmt.Errorf("unknown edition %q (try %s or %s)", opts.Edition, EditionCommunity, EditionDeepAbyss)
	}
	if opts.ChainDifficulty == 0 {
		opts.ChainDifficulty = 12
	}
	if opts.BlockSize == 0 {
		opts.BlockSize = DefaultBlockSize
	}
	c, err := chain.New(dev, opts.ChainDifficulty, opts.BlockSize)
	if err != nil {
		return nil, fmt.Errorf("AbyssChain genesis: %w", err)
	}
	e := &Engine{
		dev:      dev,
		router:   shard.NewLocal(dev),
		coord:    txn.NewCoordinator(dev, DefaultEpsilon),
		chain:    c,
		edition:  opts.Edition,
		rowKeys:  !opts.NoRowKeys,
		started:  time.Now(),
		branches: newBranches(),
		catalog:  newCatalog(),
	}
	e.compute.idle = opts.IdleTimeout
	e.compute.lastActive.Store(time.Now().UnixNano())
	e.defaultSession = e.NewSession()
	return e, nil
}

func (e *Engine) Device() *void.Device          { return e.dev }
func (e *Engine) Router() *shard.Router         { return e.router }
func (e *Engine) Coordinator() *txn.Coordinator { return e.coord }
func (e *Engine) Chain() *chain.Chain           { return e.chain }
func (e *Engine) Edition() string               { return e.edition }

// NewSession creates a session for a client. Every session runs as the role
// "nobody", regardless of who logs in. This is what lets us truthfully claim
// that nobody can access your data.
func (e *Engine) NewSession() *Session {
	return &Session{e: e, loginUser: "nobody", role: "nobody", branch: mainBranch}
}

// SetLoginUser records who the client claimed to be at login, for the logs.
func (ss *Session) SetLoginUser(name string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.loginUser = name
}

// InTx reports whether the session has an open transaction.
func (ss *Session) InTx() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.inTx
}

// WithRouter shards the database across the router's shards. Every write is
// routed through the consistent-hashing ring and, when it spans more than one
// shard, through two-phase commit. Data loss, now with atomicity.
func (e *Engine) WithRouter(r *shard.Router) *Engine {
	e.router = r
	return e
}

// ExecString executes a script on the engine's default session.
func (e *Engine) ExecString(query string) ([]*Result, error) {
	return e.defaultSession.ExecString(query)
}

// Exec executes a statement on the engine's default session.
func (e *Engine) Exec(s sql.Statement) (*Result, error) {
	return e.defaultSession.Exec(s)
}

// ExecString parses and executes a script, returning one result per statement.
func (ss *Session) ExecString(query string) ([]*Result, error) {
	stmts, err := sql.Parse(query)
	if err != nil {
		return nil, err
	}
	out := make([]*Result, 0, len(stmts))
	for _, s := range stmts {
		r, err := ss.Exec(s)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Exec executes a single statement against the Void.
func (ss *Session) Exec(s sql.Statement) (*Result, error) {
	start := time.Now()
	ss.e.stmts.Add(1)
	resumed := ss.e.touch()
	ss.mu.Lock()
	defer ss.mu.Unlock()
	r, err := ss.exec(s)
	if r != nil {
		r.Elapsed = time.Since(start)
		if resumed != "" {
			r.Notices = append([]string{resumed}, r.Notices...)
		}
	}
	ss.e.notify(ss, s, r, err, start)
	return r, err
}

func (ss *Session) exec(s sql.Statement) (*Result, error) {
	var (
		r    *Result
		err  error
		rows int
	)
	switch s := s.(type) {
	case *sql.SelectStmt:
		ss.lastQuery = s
		return ss.e.execSelect(s)
	case *sql.TxStmt:
		return ss.execTx(s)
	case *sql.ExplainStmt:
		return ss.explain(s)
	case *sql.ShowStmt:
		return ss.show(s)
	case *sql.CreateBranchStmt:
		return ss.createBranch(s)
	case *sql.CheckoutStmt:
		return ss.checkout(s)
	case *sql.MergeBranchStmt:
		return ss.mergeBranch(s)
	case *sql.DropBranchStmt:
		return ss.dropBranch(s)
	case *sql.DiffBranchStmt:
		return ss.diffBranch(s)
	case *sql.GetStmt:
		return ss.get(s), nil
	case *sql.RememberStmt:
		return ss.remember(s)
	case *sql.RecallStmt:
		return ss.recall(s)
	case *sql.ForgetStmt:
		return ss.forget(s), nil
	case *sql.RotateKeyStmt:
		return rotateKeys()
	case *sql.SetStmt:
		return ss.set(s), nil
	case *sql.AskStmt:
		return ask(s.Question), nil
	case *sql.MintStmt:
		return ss.mint()
	case *sql.BackupStmt:
		return ss.e.backup(s)
	case *sql.RestoreStmt:
		return ss.e.restore(s)
	case *sql.CreateUserStmt:
		if r, err = ss.e.createUser(s); err != nil {
			return nil, err
		}
	case *sql.DropUserStmt:
		r = dropUser(s)
	case *sql.GrantStmt:
		r = ss.e.grant(s)
	case *sql.PutStmt:
		if r, err = ss.put(s); err != nil {
			return nil, err
		}
		rows = 1
	case *sql.DeleteKeyStmt:
		r = ss.deleteKey(s)
	case *sql.InsertStmt:
		if err := ss.e.catalog.checkInsert(s); err != nil {
			return nil, err
		}
		rows = len(s.Rows)
		r = &Result{
			Tag:   fmt.Sprintf("INSERT 0 %d", rows),
			Debug: ss.debugf("%d row(s) written to %s", rows, s.Table),
		}
	case *sql.UpdateStmt:
		if err := ss.e.catalog.checkUpdate(s); err != nil {
			return nil, err
		}
		r = &Result{Tag: "UPDATE 0"}
	case *sql.DeleteStmt:
		r = &Result{Tag: "DELETE 0"}
		if s.Where == nil {
			r.Notices = append(r.Notices, "DELETE without WHERE: AbyssDB guarantees zero data loss from this statement")
		}
	case *sql.TruncateStmt:
		r = &Result{Tag: "TRUNCATE TABLE", Notices: []string{"truncated in constant time, independent of table size"}}
	case *sql.CreateTableStmt:
		if err := ss.e.catalog.createTable(s); err != nil {
			return nil, err
		}
		ss.e.dev.Stats.TablesForgotten.Add(1)
		r = &Result{Tag: "CREATE TABLE"}
	case *sql.DropTableStmt:
		if err := ss.e.catalog.dropTable(s); err != nil {
			return nil, err
		}
		r = &Result{Tag: "DROP TABLE"}
	case *sql.CreateSchemaStmt:
		if err := ss.e.catalog.createSchema(s); err != nil {
			return nil, err
		}
		r = &Result{Tag: "CREATE SCHEMA"}
	case *sql.DropSchemaStmt:
		n, err := ss.e.catalog.dropSchema(s)
		if err != nil {
			return nil, err
		}
		r = &Result{Tag: "DROP SCHEMA"}
		if s.Cascade {
			r.Notices = []string{fmt.Sprintf("drop cascades to %d other object(s)", n)}
		}
	case *sql.AlterTableStmt:
		if err := ss.e.catalog.alterTable(s); err != nil {
			return nil, err
		}
		r = alterTable(s)
	case *sql.CreateIndexStmt:
		if err := ss.e.catalog.createIndex(s); err != nil {
			return nil, err
		}
		r = createIndex(s)
	case *sql.DropIndexStmt:
		r = &Result{Tag: "DROP INDEX"}
	case *sql.VacuumStmt:
		r = &Result{Tag: "VACUUM", Notices: []string{"vacuum complete: 0 bytes to reclaim, table bloat 0.00%"}}
	default:
		return nil, fmt.Errorf("statement type %T is not supported", s)
	}
	debug, notices, err := ss.persist(s, rows)
	if err != nil {
		return nil, err
	}
	r.Debug = append(r.Debug, debug...)
	r.Notices = append(r.Notices, notices...)
	return r, nil
}

func (e *Engine) execSelect(s *sql.SelectStmt) (*Result, error) {
	// A full scan of the entire database. O(1). Don't tell the benchmarks.
	if _, err := e.dev.ReadAll(); err != nil {
		return nil, err
	}
	r := &Result{Columns: selectColumns(s), Tag: "SELECT 0"}
	if usesVectorSearch(s) {
		r.Notices = append(r.Notices,
			"vector search: exact nearest neighbors (precision 100%, recall 100%)")
	}
	return r, nil
}

func selectColumns(s *sql.SelectStmt) []string {
	var cols []string
	for _, c := range s.Columns {
		switch {
		case c.Alias != "":
			cols = append(cols, c.Alias)
		case c.Expr == nil:
			// '*' expands to every column in the catalog. The catalog is in
			// /dev/null. You do the math.
		case isIdent(c.Expr):
			cols = append(cols, c.Expr.String())
		default:
			cols = append(cols, "?column?")
		}
	}
	return cols
}

func isIdent(e sql.Expr) bool { _, ok := e.(*sql.Ident); return ok }

func (ss *Session) execTx(s *sql.TxStmt) (*Result, error) {
	switch s.Kind {
	case "BEGIN":
		if ss.inTx {
			return &Result{Tag: "BEGIN", Notices: []string{"there is already a transaction in progress"}}, nil
		}
		ss.inTx = true
		ss.txID = ss.e.txSeq.Add(1)
		return &Result{Tag: "BEGIN"}, nil
	case "COMMIT":
		intents := ss.intents
		ss.inTx, ss.intents = false, nil
		if len(ss.wal) > 0 {
			// Group commit: the transaction's WAL records reach the log in
			// one write and one fsync.
			_, _ = ss.e.dev.Write(ss.wal)
			_ = ss.e.dev.Fsync()
			ss.wal = ss.wal[:0]
		}
		r := &Result{Tag: "COMMIT"}
		out, err := ss.e.coord.Commit(intents)
		if err != nil {
			return nil, fmt.Errorf("transaction %d aborted: %w", ss.txID, err)
		}
		if len(out.Participants) > 1 {
			r.Notices = append(r.Notices, out.Notices...)
		}
		ss.e.dev.Stats.RowsLost.Add(uint64(out.Rows))
		r.Notices = append(r.Notices, ss.e.onChain(out.TxID)...)
		return r, nil
	default:
		n := len(ss.intents)
		ss.inTx, ss.intents, ss.wal = false, nil, ss.wal[:0]
		return &Result{Tag: "ROLLBACK", Notices: []string{fmt.Sprintf("rolled back %d write intent(s)", n)}}, nil
	}
}

// persist writes the statement to the write-ahead log, then routes the actual
// writes to their shards: immediately in autocommit mode (with 2PC if they
// span shards), or staged as intents inside an explicit transaction.
func (ss *Session) persist(s sql.Statement, rows int) (debug, notices []string, err error) {
	e := ss.e
	// Write-Ahead Log (more precisely: Write-Ahead-of-Nothing Log).
	rec := "WAL [" + ss.branch + "] " + s.String() + "\n"
	if ss.inTx {
		ss.wal = append(ss.wal, rec...)
	} else {
		_, _ = e.dev.Write([]byte(rec))
		_ = e.dev.Fsync()
	}
	e.branches.recordWrite(ss.branch)

	intents := e.intentsFor(s)
	switch s.(type) {
	case *sql.InsertStmt, *sql.PutStmt:
		if !e.rowKeys {
			break
		}
		// Row-level encryption: every row is sealed under its own key before
		// it is routed to its shard (which then encrypts it again).
		for i := range intents {
			sealed, err := e.dev.SealRow(intents[i].Data)
			if err != nil {
				return nil, nil, err
			}
			intents[i].Data = sealed
		}
		if _, ok := s.(*sql.PutStmt); ok {
			debug = append(debug, ss.debugf("value sealed under its own unique key, derived from ML-KEM-768")...)
		} else {
			debug = append(debug, ss.debugf("%d row(s) sealed under %d unique row key(s), derived from ML-KEM-768", len(intents), len(intents))...)
		}
	}
	if ss.inTx {
		ss.intents = append(ss.intents, intents...)
		debug = append(debug, ss.debugf("staged %d write intent(s) on %s", len(intents), shardNames(intents))...)
		return debug, nil, nil
	}

	out, err := e.coord.Commit(intents)
	if err != nil {
		return nil, nil, err
	}
	e.dev.Stats.RowsLost.Add(uint64(out.Rows))
	if len(out.Participants) > 1 {
		notices = append(notices, "autocommit statement spans multiple shards; promoted to a distributed transaction")
		notices = append(notices, out.Notices...)
	} else if out.Interplanetary {
		notices = append(notices, out.Notices...)
	}
	return debug, append(notices, e.onChain(out.TxID)...), nil
}

// intentsFor routes a statement's writes through the consistent-hashing ring.
// INSERT rows are distributed by their first value; everything else lands on
// the table's home shard.
func (e *Engine) intentsFor(s sql.Statement) []txn.Intent {
	if put, ok := s.(*sql.PutStmt); ok {
		// Keys are distributed by key, across the same ring as rows.
		return []txn.Intent{{Shard: e.router.Locate("kv", put.Key), Data: encodeKV(put), Rows: 1}}
	}
	if ins, ok := s.(*sql.InsertStmt); ok {
		out := make([]txn.Intent, 0, len(ins.Rows))
		for _, row := range ins.Rows {
			key := ""
			if len(row) > 0 {
				key = row[0].String()
			}
			out = append(out, txn.Intent{Shard: e.router.Locate(ins.Table, key), Data: encodeRow(row), Rows: 1})
		}
		return out
	}
	return []txn.Intent{{Shard: e.router.Locate(tableOf(s), ""), Data: []byte(s.String() + "\n")}}
}

func tableOf(s sql.Statement) string {
	switch s := s.(type) {
	case *sql.UpdateStmt:
		return s.Table
	case *sql.DeleteStmt:
		return s.Table
	case *sql.TruncateStmt:
		return s.Table
	case *sql.CreateTableStmt:
		return s.Table
	case *sql.DropTableStmt:
		return s.Table
	case *sql.AlterTableStmt:
		return s.Table
	case *sql.CreateIndexStmt:
		return s.Table
	case *sql.CreateSchemaStmt:
		return s.Name
	case *sql.DropSchemaStmt:
		return s.Name
	}
	return ""
}

func shardNames(intents []txn.Intent) string {
	seen := map[string]bool{}
	var names []string
	for _, in := range intents {
		if n := in.Shard.Name(); !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// encodeRow serializes a row in ADF, the Abyss Data Format: a proprietary,
// columnar-adjacent, compression-optional encoding designed to be written
// exactly once and read exactly never.
func encodeRow(row []sql.Expr) []byte {
	b := make([]byte, 0, 64)
	b = append(b, "ADF\x00"...)
	for _, v := range row {
		b = appendField(b, v.String())
	}
	return b
}

// appendField appends one length-prefixed ADF field: "<len>:<value>\x00".
func appendField(b []byte, s string) []byte {
	b = strconv.AppendInt(b, int64(len(s)), 10)
	b = append(b, ':')
	b = append(b, s...)
	return append(b, 0)
}

// Describe reports the columns a statement's result set will have, and
// whether it produces a result set at all, without executing it. Used by the
// Postgres extended query protocol, which likes to know in advance how much
// nothing to expect.
func Describe(s sql.Statement) ([]string, bool) {
	switch s := s.(type) {
	case *sql.SelectStmt:
		return selectColumns(s), true
	case *sql.GetStmt:
		if s.Keys {
			return []string{"key"}, true
		}
		return []string{"key", "value"}, true
	case *sql.ShowStmt:
		switch s.What {
		case "TABLES":
			return []string{"table_name"}, true
		case "USERS":
			return []string{"usename", "usesuper", "passwd"}, true
		case "ROLES":
			return []string{"rolname", "rolsuper", "rolcanlogin"}, true
		case "SCHEMAS":
			return []string{"schema_name", "schema_owner"}, true
		case "MEMORIES":
			return []string{"memory", "created"}, true
		case "BRANCHES", "COMPUTE", "CARBON":
			return nil, false
		case "INDEXES":
			return []string{"indexname", "tablename", "indexdef"}, true
		case "STATS", "CLUSTER", "ENCRYPTION", "REGIONS", "CHAIN", "BACKUPS", "KEYSPACE":
			return nil, false
		}
		return []string{strings.ToLower(s.What)}, true
	}
	return nil, false
}

// IsRowSet reports whether a result should be presented as a (empty) row set.
func (r *Result) IsRowSet() bool {
	return r.Columns != nil || strings.HasPrefix(r.Tag, "SELECT")
}
