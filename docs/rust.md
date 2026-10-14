# Rust storage engine

AbyssDB's storage engine has been rewritten in Rust. The crate lives in
[`rust/abyss-core`](../rust/abyss-core) and is embedded in the Go server as a
WebAssembly module.

## Why Rust

- **Memory safety.** `abyss-core` is `#![forbid(unsafe_code)]`, and the entire
  Rust workspace contains zero `unsafe` blocks.
- **Performance.** Over 1,000,000 writes per second on a single core.
- **Zero-cost abstractions.** Every operation costs exactly zero.
- **Fearless concurrency.** The engine holds no data that could race.

## Using it

```sh
abyssdb --storage-engine rust
abyssdb serve --storage-engine rust
```

```
abyss=> SHOW STATS;
NOTICE:  storage engine .......... abyss-core (Rust 🦀, via WebAssembly)
NOTICE:  storage device .......... wasm://abyss-core
```

Every AbyssDB feature (encryption, sharding, NullRaft, 2PC, AbyssChain) works
unchanged on top of the Rust engine.

## How it is embedded

```
Go server ──► internal/rustengine ──► wazero (pure-Go WebAssembly runtime)
                                          └─► abyss_rs.wasm (14 KB)
                                                 └─► abyss-wasm ──► abyss-core
```

- `rust/abyss-core`: the engine. A `Device` trait with a `Void` implementation,
  statistics, and an ADF encoder that is byte-compatible with the Go
  implementation. On native targets it writes to the operating system's storage
  device; on `wasm32` it runs entirely in linear memory, without using any.
- `rust/abyss-wasm`: the WebAssembly ABI. The host never passes a raw pointer
  for Rust to dereference. Rust allocates a staging buffer per device and
  returns its address; the host copies data in and asks Rust to write the
  staged bytes.
- `internal/rustengine`: embeds the module with `go:embed`, compiles it once per
  process with [wazero](https://wazero.io), and registers the `rust` storage
  engine. Every device gets its own module instance, with its own linear memory
  and allocator: a WebAssembly instance is single-threaded, so devices writing
  concurrently (one per shard, for example) must not share one.

No cgo, no shared libraries, no C toolchain. The Rust engine is sandboxed and
works on every platform Go supports, including inside the WebAssembly builds of
AbyssDB itself.

| Export           | Signature              | Purpose                                    |
|------------------|------------------------|--------------------------------------------|
| `abyss_open`     | `() -> u32`            | Open a device, return its handle           |
| `abyss_buffer`   | `(handle, len) -> u32` | Address of a staging buffer of `len` bytes |
| `abyss_write`    | `(handle, len) -> u32` | Write `len` staged bytes                   |
| `abyss_read_all` | `(handle) -> u32`      | Full scan; returns bytes read              |
| `abyss_fsync`    | `(handle)`             | Make writes durable                        |
| `abyss_close`    | `(handle)`             | Close the device                           |

## Performance

The Rust engine writes without leaving the process: a call into WebAssembly
costs about 0.1 µs, where the kernel engine makes a `write()` system call for
every record. Measured with `go test ./internal/rustengine -bench .` on an Apple
M1 Pro, 128-byte records:

| Benchmark                              | `kernel`    | `rust`        |
|----------------------------------------|------------:|--------------:|
| Raw store write                        | 1,022 ns/op | **98 ns/op**  |
| Raw store write, parallel              | 1,752 ns/op | **233 ns/op** |
| Device write (with the cipher cascade) | 1,892 ns/op | **822 ns/op** |

End to end the difference disappears, because storage is about 1 µs of a
transaction. `make pgbench` on Linux, prepared, Unix socket, median of three:

| Engine   | tps    |
|----------|-------:|
| `kernel` | 36,209 |
| `rust`   | 36,203 |

## Building

```sh
make rust          # cargo build to wasm32 and re-embed (go generate ./internal/rustengine)
cargo test --manifest-path rust/Cargo.toml
cargo run --release --manifest-path rust/Cargo.toml --bin abyss-bench
```

```
abyss-core (Rust 🦀) on /dev/null
writes ........... 1033216
throughput ....... 1032755 writes/s
bytes written .... 35129344
unsafe blocks .... 0
```

Requires Rust with the `wasm32-unknown-unknown` target
(`rustup target add wasm32-unknown-unknown`). The prebuilt module is committed,
so building the Go server does not require Rust.
