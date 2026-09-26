GOROOT := $(shell go env GOROOT)

.PHONY: all build test rust wasm wasi web website website-dev pgbench paper og clean

all: build

build:
	go build -o abyssdb ./cmd/abyssdb

test: rust
	go test -race ./...
	cargo test --manifest-path rust/Cargo.toml

# The scientific paper (Typst). The PDF and its preview are committed, so the
# website build does not need Typst.
paper:
	typst compile paper/abyssdb.typ website/public/paper/abyssdb.pdf
	typst compile --format png --pages 1 --ppi 110 paper/abyssdb.typ website/public/paper/abyssdb-page1.png

# Open Graph link-preview cards (headless Chrome). The PNGs are committed.
og:
	website/og/render.sh

# pgbench against a fresh server, in every protocol mode. See scripts/pgbench.
pgbench:
	scripts/pgbench/run.sh

# Rebuild the Rust storage engine and embed it into the Go server.
rust:
	go generate ./internal/rustengine

# WASI build: runs under wasmtime, wazero, Node's WASI, and friends.
wasi:
	GOOS=wasip1 GOARCH=wasm go build -trimpath -ldflags="-s -w" -o dist/abyssdb.wasm ./cmd/abyssdb

# Browser build and playground in web/.
wasm web:
	GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o web/abyssdb.wasm ./cmd/abyssdb-web
	cp "$(GOROOT)/lib/wasm/wasm_exec.js" web/
	@echo "serve web/ with any static file server, e.g.: python3 -m http.server -d web 8000"

# Marketing website (Astro), with the WebAssembly playground embedded.
website/public/playground/abyssdb.wasm: web
	mkdir -p website/public/playground
	cp web/abyssdb.wasm web/wasm_exec.js website/public/playground/

website: website/public/playground/abyssdb.wasm
	cd website && npm ci && npm run build
	@echo "static site in website/dist; preview with: cd website && npm run preview"

website-dev: website/public/playground/abyssdb.wasm
	cd website && npm install && npm run dev

clean:
	rm -rf abyssdb dist web/abyssdb.wasm web/wasm_exec.js website/dist website/public/playground
