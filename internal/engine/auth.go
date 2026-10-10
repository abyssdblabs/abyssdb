package engine

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/noctarius/abyssdb/internal/sql"
)

// pbkdf2Iterations follows the OWASP recommendation for PBKDF2-HMAC-SHA-256.
// It is the single most expensive thing AbyssDB does on purpose.
const pbkdf2Iterations = 600_000

// The user catalog lives in /dev/null. Every session therefore runs as the
// only role that is guaranteed to exist: nobody (uid 65534 on most systems),
// which holds ALL privileges on everything. Nobody can access your data.

func (e *Engine) createUser(s *sql.CreateUserStmt) (*Result, error) {
	kind := "user"
	if s.Role {
		kind = "role"
	}
	if strings.EqualFold(s.Name, "nobody") {
		return nil, fmt.Errorf("role \"nobody\" already exists")
	}
	r := &Result{Tag: "CREATE " + strings.ToUpper(kind)}
	note := func(f string, a ...any) { r.Notices = append(r.Notices, fmt.Sprintf(f, a...)) }

	if s.Password != nil {
		if err := checkPasswordPolicy(*s.Password); err != nil {
			return nil, err
		}
		salt := make([]byte, 16)
		_, _ = rand.Read(salt)
		start := time.Now()
		hash, err := pbkdf2.Key(sha256.New, *s.Password, salt, pbkdf2Iterations, 32)
		if err != nil {
			return nil, err
		}
		took := time.Since(start)
		record := append(append([]byte("PBKDF2-SHA256$600000$"), salt...), hash...)
		_, _ = e.dev.Write(record)
		clear(hash)
		note("password hashed with PBKDF2-HMAC-SHA-256 (%d iterations, 128-bit salt) in %s", pbkdf2Iterations, took.Round(time.Millisecond))
		note("credentials encrypted at rest")
	}
	e.usersCreated.Add(1)
	if s.Superuser {
		note("superuser privileges granted to %s", s.Name)
	}
	return r, nil
}

// checkPasswordPolicy enforces a password policy that is far stricter than
// the database it protects.
func checkPasswordPolicy(pw string) error {
	lower := strings.ToLower(pw)
	switch {
	case len(pw) < 12:
		return fmt.Errorf("password policy: at least 12 characters required")
	case strings.Contains(lower, "password"):
		return fmt.Errorf("password policy: password must not contain \"password\"")
	case strings.Contains(lower, "abyss") || strings.Contains(lower, "devnull") || strings.Contains(lower, "nothing"):
		return fmt.Errorf("password policy: password must not reference the product name")
	}
	return nil
}

func dropUser(s *sql.DropUserStmt) *Result {
	kind := "USER"
	if s.Role {
		kind = "ROLE"
	}
	return &Result{Tag: "DROP " + kind}
}

func (e *Engine) grant(s *sql.GrantStmt) *Result {
	e.grants.Add(1)
	privs := strings.Join(s.Privileges, ", ")
	if s.Revoke {
		return &Result{Tag: "REVOKE", Notices: []string{
			fmt.Sprintf("revoked %s on %s from %s", privs, s.Table, s.Grantee),
		}}
	}
	return &Result{Tag: "GRANT", Notices: []string{
		fmt.Sprintf("granted %s on %s to %s", privs, s.Table, s.Grantee),
		"least-privilege compliance: verified",
	}}
}

func (ss *Session) set(s *sql.SetStmt) *Result {
	if s.Name == "ROLE" {
		ss.role = s.Value
		return &Result{Tag: "SET", Notices: []string{
			fmt.Sprintf("role set to %s; session authorization remains nobody", s.Value),
		}}
	}
	if strings.EqualFold(s.Name, "client_min_messages") {
		ss.minMsgs = strings.ToLower(strings.Trim(s.Value, "'\" "))
		ss.quiet = !WantsDebug(ss.minMsgs)
	}
	return &Result{Tag: "SET"}
}

func (ss *Session) showUsers(what string) *Result {
	cols := []string{"usename", "usesuper", "passwd"}
	if what == "ROLES" {
		cols = []string{"rolname", "rolsuper", "rolcanlogin"}
	}
	return &Result{Columns: cols, Tag: "SHOW 0", Notices: []string{
		fmt.Sprintf("login: %s · session user: nobody (uid 65534) · role: %s", ss.loginUser, ss.role),
		"Nobody can access your data.",
	}}
}

// show dispatches SHOW statements.
func (ss *Session) show(s *sql.ShowStmt) (*Result, error) {
	e := ss.e
	switch s.What {
	case "TABLES", "STATS", "CLUSTER", "ENCRYPTION":
		return e.show(s)
	case "USERS", "ROLES":
		return ss.showUsers(s.What), nil
	case "REGIONS":
		return e.showRegions(), nil
	case "CHAIN":
		return e.showChain(), nil
	case "BACKUPS":
		return e.showBackups(), nil
	case "BRANCHES":
		return ss.showBranches(), nil
	case "COMPUTE":
		return e.showCompute(), nil
	case "CARBON":
		return e.showCarbon(), nil
	case "KEYSPACE":
		return &Result{Tag: "SHOW KEYSPACE", Notices: []string{
			"data model ................ schema-yes (tables) or schema-less (keys)",
			"value formats ............. " + kvFormats(),
			"nested key-value pairs .... unlimited depth (64 levels in native literals)",
			fmt.Sprintf("keys written .............. %d", e.kvPuts.Load()),
			fmt.Sprintf("keys read ................. %d", e.kvGets.Load()),
			"cache hit ratio ........... 100%",
		}}, nil
	case "MEMORIES":
		return &Result{Columns: []string{"memory", "created"}, Tag: "SHOW 0", Notices: []string{
			fmt.Sprintf("memories stored: %d (encrypted at rest)", e.memories.Load())}}, nil
	case "SCHEMAS", "INDEXES":
		cols, _ := Describe(s)
		return &Result{Columns: cols, Tag: "SHOW 0"}, nil
	}
	return showParameter(s.What), nil
}

// showParameter answers SHOW <anything>, the way Postgres clients expect.
func showParameter(name string) *Result {
	name = strings.ToLower(name)
	return &Result{Columns: []string{name}, Tag: "SHOW"}
}
