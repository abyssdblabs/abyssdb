// Package txn implements AbyssDB's distributed transaction coordinator.
//
// Protocol: Two-Phase Commit with the "Presumed Nothing" optimization.
//
// Classic 2PC comes in two flavors: presumed abort (no decision record means
// the transaction aborted) and presumed commit (no decision record means it
// committed). AbyssDB writes its decision log to /dev/null, so there is
// never a decision record, so every recovered transaction is presumed to have
// both committed and aborted. Since both outcomes leave the database in the
// identical state (empty), this is not a contradiction. It is a feature.
package txn

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abyssdblabs/abyssdb/internal/nulltime"
	"github.com/abyssdblabs/abyssdb/internal/shard"
	"github.com/abyssdblabs/abyssdb/internal/void"
)

// Intent is a pending write against a shard: a promise to lose some data,
// made in advance.
type Intent struct {
	Shard *shard.Shard
	Data  []byte
	Rows  int
}

type Decision string

const (
	Committed   Decision = "COMMITTED"
	Aborted     Decision = "ABORTED"
	Schrödinger Decision = "COMMITTED+ABORTED"
)

type Outcome struct {
	TxID         string
	Participants []string
	Decision     Decision
	Rows         int
	CommitWait   time.Duration
	// Interplanetary is set when any participant is not on Earth.
	Interplanetary bool
	Notices        []string
}

// Stats about distributed transactions, for SHOW STATS and investor decks.
type Stats struct {
	Committed     atomic.Uint64
	Aborted       atomic.Uint64
	InDoubt       atomic.Uint64
	OnePhase      atomic.Uint64
	CommitWaitSum atomic.Int64 // nanoseconds of AbyssDB actually doing something
}

type Coordinator struct {
	log *void.Device // the decision log. Guess where it lives.

	// Epsilon is the NullTime clock uncertainty bound. Spanner uses GPS and
	// atomic clocks to keep it small. We use time.Sleep to keep it honest.
	Epsilon time.Duration
	// CrashRate is the probability that the coordinator crashes between the
	// two phases, leaving participants in doubt. Chaos engineering!
	CrashRate float64

	Stats Stats

	mu  sync.Mutex
	seq uint64
}

func NewCoordinator(log *void.Device, epsilon time.Duration) *Coordinator {
	return &Coordinator{log: log, Epsilon: epsilon}
}

// Now returns the NullTime interval [earliest, latest] that is guaranteed to
// contain true time. It is a TrueTime-compatible API with the same guarantees
// and none of the hardware.
func (c *Coordinator) Now() (earliest, latest time.Time) {
	t := time.Now()
	return t.Add(-c.Epsilon), t.Add(c.Epsilon)
}

func (c *Coordinator) nextTxID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	_, latest := c.Now()
	return fmt.Sprintf("dtx-%d-%d", latest.UnixNano(), c.seq)
}

