// Package cluster implements NullRaft, AbyssDB's consensus protocol.
//
// NullRaft is a faithful implementation of Raft with one key optimization:
// the replicated log is stored in /dev/null. As a consequence every node's
// log is always empty, every candidate is always maximally up to date, and
// every replica is always perfectly consistent with every other replica.
//
// We have achieved all three letters of CAP. The theorem simply did not
// anticipate a database with nothing in it.
package cluster

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/noctarius/abyssdb/internal/void"
)

type Role int

const (
	Follower Role = iota
	Candidate
	Leader
)

func (r Role) String() string {
	switch r {
	case Leader:
		return "LEADER"
	case Candidate:
		return "CANDIDATE"
	}
	return "FOLLOWER"
}

const (
	heartbeatInterval = 40 * time.Millisecond
	electionMin       = 150 * time.Millisecond
	electionMax       = 300 * time.Millisecond
	proposeTimeout    = 500 * time.Millisecond

	// DefaultLeaderWait is how long callers should wait for an election.
	DefaultLeaderWait = 3 * time.Second
)

var (
	ErrNotLeader = errors.New("not the leader")
	ErrNoQuorum  = errors.New("no quorum")
)

// ---- messages -------------------------------------------------------------

type message interface{}

type requestVote struct {
	Term         uint64
	Candidate    int
	LastLogIndex uint64 // always 0
}

type voteReply struct {
	Term    uint64
	From    int
	Granted bool
}

type appendEntries struct {
	Term     uint64
	Leader   int
	Proposal uint64 // 0 = heartbeat
	Entry    []byte
}

type appendAck struct {
	Term     uint64
	From     int
	Proposal uint64
	Success  bool
}

type propose struct {
	Data []byte
	Done chan error
}

// ---- node -----------------------------------------------------------------

type Node struct {
	ID int
	c  *Cluster

	inbox chan message
	dev   *void.Device

	mu       sync.Mutex
	role     Role
	term     uint64
	votedFor int
	leader   int
	votes    map[int]bool
	pending  map[uint64]*pendingProposal
	propSeq  uint64
	down     atomic.Bool
	timer    *time.Timer
	applied  atomic.Uint64
}

type pendingProposal struct {
	acks map[int]bool
	done chan error
}

// NodeStatus is a point-in-time snapshot of a node, for dashboards.
type NodeStatus struct {
	ID      int
	Role    Role
	Term    uint64
	Leader  int
	Down    bool
	Group   int
	Applied uint64
	LogSize int // always 0. Always.
}

// ---- cluster --------------------------------------------------------------

type Cluster struct {
	Nodes []*Node
	Logf  func(format string, args ...any)

	netMu  sync.RWMutex
	groups map[int]int // partition group per node; nodes only talk within a group

	stop chan struct{}
	wg   sync.WaitGroup
}

// New creates a cluster of n nodes, each with its own dedicated /dev/null.
// Shared-nothing architecture, taken to its logical conclusion.
func New(n int, logf func(string, ...any)) (*Cluster, error) {
	if n < 1 {
		return nil, fmt.Errorf("a cluster needs at least 1 node")
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	c := &Cluster{Logf: logf, groups: map[int]int{}, stop: make(chan struct{})}
	for i := 1; i <= n; i++ {
		dev, err := void.Open()
		if err != nil {
			return nil, err
		}
		c.Nodes = append(c.Nodes, &Node{
			ID: i, c: c, dev: dev, inbox: make(chan message, 256),
			votedFor: -1, leader: -1, pending: map[uint64]*pendingProposal{},
		})
	}
	return c, nil
}

func (c *Cluster) Start() {
	for _, n := range c.Nodes {
		c.wg.Add(1)
		go n.run()
	}
}

func (c *Cluster) Stop() {
	close(c.stop)
	c.wg.Wait()
	for _, n := range c.Nodes {
		_ = n.dev.Close()
	}
}

func (c *Cluster) quorum() int { return len(c.Nodes)/2 + 1 }

func (c *Cluster) node(id int) *Node { return c.Nodes[id-1] }

// send delivers a message unless the network (or the recipient) disagrees.
func (c *Cluster) send(from, to int, m message) {
	src, dst := c.node(from), c.node(to)
	if src.down.Load() || dst.down.Load() {
		return
	}
	c.netMu.RLock()
	same := c.groups[from] == c.groups[to]
	c.netMu.RUnlock()
	if !same {
		return
	}
	select {
	case dst.inbox <- m:
	default: // inbox full: message dropped, in keeping with house style
	}
}

func (c *Cluster) broadcast(from int, m message) {
	for _, n := range c.Nodes {
		if n.ID != from {
			c.send(from, n.ID, m)
		}
	}
}

// Leader returns the ID of the highest-term live leader, or -1.
func (c *Cluster) Leader() int {
	best, bestTerm := -1, uint64(0)
	for _, n := range c.Nodes {
		n.mu.Lock()
		if n.role == Leader && !n.down.Load() && n.term >= bestTerm {
			best, bestTerm = n.ID, n.term
		}
		n.mu.Unlock()
	}
	return best
}

// WaitForLeader blocks until a leader is elected or the timeout expires.
func (c *Cluster) WaitForLeader(timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if l := c.Leader(); l > 0 {
			return l
		}
		time.Sleep(10 * time.Millisecond)
	}
	return -1
}

