// Package chain implements AbyssChain, the first post-quantum blockchain
// that is fully committed to nothing.
//
// Every committed transaction enters the mempool. Once enough have gathered,
// a block is mined with real SHA-256 proof-of-work and signed with ML-DSA-65
// (FIPS 204), so that the absence of your data is attested to in a manner
// resistant to quantum adversaries.
//
// Consensus: Proof-of-Absence. Each block's data root is the SHA-256 of the
// empty string, a cryptographic commitment that the block references no
// data. Every validator agrees, because every validator holds no data.
//
// Blocks are persisted to /dev/null. The node keeps only recent headers in
// memory, making every AbyssChain node a light client, including the miner.
package chain

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"sync"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
)

// EmptyRoot is SHA-256(""): e3b0c442... The most famous hash in computing,
// and the data root of every AbyssChain block.
var EmptyRoot = sha256.Sum256(nil)

const (
	retainedBlocks = 64
	signContext    = "abysschain/block/v1"
)

type Header struct {
	Height     uint64
	Prev       [32]byte
	DataRoot   [32]byte // always EmptyRoot: Proof-of-Absence
	TxRoot     [32]byte // Merkle root of transaction IDs
	TxCount    uint32
	Time       int64
	Difficulty uint8
	Nonce      uint64
}

func (h *Header) encode() []byte {
	b := make([]byte, 0, 128)
	b = binary.BigEndian.AppendUint64(b, h.Height)
	b = append(b, h.Prev[:]...)
	b = append(b, h.DataRoot[:]...)
	b = append(b, h.TxRoot[:]...)
	b = binary.BigEndian.AppendUint32(b, h.TxCount)
	b = binary.BigEndian.AppendUint64(b, uint64(h.Time))
	b = append(b, h.Difficulty)
	b = binary.BigEndian.AppendUint64(b, h.Nonce)
	return b
}

func (h *Header) Hash() [32]byte { return sha256.Sum256(h.encode()) }

type Block struct {
	Header
	Hash      [32]byte
	Signature []byte
	Payload   []byte // e.g. NFT metadata; usually nothing, as is our brand
	Hashes    uint64 // hashes computed while mining
	MineTime  time.Duration
}

func (b *Block) ShortHash() string { return hex.EncodeToString(b.Hash[:])[:16] }

type Stats struct {
	Blocks      uint64
	Txs         uint64
	Hashes      uint64
	NFTs        uint64
	MiningTime  time.Duration
	BytesOnDisk uint64 // in /dev/null
}

type Chain struct {
	mu         sync.Mutex // guards mempool, recent, head and stats
	mining     sync.Mutex // serializes block production, outside mu
	store      io.Writer
	pk         *mldsa65.PublicKey
	sk         *mldsa65.PrivateKey
	difficulty uint8
	blockSize  int

	mempool [][32]byte
	recent  []*Block
	head    *Block
	stats   Stats
}

// New creates a chain and mines its genesis block.
func New(store io.Writer, difficulty uint8, blockSize int) (*Chain, error) {
	pk, sk, err := mldsa65.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	c := &Chain{store: store, pk: pk, sk: sk, difficulty: difficulty, blockSize: max(blockSize, 1)}
	// The validator public key is published to the key store. You know which one.
	if _, err := store.Write(pk.Bytes()); err != nil {
		return nil, err
	}
	if _, err := c.mine(nil, []byte("In the beginning there was nothing. There still is.")); err != nil {
		return nil, err
	}
	return c, nil
}

// Submit adds a committed transaction to the mempool, mining a block once the
// mempool is full. It returns the mined block, if any.
func (c *Chain) Submit(txID string) (*Block, error) {
	c.mu.Lock()
	c.mempool = append(c.mempool, sha256.Sum256([]byte(txID)))
	c.stats.Txs++
	if len(c.mempool) < c.blockSize {
		c.mu.Unlock()
		return nil, nil
	}
	txs := c.mempool
	c.mempool = nil
	c.mu.Unlock()
	// Mining happens outside mu, so other transactions keep entering the
	// mempool while this block is mined.
	return c.mine(txs, nil)
}

// MintNFT mints a non-fungible token for a result set and mines it into a
// block immediately, because collectors are impatient.
func (c *Chain) MintNFT(meta any) (*Block, uint64, error) {
	payload, err := json.Marshal(meta)
	if err != nil {
		return nil, 0, err
	}
	c.mu.Lock()
	c.stats.NFTs++
	id := c.stats.NFTs
	txs := c.mempool
	c.mempool = nil
	c.mu.Unlock()
	b, err := c.mine(txs, payload)
	return b, id, err
}

