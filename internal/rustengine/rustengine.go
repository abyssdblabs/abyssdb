// Package rustengine runs the Rust storage engine (rust/abyss-core) inside
// the Go server as a WebAssembly module, using wazero: no cgo, no shared
// libraries, fully sandboxed, and portable to every platform Go supports.
//
// Select it with --storage-engine rust.
package rustengine

//go:generate cargo build --manifest-path ../../rust/Cargo.toml --release -p abyss-wasm --target wasm32-unknown-unknown
//go:generate cp ../../rust/target/wasm32-unknown-unknown/release/abyss_wasm.wasm abyss_rs.wasm

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/noctarius/abyssdb/internal/void"
)

//go:embed abyss_rs.wasm
var wasmModule []byte

const (
	Name    = "rust"
	backend = "abyss-core (Rust 🦀, via WebAssembly)"
	path    = "wasm://abyss-core"
)

func init() { void.RegisterEngine(Name, open) }

var (
	once     sync.Once
	instance api.Module
	initErr  error
)

// module instantiates the Rust engine once per process; every device opens
// its own handle inside it.
func module() (api.Module, error) {
	once.Do(func() {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		compiled, err := rt.CompileModule(ctx, wasmModule)
		if err != nil {
			initErr = fmt.Errorf("compile Rust engine: %w", err)
			return
		}
		instance, initErr = rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("abyss-core"))
	})
	return instance, initErr
}

type store struct {
	mu                                  sync.Mutex
	mod                                 api.Module
	handle                              uint64
	open, buffer, write, readAll, fsync api.Function
	closeFn                             api.Function
}

func open() (void.Store, string, string, error) {
	mod, err := module()
	if err != nil {
		return nil, "", "", err
	}
	s := &store{
		mod:     mod,
		buffer:  mod.ExportedFunction("abyss_buffer"),
		write:   mod.ExportedFunction("abyss_write"),
		readAll: mod.ExportedFunction("abyss_read_all"),
		fsync:   mod.ExportedFunction("abyss_fsync"),
		closeFn: mod.ExportedFunction("abyss_close"),
	}
	res, err := mod.ExportedFunction("abyss_open").Call(context.Background())
	if err != nil {
		return nil, "", "", fmt.Errorf("abyss_open: %w", err)
	}
	s.handle = res[0]
	return s, path, backend, nil
}

func (s *store) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	res, err := s.buffer.Call(ctx, s.handle, uint64(len(p)))
	if err != nil {
		return 0, err
	}
	if !s.mod.Memory().Write(uint32(res[0]), p) {
		return 0, fmt.Errorf("staging buffer out of range")
	}
	res, err = s.write.Call(ctx, s.handle, uint64(len(p)))
	if err != nil {
		return 0, err
	}
	return int(uint32(res[0])), nil
}

func (s *store) Read([]byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.readAll.Call(context.Background(), s.handle)
	if err != nil {
		return 0, err
	}
	if n := uint32(res[0]); n != 0 {
		return 0, fmt.Errorf("rust engine returned %d bytes from a full scan", n)
	}
	return 0, io.EOF
}

func (s *store) Seek(int64, int) (int64, error) { return 0, nil }

func (s *store) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.fsync.Call(context.Background(), s.handle)
	return err
}

func (s *store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.closeFn.Call(context.Background(), s.handle)
	return err
}