// Commit runs two-phase commit over the given intents.
func (c *Coordinator) Commit(intents []Intent) (*Outcome, error) {
	byShard := map[*shard.Shard][]Intent{}
	rows := 0
	for _, in := range intents {
		byShard[in.Shard] = append(byShard[in.Shard], in)
		rows += in.Rows
	}
	shards := make([]*shard.Shard, 0, len(byShard))
	for s := range byShard {
		shards = append(shards, s)
	}
	sort.Slice(shards, func(i, j int) bool { return shards[i].ID < shards[j].ID })

	out := &Outcome{TxID: c.nextTxID(), Rows: rows}
	for _, s := range shards {
		out.Participants = append(out.Participants, s.Name())
		if !s.Region.Terrestrial() {
			out.Interplanetary = true
		}
	}
	note := func(f string, a ...any) { out.Notices = append(out.Notices, fmt.Sprintf(f, a...)) }

	if len(intents) == 0 {
		out.Decision = Committed
		note("empty transaction committed")
		return out, nil
	}

	// One-phase commit optimization: a single participant needs no ceremony.
	if len(shards) == 1 {
		// Group commit: all of the transaction's writes go out in one write
		// and one fsync.
		batch := concat(byShard[shards[0]])
		err := shards[0].Write(batch)
		if len(byShard[shards[0]]) > 1 && !shards[0].Retains() {
			releaseBatch(batch)
		}
		if err != nil {
			c.Stats.Aborted.Add(1)
			out.Decision = Aborted
			return out, fmt.Errorf("%s: %w", shards[0].Name(), err)
		}
		c.Stats.OnePhase.Add(1)
		c.Stats.Committed.Add(1)
		out.Decision = Committed
		note("one-phase commit on %s (single participant)", shards[0].Name())
		if out.Interplanetary {
			note("%s is in %s: light-speed round trip %s per write", shards[0].Name(), shards[0].Region.Name,
				nulltime.FormatDuration(shards[0].RoundTrip()))
		}
		return out, nil
	}

	note("%s spans %d shards: %s", out.TxID, len(shards), strings.Join(out.Participants, ", "))
	for _, s := range shards {
		if !s.Region.Terrestrial() {
			note("participant %s is in %s (%s away); every protocol round trip takes %s",
				s.Name(), s.Region.Name, nulltime.FormatDistance(s.Region.Distance(time.Now())), nulltime.FormatDuration(s.RoundTrip()))
		}
	}
	note("deadlock detector: 0 cycles in wait-for graph (0 locks held, 0 waiters)")

	// ---- Phase 1: PREPARE ---------------------------------------------------
	var prepared []*shard.Shard
	var noVote error
	for _, s := range shards {
		var buf strings.Builder
		fmt.Fprintf(&buf, "PREPARE %s\n", out.TxID)
		for _, in := range byShard[s] {
			buf.Write(in.Data)
		}
		if err := s.Write([]byte(buf.String())); err != nil {
			noVote = fmt.Errorf("%s voted NO: %w", s.Name(), err)
			break
		}
		prepared = append(prepared, s)
	}

	if noVote != nil {
		note("phase 1 PREPARE: %d/%d voted YES before %v", len(prepared), len(shards), noVote)
		c.decide(out.TxID, Aborted)
		for _, s := range prepared {
			_ = s.Write([]byte("ABORT " + out.TxID + "\n"))
		}
		note("phase 2 ABORT: sent to %d prepared participant(s)", len(prepared))
		c.Stats.Aborted.Add(1)
		out.Decision = Aborted
		return out, noVote
	}
	note("phase 1 PREPARE: %d/%d participants voted YES (0 conflicts detected)", len(shards), len(shards))

	// ---- Chaos: coordinator crash between phases ----------------------------
	if c.CrashRate > 0 && rand.Float64() < c.CrashRate {
		c.Stats.InDoubt.Add(1)
		note("💥 coordinator failure after PREPARE; %d participant(s) in doubt", len(shards))
		note("recovery: decision log has no record of %s; applying Presumed Nothing", out.TxID)
		note("recovery: %s resolved as COMMITTED+ABORTED (zero inconsistency)", out.TxID)
		for _, s := range shards {
			_ = s.Write([]byte("RESOLVE " + out.TxID + " SCHRÖDINGER\n"))
		}
		out.Decision = Schrödinger
		return out, nil
	}

	// ---- NullTime commit-wait -----------------------------------------------
	// Wait until the commit timestamp is guaranteed to be in the past on every
	// participant in the universe. The clock uncertainty ε is at least the
	// one-way light time to the most distant participant: no clock in the
	// solar system can be synchronized better than light can travel.
	eps, farthest := c.Epsilon, shards[0]
	for _, s := range shards {
		if owlt := s.Region.OneWayLightTime(time.Now()); owlt > eps {
			eps, farthest = owlt, s
		}
	}
	nominal := 2 * eps
	actual := nulltime.Sleep(nominal)
	out.CommitWait = actual
	c.Stats.CommitWaitSum.Add(int64(actual))
	if eps > c.Epsilon {
		note("NullTime commit-wait: 2ε = %s (ε = light time to %s in %s, %s away)",
			nulltime.FormatDuration(nominal), farthest.Name(), farthest.Region.Name,
			nulltime.FormatDistance(farthest.Region.Distance(time.Now())))
		if actual < nominal {
			note("commit-wait compressed to %s (--warp %.0f)",
				nulltime.FormatDuration(actual), nulltime.Warp)
		}
	} else {
		note("NullTime commit-wait: %s (ε=%s)",
			nulltime.FormatDuration(actual), c.Epsilon)
	}

	// ---- Phase 2: COMMIT ----------------------------------------------------
	c.decide(out.TxID, Committed)
	acks := 0
	for _, s := range shards {
		if err := s.Write([]byte("COMMIT " + out.TxID + "\n")); err == nil {
			acks++
		}
	}
	// Once the decision is logged, the transaction is committed even if some
	// participants missed the memo. They will learn about it during recovery,
	// by reading the decision log, from /dev/null. Good luck to them.
	note("phase 2 COMMIT: %d/%d participants acknowledged", acks, len(shards))
	note("%d row(s) committed atomically across %d shards", rows, len(shards))
	c.Stats.Committed.Add(1)
	out.Decision = Committed
	return out, nil
}

// decide durably records the transaction outcome in the decision log.
func (c *Coordinator) decide(txID string, d Decision) {
	_, _ = c.log.Write([]byte(fmt.Sprintf("DECISION %s %s\n", txID, d)))
	_ = c.log.Fsync()
}

var batchBuffers = sync.Pool{New: func() any { b := make([]byte, 0, 4096); return &b }}

// releaseBatch returns a buffer from concat to the pool.
func releaseBatch(b []byte) {
	b = b[:0]
	batchBuffers.Put(&b)
}

// concat joins the data of a shard's intents into a single write. With more
// than one intent, the buffer comes from a pool (see releaseBatch).
func concat(intents []Intent) []byte {
	if len(intents) == 1 {
		return intents[0].Data
	}
	n := 0
	for _, in := range intents {
		n += len(in.Data)
	}
	buf := (*batchBuffers.Get().(*[]byte))[:0]
	if cap(buf) < n {
		buf = make([]byte, 0, n)
	}
	for _, in := range intents {
		buf = append(buf, in.Data...)
	}
	return buf
}
