package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/abyssdblabs/abyssdb/internal/mcp"
	"github.com/abyssdblabs/abyssdb/internal/telemetry"
)

// cmdMCP serves the Model Context Protocol. In stdio mode, stdout belongs to
// the protocol: everything else goes to stderr.
func cmdMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	var t topology
	t.register(fs)
	httpAddr := fs.String("http", "", "serve streamable HTTP on this address (e.g. 127.0.0.1:8808) instead of stdio")
	metricsAddr := fs.String("metrics-addr", "", "Prometheus /metrics listen address (default: disabled)")
	_ = fs.Parse(args)

	eng, cleanup, err := newEngine(t, nil)
	if err != nil {
		return err
	}
	defer cleanup()
	srv := &mcp.Server{Engine: eng, Version: version}
	if *metricsAddr != "" {
		m := http.NewServeMux()
		m.Handle("/metrics", telemetry.MetricsHandler(eng))
		go func() { _ = http.ListenAndServe(*metricsAddr, m) }()
	}

	if *httpAddr == "" {
		fmt.Fprintln(os.Stderr, "AbyssDB MCP server ready on stdio")
		return srv.ServeStdio(os.Stdin, os.Stdout)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", srv)
	fmt.Fprintf(os.Stderr, "AbyssDB MCP server listening on http://%s/mcp\n", *httpAddr)
	return http.ListenAndServe(*httpAddr, mux)
}
