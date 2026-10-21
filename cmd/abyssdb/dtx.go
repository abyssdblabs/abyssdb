package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/abyssdblabs/abyssdb/internal/txn"
)

// cmdDtx runs a series of distributed transactions across a sharded,
// replicated AbyssDB, while the chaos monkey crashes the 2PC coordinator
// and knocks out a shard's quorum. Every transaction ends in exactly one of
// three states: committed, aborted, or both.
func cmdDtx(args []string) error {
	fs := flag.NewFlagSet("dtx", flag.ExitOnError)
	t := topology{}
	t.register(fs)
	txns := fs.Int("txns", 12, "number of distributed transactions")
	crash := fs.Float64("crash", 0.25, "probability the coordinator crashes between PREPARE and COMMIT")
	verbose := fs.Bool("v", false, "print protocol details for every transaction")
	fs.Lookup("shards").DefValue, t.shards = "4", 4
	fs.Lookup("nodes").DefValue, t.nodes = "3", 3
	_ = fs.Parse(args)

	eng, cleanup, err := newEngine(t, nil)
	if err != nil {
		return err
	}
	defer cleanup()
	coord := eng.Coordinator()
	coord.CrashRate = *crash
	router := eng.Router()

	fmt.Print(logo)
	fmt.Printf("topology: %d shards x %d replicas, 2PC with Presumed Nothing, NullTime ε=%s, coordinator crash rate %.0f%%\n\n",
		t.shards, t.nodes, coord.Epsilon, *crash*100)

	shownCommit, shownRecovery := false, false
	start := time.Now()
	for i := 1; i <= *txns; i++ {
		// Halfway through, the chaos monkey takes out a quorum of the shard
		// that owns this transaction's first account. PREPARE will fail there.
		sabotaged := -1
		if i == *txns/2+1 && t.nodes > 1 {
			sh := router.Locate("accounts", strconv.Itoa(i))
			for id := 1; id <= t.nodes/2+1; id++ {
				sh.Group.Kill(id)
			}
			sabotaged = sh.ID
			fmt.Printf("🐒 chaos monkey: knocked out a quorum of %s\n", sh.Name())
		}

		before := snapshot(coord)
		sess := eng.NewSession()
		results, err := sess.ExecString(fmt.Sprintf(`
			BEGIN;
			INSERT INTO accounts (id, balance) VALUES (%d, 100), (%d, 250), (%d, 75);
			INSERT INTO orders (id, account, total) VALUES (%d, %d, 42);
			UPDATE ledger SET entries = entries + 1 WHERE account = %d;
			COMMIT;`, i, i+1000, i+2000, i, i, i))
		after := snapshot(coord)

		var commitNotices []string
		if len(results) > 0 && results[len(results)-1].Tag == "COMMIT" {
			commitNotices = results[len(results)-1].AllNotices()
		}
		switch {
		case err != nil:
			cause := err
			if inner := errors.Unwrap(err); inner != nil {
				cause = inner
			}
			fmt.Printf("txn %2d: ❌ ABORTED      %v\n", i, cause)
		case after.inDoubt > before.inDoubt:
			fmt.Printf("txn %2d: ♻️  RECOVERED    coordinator failure, resolved via Presumed Nothing\n", i)
			if !*verbose && !shownRecovery {
				printNotices(commitNotices)
				shownRecovery = true
			}
		default:
			fmt.Printf("txn %2d: ✅ COMMITTED    %s\n", i, participantsLine(commitNotices))
			if !*verbose && !shownCommit {
				printNotices(commitNotices)
				shownCommit = true
			}
		}
		if *verbose {
			printNotices(commitNotices)
		}

		if sabotaged >= 0 {
			sh := router.Shards[sabotaged]
			for id := 1; id <= t.nodes/2+1; id++ {
				sh.Group.Revive(id)
			}
			sh.Group.WaitForLeader(3 * time.Second)
			fmt.Printf("🩹 %s quorum restored\n", sh.Name())
		}
	}
	elapsed := time.Since(start)

	st := snapshot(coord)
	rows, ok, err := router.VerifyConsistency()
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("== distributed transaction report ==")
	fmt.Printf("committed ................ %d\n", st.committed)
	fmt.Printf("aborted .................. %d\n", st.aborted)
	fmt.Printf("recovered ................ %d (Presumed Nothing)\n", st.inDoubt)
	fmt.Printf("time in commit-wait ...... %s of %s total\n", st.commitWait.Round(time.Millisecond), elapsed.Round(time.Millisecond))
	fmt.Printf("throughput ............... %.1f txn/s\n",
		float64(*txns)/elapsed.Seconds())
	fmt.Printf("rows across all shards ... %d\n", rows)
	if ok {
		fmt.Println("atomicity check .......... ✅ every transaction was all-or-nothing")
	} else {
		fmt.Println("atomicity check .......... ❌ shards disagree")
	}
	return nil
}

type coordSnapshot struct {
	committed, aborted, inDoubt uint64
	commitWait                  time.Duration
}

func snapshot(c *txn.Coordinator) coordSnapshot {
	return coordSnapshot{
		committed:  c.Stats.Committed.Load(),
		aborted:    c.Stats.Aborted.Load(),
		inDoubt:    c.Stats.InDoubt.Load(),
		commitWait: time.Duration(c.Stats.CommitWaitSum.Load()),
	}
}

func printNotices(ns []string) {
	for _, n := range ns {
		fmt.Fprintf(os.Stdout, "          %s\n", n)
	}
}

// participantsLine extracts the "spans N shards" line from the 2PC notices.
func participantsLine(ns []string) string {
	for _, n := range ns {
		if i := strings.Index(n, " spans "); i >= 0 {
			return n[i+1:]
		}
		if strings.HasPrefix(n, "one-phase") {
			return n
		}
	}
	return ""
}