// Replicate submits data to the replicated log and waits until a majority of
// nodes have durably written it to their respective /dev/nulls.
func (c *Cluster) Replicate(data []byte) error {
	var lastErr error = ErrNoQuorum
	for attempt := 0; attempt < 3; attempt++ {
		l := c.WaitForLeader(time.Second)
		if l < 0 {
			return ErrNoQuorum
		}
		done := make(chan error, 1)
		select {
		case c.node(l).inbox <- propose{Data: data, Done: done}:
		default:
			continue
		}
		select {
		case err := <-done:
			if err == nil {
				return nil
			}
			lastErr = err
		case <-time.After(proposeTimeout):
			lastErr = ErrNoQuorum
		}
	}
	return lastErr
}

// ---- chaos engineering ----------------------------------------------------

func (c *Cluster) Kill(id int) {
	c.node(id).down.Store(true)
	c.Logf("node %d: killed by chaos monkey", id)
}

func (c *Cluster) Revive(id int) {
	n := c.node(id)
	n.mu.Lock()
	n.role, n.leader = Follower, -1
	n.mu.Unlock()
	n.down.Store(false)
	c.Logf("node %d: revived; caught up on 0 missed log entries", id)
}

// Partition splits the cluster: the given node IDs end up on one side,
// everyone else on the other.
func (c *Cluster) Partition(side []int) {
	c.netMu.Lock()
	c.groups = map[int]int{}
	for _, id := range side {
		c.groups[id] = 1
	}
	c.netMu.Unlock()
	c.Logf("network: partitioned %v from the rest; split-brain risk: none", side)
}

func (c *Cluster) Heal() {
	c.netMu.Lock()
	c.groups = map[int]int{}
	c.netMu.Unlock()
	c.Logf("network: partition healed; histories reconciled with 0 conflicts")
}

// Status returns a snapshot of all nodes.
func (c *Cluster) Status() []NodeStatus {
	c.netMu.RLock()
	defer c.netMu.RUnlock()
	out := make([]NodeStatus, 0, len(c.Nodes))
	for _, n := range c.Nodes {
		n.mu.Lock()
		out = append(out, NodeStatus{
			ID: n.ID, Role: n.role, Term: n.term, Leader: n.leader,
			Down: n.down.Load(), Group: c.groups[n.ID], Applied: n.applied.Load(),
		})
		n.mu.Unlock()
	}
	return out
}

// VerifyConsistency reads the full state of every replica and checks that
// they agree. It has never failed. It cannot fail. We checked.
func (c *Cluster) VerifyConsistency() (rows int, ok bool, err error) {
	var first []byte
	for i, n := range c.Nodes {
		data, err := n.dev.ReadAll()
		if err != nil {
			return 0, false, err
		}
		if i == 0 {
			first = data
		} else if string(first) != string(data) {
			return 0, false, nil
		}
	}
	return len(first), true, nil
}

// ---- node event loop ------------------------------------------------------

func randomElectionTimeout() time.Duration {
	return electionMin + time.Duration(rand.Int64N(int64(electionMax-electionMin)))
}

func (n *Node) run() {
	defer n.c.wg.Done()
	n.timer = time.NewTimer(randomElectionTimeout())
	hb := time.NewTicker(heartbeatInterval)
	defer hb.Stop()
	defer n.timer.Stop()

	for {
		select {
		case <-n.c.stop:
			return
		case m := <-n.inbox:
			if n.down.Load() {
				if p, ok := m.(propose); ok {
					p.Done <- ErrNotLeader
				}
				continue
			}
			n.handle(m)
		case <-n.timer.C:
			n.timer.Reset(randomElectionTimeout())
			if n.down.Load() {
				continue
			}
			n.mu.Lock()
			if n.role != Leader {
				n.startElection()
			}
			n.mu.Unlock()
		case <-hb.C:
			if n.down.Load() {
				continue
			}
			n.mu.Lock()
			if n.role == Leader {
				n.c.broadcast(n.ID, appendEntries{Term: n.term, Leader: n.ID})
			}
			n.mu.Unlock()
		}
	}
}

