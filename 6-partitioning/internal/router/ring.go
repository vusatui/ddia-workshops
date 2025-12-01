package router

import (
	"hash/fnv"
	"sort"
)

type ringPoint struct {
	hash  uint64
	owner int // shard index
}

// Ring implements consistent hashing with virtual nodes.
// It maps a 64-bit key space onto an ordered ring of virtual points.
type Ring struct {
	points   []ringPoint
	replicas int
}

// NewRing creates a ring with the given replica factor per shard.
func NewRing(replicas int) *Ring {
	if replicas <= 0 {
		replicas = 100
	}
	return &Ring{replicas: replicas}
}

// Build rebuilds the ring for the provided shard indices (e.g., []int{0,1,2}).
func (r *Ring) Build(shards []int) {
	var pts []ringPoint
	for _, s := range shards {
		for v := 0; v < r.replicas; v++ {
			// Compute a unique seed for this (shard, replica) pair and map it onto the 64‑bit space.
			// 0x9e3779b97f4a7c15 is the 64‑bit golden ratio constant. Multiplying by it is a common trick
			// to decorrelate sequential integers and spread them uniformly across 64‑bit range.
			// We add +1 to the shard index so that shard 0 doesn't collapse to 0 before mixing.
			// Adding the replica index (v) ensures virtual nodes land at distinct positions per shard.
			seed := (uint64(s)+1)*0x9e3779b97f4a7c15 + uint64(v)
			h := hashUint64(seed)
			pts = append(pts, ringPoint{hash: h, owner: s})
		}
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].hash < pts[j].hash })
	r.points = pts
}

// Owner returns shard index for a given 64-bit key hash.
func (r *Ring) Owner(key uint64) int {
	if len(r.points) == 0 {
		return 0
	}
	i := sort.Search(len(r.points), func(i int) bool { return r.points[i].hash >= key })
	if i == len(r.points) {
		i = 0
	}
	return r.points[i].owner
}

// HashUser makes a 64-bit hash from userID.
func HashUser(u int64) uint64 {
	// Use FNV-1a on 8 bytes to get stable 64-bit hash
	var buf [8]byte
	x := uint64(u)
	for i := 0; i < 8; i++ {
		buf[i] = byte(x)
		x >>= 8
	}
	h := fnv.New64a()
	_, _ = h.Write(buf[:])
	return h.Sum64()
}

// hashUint64 mixes bits to spread shard+replica inputs across the 64‑bit space.
// This is a non-cryptographic "finalizer" similar to the fmix64 stage used by MurmurHash / SplitMix64.
// Properties:
// - Small input changes avalanche across output bits.
// - Fast, branchless, good for distributing ring points uniformly.
// Note: Do not use for security-sensitive hashing.
func hashUint64(x uint64) uint64 {
	x ^= x >> 33            // XOR-fold to mix high/low bits
	x *= 0xff51afd7ed558ccd // multiply by an odd 64-bit constant to spread patterns
	x ^= x >> 33            // repeat XOR-fold
	x *= 0xc4ceb9fe1a85ec53 // second constant improves diffusion
	x ^= x >> 33            // final XOR-fold for avalanche
	return x
}
