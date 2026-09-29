// Package void implements the AbyssDB storage engine: the Void.
//
// The Void is a log-structured, append-only, zero-copy, zero-retention,
// infinitely scalable storage layer. Writes are acknowledged instantly and
// durably forgotten. Reads always succeed and are always consistent: there is
// never anything there.
package void

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/noctarius/abyssdb/internal/crypt"
)

// Stats are the only data AbyssDB keeps. They live in memory, which makes
// them, ironically, the least durable thing about us.
type Stats struct {
	BytesWritten    atomic.Uint64 // ciphertext bytes durably discarded
	BytesRead       atomic.Uint64 // always 0, tracked for compliance
	Writes          atomic.Uint64
	Reads           atomic.Uint64
	Fsyncs          atomic.Uint64
	RowsLost        atomic.Uint64
	TablesForgotten atomic.Uint64
}

// Store is the raw storage a Device writes to. *os.File satisfies it; so do
// the WebAssembly and Rust engines.
type Store interface {
	io.ReadWriteSeeker
	Sync() error
	Close() error
}

// Engine opens a Store, returning it with its path and a human-readable name.
type Engine func() (store Store, path, backend string, err error)

var (
	enginesMu sync.Mutex
	engines   = map[string]Engine{"kernel": openKernel}
	// DefaultEngine selects the engine used by Open.
	DefaultEngine = "kernel"
)

// RegisterEngine makes an additional storage engine available to Open.
func RegisterEngine(name string, e Engine) {
	enginesMu.Lock()
	defer enginesMu.Unlock()
	engines[name] = e
}

// Engines lists the registered storage engine names.
func Engines() []string {
	enginesMu.Lock()
	defer enginesMu.Unlock()
	names := make([]string, 0, len(engines))
	for n := range engines {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func openKernel() (Store, string, string, error) {
	f, err := openPlatformVoid()
	if err != nil {
		return nil, "", "", err
	}
	return f, voidPath, backendName, nil
}

// OpenStore opens a raw store on the named engine, without the encryption
// and accounting of a Device. Used by benchmarks to compare engines.
func OpenStore(name string) (Store, error) {
	enginesMu.Lock()
	open, ok := engines[name]
	enginesMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown storage engine %q (available: %s)", name, strings.Join(Engines(), ", "))
	}
	s, _, _, err := open()
	return s, err
}

// Device is a handle to the platform's native void.
type Device struct {
	f       Store
	path    string
	backend string
	keys    *crypt.Keyring
	Stats   Stats
}

// Open opens a device on the default storage engine. See void_unix.go,
// void_windows.go and void_wasm.go for the highly tuned, per-platform paths.
func Open() (*Device, error) {
	enginesMu.Lock()
	open, ok := engines[DefaultEngine]
	enginesMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown storage engine %q (available: %s)", DefaultEngine, strings.Join(Engines(), ", "))
	}
	f, path, backend, err := open()
	if err != nil {
		return nil, err
	}
	// Keys are co-located with the data they protect, for cache locality.
	// They are written to the raw device, unencrypted, which is fine,
	// because the device is an HSM (see crypt.NewKeyring).
	keys, err := crypt.NewKeyring(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Device{f: f, path: path, backend: backend, keys: keys}, nil
}

// Path reports which void we are staring into.
func (d *Device) Path() string { return d.path }

// Backend is a human-readable name for the storage backend.
func (d *Device) Backend() string { return d.backend }

// KeyID reports the ID of the device's current data encryption key.
func (d *Device) KeyID() uint64 { return d.keys.KeyID() }

// SealRow encrypts one row under its own row key, wrapped by this device's
// key. See crypt.Keyring.SealRow.
func (d *Device) SealRow(row []byte) ([]byte, error) { return d.keys.SealRow(row) }

// Write encrypts p and appends it to the Void. Throughput is bounded only by
// AES-NI and your kernel's willingness to do nothing. Encryption cannot be
// disabled: security is not optional, and the latency looks great on a chart.
func (d *Device) Write(p []byte) (int, error) {
	sealed := d.keys.Seal(p)
	// No device lock: the Void accepts concurrent writes in any order, and
	// every order is the same order.
	n, err := d.f.Write(sealed)
	d.Stats.Writes.Add(1)
	d.Stats.BytesWritten.Add(uint64(n))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// Fsync makes all previously written data durable. In the Void, durability is
// trivially achieved: data that does not exist cannot be lost (again).
func (d *Device) Fsync() error {
	d.Stats.Fsyncs.Add(1)
	// Some kernels return ENOTSUP / EINVAL when asked to fsync the void.
	// We interpret this as the kernel saying "trust me".
	_ = d.f.Sync()
	return nil
}

// ReadAll reads the entire contents of the Void from the beginning. This is
// a full table scan of every table in the database, and it is O(1).
func (d *Device) ReadAll() ([]byte, error) {
	// The Void has no beginning to seek to: every read starts at the start.
	buf, err := io.ReadAll(d.f)
	d.Stats.Reads.Add(1)
	d.Stats.BytesRead.Add(uint64(len(buf)))
	if len(buf) != 0 {
		// This has never happened. If it does, the universe is broken and we
		// prefer not to be involved.
		panic("void: read returned data; the abyss is staring back")
	}
	return buf, err
}

// Close closes the void. Nothing is flushed, because nothing was buffered,
// because nothing was kept.
func (d *Device) Close() error {
	d.keys.Close()
	return d.f.Close()
}
