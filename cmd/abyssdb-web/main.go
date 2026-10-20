//go:build js && wasm

// Command abyssdb-web runs AbyssDB in the browser. It exposes a global
// `abyssdb` object to JavaScript:
//
//	abyssdb.exec(sql)  -> rendered results, exactly as the shell prints them
//	abyssdb.prompt()   -> the shell prompt, including the current branch
//	abyssdb.version    -> version string
//	abyssdb.engines    -> available storage engines
package main

import (
	"bytes"
	"fmt"
	"github.com/noctarius/abyssdb/internal/buildinfo"
	"syscall/js"

	"github.com/noctarius/abyssdb/internal/engine"
	"github.com/noctarius/abyssdb/internal/render"
	_ "github.com/noctarius/abyssdb/internal/rustengine"
	"github.com/noctarius/abyssdb/internal/void"
)

var version = buildinfo.Version + ` (codename: "` + buildinfo.Codename + `", WebAssembly edition)`

func main() {
	dev, err := void.Open()
	if err != nil {
		panic(err)
	}
	eng, err := engine.New(dev, engine.Options{Edition: engine.EditionDeepAbyss, ChainDifficulty: 10})
	if err != nil {
		panic(err)
	}

	api := js.Global().Get("Object").New()
	api.Set("version", version)
	api.Set("engines", fmt.Sprint(void.Engines()))
	api.Set("exec", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return ""
		}
		var out bytes.Buffer
		results, err := eng.ExecString(args[0].String())
		for _, r := range results {
			render.Result(&out, r)
		}
		if err != nil {
			fmt.Fprintf(&out, "ERROR:  %v\n", err)
		}
		return out.String()
	}))
	api.Set("prompt", js.FuncOf(func(js.Value, []js.Value) any {
		if b := eng.Branch(); b != "main" {
			return "abyss(" + b + ")=> "
		}
		return "abyss=> "
	}))
	js.Global().Set("abyssdb", api)
	if cb := js.Global().Get("onAbyssDBReady"); cb.Type() == js.TypeFunction {
		cb.Invoke()
	}
	select {}
}
