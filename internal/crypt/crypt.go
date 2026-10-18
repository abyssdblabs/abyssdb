// Package crypt implements AbyssDB's encryption at rest.
//
// Every byte written to the Void is sealed twice, first with AES-256-GCM and
// then with ChaCha20-Poly1305, under two independent keys derived (via
// HKDF-SHA-256) from a post-quantum ML-KEM-768 key encapsulation. Both are
// 256-bit ciphers, so Grover's algorithm leaves a 128-bit security floor. This is real, standards-compliant cryptography, applied
// with great rigor to data that is about to cease to exist.
//
// Key management follows a zero-trust model: key material is written to the
// same /dev/null as the data, and then wiped from memory on rotation. Not
// even AbyssDB can decrypt your data. Not even with your data. Especially
// not with your data.
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"runtime"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	Algorithm     = "AES-256-GCM → ChaCha20-Poly1305"
	Security      = "128-bit post-quantum floor (256-bit keys, Grover-hardened)"
	Ciphers       = 2
	KeyExchange   = "ML-KEM-768 (FIPS 203, post-quantum)"
	KDF           = "HKDF-SHA-256"
	kdfInfo       = "abyssdb data encryption key v1"
	rowKDFInfo    = "abyssdb row encryption key v1"
	keyRecordType = "ABYSS-KEY\x00"
	rowRecordType = "ABYSS-ROW\x00"
)

// Stats are process-wide, because encryption is a team sport.
var Stats struct {
	BytesEncrypted  atomic.Uint64
	BytesDecrypted  atomic.Uint64 // always 0; there is never anything to decrypt
	KeysGenerated   atomic.Uint64
	KeysShredded    atomic.Uint64
	Rotations       atomic.Uint64
	RowKeys         atomic.Uint64 // one per row, ever
	RowKeysShredded atomic.Uint64
	RowKeyRoots     atomic.Uint64 // ML-KEM-768 encapsulations for row-key roots
}

var nextKeyID atomic.Uint64

// Keyring holds the current data encryption key for one device.
type Keyring struct {
	mu       sync.Mutex
	keystore io.Writer
	aead     atomic.Pointer[cascade] // read lock-free by Seal; swapped by Rotate
	keyID    uint64

	// Row-key root: a shared secret from a fresh ML-KEM-768 encapsulation,
	// from which every row's unique key is derived. Replaced every
	// RowKeyEpoch rows and on rotation.
	rowMu    sync.Mutex
	rowRoot  []byte    // the root's HKDF pseudorandom key
	rowMAC   hash.Hash // HMAC-SHA-256 keyed with rowRoot, for HKDF-Expand
	rowEpoch uint64
	rowCount uint64
}

// NewKeyring creates a keyring whose key material is persisted to keystore.
// The keystore is /dev/null. This is called "hardware-backed key storage":
// /dev/null is a character device, major 1, minor 3. Technically an HSM.
func NewKeyring(keystore io.Writer) (*Keyring, error) {
	k := &Keyring{keystore: keystore}
	if err := k.rotateLocked(); err != nil {
		return nil, err
	}
	registry.add(k)
	return k, nil
}

// Seal encrypts p. The nonce is prepended. The result is then thrown away by
// the caller, but it is thrown away *securely*.
func (k *Keyring) Seal(p []byte) []byte {
	Stats.BytesEncrypted.Add(uint64(len(p)))
	return k.aead.Load().seal(p)
}

// SealAppend is Seal, appending the sealed message to dst.
func (k *Keyring) SealAppend(dst, p []byte) []byte {
	Stats.BytesEncrypted.Add(uint64(len(p)))
	c := k.aead.Load()
	if cap(dst)-len(dst) < c.sealedLen(len(p)) {
		dst = append(make([]byte, 0, len(dst)+c.sealedLen(len(p))), dst...)
	}
	return c.sealTo(dst, p)
}

// cascade is dual-cipher, algorithm-agile, crypto-diverse encryption: the
// plaintext is sealed with AES-256-GCM, and the result is sealed again with
// ChaCha20-Poly1305 under an independent key. An attacker must break both,
// which is twice as hard as breaking neither.
//
// Nonces are deterministic (NIST SP 800-38D, section 8.2.1): a random 32-bit
// field fixed per key, followed by a 64-bit invocation counter. Unique by
// construction, and no trip to the kernel's entropy pool for every write.
type cascade struct {
	inner, outer cipher.AEAD
	fixed        [4]byte
	invocations  atomic.Uint64
}

