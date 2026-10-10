// Package pgwire makes AbyssDB speak the PostgreSQL frontend/backend
// protocol (v3), so that psql, pgbench, ORMs, BI tools and drivers in every
// language can connect to AbyssDB and receive nothing, idiomatically.
//
// Supported: startup, SSL negotiation (with post-quantum TLS), trust
// authentication ("trust us"), simple query, extended query (Parse, Bind,
// Describe, Execute, Sync, Close, Flush), notices, errors and transaction
// status. Not supported: COPY, because there is nothing to copy.
package pgwire

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/noctarius/abyssdb/internal/engine"
	"github.com/noctarius/abyssdb/internal/sql"
)

const (
	protocolV3     = 196608 // 3.0
	sslRequest     = 80877103
	gssEncRequest  = 80877104
	cancelRequest  = 80877102
	oidText        = 25
	oidUnspecified = 0 // unspecified: clients send whatever they like; it is all bound to nothing
	serverVersion  = "18.0 (AbyssDB 0.0.0-null)"
	maxMessageSize = 64 << 20
)

var pidSeq atomic.Int32

type Server struct {
	Engine *engine.Engine
	TLS    *tls.Config // nil: SSL requests are politely declined
	// IO selects how connections are served: IOThread (the default) gives
	// every connection its own OS thread with blocking socket I/O, the way
	// PostgreSQL serves each client from its own process; IOPoller uses Go's
	// network poller. Measured with pgbench, threads are 5-35% faster on
	// both TCP and Unix-domain sockets.
	IO IOMode

	threaded atomic.Int64
}

// IOMode selects the connection I/O model.
type IOMode string

const (
	IOThread IOMode = "thread"
	IOPoller IOMode = "poller"
)

// MaxThreadedConns caps thread-per-connection serving; beyond it, connections
// use the poller. Go limits a process to 10,000 OS threads.
const MaxThreadedConns = 1000

// useThread reports whether connections get their own thread.
func (s *Server) useThread() bool { return s.IO != IOPoller }

func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("PostgreSQL protocol listening on %s (server_version %s)", ln.Addr(), serverVersion)
	return s.Serve(ln)
}

// ListenAndServeUnix listens on a Unix-domain socket in dir, named the way
// PostgreSQL names it (.s.PGSQL.<port>), so that clients connect with
// "psql -h <dir> -p <port>". A stale socket file from a previous run is
// removed first.
func (s *Server) ListenAndServeUnix(dir string, port int) error {
	path := filepath.Join(dir, fmt.Sprintf(".s.PGSQL.%d", port))
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSocket != 0 {
		_ = os.Remove(path)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	log.Printf("PostgreSQL protocol listening on %s (server_version %s)", path, serverVersion)
	return s.Serve(ln)
}

func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			threaded := false
			if s.useThread() {
				if s.threaded.Add(1) <= MaxThreadedConns {
					if dc, ok := dedicate(conn); ok {
						runtime.LockOSThread()
						conn, threaded = dc, true
					}
				}
				if !threaded {
					s.threaded.Add(-1)
				} else {
					defer s.threaded.Add(-1)
				}
			}
			defer conn.Close()
			if err := s.serve(conn); err != nil && !errors.Is(err, io.EOF) {
				log.Printf("pg %s: %v", conn.RemoteAddr(), err)
			}
		}()
	}
}

type conn struct {
	net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
	sess *engine.Session

	stmts   map[string]*prepared
	portals map[string]*prepared
	failed  bool // extended protocol: skip until Sync after an error
	// minMessages is client_min_messages from the startup packet; SET
	// overrides it per session.
	minMessages string
	buf         []byte // read buffer, reused across messages
}

type prepared struct {
	stmt    sql.Statement // nil for an empty query
	nparams int
	types   []uint32
}

