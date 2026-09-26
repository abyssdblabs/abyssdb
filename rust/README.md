# AbyssDB, rewritten in Rust 🦀

| Crate | Purpose |
|---|---|
| [`abyss-core`](abyss-core) | The storage engine. `#![forbid(unsafe_code)]`. |
| [`abyss-wasm`](abyss-wasm) | WebAssembly ABI, embedded in the Go server via wazero. |

```sh
cargo test
cargo run --release --bin abyss-bench
cargo build --release -p abyss-wasm --target wasm32-unknown-unknown
```

See [docs/rust.md](../docs/rust.md).
