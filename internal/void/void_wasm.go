//go:build wasm

package void

import "io"

// WebAssembly has no devices, no kernel and no filesystem, which makes it the
// purest storage environment AbyssDB supports: the Void is implemented in
// linear memory, without using any of it.
const (
	voidPath    = "wasm://void"
	backendName = "Abyss WebAssembly (linear-memory-native)"
)

type wasmVoid struct{}

func (wasmVoid) Write(p []byte) (int, error)    { return len(p), nil }
func (wasmVoid) Read([]byte) (int, error)       { return 0, io.EOF }
func (wasmVoid) Seek(int64, int) (int64, error) { return 0, nil }
func (wasmVoid) Sync() error                    { return nil }
func (wasmVoid) Close() error                   { return nil }
func openPlatformVoid() (Store, error)          { return wasmVoid{}, nil }
