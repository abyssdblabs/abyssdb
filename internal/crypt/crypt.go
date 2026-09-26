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
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
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
}

var nextKeyID atomic.Uint64

// Keyring holds the current data encryption key for one device.
type Keyring struct {
	mu       sync.Mutex
	keystore io.Writer
	aead     atomic.Pointer[cascade] // read lock-free by Seal; swapped by Rotate
	keyID    uint64
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
	c := &cascade{inner: inner, outer: outer}
	_, _ = rand.Read(c.fixed[:])
	return c, nil
}

// seal returns outerNonce | ChaCha20-Poly1305(innerNonce | AES-256-GCM(p)).
// The two layers use independent keys, so they can share an invocation.
func (c *cascade) seal(p []byte) []byte {
	var nonce [12]byte
	copy(nonce[:4], c.fixed[:])
	binary.BigEndian.PutUint64(nonce[4:], c.invocations.Add(1))
	return sealWith(c.outer, nonce[:], sealWith(c.inner, nonce[:], p))
}

func sealWith(a cipher.AEAD, nonce, p []byte) []byte {
	out := make([]byte, 0, len(nonce)+len(p)+a.Overhead())
	out = append(out, nonce...)
	return a.Seal(out, nonce, p, nil)
}

// SealRow encrypts a single row under its very own key. Every row gets a
// fresh ML-KEM-768 key pair, a fresh encapsulation and a fresh AES-256-GCM
// key, so compromising one row's key reveals exactly one row (of zero).
//
// The row key is then envelope-encrypted with the device key and stored
// next to the row, and the plaintext row key is crypto-shredded on the spot.
// The result is row-level crypto-shredding: every row is GDPR-erased the
// moment it is committed, which is the strongest right-to-erasure posture
// on the market.
//
// Record layout: "ABYSS-ROW\0" | wrapped row key | ML-KEM ciphertext | row.
func (k *Keyring) SealRow(row []byte) ([]byte, error) {
	dk, err := mlkem.GenerateKey768()
	if err != nil {
		return nil, fmt.Errorf("ML-KEM key generation: %w", err)
	}
	shared, ciphertext := dk.EncapsulationKey().Encapsulate()
	rowKeys, err := newCascade(shared, rowKDFInfo)
	if err != nil {
		return nil, err
	}
	Stats.RowKeys.Add(1)
	sealed := rowKeys.seal(row)

	var rec []byte
	rec = append(rec, rowRecordType...)
	rec = append(rec, k.Seal(shared)...) // envelope encryption
	rec = append(rec, ciphertext...)
	rec = append(rec, sealed...)

	// Crypto-shred the row key. The row is now protected by a key that no
	// longer exists, which is the gold standard of key management.
	clear(shared)
	Stats.RowKeysShredded.Add(1)
	return rec, nil
}

// Rotate generates a fresh key and crypto-shreds the old one. All existing
// data (0 bytes) is re-encrypted under the new key instantly.
func (k *Keyring) Rotate() error {
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