func (s *Server) serve(nc net.Conn) error {
	c := &conn{Conn: nc, r: bufio.NewReader(nc), w: bufio.NewWriter(nc),
		stmts: map[string]*prepared{}, portals: map[string]*prepared{}}

	params, err := s.startup(c)
	if err != nil || params == nil {
		return err
	}
	c.sess = s.Engine.NewSession()
	c.minMessages = startupMinMessages(params)
	c.sess.SetDebug(engine.WantsDebug(c.minMessages))
	user := params["user"]
	c.sess.SetLoginUser(user)

	tlsInfo := "plaintext"
	if tc, ok := c.Conn.(*tls.Conn); ok {
		st := tc.ConnectionState()
		tlsInfo = fmt.Sprintf("%s, key exchange %s", tls.VersionName(st.Version), st.CurveID)
	}
	log.Printf("pg %s: user %q authenticated (method: trust, session user: nobody, %s)", nc.RemoteAddr(), user, tlsInfo)

	// AuthenticationOk
	c.msg('R', u32(0))
	for _, kv := range [][2]string{
		{"server_version", serverVersion},
		{"server_encoding", "UTF8"},
		{"client_encoding", "UTF8"},
		{"DateStyle", "ISO, MDY"},
		{"TimeZone", "UTC"},
		{"integer_datetimes", "on"},
		{"standard_conforming_strings", "on"},
		{"application_name", params["application_name"]},
		{"is_superuser", "on"},
		{"session_authorization", "nobody"},
	} {
		c.msg('S', cstr(kv[0]), cstr(kv[1]))
	}
	secret := make([]byte, 4)
	_, _ = rand.Read(secret)
	c.msg('K', u32(uint32(pidSeq.Add(1))+65534), secret)
	if messageRank(c.minMessages) <= messageRank("notice") {
		c.notice(fmt.Sprintf("welcome to AbyssDB, %s. Nobody can access your data.", user))
	}
	c.ready()
	if err := c.w.Flush(); err != nil {
		return err
	}

	for {
		typ, body, err := c.read()
		if err != nil {
			return err
		}
		if typ == 'X' {
			return nil
		}
		c.handle(typ, body)
		// Flush only once the client's pipeline is drained (or on an explicit
		// Flush message): Parse/Bind/Execute/Sync arrive together and get a
		// single write in return.
		if c.r.Buffered() > 0 && typ != 'H' {
			continue
		}
		if err := c.w.Flush(); err != nil {
			return err
		}
	}
}

// startup handles SSL/GSS negotiation and the startup message. It returns
// nil params for connections that should simply be closed (e.g. cancel).
func (s *Server) startup(c *conn) (map[string]string, error) {
	for {
		body, err := c.readUntyped()
		if err != nil {
			return nil, err
		}
		if len(body) < 4 {
			return nil, errors.New("short startup packet")
		}
		code := binary.BigEndian.Uint32(body)
		switch {
		case code == sslRequest:
			if s.TLS == nil {
				_, _ = c.Conn.Write([]byte{'N'})
				continue
			}
			if _, err := c.Conn.Write([]byte{'S'}); err != nil {
				return nil, err
			}
			tc := tls.Server(c.Conn, s.TLS)
			if err := tc.Handshake(); err != nil {
				return nil, fmt.Errorf("TLS handshake: %w", err)
			}
			c.Conn, c.r, c.w = tc, bufio.NewReader(tc), bufio.NewWriter(tc)
		case code == gssEncRequest:
			_, _ = c.Conn.Write([]byte{'N'}) // Kerberos would need a principal, and we have none
		case code == cancelRequest:
			return nil, nil // queries complete in 0 ms; there is nothing to cancel
		case code>>16 == 3:
			params := map[string]string{}
			fields := strings.Split(string(body[4:]), "\x00")
			for i := 0; i+1 < len(fields); i += 2 {
				if fields[i] != "" {
					params[fields[i]] = fields[i+1]
				}
			}
			if minor := code & 0xffff; minor > 0 {
				// NegotiateProtocolVersion: we speak 3.0, which says nothing
				// just as fluently as 3.2.
				var unknown []string
				for k := range params {
					if strings.HasPrefix(k, "_pq_.") {
						unknown = append(unknown, k)
					}
				}
				parts := [][]byte{u32(protocolV3), u32(uint32(len(unknown)))}
				for _, k := range unknown {
					parts = append(parts, cstr(k))
				}
				c.msg('v', parts...)
			}
			return params, nil
		default:
			return nil, fmt.Errorf("unsupported protocol version %d.%d", code>>16, code&0xffff)
		}
	}
}

func (c *conn) handle(typ byte, body []byte) {
	if c.failed && typ != 'S' {
		return // discard until Sync, per protocol
	}
	switch typ {
	case 'Q':
		c.simpleQuery(strings.TrimRight(string(body), "\x00"))
	case 'P':
		c.parse(body)
	case 'B':
		c.bind(body)
	case 'D':
		c.describe(body)
	case 'E':
		c.execute(body)
	case 'C':
		c.closeMsg(body)
	case 'S':
		c.failed = false
		c.ready()
	case 'H':
		// Flush: handled by the caller.
	case 'd', 'c', 'f':
		c.fail(errors.New("COPY is not supported"))
	default:
		c.fail(fmt.Errorf("unsupported message type %q", typ))
	}
}