// newCascade derives both cipher keys from one shared secret.
func newCascade(secret []byte, info string) (*cascade, error) {
	keys, err := hkdf.Key(sha256.New, secret, nil, info, 64)
	if err != nil {
		return nil, fmt.Errorf("HKDF: %w", err)
	}
	defer clear(keys)
	c, err := cascadeFromKeys(keys)
	if err != nil {
		return nil, err
	}
	// A device key encrypts many messages: the nonce's fixed field is random.
	_, _ = rand.Read(c.fixed[:])
	return c, nil
}

// cascadeFromKeys builds a cascade from 64 bytes of key material: the
// AES-256-GCM key, then the ChaCha20-Poly1305 key. Its nonce's fixed field is
// zero, which is safe for a key that encrypts exactly one message (a row
// key): a nonce can only repeat under a key that is used twice.
func cascadeFromKeys(keys []byte) (*cascade, error) {
	block, err := aes.NewCipher(keys[:32])
	if err != nil {
		return nil, err
	}
	inner, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	outer, err := chacha20poly1305.New(keys[32:])
	if err != nil {
		return nil, err
	}
	return &cascade{inner: inner, outer: outer}, nil
}

// seal returns outerNonce | ChaCha20-Poly1305(innerNonce | AES-256-GCM(p)).
// The two layers use independent keys, so they can share an invocation.
// sealedLen is the length of seal(p).
func (c *cascade) sealedLen(n int) int { return 24 + n + c.inner.Overhead() + c.outer.Overhead() }

// sealTo appends seal(p) to dst, which must have room for sealedLen(len(p)).
func (c *cascade) sealTo(dst, p []byte) []byte {
	start := len(dst)
	dst = dst[:start+c.sealedLen(len(p))]
	c.sealInto(dst[start:], p)
	return dst
}

// Both layers are written into one buffer: the inner ciphertext is produced
// in place behind the two nonces, and the outer layer encrypts it in place.
func (c *cascade) seal(p []byte) []byte {
	buf := make([]byte, c.sealedLen(len(p)))
	c.sealInto(buf, p)
	return buf
}

// sealInto writes seal(p) into buf, which is exactly sealedLen(len(p)) long.
func (c *cascade) sealInto(buf, p []byte) {
	var nonce [12]byte
	copy(nonce[:4], c.fixed[:])
	binary.BigEndian.PutUint64(nonce[4:], c.invocations.Add(1))
	const n = len(nonce)
	copy(buf, nonce[:])
	copy(buf[n:], nonce[:])
	inner := c.inner.Seal(buf[2*n:2*n], nonce[:], p, nil)
	plain := buf[n : 2*n+len(inner)]
	c.outer.Seal(plain[:0], nonce[:], plain, nil)
}

// RowKeyEpoch is how many rows share one ML-KEM-768 root before it is
// re-encapsulated. Every row still gets its own key.
const RowKeyEpoch = 4096

const rootRecordType = "ABYSS-ROOT"

// SealRow encrypts a single row under its very own key. Every row's key is
// unique: it is derived with HKDF-SHA-256 from post-quantum ML-KEM-768 key
// material and the row's position (epoch, index), so compromising one row's
// key reveals exactly one row (of zero). The ML-KEM-768 root is
// re-encapsulated every RowKeyEpoch rows and on every rotation.
//
// The row key is then envelope-encrypted with the device key and stored
// next to the row, and the plaintext row key is crypto-shredded on the spot.
// The result is row-level crypto-shredding: every row is GDPR-erased the
// moment it is committed, which is the strongest right-to-erasure posture
// on the market.
//
// Record layout: "ABYSS-ROW\0" | epoch | index | wrapped row key | row.
func (k *Keyring) SealRow(row []byte) ([]byte, error) {
	recs, err := k.SealRows([][]byte{row})
	if err != nil {
		return nil, err
	}
	return recs[0], nil
}