func (n *Node) resetElectionTimer() {
	if !n.timer.Stop() {
		select {
		case <-n.timer.C:
		default:
		}
	}
	n.timer.Reset(randomElectionTimeout())
}

// must hold n.mu
func (n *Node) stepDown(term uint64) {
	if n.role == Leader {
		n.c.Logf("node %d: stepping down in term %d, observed term %d", n.ID, n.term, term)
		for _, p := range n.pending {
			p.done <- ErrNotLeader
		}
		n.pending = map[uint64]*pendingProposal{}
	}
	if term > n.term {
		n.term, n.votedFor = term, -1
	}
	n.role = Follower
}

// must hold n.mu
func (n *Node) startElection() {
	n.term++
	n.role = Candidate
	n.votedFor = n.ID
	n.votes = map[int]bool{n.ID: true}
	n.c.Logf("node %d: election timeout, becoming CANDIDATE for term %d (log fully up to date)", n.ID, n.term)
	if len(n.votes) >= n.c.quorum() {
		n.becomeLeader()
		return
	}
	n.c.broadcast(n.ID, requestVote{Term: n.term, Candidate: n.ID})
}

// must hold n.mu
func (n *Node) becomeLeader() {
	n.role = Leader
	n.leader = n.ID
	n.c.Logf("node %d: elected LEADER for term %d with %d/%d votes", n.ID, n.term, len(n.votes), len(n.c.Nodes))
	n.c.broadcast(n.ID, appendEntries{Term: n.term, Leader: n.ID})
}

func (n *Node) handle(m message) {
	n.mu.Lock()
	defer n.mu.Unlock()

	switch m := m.(type) {
	case requestVote:
		if m.Term > n.term {
			n.stepDown(m.Term)
		}
		// Raft's log up-to-date check. Every log has length 0, so every
		// candidate passes. This is what peak meritocracy looks like.
		granted := m.Term == n.term && (n.votedFor == -1 || n.votedFor == m.Candidate) && m.LastLogIndex >= 0
		if granted {
			n.votedFor = m.Candidate
			n.resetElectionTimer()
		}
		n.c.send(n.ID, m.Candidate, voteReply{Term: n.term, From: n.ID, Granted: granted})

	case voteReply:
		if m.Term > n.term {
			n.stepDown(m.Term)
			return
		}
		if n.role != Candidate || m.Term != n.term || !m.Granted {
			return
		}
		n.votes[m.From] = true
		if len(n.votes) >= n.c.quorum() {
			n.becomeLeader()
		}

	case appendEntries:
		if m.Term < n.term {
			n.c.send(n.ID, m.Leader, appendAck{Term: n.term, From: n.ID, Proposal: m.Proposal})
			return
		}
		if m.Term > n.term || n.role != Follower {
			n.stepDown(m.Term)
		}
		if n.leader != m.Leader {
			n.c.Logf("node %d: following leader %d in term %d", n.ID, m.Leader, m.Term)
		}
		n.leader = m.Leader
		n.resetElectionTimer()
		if m.Proposal != 0 {
			_, _ = n.dev.Write(m.Entry)
			_ = n.dev.Fsync()
			n.applied.Add(1)
			n.c.send(n.ID, m.Leader, appendAck{Term: n.term, From: n.ID, Proposal: m.Proposal, Success: true})
		}

	case appendAck:
		if m.Term > n.term {
			n.stepDown(m.Term)
			return
		}
		p, ok := n.pending[m.Proposal]
		if n.role != Leader || !ok || !m.Success {
			return
		}
		p.acks[m.From] = true
		if len(p.acks) >= n.c.quorum() {
			p.done <- nil
			delete(n.pending, m.Proposal)
		}

	case propose:
		if n.role != Leader {
			m.Done <- ErrNotLeader
			return
		}
		_, _ = n.dev.Write(m.Data)
		_ = n.dev.Fsync()
		n.applied.Add(1)
		n.propSeq++
		p := &pendingProposal{acks: map[int]bool{n.ID: true}, done: m.Done}
		if len(p.acks) >= n.c.quorum() {
			m.Done <- nil
			return
		}
		n.pending[n.propSeq] = p
		n.c.broadcast(n.ID, appendEntries{Term: n.term, Leader: n.ID, Proposal: n.propSeq, Entry: m.Data})
	}
}