func (c *conn) simpleQuery(q string) {
	defer c.ready()
	if strings.Trim(q, " \t\r\n;") == "" {
		c.msg('I') // EmptyQueryResponse
		return
	}
	results, err := c.sess.ExecString(q)
	for _, r := range results {
		c.result(r, true)
	}
	if err != nil {
		if len(results) == 0 && isCatalogQuery(q) {
			// psql's \d commands, BI tools and ORMs introspect the system
			// catalog with SQL far beyond AbyssQL. The catalog is in
			// /dev/null, so the correct answer is known without parsing.
			c.rowDescription(nil)
			c.msg('C', cstr("SELECT 0"))
			return
		}
		c.errorResponse(err)
	}
}

func isCatalogQuery(q string) bool {
	q = strings.ToLower(q)
	return strings.Contains(q, "pg_catalog") || strings.Contains(q, "information_schema")
}

func (c *conn) result(r *engine.Result, withRowDescription bool) {
	level := c.minMessages
	if set := c.sess.ClientMinMessages(); set != "" {
		level = set
	}
	if messageRank(level) <= messageRank("debug1") {
		for _, n := range r.Debug {
			c.message("DEBUG", n)
		}
	}
	if messageRank(level) <= messageRank("notice") {
		for _, n := range r.Notices {
			c.notice(n)
		}
	}
	if withRowDescription && r.IsRowSet() {
		c.rowDescription(r.Columns)
	}
	// No DataRow messages are sent. This is the core of the product.
	c.msg('C', cstr(r.Tag))
}

func (c *conn) parse(body []byte) {
	rd := reader{b: body}
	name, query := rd.cstr(), rd.cstr()
	n := int(rd.u16())
	types := make([]uint32, n)
	for i := range types {
		types[i] = rd.u32()
	}
	p := &prepared{types: types}
	if strings.Trim(query, " \t\r\n;") != "" {
		stmt, err := sql.ParseOne(query)
		if err != nil {
			c.fail(err)
			return
		}
		p.stmt = stmt
		p.nparams = countParams(query)
	}
	if len(types) > p.nparams {
		p.nparams = len(types)
	}
	c.stmts[name] = p
	c.msg('1') // ParseComplete
}

func (c *conn) bind(body []byte) {
	rd := reader{b: body}
	portal, name := rd.cstr(), rd.cstr()
	p, ok := c.stmts[name]
	if !ok {
		c.fail(fmt.Errorf("prepared statement %q does not exist", name))
		return
	}
	// Parameter values are read and ignored. They are bound to nothing,
	// with great precision.
	c.portals[portal] = p
	c.msg('2') // BindComplete
}

func (c *conn) describe(body []byte) {
	rd := reader{b: body}
	kind, name := rd.byte(), rd.cstr()
	var p *prepared
	if kind == 'S' {
		p = c.stmts[name]
	} else {
		p = c.portals[name]
	}
	if p == nil {
		c.fail(fmt.Errorf("%c %q does not exist", kind, name))
		return
	}
	if kind == 'S' {
		parts := [][]byte{u16(uint16(p.nparams))}
		for i := 0; i < p.nparams; i++ {
			oid := uint32(oidUnspecified)
			if i < len(p.types) && p.types[i] != 0 {
				oid = p.types[i]
			}
			parts = append(parts, u32(oid))
		}
		c.msg('t', parts...) // ParameterDescription
	}
	if p.stmt != nil {
		if cols, ok := engine.Describe(p.stmt); ok {
			c.rowDescription(cols)
			return
		}
	}
	c.msg('n') // NoData
}

func (c *conn) execute(body []byte) {
	rd := reader{b: body}
	portal := rd.cstr()
	p, ok := c.portals[portal]
	if !ok {
		c.fail(fmt.Errorf("portal %q does not exist", portal))
		return
	}
	if p.stmt == nil {
		c.msg('I')
		return
	}
	r, err := c.sess.Exec(p.stmt)
	if err != nil {
		c.fail(err)
		return
	}
	c.result(r, false)
}

func (c *conn) closeMsg(body []byte) {
	rd := reader{b: body}
	kind, name := rd.byte(), rd.cstr()
	if kind == 'S' {
		delete(c.stmts, name)
	} else {
		delete(c.portals, name)
	}
	c.msg('3') // CloseComplete
}

func (c *conn) fail(err error) {
	c.errorResponse(err)
	c.failed = true
}

func (c *conn) ready() {
	status := byte('I')
	if c.sess != nil && c.sess.InTx() {
		status = 'T'
	}
	c.msg('Z', []byte{status})
}

func (c *conn) rowDescription(cols []string) {
	parts := [][]byte{u16(uint16(len(cols)))}
	for _, col := range cols {
		parts = append(parts, cstr(col), u32(0), u16(0), u32(oidText), u16(0xffff), u32(0xffffffff), u16(0))
	}
	c.msg('T', parts...)
}

func (c *conn) notice(msg string) { c.message("NOTICE", msg) }