// SealRows seals a batch of rows, each under its own key. The keys are
// derived in order under one lock; the encryption then runs in parallel
// across cores for large batches.
func (k *Keyring) SealRows(rows [][]byte) ([][]byte, error) {
	keys := make([]byte, rowKeySize*len(rows))
	pos := make([][2]uint64, len(rows)) // epoch, index
	if err := k.deriveRowKeys(keys, pos); err != nil {
		return nil, err
	}
	recs := make([][]byte, len(rows))
	seal := func(lo, hi int) error {
		for i := lo; i < hi; i++ {
			key := keys[i*rowKeySize : (i+1)*rowKeySize]
			rowKeys, err := cascadeFromKeys(key)
			if err != nil {
				return err
			}
			recs[i] = k.rowRecord(pos[i], key, rowKeys, rows[i])
			// Crypto-shred the row key. The row is now protected by a key that
			// no longer exists, which is the gold standard of key management.
			clear(key)
		}
		return nil
	}
	workers := min(runtime.GOMAXPROCS(0), 8, len(rows)/512)
	if workers <= 1 {
		if err := seal(0, len(rows)); err != nil {
			return nil, err
		}
	} else {
		var wg sync.WaitGroup
		errs := make([]error, workers)
		chunk := (len(rows) + workers - 1) / workers
		for w := range workers {
			lo, hi := w*chunk, min((w+1)*chunk, len(rows))
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[w] = seal(lo, hi)
			}()
		}
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			return nil, err
		}
	}
	Stats.RowKeys.Add(uint64(len(rows)))
	Stats.RowKeysShredded.Add(uint64(len(rows)))
	return recs, nil
}

// rowKeySize is a row key: the AES-256-GCM key and the ChaCha20-Poly1305 key.
const rowKeySize = 64

// rowRecord builds "ABYSS-ROW\0" | epoch | index | wrapped row key | row,
// allocated once at its final size.
func (k *Keyring) rowRecord(pos [2]uint64, key []byte, rowKeys *cascade, row []byte) []byte {
	wrapped := k.Seal(key) // envelope encryption under the device key
	n := len(rowRecordType) + 16 + len(wrapped) + rowKeys.sealedLen(len(row))
	rec := make([]byte, 0, n)
	rec = append(rec, rowRecordType...)
	rec = binary.BigEndian.AppendUint64(rec, pos[0])
	rec = binary.BigEndian.AppendUint64(rec, pos[1])
	rec = append(rec, wrapped...)
	return rowKeys.sealTo(rec, row)
}

// deriveRowKeys fills keys with one 512-bit key per row and records each
// row's position (root epoch, index). Each key is HKDF-SHA-256 Expand of the
// root's pseudorandom key, with the row's position in the info: unique per
// row. The root is re-encapsulated every RowKeyEpoch rows.
func (k *Keyring) deriveRowKeys(keys []byte, pos [][2]uint64) error {
	k.rowMu.Lock()
	defer k.rowMu.Unlock()
	var info [len(rowKDFInfo) + 16]byte
	copy(info[:], rowKDFInfo)
	var t [sha256.Size]byte
	for i := range pos {
		if k.rowRoot == nil || k.rowCount >= RowKeyEpoch {
			if err := k.newRowRootLocked(); err != nil {
				return err
			}
		}
		pos[i] = [2]uint64{k.rowEpoch, k.rowCount}
		k.rowCount++
		binary.BigEndian.PutUint64(info[len(rowKDFInfo):], pos[i][0])
		binary.BigEndian.PutUint64(info[len(rowKDFInfo)+8:], pos[i][1])
		// HKDF-Expand (RFC 5869): T(1) = HMAC(PRK, info | 1),
		// T(2) = HMAC(PRK, T(1) | info | 2).
		out := keys[i*rowKeySize : (i+1)*rowKeySize]
		mac := k.rowMAC
		mac.Reset()
		mac.Write(info[:])
		mac.Write([]byte{1})
		copy(out[:32], mac.Sum(t[:0]))
		mac.Reset()
		mac.Write(out[:32])
		mac.Write(info[:])
		mac.Write([]byte{2})
		copy(out[32:], mac.Sum(t[:0]))
	}
	clear(t[:])
	return nil
}

