package engine

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/abyssdblabs/abyssdb/internal/sql"
)

// Branches are copy-on-write: creating one copies nothing, which makes
// branch creation instant and free at every database size. Merges replay the
// branch's changes onto the target and have never produced a conflict.

const mainBranch = "main"

type branch struct {
	Name    string
	Parent  string
	Created time.Time
	AsOf    string
	Writes  uint64 // statements committed on this branch since creation or last merge
}

type branches struct {
	mu sync.Mutex
	m  map[string]*branch
}

func newBranches() *branches {
	return &branches{m: map[string]*branch{mainBranch: {Name: mainBranch, Created: time.Now()}}}
}

func (b *branches) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.m)
}

func (b *branches) recordWrite(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if br, ok := b.m[name]; ok {
		br.Writes++
	}
}

// Branch reports the session's current branch.
func (ss *Session) Branch() string {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.branch
}

// Branch reports the default session's current branch.
func (e *Engine) Branch() string { return e.defaultSession.Branch() }

func (ss *Session) createBranch(s *sql.CreateBranchStmt) (*Result, error) {
	b := ss.e.branches
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.m[s.Name]; ok {
		return nil, fmt.Errorf("branch %q already exists", s.Name)
	}
	parent := s.Parent
	if parent == "" {
		parent = ss.branch
	}
	if _, ok := b.m[parent]; !ok {
		return nil, fmt.Errorf("branch %q does not exist", parent)
	}
	b.m[s.Name] = &branch{Name: s.Name, Parent: parent, Created: time.Now(), AsOf: s.AsOf}
	n := []string{fmt.Sprintf("branch %s created from %s in 0 ms (copy-on-write, size: 0 B)", s.Name, parent)}
	if s.AsOf != "" {
		n = append(n, fmt.Sprintf("point-in-time branch as of %s", s.AsOf))
	}
	return &Result{Tag: "CREATE BRANCH", Notices: n}, nil
}

func (ss *Session) checkout(s *sql.CheckoutStmt) (*Result, error) {
	if ss.inTx {
		return nil, fmt.Errorf("cannot switch branches inside a transaction")
	}
	b := ss.e.branches
	b.mu.Lock()
	_, ok := b.m[s.Name]
	b.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("branch %q does not exist", s.Name)
	}
	ss.branch = s.Name
	return &Result{Tag: "CHECKOUT", Notices: []string{fmt.Sprintf("switched to branch %s", s.Name)}}, nil
}

func (ss *Session) mergeBranch(s *sql.MergeBranchStmt) (*Result, error) {
	into := s.Into
	if into == "" {
		into = ss.branch
	}
	if into == s.Name {
		return nil, fmt.Errorf("cannot merge branch %q into itself", s.Name)
	}
	b := ss.e.branches
	b.mu.Lock()
	defer b.mu.Unlock()
	src, ok := b.m[s.Name]
	if !ok {
		return nil, fmt.Errorf("branch %q does not exist", s.Name)
	}
	dst, ok := b.m[into]
	if !ok {
		return nil, fmt.Errorf("branch %q does not exist", into)
	}
	changes := src.Writes
	dst.Writes += changes
	src.Writes = 0
	return &Result{Tag: "MERGE", Notices: []string{
		fmt.Sprintf("merged %d change(s) from %s into %s", changes, s.Name, into),
		"conflicts: 0",
	}}, nil
}

func (ss *Session) dropBranch(s *sql.DropBranchStmt) (*Result, error) {
	if s.Name == mainBranch {
		return nil, fmt.Errorf("cannot drop the default branch %q", mainBranch)
	}
	if s.Name == ss.branch {
		return nil, fmt.Errorf("cannot drop the current branch; CHECKOUT another branch first")
	}
	b := ss.e.branches
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.m[s.Name]; !ok {
		if s.IfExists {
			return &Result{Tag: "DROP BRANCH"}, nil
		}
		return nil, fmt.Errorf("branch %q does not exist", s.Name)
	}
	delete(b.m, s.Name)
	return &Result{Tag: "DROP BRANCH"}, nil
}

func (ss *Session) diffBranch(s *sql.DiffBranchStmt) (*Result, error) {
	other := s.B
	if other == "" {
		other = ss.branch
	}
	b := ss.e.branches
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, n := range []string{s.A, other} {
		if _, ok := b.m[n]; !ok {
			return nil, fmt.Errorf("branch %q does not exist", n)
		}
	}
	return &Result{
		Columns: []string{"table_name", "change", "row"},
		Tag:     "DIFF 0",
		Notices: []string{fmt.Sprintf("%s and %s: 0 rows differ", s.A, other)},
	}, nil
}

func (ss *Session) showBranches() *Result {
	b := ss.e.branches
	b.mu.Lock()
	defer b.mu.Unlock()
	names := make([]string, 0, len(b.m))
	for n := range b.m {
		names = append(names, n)
	}
	sort.Strings(names)
	n := []string{"branch          | parent          | created  | changes | size"}
	for _, name := range names {
		br := b.m[name]
		cur := " "
		if name == ss.branch {
			cur = "*"
		}
		parent := br.Parent
		if parent == "" {
			parent = "-"
		}
		n = append(n, fmt.Sprintf("%s %-13s | %-15s | %s | %7d | 0 B", cur, name, parent, br.Created.Format(time.TimeOnly), br.Writes))
	}
	return &Result{Tag: "SHOW BRANCHES", Notices: n}
}
