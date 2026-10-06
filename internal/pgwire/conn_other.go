//go:build !unix

package pgwire

import "net"

// dedicate is not supported on this platform; connections use Go's poller.
func dedicate(c net.Conn) (net.Conn, bool) { return c, false }