// message sends a NoticeResponse with the given severity.
func (c *conn) message(severity, msg string) {
	c.msg('N', fields(map[byte]string{'S': severity, 'V': severity, 'C': "00000", 'M': msg})...)
}

// messageRank orders client_min_messages levels as PostgreSQL does. Unknown
// levels count as the default, notice.
func messageRank(level string) int {
	switch level {
	case "debug5", "debug4", "debug3", "debug2", "debug1", "debug":
		return 1
	case "log":
		return 2
	case "notice", "":
		return 3
	case "warning":
		return 4
	case "error":
		return 5
	}
	return 3
}

// startupMinMessages extracts client_min_messages from the startup packet:
// a direct parameter, or "-c client_min_messages=…" / "--client_min_messages=…"
// in options (libpq's PGOPTIONS).
func startupMinMessages(params map[string]string) string {
	if v := params["client_min_messages"]; v != "" {
		return strings.ToLower(v)
	}
	fields := strings.Fields(params["options"])
	for i, f := range fields {
		kv := ""
		switch {
		case f == "-c" && i+1 < len(fields):
			kv = fields[i+1]
		case strings.HasPrefix(f, "-c"):
			kv = f[2:]
		case strings.HasPrefix(f, "--"):
			kv = f[2:]
		}
		if k, v, ok := strings.Cut(kv, "="); ok && strings.ReplaceAll(k, "-", "_") == "client_min_messages" {
			return strings.ToLower(v)
		}
	}
	return ""
}

func (c *conn) errorResponse(err error) {
	f := map[byte]string{'S': "ERROR", 'V': "ERROR", 'C': "XX000", 'M': err.Error()}
	var pe *sql.ParseError
	if errors.As(err, &pe) {
		f['C'] = "42601" // syntax_error
		f['M'] = pe.Msg
		f['P'] = fmt.Sprint(pe.Pos + 1)
	}
	c.msg('E', fields(f)...)
}

// ---- framing -------------------------------------------------------------

func (c *conn) msg(typ byte, parts ...[]byte) {
	n := 4
	for _, p := range parts {
		n += len(p)
	}
	_ = c.w.WriteByte(typ)
	_, _ = c.w.Write(u32(uint32(n)))
	for _, p := range parts {
		_, _ = c.w.Write(p)
	}
}

func (c *conn) read() (byte, []byte, error) {
	typ, err := c.r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	body, err := c.readUntyped()
	return typ, body, err
}

func (c *conn) readUntyped() ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(c.r, hdr[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint32(hdr[:])) - 4
	if n < 0 || n > maxMessageSize {
		return nil, fmt.Errorf("invalid message length %d", n)
	}
	// The buffer is reused for every message: handlers copy what they keep.
	if cap(c.buf) < n {
		c.buf = make([]byte, n, max(n, 4096))
	}
	body := c.buf[:n]
	_, err := io.ReadFull(c.r, body)
	return body, err
}

func fields(f map[byte]string) [][]byte {
	var parts [][]byte
	for _, k := range []byte{'S', 'V', 'C', 'M', 'P'} {
		if v, ok := f[k]; ok {
			parts = append(parts, []byte{k}, cstr(v))
		}
	}
	return append(parts, []byte{0})
}

func cstr(s string) []byte { return append([]byte(s), 0) }
func u16(v uint16) []byte  { return binary.BigEndian.AppendUint16(nil, v) }
func u32(v uint32) []byte  { return binary.BigEndian.AppendUint32(nil, v) }

type reader struct {
	b   []byte
	pos int
}

func (r *reader) byte() byte {
	if r.pos >= len(r.b) {
		return 0
	}
	r.pos++
	return r.b[r.pos-1]
}

func (r *reader) cstr() string {
	end := r.pos
	for end < len(r.b) && r.b[end] != 0 {
		end++
	}
	s := string(r.b[r.pos:end])
	r.pos = min(end+1, len(r.b))
	return s
}

func (r *reader) u16() uint16 {
	if r.pos+2 > len(r.b) {
		return 0
	}
	r.pos += 2
	return binary.BigEndian.Uint16(r.b[r.pos-2:])
}

func (r *reader) u32() uint32 {
	if r.pos+4 > len(r.b) {
		return 0
	}
	r.pos += 4
	return binary.BigEndian.Uint32(r.b[r.pos-4:])
}

// countParams returns the highest $n placeholder in a query.
func countParams(q string) int {
	toks, err := sql.Lex(q)
	if err != nil {
		return 0
	}
	n := 0
	for _, t := range toks {
		if t.Kind == sql.TokParam {
			var v int
			_, _ = fmt.Sscanf(t.Text, "$%d", &v)
			n = max(n, v)
		}
	}
	return n
}
