package cluster

import (
	"testing"
	"time"
)

func TestLeaderElectionAndFailover(t *testing.T) {
	c, err := New(5, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	defer c.Stop()

	l1 := c.WaitForLeader(3 * time.Second)
	if l1 < 0 {
		t.Fatal("no leader elected")
	}
	if err := c.Replicate([]byte("precious")); err != nil {
		t.Fatal(err)
	}

	c.Kill(l1)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if l := c.Leader(); l > 0 && l != l1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no failover")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := c.Replicate([]byte("also precious")); err != nil {
		t.Fatal(err)
	}
}

func TestMinorityCannotCommit(t *testing.T) {
	c, err := New(3, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	defer c.Stop()
	if c.WaitForLeader(3*time.Second) < 0 {
		t.Fatal("no leader elected")
	}
	c.Kill(1)
	c.Kill(2)
	c.Kill(3)
	if err := c.Replicate([]byte("x")); err == nil {
		t.Fatal("replicated with no live nodes; that would be too much even for us")
	}
}