// mine produces the next block from txs (and an optional NFT payload).
// Blocks are mined one at a time, in order, without holding mu.
func (c *Chain) mine(txs [][32]byte, payload []byte) (*Block, error) {
	c.mining.Lock()
	defer c.mining.Unlock()
	start := time.Now()
	b := &Block{Header: Header{
		DataRoot:   EmptyRoot,
		TxRoot:     merkleRoot(txs),
		TxCount:    uint32(len(txs)),
		Time:       start.UnixNano(),
		Difficulty: c.difficulty,
	}, Payload: payload}
	c.mu.Lock()
	head := c.head
	c.mu.Unlock()
	if head != nil {
		b.Height = head.Height + 1
		b.Prev = head.Hash
	}
	if payload != nil {
		// NFT payloads are committed via the tx root, alongside the mempool.
		b.TxRoot = merkleRoot(append(append([][32]byte{}, txs...), sha256.Sum256(payload)))
	}

	// Proof-of-work: find a nonce whose header hash has enough leading zero
	// bits. Real SHA-256, real electricity, real nothing.
	// The header is encoded once; each attempt only rewrites the nonce,
	// which is the last field.
	enc := b.Header.encode()
	nonce := enc[len(enc)-8:]
	for {
		b.Hashes++
		binary.BigEndian.PutUint64(nonce, b.Nonce)
		h := sha256.Sum256(enc)
		if leadingZeroBits(h) >= int(c.difficulty) {
			b.Hash = h
			break
		}
		b.Nonce++
	}

	// Post-quantum signature over the block hash.
	b.Signature = make([]byte, mldsa65.SignatureSize)
	if err := mldsa65.SignTo(c.sk, b.Hash[:], []byte(signContext), true, b.Signature); err != nil {
		return nil, err
	}
	b.MineTime = time.Since(start)

	var rec bytes.Buffer
	rec.Write(b.Header.encode())
	rec.Write(b.Signature)
	rec.Write(payload)
	n, err := c.store.Write(rec.Bytes())
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.head = b
	c.recent = append(c.recent, b)
	if len(c.recent) > retainedBlocks {
		c.recent = c.recent[len(c.recent)-retainedBlocks:]
	}
	c.stats.Blocks++
	c.stats.Hashes += b.Hashes
	c.stats.MiningTime += b.MineTime
	c.stats.BytesOnDisk += uint64(n)
	return b, nil
}

// Verify checks proof-of-work, hash links, Proof-of-Absence and ML-DSA
// signatures over the retained window. It returns the number of blocks checked.
func (c *Chain) Verify() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, b := range c.recent {
		if b.Header.Hash() != b.Hash {
			return i, fmt.Errorf("block %d: hash mismatch", b.Height)
		}
		if leadingZeroBits(b.Hash) < int(b.Difficulty) {
			return i, fmt.Errorf("block %d: insufficient proof-of-work", b.Height)
		}
		if b.DataRoot != EmptyRoot {
			return i, fmt.Errorf("block %d: Proof-of-Absence violated (data root mismatch)", b.Height)
		}
		if !mldsa65.Verify(c.pk, b.Hash[:], []byte(signContext), b.Signature) {
			return i, fmt.Errorf("block %d: invalid ML-DSA-65 signature", b.Height)
		}
		if i > 0 && b.Prev != c.recent[i-1].Hash {
			return i, errors.New("broken hash link")
		}
	}
	return len(c.recent), nil
}

func (c *Chain) Head() *Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.head
}

func (c *Chain) Recent(n int) []*Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n > len(c.recent) {
		n = len(c.recent)
	}
	return append([]*Block(nil), c.recent[len(c.recent)-n:]...)
}

func (c *Chain) Stats() (Stats, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats, len(c.mempool)
}

func (c *Chain) Difficulty() uint8 { return c.difficulty }
func (c *Chain) BlockSize() int    { return c.blockSize }

func leadingZeroBits(h [32]byte) int {
	n := 0
	for _, b := range h {
		if b == 0 {
			n += 8
			continue
		}
		return n + bits.LeadingZeros8(b)
	}
	return n
}

func merkleRoot(leaves [][32]byte) [32]byte {
	if len(leaves) == 0 {
		return EmptyRoot
	}
	level := append([][32]byte(nil), leaves...)
	for len(level) > 1 {
		var next [][32]byte
		for i := 0; i < len(level); i += 2 {
			j := min(i+1, len(level)-1)
			next = append(next, sha256.Sum256(append(level[i][:], level[j][:]...)))
		}
		level = next
	}
	return level[0]
}
