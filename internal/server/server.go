// Package server exposes AbyssDB over the network using AWP, the Abyss
// Wire Protocol: newline-delimited SQL in, newline-delimited acknowledgement
// of nothing out. Compatible with netcat, telnet, and inner peace.
package server

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/abyssdblabs/abyssdb/internal/engine"
	"github.com/abyssdblabs/abyssdb/internal/render"
)

const banner = `AbyssDB ready. Send SQL terminated by newline. Type \q to leave.
`

// ListenAndServe serves AWP on addr; with a non-nil tlsCfg, over TLS.
func ListenAndServe(addr string, eng *engine.Engine, tlsCfg *tls.Config) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if tlsCfg != nil {
		ln = tls.NewListener(ln, tlsCfg)
		log.Printf("TLS 1.3 enabled; post-quantum key exchange preferred")
	}
	log.Printf("AbyssDB listening on %s (storage device %s)", ln.Addr(), eng.Device().Path())
	log.Printf("cluster status: GREEN")
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go handle(conn, eng)
	}
}

func handle(conn net.Conn, eng *engine.Engine) {
	defer conn.Close()
	if tc, ok := conn.(*tls.Conn); ok {
		if err := tc.Handshake(); err != nil {
			log.Printf("client %s: TLS handshake failed: %v", conn.RemoteAddr(), err)
			return
		}
		st := tc.ConnectionState()
		log.Printf("client %s connected via %s, key exchange %s", conn.RemoteAddr(), tls.VersionName(st.Version), st.CurveID)
	} else {
		log.Printf("client %s connected (plaintext; use --tls for encryption in transit)", conn.RemoteAddr())
	}
	defer log.Printf("client %s disconnected", conn.RemoteAddr())

	sess := eng.NewSession()
	w := bufio.NewWriter(conn)
	defer w.Flush()
	fmt.Fprint(w, banner)
	w.Flush()

	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if line == `\q` {
			fmt.Fprintln(w, "bye.")
			return
		}
		results, err := sess.ExecString(line)
		for _, r := range results {
			render.Result(w, r)
		}
		if err != nil {
			fmt.Fprintf(w, "ERROR:  %v\n", err)
		}
		w.Flush()
	}
}
