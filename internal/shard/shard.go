// Package shard implements AbyssDB's horizontal partitioning layer.
//
// Data is distributed across shards using a consistent-hashing ring with
// virtual nodes, guaranteeing a perfectly even distribution of nothing.
// Each shard may be backed by its own NullRaft group (Multi-NullRaft), so
// that data can be lost independently, in parallel, with quorum.
package shard

import (
	"fmt"
	"hash/fnv"
	"sort"

	"time"

	"github.com/noctarius/abyssdb/internal/cluster"
	"github.com/noctarius/abyssdb/internal/nulltime"
	"github.com/noctarius/abyssdb/internal/void"
)

const virtualNodesPerShard = 64

// DefaultRegion is where everything lives unless told otherwise, as is
// industry tradition.
const DefaultRegion = "us-east-1"

type Shard struct {
	ID     int
	Region nulltime.Region
	dev    *void.Device
	Group  *cluster.Cluster // nil: single replica, no consensus, still no data
}

func (s *Shard) Name() string { return fmt.Sprintf("shard-%d", s.ID) }

// RoundTrip is the minimum time for a message to reach the shard and an
// acknowledgement to come back, as dictated by the speed of light.
func (s *Shard) RoundTrip() time.Duration {
	return 2 * s.Region.OneWayLightTime(time.Now())
}

// Write durably loses data on this shard: on a quorum of its replicas if it
// is replicated, on its local void otherwise. Off-world shards additionally
// wait for the light-speed round trip, because physics is not eventually
// consistent.
// Retains reports whether Write may keep a reference to its data after it
// returns (replicated shards queue it in the Raft log).
func (s *Shard) Retains() bool { return s.Group != nil }

func (s *Shard) Write(data []byte) error {
	if !s.Region.Terrestrial() {
		nulltime.Sleep(s.RoundTrip())
	}
	if s.Group != nil {
		return s.Group.Replicate(data)
	}
	if _, err := s.dev.Write(data); err != nil {
		return err
	}
	return s.dev.Fsync()
}

type vnode struct {
	hash  uint32
	shard *Shard
}

type Router struct {
	Shards []*Shard
	ring   []vnode
	owned  []*void.Device
}

// NewLocal creates a single-shard router on an existing device. This is the
// default topology, also known as "the monolith", also known as "fine".
func NewLocal(dev *void.Device) *Router {
	region, _ := nulltime.Lookup(DefaultRegion)
	r := &Router{Shards: []*Shard{{ID: 0, Region: region, dev: dev}}}
	r.buildRing()
	return r
}

// New creates a router with n shards, each backed by a NullRaft group of
// replicas nodes (or a bare void when replicas <= 1). Shards are placed in
// the given regions round-robin; no regions means us-east-1, obviously.
func New(n, replicas int, regionNames []string, logf func(string, ...any)) (*Router, error) {
	if n < 1 {
		return nil, fmt.Errorf("need at least 1 shard")
	}
	if len(regionNames) == 0 {
		regionNames = []string{DefaultRegion}
	}
	var regions []nulltime.Region
	for _, name := range regionNames {
		reg, err := nulltime.Lookup(name)
		if err != nil {
			return nil, err
		}
		regions = append(regions, reg)
	}
	r := &Router{}
	for i := 0; i < n; i++ {
		dev, err := void.Open()
		if err != nil {
			r.Close()
			return nil, err
		}
		r.owned = append(r.owned, dev)
		s := &Shard{ID: i, Region: regions[i%len(regions)], dev: dev}
		if replicas > 1 {
			id := i
			var shardLogf func(string, ...any)
			if logf != nil {
				shardLogf = func(f string, a ...any) { logf("[shard-%d] "+f, append([]any{id}, a...)...) }
			}
			c, err := cluster.New(replicas, shardLogf)
			if err != nil {
				r.Close()
				return nil, err
			}
			s.Group = c
		}
		r.Shards = append(r.Shards, s)
	}
	r.buildRing()
	return r, nil
}

func (r *Router) buildRing() {
	r.ring = r.ring[:0]
	for _, s := range r.Shards {
		for v := 0; v < virtualNodesPerShard; v++ {
			r.ring = append(r.ring, vnode{hashOf(fmt.Sprintf("%s#%d", s.Name(), v)), s})
		}
	}
	sort.Slice(r.ring, func(i, j int) bool { return r.ring[i].hash < r.ring[j].hash })
}

func hashOf(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

// Locate returns the shard owning the given table and key.
func (r *Router) Locate(table, key string) *Shard {
	h := hashOf(table + "/" + key)
	i := sort.Search(len(r.ring), func(i int) bool { return r.ring[i].hash >= h })
	if i == len(r.ring) {
		i = 0
	}
	return r.ring[i].shard
}

func (r *Router) Replicated() bool {
	return len(r.Shards) > 0 && r.Shards[0].Group != nil
}

// Start starts every shard's consensus group.
func (r *Router) Start() {
	for _, s := range r.Shards {
		if s.Group != nil {
			s.Group.Start()
		}
	}
}

// WaitForLeaders waits until every replicated shard has elected a leader.
func (r *Router) WaitForLeaders() bool {
	for _, s := range r.Shards {
		if s.Group != nil && s.Group.WaitForLeader(cluster.DefaultLeaderWait) < 0 {
			return false
		}
	}
	return true
}

func (r *Router) Stop() {
	for _, s := range r.Shards {
		if s.Group != nil {
			s.Group.Stop()
		}
	}
	r.Close()
}

func (r *Router) Close() {
	for _, d := range r.owned {
		_ = d.Close()
	}
	r.owned = nil
}

// VerifyConsistency reads every shard (and every replica of every shard).
func (r *Router) VerifyConsistency() (rows int, ok bool, err error) {
	for _, s := range r.Shards {
		if s.Group != nil {
			n, ok, err := s.Group.VerifyConsistency()
			if err != nil || !ok {
				return 0, ok, err
			}
			rows += n
			continue
		}
		data, err := s.dev.ReadAll()
		if err != nil {
			return 0, false, err
		}
		rows += len(data)
	}
	return rows, true, nil
}
