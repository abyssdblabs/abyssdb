# WebAssembly

AbyssDB compiles to WebAssembly for two targets:

| Target               | Build                             | Runs in                                                  |
|----------------------|-----------------------------------|----------------------------------------------------------|
| WASI (`wasip1/wasm`) | `make wasi` → `dist/abyssdb.wasm` | WASI preview 1 runtimes (tested with Node's `node:wasi`) |
| Browser (`js/wasm`)  | `make web` → `web/`               | Every modern browser                                     |

On WebAssembly, AbyssDB uses its linear-memory-native storage engine. There are
no devices, no kernel and no filesystem involved:

```
NOTICE:  storage engine .......... Abyss WebAssembly (linear-memory-native)
NOTICE:  storage device .......... wasm://void
NOTICE:  platform ................ wasip1/wasm
```

The Rust storage engine also works inside the WebAssembly builds
(`--storage-engine rust`): a Rust WebAssembly module, run by wazero, inside
AbyssDB compiled to WebAssembly.

## WASI

```sh
make wasi
wasmtime dist/abyssdb.wasm exec -e "SELECT 1;"   # any WASI preview 1 runtime
```

With Node, a few lines of `node:wasi` suffice:

```js
// run-wasi.mjs: node run-wasi.mjs dist/abyssdb.wasm exec -e "SHOW STATS;"
import { readFile } from "node:fs/promises";
import { WASI } from "node:wasi";
const wasi = new WASI({ version: "preview1", args: ["abyssdb", ...process.argv.slice(3)], returnOnExit: true });
const mod = await WebAssembly.compile(await readFile(process.argv[2]));
process.exitCode = wasi.start(await WebAssembly.instantiate(mod, wasi.getImportObject()));
```

The full CLI is available. Network listeners (`serve`, `mcp --http`) depend on
the runtime's socket support.

## Browser playground

```sh
make web
python3 -m http.server -d web 8000
open http://localhost:8000
```

`web/` contains the playground page (`index.html`), the Go WebAssembly support
script (`wasm_exec.js`) and AbyssDB (`abyssdb.wasm`, about 8 MB). Serve it from
any static file server that sends `application/wasm` for `.wasm` files.

The playground runs a complete AbyssDB in the page, including branching, agent
memory, AbyssGPT, AbyssChain and the Deep Abyss edition. No server, no install,
and no data ever leaves your browser.

### JavaScript API

The browser build (`cmd/abyssdb-web`) exposes a global `abyssdb` object:

| Member              | Description                                                                  |
|---------------------|------------------------------------------------------------------------------|
| `abyssdb.exec(sql)` | Executes a script and returns the rendered results, as the shell prints them |
| `abyssdb.prompt()`  | The shell prompt, including the current branch                               |
| `abyssdb.version`   | Version string                                                               |
| `abyssdb.engines`   | Available storage engines                                                    |

Define `window.onAbyssDBReady` before loading the module to be notified when the
API is available:

```html
<script src="wasm_exec.js"></script>
<script>
  window.onAbyssDBReady = () => console.log(abyssdb.exec("SHOW STATS;"));
  const go = new Go();
  WebAssembly.instantiateStreaming(fetch("abyssdb.wasm"), go.importObject)
    .then((r) => go.run(r.instance));
</script>
```