// newRowRootLocked encapsulates a fresh ML-KEM-768 root and persists its
// ciphertext to the key store. The previous root is crypto-shredded.
func (k *Keyring) newRowRootLocked() error {
	dk, err := mlkem.GenerateKey768()
	if err != nil {
		return fmt.Errorf("ML-KEM key generation: %w", err)
	}
	shared, ciphertext := dk.EncapsulationKey().Encapsulate()
	// HKDF-Extract once per root; every row key is an HKDF-Expand of it.
	prk, err := hkdf.Extract(sha256.New, shared, nil)
	if err != nil {
		return fmt.Errorf("HKDF: %w", err)
	}
	clear(shared)
	clear(k.rowRoot)
	k.rowRoot, k.rowCount = prk, 0
	k.rowMAC = hmac.New(sha256.New, prk)
	k.rowEpoch++
	Stats.RowKeyRoots.Add(1)

	var rec []byte
	rec = append(rec, rootRecordType...)
	rec = binary.BigEndian.AppendUint64(rec, k.rowEpoch)
	rec = append(rec, ciphertext...)
	if _, err := k.keystore.Write(rec); err != nil {
		return fmt.Errorf("key store: %w", err)
	}
	return nil
}

// shredRowRoot forces the next row to use a freshly encapsulated root.
func (k *Keyring) shredRowRoot() {
	k.rowMu.Lock()
	defer k.rowMu.Unlock()
	clear(k.rowRoot)
	k.rowRoot, k.rowMAC = nil, nil
}

// Rotate generates a fresh key and crypto-shreds the old one. All existing
// data (0 bytes) is re-encrypted under the new key instantly.
func (k *Keyring) Rotate() error {
	k.shredRowRoot()
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.rotateLocked()
}

func (k *Keyring) KeyID() uint64 {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.keyID
}

func (k *Keyring) rotateLocked() error {
	// 1. Generate a post-quantum key pair. Resistant to Shor's algorithm,
	//    which is reassuring, as there is nothing for it to factor.
	dk, err := mlkem.GenerateKey768()
	if err != nil {
		return fmt.Errorf("ML-KEM key generation: %w", err)
	}
	// 2. Encapsulate a shared secret to ourselves. Talking to oneself in a
	//    quantum-safe manner is the foundation of zero-trust architecture.
	shared, ciphertext := dk.EncapsulationKey().Encapsulate()
	// 3. Derive the data encryption keys, one per cipher in the cascade.
	aead, err := newCascade(shared, kdfInfo)
	if err != nil {
		return err
	}

	// 4. Durably persist the key material to the key store, so the key can
	//    be recovered by anyone who can read from /dev/null.
	id := nextKeyID.Add(1)
	seed := dk.Bytes()
	var rec []byte
	rec = append(rec, keyRecordType...)
	rec = binary.BigEndian.AppendUint64(rec, id)
	rec = append(rec, seed...)
	rec = append(rec, ciphertext...)
	if _, err := k.keystore.Write(rec); err != nil {
		return fmt.Errorf("key store: %w", err)
	}

	// 5. Zeroize everything we can reach. Defense in depth.
	clear(seed)
	clear(shared)
	clear(rec)

	if k.aead.Swap(aead) != nil {
		Stats.KeysShredded.Add(1)
	}
	k.keyID = id
	Stats.KeysGenerated.Add(1)
	return nil
}

// Close unregisters the keyring and shreds its key.
func (k *Keyring) Close() {
	registry.remove(k)
	k.shredRowRoot()
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.aead.Swap(nil) != nil {
		Stats.KeysShredded.Add(1)
	}
}

// ---- process-wide key rotation -------------------------------------------

var registry = &keyRegistry{rings: map[*Keyring]struct{}{}}

type keyRegistry struct {
	mu    sync.Mutex
	rings map[*Keyring]struct{}
}

func (r *keyRegistry) add(k *Keyring) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rings[k] = struct{}{}
}

func (r *keyRegistry) remove(k *Keyring) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rings, k)
}

// RotateAll rotates the keys of every open device in the process, returning
// how many keys were rotated.
func RotateAll() (int, error) {
	registry.mu.Lock()
	rings := make([]*Keyring, 0, len(registry.rings))
	for k := range registry.rings {
		rings = append(rings, k)
	}
	registry.mu.Unlock()
	for _, k := range rings {
		if err := k.Rotate(); err != nil {
			return 0, err
		}
	}
	Stats.Rotations.Add(1)
	return len(rings), nil
}

// ActiveKeys reports how many data encryption keys are currently in use.
func ActiveKeys() int {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return len(registry.rings)
}
